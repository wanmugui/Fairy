package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"agentloop/agent/internal/biz/tool/httptool"
)

func estimateMessageTokens(m Message) int {
	n := len(m.Content)/3 + 4
	if len(m.ToolCalls) > 0 {
		for _, tc := range m.ToolCalls {
			n += len(tc.Function.Arguments)/3 + 8
		}
	}
	return n
}

func isSummaryMarker(m Message) bool {
	return strings.HasPrefix(strings.TrimSpace(m.Content), "<summary>")
}

const toolResultPruneMarker = "\n\n[... tool result middle pruned ...]\n\n"

func pruneToolResults(msgs []Message, cfg *Config) bool {
	threshold := cfg.ToolResultPruner.ThresholdChars
	headChars := cfg.ToolResultPruner.HeadChars
	tailChars := cfg.ToolResultPruner.TailChars
	if threshold <= 0 || headChars < 0 || tailChars < 0 {
		return false
	}
	pruned := false
	for i := range msgs {
		m := &msgs[i]
		if m.Role != "tool" {
			continue
		}
		runes := []rune(m.Content)
		if len(runes) <= threshold || headChars+tailChars >= len(runes) {
			continue
		}
		m.Content = string(runes[:headChars]) + toolResultPruneMarker + string(runes[len(runes)-tailChars:])
		pruned = true
	}
	return pruned
}

type SessionUsage struct {
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	DurationMs       int64 `json:"duration_ms"`
}

type AgentResult struct {
	Messages        []Message
	Steps           int
	Usage           *SessionUsage
	PerMessageUsage map[int]*PerMsgUsageEntry
	Title           string
	Trace           []map[string]interface{}
}

type PerMsgUsageEntry struct {
	Usage      *UsageInfo `json:"usage"`
	DurationMs int64      `json:"duration_ms"`
}

func NewMessage(role, content string, toolCalls []ToolCall, toolCallID, name string) Message {
	m := Message{Role: role}
	if content != "" {
		m.Content = content
	}
	if len(toolCalls) > 0 {
		m.ToolCalls = toolCalls
	}
	if toolCallID != "" {
		m.ToolCallID = toolCallID
	}
	if name != "" {
		m.Name = name
	}
	return m
}

// emitEvent prints a JSON event to stdout for real-time SSE forwarding
func emitEvent(eventType string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}
	data["type"] = eventType
	line, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Println(string(line))
}

func RunAgentLoop(
	cfg *Config,
	registry *ToolRegistry,
	systemPrompt, userPrompt string,
	initialMessages []Message,
	initialSessionUsage *SessionUsage,
	summaryPrompt, generateTitlePrompt, reflectionPrompt, finalizePrompt string,
	sessionFile, modelName string,
	autoAnswerAskUser bool,
) (*AgentResult, error) {
	if registry == nil {
		return nil, fmt.Errorf("tool registry is nil")
	}
	// ???? JSON ??????? session_id?httptool ??? X-FAIRY-Session-ID ?????/?????
	if sessionFile != "" {
		httptool.EnsureSessionID(sessionFile)
	}
	toolDefs := registry.ListSchemas()
	dispatcher := &ToolDispatcher{Registry: registry, MaxConcurrency: 4}

	var messages []Message
	// readFiles: files successfully read via read_file in this agent run.
	// Kept across summary compressions so the model never re-reads them after
	// compression forgets the earlier reads (graded protection: system > user
	// request > skill docs > tool call history).
	readFiles := map[string]bool{}
	// deliveredItems: final reports/answers already produced in this
	// conversation. Survives summary compression so later turns never redo
	// completed work (code-level state, injected back after compression).
	var deliveredItems []string
	if len(initialMessages) > 0 {
		messages = append([]Message{}, initialMessages...)
		// Seed delivered items from a resumed session so completion state
		// survives across processes (no redo after resume).
		for _, m := range initialMessages {
			if m.Role == "assistant" {
				if title := extractReportTitle(m.Content); title != "" {
					deliveredItems = addUnique(deliveredItems, title)
				}
			}
		}
		messages = append(messages, NewMessage("user", userPrompt, nil, "", ""))
	} else {
		messages = append(messages, NewMessage("system", systemPrompt, nil, "", ""))
		messages = append(messages, NewMessage("user", userPrompt, nil, "", ""))
	}

	// AI working view: if the loaded session already contains a <summary>
	// marker, continue from it (summary + everything after); otherwise the
	// working view equals the full history. The full history (messages) is
	// always persisted so the frontend renders the real conversation.
	var workingMsgs []Message
	{
		lastSummary := -1
		for i, m := range messages {
			if m.Role == "assistant" && strings.HasPrefix(m.Content, "<summary>") {
				lastSummary = i
			}
		}
		if lastSummary > 0 {
			workingMsgs = append([]Message{}, messages[0])
			workingMsgs = append(workingMsgs, messages[lastSummary:]...)
		} else {
			workingMsgs = append([]Message{}, messages...)
		}
	}

	perMsgUsage := make(map[int]*PerMsgUsageEntry)
	postReflection := false // Track if reflection has been injected
	lastPromptTokens := 0   // prompt tokens of the last LLM call (context size)
	executionNudges := 0    // times the loop nudged a no-tool tutorial back to execution

	sessionUsage := &SessionUsage{}
	if initialSessionUsage != nil {
		sessionUsage.PromptTokens = initialSessionUsage.PromptTokens
		sessionUsage.CompletionTokens = initialSessionUsage.CompletionTokens
		sessionUsage.DurationMs = initialSessionUsage.DurationMs
	}

	maxSteps := cfg.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 60
	}

	// Trace log (matches AgentLoop loop.go)
	trace := []map[string]interface{}{}
	trace = append(trace, map[string]interface{}{
		"event":          "loop_start",
		"step":           0,
		"messages_count": len(messages),
	})

	// Accumulated text for streaming route detection
	accumulatedText := ""

	// Hard cap on research/network tool calls per agent loop (web_search / fetch_url / image_search / image_generate).
	// Prevents runaway research loops (observed 32x web_search + 35x fetch_url in one deep-research subtask).
	networkCalls := 0

	lastTruncated := false // previous response hit max_tokens (output cut)

	step := 0
	for step < maxSteps {
		step++

		// Emit progress event
		emitEvent("status", map[string]interface{}{"step": step, "message": fmt.Sprintf("正在调用 LLM（第 %d 步）...", step), "total_steps": cfg.MaxSteps})

		// If the previous response hit max_tokens, its output (and any pending
		// tool arguments) was cut - tell the model once to keep it concise and
		// prefer tool execution, so it does not re-emit empty tool arguments.
		if lastTruncated {
			hint := "\u4f60\u7684\u4e0a\u4e00\u6761\u56de\u590d\u56e0\u8d85\u51fa\u8f93\u51fa\u4e0a\u9650\u88ab\u622a\u65ad\u3002\u8bf7\u7cbe\u7b80\uff1a\u5148\u6267\u884c\u5de5\u5177/\u5199\u6587\u4ef6\uff0c\u8bf4\u660e\u653e\u5230\u6700\u540e\uff0c\u907f\u514d\u518d\u6b21\u622a\u65ad\u3002"
			workingMsgs = append(workingMsgs, NewMessage("user", hint, nil, "", ""))
			lastTruncated = false
		}

		// Call LLM
		resp, err := CallConfiguredLLM(cfg, workingMsgs, toolDefs)
		if err != nil {
			trace = append(trace, map[string]interface{}{"event": "loop_error", "step": step, "error": err.Error()})
			return nil, fmt.Errorf("API call at step %d: %w", step, err)
		}
		lastTruncated = resp.Usage != nil && cfg.API.MaxTokens > 0 && resp.Usage.CompletionTokens >= cfg.API.MaxTokens
		// Some providers emit functions.name({...}) as plain text instead of native
		// tool_calls. Convert those into real ToolCalls so the loop keeps executing.
		if len(resp.ToolCalls) == 0 {
			if calls, cleaned := extractInlineToolCalls(resp.Content); len(calls) > 0 {
				resp.ToolCalls = calls
				resp.Content = strings.TrimSpace(cleaned)
			}
		}

		// Track usage
		if resp.Usage != nil {
			sessionUsage.PromptTokens += resp.Usage.PromptTokens
			sessionUsage.CompletionTokens += resp.Usage.CompletionTokens
			lastPromptTokens = resp.Usage.PromptTokens
		}
		sessionUsage.DurationMs += resp.DurationMs

		// --- Compliance check (port from AgentLoop loop.go) ---
		hasToolCalls := len(resp.ToolCalls) > 0
		route := GetContentRoute(resp.Content, hasToolCalls, accumulatedText)

		var compliance ComplianceResult
		if hasToolCalls {
			compliance = CheckIntermediateTurnCompliant(resp.Content)
			if !compliance.IsCompliant {
				fixed := RepairIntermediateContent(resp.Content)
				if fixed != resp.Content {
					resp.Content = fixed
					compliance = CheckIntermediateTurnCompliant(fixed)
				}
			}
		} else {
			compliance = CheckFinalTurnCompliant(resp.Content)
			if !compliance.IsCompliant {
				fixed := RepairFinalContent(resp.Content)
				if fixed != resp.Content {
					resp.Content = fixed
					compliance = CheckFinalTurnCompliant(fixed)
				}
			}
		}

		contentPreview := ""
		if resp.Content != "" {
			if len(resp.Content) > 120 {
				contentPreview = resp.Content[:120]
			} else {
				contentPreview = resp.Content
			}
		}

		trace = append(trace, map[string]interface{}{
			"event":           "model_response",
			"step":            step,
			"content_preview": contentPreview,
			"tool_call_count": len(resp.ToolCalls),
			"route":           route,
			"compliant":       compliance.IsCompliant,
			"violations":      strings.Join(compliance.Violations, ","),
		})

		if !compliance.IsCompliant {
			fmt.Fprintf(os.Stderr, "[AgentLoop] step %d non-compliant: route=%s violations=%s\n",
				step, route, strings.Join(compliance.Violations, ","))
		}

		accumulatedText += resp.Content

		// Build assistant message
		assistantMsg := NewMessage("assistant", resp.Content, resp.ToolCalls, "", "")
		assistantMsg.Usage = resp.Usage
		assistantMsg.DurationMs = resp.DurationMs
		messages = append(messages, assistantMsg)
		workingMsgs = append(workingMsgs, assistantMsg)

		// Save per-message usage
		msgIdx := len(messages)
		if resp.Usage != nil {
			perMsgUsage[msgIdx] = &PerMsgUsageEntry{
				Usage:      resp.Usage,
				DurationMs: resp.DurationMs,
			}
		}

		// Save session after each step (enables mid-conversation recovery)
		if sessionFile != "" {
			if err := SaveSession(sessionFile, messages, modelName); err != nil {
				fmt.Fprintf(os.Stderr, "[AgentLoop] WARN: mid-loop save session: %v\n", err)
			}
		}

		// A no-tool-call response may be followed by reflection injection; mark
		// it as a draft so the frontend can show "drafting" and then replace it
		// with the post-reflection final report.
		willReflect := !hasToolCalls && reflectionPrompt != "" && cfg.Reflection.Enabled && !postReflection

		// Emit compliance info via SSE
		des := GetIntermediateDescription(resp.Content)
		emitEvent("assistant", map[string]interface{}{
			"content":                  resp.Content,
			"tool_calls":               resp.ToolCalls,
			"route":                    route,
			"compliant":                compliance.IsCompliant,
			"violations":               compliance.Violations,
			"behavior_description":     des,
			"intermediate_description": des,
			"is_final":                 !hasToolCalls,
			"draft":                    willReflect,
		})

		// Process tool calls
		if !hasToolCalls {
			// If the request was actionable but the model only produced a generic
			// tutorial, nudge it back into tool execution before finalizing.
			if executionNudges < 2 && shouldNudgeToExecute(messages, resp.Content) {
				executionNudges++
				emitEvent("status", map[string]interface{}{"step": step, "message": "检测到只给了方案，正在提醒直接执行..."})
				workingMsgs = append(workingMsgs, NewMessage("user", "你刚才只给了说明，没有实际动手。请先读取相关文件并直接修改验证，禁止只给方案/示例。", nil, "", ""))
				continue
			}

			// No tool calls - check if we should inject reflection first
			if reflectionPrompt != "" && cfg.Reflection.Enabled && !postReflection {
				// Inject reflection prompt as a system message for self-check
				postReflection = true
				emitEvent("status", map[string]interface{}{"step": step, "message": "正在起草稿并反思…"})
				workingMsgs = append(workingMsgs, NewMessage("system", reflectionPrompt, nil, "", ""))
				// The Clotho gateway rejects a trailing system message ("last message
				// role must be user"), so follow reflection with a neutral user trigger.
				workingMsgs = append(workingMsgs, NewMessage("user", "请根据以上反思，给出你的最终回答。", nil, "", ""))
				continue
			}
			// After reflection (or no reflection prompt) - end the loop
			// Record this final delivery so post-compression turns never redo it.
			if title := extractReportTitle(resp.Content); title != "" {
				deliveredItems = addUnique(deliveredItems, title)
			} else if s := strings.TrimSpace(resp.Content); len(s) >= 15 {
				snippet := strings.Split(s, "\n")[0]
				deliveredItems = addUnique(deliveredItems, truncateStr(snippet, 40))
			}
			trace = append(trace, map[string]interface{}{"event": "loop_end", "step": step, "reason": "no_tool_calls"})
			emitEvent("done", map[string]interface{}{"finish_reason": "no_tool_calls"})
			break
		}

		// 将模型工具调用转换为与平台无关的调用对象。受网络预算限制的调用
		// 仍会在原始位置获得结果；其余调用由 Dispatcher 并发处理，最后按照
		// resp.ToolCalls 中的原始顺序回放结果。
		var invocations []ToolInvocation
		cappedResults := make([]ToolInvocationResult, len(resp.ToolCalls))
		completed := make([]bool, len(resp.ToolCalls))
		for idx, tc := range resp.ToolCalls {
			tname := tc.Function.Name
			trace = append(trace, map[string]interface{}{
				"event":    "tool_invoked",
				"step":     step,
				"tool":     tname,
				"call_id":  tc.ID,
				"args_raw": tc.Function.Arguments,
			})
			emitEvent("tool_call", map[string]interface{}{"tool": tname, "status": "start", "arguments": tc.Function.Arguments})
			// Research-cap: stop dispatching more network tools once the budget is exhausted.
			// The model still gets a tool result telling it to wrap up, so it does not
			// loop forever on search/fetch.
			isNetworkTool := tname == "web_search" || tname == "fetch_url" || tname == "image_search"
			if isNetworkTool && cfg.MaxNetworkCalls > 0 && networkCalls >= cfg.MaxNetworkCalls {
				capMsg := "\u641c\u7d22/\u6293\u53d6\u914d\u989d\u5df2\u7528\u5c3d\uff08\u6700\u591a " + fmt.Sprintf("%d", cfg.MaxNetworkCalls) + " \u6b21\uff09\uff0c\u8bf7\u57fa\u4e8e\u5df2\u83b7\u53d6\u7684\u8d44\u6599\u76f4\u63a5\u7ed9\u51fa\u7ed3\u8bba\uff0c\u4e0d\u8981\u518d\u8c03\u7528\u641c\u7d22/\u6293\u53d6\u5de5\u5177\u3002"
				cappedResults[idx] = ToolInvocationResult{
					Index:  idx,
					CallID: tc.ID,
					Name:   tname,
					Result: ToolResult{Value: map[string]any{"error": capMsg}, IsError: true},
				}
				completed[idx] = true
				trace = append(trace, map[string]interface{}{"event": "tool_capped", "step": step, "tool": tname, "call_id": tc.ID})
				continue
			}
			if isNetworkTool {
				networkCalls++
			}
			timeoutSec := cfg.API.TimeoutSec
			// Subtasks run a full child agent loop (deep research, file work);
			// give them a much longer budget than a normal tool call.
			if tname == "create_subtask" && timeoutSec < 3600 {
				timeoutSec = 3600
			}
			invocations = append(invocations, ToolInvocation{
				Index:       idx,
				CallID:      tc.ID,
				Name:        tname,
				Args:        json.RawMessage(tc.Function.Arguments),
				Timeout:     time.Duration(timeoutSec) * time.Second,
				Workspace:   cfg.ResolvePath(cfg.WorkspaceDir),
				SessionFile: sessionFile,
			})
		}

		if snapshots := snapshotFilesForRollback(cfg, invocations); len(snapshots) > 0 {
			emitEvent("status", map[string]interface{}{"step": step, "message": "修改前已创建回退快照..."})
		}
		dispatchedResults := dispatcher.Execute(context.Background(), invocations)
		results := make([]ToolInvocationResult, len(resp.ToolCalls))
		for idx, result := range cappedResults {
			if completed[idx] {
				results[idx] = result
			}
		}
		for _, result := range dispatchedResults {
			if result.Index >= 0 && result.Index < len(results) {
				results[result.Index] = result
				completed[result.Index] = true
			}
		}

		// Replay results in original order.
		for _, r := range results {
			result := r.Result
			errorText := ""
			if r.Err != nil {
				errorText = r.Err.Error()
				result = ToolResult{Value: map[string]any{"error": errorText}, IsError: true}
			} else if result.IsError {
				if value, ok := result.Value["error"].(string); ok {
					errorText = value
				} else {
					errorText = "tool returned an error"
				}
			}
			resultBytes, marshalErr := result.JSON()
			if marshalErr != nil {
				errorText = marshalErr.Error()
				result = ToolResult{Value: map[string]any{"error": errorText}, IsError: true}
				resultBytes, _ = result.JSON()
			}
			if result.IsError {
				emitData := map[string]interface{}{"tool": r.Name, "status": "end", "ok": false, "call_id": r.CallID, "error": errorText}
				if result.UpstreamCode != 0 {
					emitData["upstream_code"] = result.UpstreamCode
				}
				emitEvent("tool_call", emitData)
				traceData := map[string]interface{}{"event": "tool_failed", "step": step, "tool": r.Name, "err": errorText}
				if result.UpstreamCode != 0 {
					traceData["upstream_code"] = result.UpstreamCode
				}
				trace = append(trace, traceData)
				messages = append(messages, NewMessage("tool", string(resultBytes), nil, r.CallID, r.Name))
				workingMsgs = append(workingMsgs, NewMessage("tool", string(resultBytes), nil, r.CallID, r.Name))
				continue
			}
			// Graded protection: remember files read successfully so summary
			// compression cannot make the model forget it already read them.
			if r.Name == "read_file" {
				if p, ok := result.Value["path"].(string); ok && p != "" {
					readFiles[p] = true
				}
			}
			messages = append(messages, NewMessage("tool", string(resultBytes), nil, r.CallID, r.Name))
			workingMsgs = append(workingMsgs, NewMessage("tool", string(resultBytes), nil, r.CallID, r.Name))
			preview := string(resultBytes)
			if len(preview) > 200 {
				preview = preview[:200]
			}
			trace = append(trace, map[string]interface{}{"event": "tool_result", "step": step, "tool": r.Name, "ok": true, "content_preview": preview})
			emitEvent("tool_call", map[string]interface{}{"tool": r.Name, "status": "end", "ok": true, "call_id": r.CallID, "result": string(resultBytes), "result_preview": truncateStr(string(resultBytes), 120)})
		}

		// If any tool waits for user input (ask_user blocks loop), stop the turn.
		for _, r := range results {
			if r.Err != nil {
				continue
			}
			waitingReply := r.Result.WaitingReply
			if waiting, ok := r.Result.Value["waiting_reply"].(bool); ok {
				waitingReply = waitingReply || waiting
			}
			if !waitingReply {
				continue
			}

			// 无人值守/批处理模式：自动回答问题并继续执行，避免等待人工输入。
			// 自动生成的答案会作为普通 user 消息注入，使 LLM 轮次及 token 消耗
			// 与真实交互流程保持一致。
			if autoAnswerAskUser {
				askType, _ := r.Result.Value["ask_type"].(string)
				answer := "\u786e\u8ba4\uff0c\u8bf7\u7ee7\u7eed\u6267\u884c\u3002"
				if strings.Contains(askType, "confirm_outline") {
					answer = "\u5927\u7eb2\u786e\u8ba4\u901a\u8fc7\uff0c\u8bf7\u6309\u5f53\u524d\u5927\u7eb2\u7ee7\u7eed\u6267\u884c\u540e\u7eed\u6b65\u9aa4\u3002"
				} else if strings.Contains(askType, "confirm_params") {
					answer = "\u53c2\u6570\u786e\u8ba4\u901a\u8fc7\uff0c\u8bf7\u6309\u4e0a\u8ff0\u914d\u7f6e\u7ee7\u7eed\u6267\u884c\uff0c\u65e0\u9700\u518d\u786e\u8ba4\u3002"
				}
				messages = append(messages, NewMessage("user", answer, nil, "", ""))
				workingMsgs = append(workingMsgs, NewMessage("user", answer, nil, "", ""))
				emitEvent("auto_answered", map[string]interface{}{
					"ask_type": askType,
					"answer":   answer,
				})
				trace = append(trace, map[string]interface{}{"event": "auto_answered", "step": step, "ask_type": askType})
				break
			}

			// qn/Claude 兼容模型有时会把数组参数 `questions` 序列化成 JSON 字符串，
			// 而不是数组。这里统一转换为数组，确保前端 AskModal 始终收到可用列表。
			askQ := r.Result.Value["questions"]
			if qs, isStr := askQ.(string); isStr {
				var arr []interface{}
				if json.Unmarshal([]byte(qs), &arr) == nil && len(arr) > 0 {
					askQ = arr
				} else {
					askQ = []interface{}{}
				}
			}
			emitEvent("waiting_user_input", map[string]interface{}{
				"ask_type":  r.Result.Value["ask_type"],
				"questions": askQ,
			})
			trace = append(trace, map[string]interface{}{"event": "waiting_user_input", "step": step})
			return &AgentResult{
				Messages:        messages,
				Steps:           step,
				Usage:           sessionUsage,
				PerMessageUsage: perMsgUsage,
				Trace:           trace,
			}, nil
		}

		// Emit summary compression status
		if summaryPrompt != "" && lastPromptTokens > cfg.SummaryThresholdTokens {
			emitEvent("status", map[string]interface{}{"step": step, "message": "正在压缩历史上下文..."})
			prunedResults := pruneToolResults(messages, cfg)
			if pruneToolResults(workingMsgs, cfg) {
				prunedResults = true
			}
			if prunedResults {
				emitEvent("status", map[string]interface{}{"step": step, "message": "正在裁剪过大的工具结果..."})
				trace = append(trace, map[string]interface{}{"event": "tool_results_pruned", "step": step})
			}

			// Default compress window: everything after the system prompt.
			// The recent tail stays verbatim for information completeness.
			compressStart := 1
			// Retain-tail budget: keep the most recent SummaryRetainTokens verbatim.
			// If the whole active window still fits inside the budget, skip compression.
			compressEnd := len(messages) - 2
			budgetReached := false
			{
				tailTokens := 0
				for i := len(messages) - 1; i >= 1; i-- {
					tailTokens += estimateMessageTokens(messages[i])
					if tailTokens >= cfg.SummaryRetainTokens {
						compressEnd = i - 1
						budgetReached = true
						break
					}
				}
				if !budgetReached {
					compressEnd = 1
				} else if compressEnd < 1 {
					compressEnd = 1
				}
			}

			// Preserve user intent: never compress the most recent user message.
			// Walk back from the cut point and move the boundary before the last
			// user message so the model always sees the latest user request verbatim.
			lastUserIdx := -1
			for i := compressEnd; i >= 1; i-- {
				if messages[i].Role == "user" {
					lastUserIdx = i
					break
				}
			}
			if lastUserIdx == 1 {
				// The user request is the very first message (e.g. a long single-turn
				// session): keep it verbatim and compress only the tool-call history
				// after it, so compression still works without eating the request.
				compressStart = 2
			} else if lastUserIdx > 1 {
				// Prefer compressing everything before the user message.
				if lastUserIdx-1 >= 5 {
					compressEnd = lastUserIdx - 1
				} else if compressEnd > lastUserIdx {
					// The user message sits too close to the start (e.g. only an
					// ask_user confirmation early in history): compressing "before it"
					// would leave nothing to summarize. Instead compress the tool-call
					// history AFTER the user message, keeping the user message + the
					// oldest context intact. Skip leading tool responses so their
					// assistant tool_calls stay paired in the kept tail.
					compressStart = lastUserIdx + 1
					for compressStart < len(messages)-1 && messages[compressStart].Role == "tool" {
						compressStart++
					}
				}
			}

			// Preserve the initial task message (the FIRST user message) verbatim too:
			// for subtasks it carries the goal/todo AND the key-rules quick-look injected
			// by create_subtask. Never let compression eat it, or the subtask forgets its
			// task intent + the pinned rules. This complements the "preserve the most
			// recent user message" logic above (both are kept; nothing is displaced).
			for i := 1; i < len(messages); i++ {
				if messages[i].Role == "user" {
					if compressStart < i+1 {
						compressStart = i + 1
					}
					break
				}
			}

			// Pairing fix: never split an assistant(tool_calls) message from its tool responses
			// at the compression boundary. If the kept segment would start with a tool message
			// (i.e. its assistant tool_call was just compressed away), extend compressEnd to
			// swallow those tool responses so both the summary request and the kept tail
			// remain valid for the API.
			for compressEnd < len(messages)-1 && messages[compressEnd+1].Role == "tool" {
				compressEnd++
			}

			// Only compress when the segment is worth it (avoids churn on small contexts)
			if compressEnd-compressStart >= 4 {
				compressMsgs := messages[compressStart : compressEnd+1]

				// Send the original messages verbatim (preserving tool_calls / tool_call_id / name),
				// so the summary request is a valid API payload. CallLLM strips usage/duration_ms.
				// Graded protection: append the read-file whitelist to the summary prompt so
				// compression keeps a "files already read + their key points" handoff instead
				// of forgetting them (which caused repeated re-reads of skill docs).
				effectiveSummaryPrompt := summaryPrompt
				if len(readFiles) > 0 {
					paths := make([]string, 0, len(readFiles))
					for fp := range readFiles {
						paths = append(paths, fp)
					}
					sort.Strings(paths)
					effectiveSummaryPrompt += "\n\n## \u5df2\u8bfb\u6587\u4ef6\u6e05\u5355\uff08\u5fc5\u987b\u4fdd\u7559\uff09\n" +
						"\u4ee5\u4e0b\u6587\u4ef6\u5df2\u5728\u672c\u4f1a\u8bdd\u88ab\u8bfb\u53d6\u8fc7\u3002summary \u7684 [Important Files and Artifacts] \u680f\u76ee\u5fc5\u987b\u9010\u6761\u4fdd\u7559\u8fd9\u4e9b\u8def\u5f84\uff0c" +
						"\u5e76\u4e3a\u6bcf\u4e2a\u6587\u4ef6\u5199 1-2 \u53e5\u5173\u952e\u5185\u5bb9/\u7ea6\u675f\u6458\u8981\u3002\u540e\u7eed\u6a21\u578b\u5e94\u636e\u6b64\u5224\u65ad\u65e0\u9700\u91cd\u590d\u8bfb\u53d6\uff1a\n" +
						strings.Join(paths, "\n")
				}
				summaryMsgs := append([]Message{}, compressMsgs...)
				summaryMsgs = append(summaryMsgs, NewMessage("user", effectiveSummaryPrompt, nil, "", ""))

				summaryResp, err := CallConfiguredLLM(cfg, summaryMsgs, nil)
				summaryText := ""
				if err != nil {
					fmt.Fprintf(os.Stderr, "[AgentLoop] summary compression failed at step %d: %v\n", step, err)
				} else if summaryResp != nil {
					summaryText = summaryResp.Content
				}
				if summaryText != "" {
					summaryMsg := NewMessage("assistant", "<summary>\n"+summaryText+"\n</summary>", nil, "", "")
					// Replaceable checkpoint: drop any prior <summary> markers from
					// the kept spans, then insert the new summary once at the boundary.
					prefixMsgs := make([]Message, 0, compressStart)
					for _, m := range messages[:compressStart] {
						if !isSummaryMarker(m) {
							prefixMsgs = append(prefixMsgs, m)
						}
					}
					tailMsgs := make([]Message, 0, len(messages)-compressEnd-1)
					for _, m := range messages[compressEnd+1:] {
						if !isSummaryMarker(m) {
							tailMsgs = append(tailMsgs, m)
						}
					}
					messages = append([]Message{}, prefixMsgs...)
					messages = append(messages, summaryMsg)
					messages = append(messages, tailMsgs...)

					// Rebuild the AI working view: system + summary + tail (tail kept
					// verbatim for information completeness - only the past is
					// compressed, recent messages are never trimmed).
					workingMsgs = append([]Message{}, prefixMsgs...)
					workingMsgs = append(workingMsgs, summaryMsg)
					// Inject a machine-generated handoff note so later turns know
					// which deliverables are already done and never redo them.
					if len(deliveredItems) > 0 {
						workingMsgs = append(workingMsgs, NewMessage("user", deliveredStatusText(deliveredItems), nil, "", ""))
					}
					workingMsgs = append(workingMsgs, tailMsgs...)
					// qn/Claude-compatible gateways require the LAST message to be
					// role=user. After compression the tail may end on a tool result
					// or an assistant message; append a neutral user prompt so the
					// next LLM call is accepted (same trick as the reflection path).
					if len(workingMsgs) > 0 && workingMsgs[len(workingMsgs)-1].Role != "user" {
						workingMsgs = append(workingMsgs, NewMessage("user", "\u8bf7\u7ee7\u7eed\u3002", nil, "", ""))
					}
					// Persist the full conversation right away so the session stays consistent
					if sessionFile != "" {
						if err := SaveSession(sessionFile, messages, modelName); err != nil {
							fmt.Fprintf(os.Stderr, "[AgentLoop] WARN: post-compress save session: %v\n", err)
						}
					}
				}
			}
		}

		if step >= maxSteps {
			trace = append(trace, map[string]interface{}{"event": "loop_end", "step": step, "reason": "max_steps"})
		}
	}

	// Generate title
	title := ""
	emitEvent("status", map[string]interface{}{"step": step, "message": "正在生成对话标题..."})

	if generateTitlePrompt != "" {
		var titleMsgs []Message
		for _, m := range messages {
			if m.Role == "system" || m.Role == "tool" {
				continue
			}
			if m.Role == "assistant" && m.Content == "" {
				continue
			}
			c := ""
			if m.Content != "" {
				c = m.Content
			}
			titleMsgs = append(titleMsgs, NewMessage(m.Role, c, nil, "", ""))
		}
		titleMsgs = append(titleMsgs, NewMessage("user", generateTitlePrompt, nil, "", ""))

		titleResp, err := CallConfiguredLLM(cfg, titleMsgs, nil)
		if err == nil && titleResp != nil && titleResp.Content != "" {
			title = strings.TrimSpace(titleResp.Content)
			if len(title) > 120 {
				title = title[:120]
			}
		}
	}

	return &AgentResult{
		Messages:        messages,
		Steps:           step,
		Usage:           sessionUsage,
		PerMessageUsage: perMsgUsage,
		Title:           title,
		Trace:           trace,
	}, nil
}

// SaveSession saves the session JSON file
func SaveSession(sessionFile string, messages []Message, model string) error {
	dir := filepath.Dir(sessionFile)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	session := map[string]interface{}{
		"messages": messages,
		"model":    model,
	}
	// ????? session_id?httptool ?????????????????
	if id := httptool.LoadSessionID(sessionFile); id != "" {
		session["session_id"] = id
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(sessionFile, data, 0644)
}

// SaveUsage saves the usage.json file
func SaveUsage(sessionFile string, usage *SessionUsage, messages []Message, perMsgUsage map[int]*PerMsgUsageEntry) error {
	dir := filepath.Dir(sessionFile)
	usageFile := filepath.Join(dir, "usage.json")

	totalRealMs := int64(0)
	for _, m := range messages {
		if m.Role == "assistant" {
			totalRealMs += m.RealMs
		}
	}

	usageData := map[string]interface{}{
		"prompt_tokens":     usage.PromptTokens,
		"completion_tokens": usage.CompletionTokens,
		"duration_ms":       usage.DurationMs,
		"real_ms":           totalRealMs,
		"turns":             []interface{}{},
	}

	var turns []map[string]interface{}
	for i, m := range messages {
		if m.Role == "assistant" && m.Usage != nil {
			dur := m.DurationMs
			if dur == 0 {
				dur = 0
			}
			turns = append(turns, map[string]interface{}{
				"message_index":     i,
				"prompt_tokens":     m.Usage.PromptTokens,
				"completion_tokens": m.Usage.CompletionTokens,
				"duration_ms":       dur,
				"real_ms":           m.RealMs,
			})
		}
	}
	usageData["turns"] = turns

	data, err := json.MarshalIndent(usageData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(usageFile, data, 0644)
}

// extractInlineToolCalls parses functions.name({...}) blocks some providers emit
// as text, returning native ToolCalls plus the content with those blocks removed.
// <report> is a final-only terminator and must never be parsed by the backend,
// so it is left untouched here.
func extractInlineToolCalls(content string) ([]ToolCall, string) {
	var calls []ToolCall
	var cleaned strings.Builder
	remaining := content
	id := 0
	for {
		idx := strings.Index(remaining, "functions.")
		if idx < 0 {
			cleaned.WriteString(remaining)
			break
		}
		cleaned.WriteString(remaining[:idx])
		rest := remaining[idx+len("functions."):]
		endName := strings.IndexByte(rest, '(')
		if endName <= 0 {
			cleaned.WriteString("functions.")
			remaining = rest
			continue
		}
		name := strings.TrimSpace(rest[:endName])
		if name == "" {
			cleaned.WriteString("functions.")
			remaining = rest
			continue
		}
		closeIdx := findInlineCallClose(rest, endName+1)
		if closeIdx < 0 {
			cleaned.WriteString("functions.")
			remaining = rest
			continue
		}
		argsRaw := strings.TrimSpace(rest[endName+1 : closeIdx])
		calls = append(calls, ToolCall{
			ID:       fmt.Sprintf("call_inline_%d", id),
			Type:     "function",
			Function: ToolCallFunc{Name: name, Arguments: argsRaw},
		})
		id++
		remaining = rest[closeIdx+1:]
	}
	return calls, cleaned.String()
}

func findInlineCallClose(rest string, open int) int {
	depth := 0
	inString := false
	escaped := false
	for i := open; i < len(rest); i++ {
		c := rest[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{', '(':
			depth++
		case '}':
			depth--
		case ')':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// snapshotFilesForRollback copies files targeted by write/edit calls into
// runs/rollback/<timestamp>/ so the user can restore the pre-change state.
func snapshotFilesForRollback(cfg *Config, invocations []ToolInvocation) []string {
	if cfg == nil || cfg.RepoRoot == "" {
		return nil
	}
	var targets []string
	for _, invocation := range invocations {
		name := strings.ToLower(invocation.Name)
		if name != "write_file" && name != "edit_file" {
			continue
		}
		var args struct {
			FilePath string `json:"file_path"`
		}
		if err := json.Unmarshal(invocation.Args, &args); err != nil || strings.TrimSpace(args.FilePath) == "" {
			continue
		}
		absPath, err := resolveRollbackPath(cfg, args.FilePath)
		if err != nil {
			continue
		}
		info, statErr := os.Stat(absPath)
		if statErr != nil || info.IsDir() {
			continue
		}
		targets = append(targets, absPath)
	}
	if len(targets) == 0 {
		return nil
	}
	rollbackRoot := filepath.Join(cfg.RepoRoot, "runs", "rollback", time.Now().Format("20060102_150405"))
	manifest := make(map[string]string)
	for _, absPath := range targets {
		rel, relErr := filepath.Rel(cfg.RepoRoot, absPath)
		if relErr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			rel = strings.Trim(strings.ReplaceAll(absPath, ":", ""), string(filepath.Separator))
		}
		target := filepath.Join(rollbackRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			continue
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}
		if err := os.WriteFile(target, data, 0o644); err != nil {
			continue
		}
		manifest[absPath] = target
	}
	if len(manifest) > 0 {
		raw, _ := json.MarshalIndent(manifest, "", "  ")
		_ = os.WriteFile(filepath.Join(rollbackRoot, "manifest.json"), raw, 0o644)
	}
	keys := make([]string, 0, len(manifest))
	for key := range manifest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func resolveRollbackPath(cfg *Config, requested string) (string, error) {
	lower := strings.ToLower(strings.TrimSpace(requested))
	raw := strings.TrimSpace(requested)
	if strings.HasPrefix(lower, "memory://") {
		raw = strings.TrimLeft(raw[len("memory://"):], "/")
		root := cfg.ResolvePath(cfg.MemoryDir)
		return resolveWithinRollbackRoot(root, raw)
	}
	for _, prefix := range []string{"local://", "knowledge://"} {
		if strings.HasPrefix(lower, prefix) {
			raw = raw[len(prefix):]
			break
		}
	}
	raw = filepath.FromSlash(raw)
	if !filepath.IsAbs(raw) {
		raw = filepath.Join(cfg.ResolvePath(cfg.WorkspaceDir), raw)
	}
	return filepath.Abs(raw)
}

func resolveWithinRollbackRoot(root, requested string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("memory root is not configured")
	}
	requested = filepath.FromSlash(strings.TrimSpace(requested))
	if filepath.IsAbs(requested) {
		return "", fmt.Errorf("path %q is outside rollback root", requested)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	full := filepath.Clean(filepath.Join(absRoot, requested))
	rel, err := filepath.Rel(absRoot, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q is outside rollback root", requested)
	}
	return full, nil
}

// shouldNudgeToExecute detects an actionable change request answered with a
// generic tutorial instead of tool calls, so the loop can force another pass.
func shouldNudgeToExecute(messages []Message, content string) bool {
	lastUser := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUser = messages[i].Content
			break
		}
	}
	if lastUser == "" {
		return false
	}
	lower := strings.ToLower(lastUser)
	actionable := false
	for _, keyword := range []string{"修改", "改成", "修复", "添加", "删除", "创建"} {
		if strings.Contains(lower, keyword) {
			actionable = true
			break
		}
	}
	if !actionable && strings.Contains(lower, "实现") && (strings.Contains(lower, "项目") || strings.Contains(lower, "文件")) {
		actionable = true
	}
	if !actionable {
		return false
	}
	if strings.Contains(lower, "解释") || strings.Contains(lower, "说明") || strings.Contains(lower, "怎么做") {
		return false
	}
	return looksLikeGenericTutorial(content)
}

func looksLikeGenericTutorial(content string) bool {
	lower := strings.ToLower(content)
	markers := []string{"通常需要", "假设前提", "以 react", "以 vue", "以 angular", "示例代码", "教程", "让用户自己去", "你可以按照", "需要在前端代码"}
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return len(content) > 400 && strings.Contains(content, "```")
}

// truncateStr truncates a string to maxLen chars
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

func NewMessageMap(role, content string, toolCalls []ToolCall, toolCallID, name string) Message {
	return NewMessage(role, content, toolCalls, toolCallID, name)
}

// extractReportTitle returns the first "# heading" inside a <report> block,
// else the first non-empty line (truncated). Empty if content has no report.
func extractReportTitle(content string) string {
	i := strings.Index(content, "<report>")
	if i < 0 {
		return ""
	}
	rest := content[i+len("<report>"):]
	for _, ln := range strings.Split(rest, "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(ln, "# "))
		}
		if ln != "" {
			return truncateStr(ln, 40)
		}
	}
	return ""
}

// addUnique appends item to list if non-empty and not already present.
func addUnique(list []string, item string) []string {
	if item == "" {
		return list
	}
	for _, x := range list {
		if x == item {
			return list
		}
	}
	return append(list, item)
}

// deliveredStatusText builds a machine-generated handoff note telling later
// turns which deliverables are already completed (so they are never redone).
func deliveredStatusText(items []string) string {
	return "[会话状态] 以下内容已完成并交付，后续不得重复执行：\n- " + strings.Join(items, "\n- ") + "\n仅处理用户新提出的请求。"
}
