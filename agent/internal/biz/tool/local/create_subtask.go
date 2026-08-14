package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// localAgentExecutableResolver chooses the current platform's Agent executable.
// Keeping it injectable lets the Local tool test the child-process contract without
// starting a real LLM session.
type localAgentExecutableResolver func(*Config) (string, error)

type localCreateSubtaskTool struct {
	schema       ToolDef
	cfg          *Config
	runner       localProcessRunner
	resolveAgent localAgentExecutableResolver
}

var localSubtaskSequence uint64

func NewLocalCreateSubtaskTool(schema ToolDef, cfg *Config) Tool {
	return newLocalCreateSubtaskTool(schema, cfg, osLocalProcessRunner{}, resolveLocalAgentExecutable)
}

func newLocalCreateSubtaskTool(schema ToolDef, cfg *Config, runner localProcessRunner, resolver localAgentExecutableResolver) Tool {
	if cfg == nil {
		cfg = &Config{}
	}
	if runner == nil {
		runner = osLocalProcessRunner{}
	}
	if resolver == nil {
		resolver = resolveLocalAgentExecutable
	}
	return &localCreateSubtaskTool{schema: schema, cfg: cfg, runner: runner, resolveAgent: resolver}
}

func (t *localCreateSubtaskTool) Name() string {
	return "create_subtask"
}

func (t *localCreateSubtaskTool) Schema() ToolDef {
	return t.schema
}

func (t *localCreateSubtaskTool) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	title := localStringArg(args, "title")
	goal := localStringArg(args, "goal")
	if strings.TrimSpace(goal) == "" {
		return localErrorResult(t.Name(), fmt.Errorf("goal is required")), nil
	}
	localContext, err := localToolContext(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}

	agentExecutable, err := t.resolveAgent(t.cfg)
	if err != nil {
		return localSubtaskUnavailableResult(err), nil
	}
	userPrompt, err := buildLocalSubtaskPrompt(t.cfg, title, goal, args)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	userFile, err := os.CreateTemp("", "agent-subtask-user-*.txt")
	if err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("create delegated prompt: %w", err)), nil
	}
	userFilePath := userFile.Name()
	defer os.Remove(userFilePath)
	if err := userFile.Chmod(0o600); err != nil {
		_ = userFile.Close()
		return localErrorResult(t.Name(), fmt.Errorf("protect delegated prompt: %w", err)), nil
	}
	if _, err := userFile.WriteString(userPrompt); err != nil {
		_ = userFile.Close()
		return localErrorResult(t.Name(), fmt.Errorf("write delegated prompt: %w", err)), nil
	}
	if err := userFile.Close(); err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("close delegated prompt: %w", err)), nil
	}

	sessionFile := localSubtaskSessionFile(invocation.SessionFile, title)
	streamFile, err := os.OpenFile(sessionFile+".stream", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("create subtask stream: %w", err)), nil
	}
	defer streamFile.Close()
	streamHeader, _ := json.Marshal(map[string]any{"type": "subtask_start", "title": title, "ts": time.Now().Unix()})
	_, _ = streamFile.Write(append(streamHeader, '\n'))

	childArgs := []string{
		"-ConfigPath", localSubtaskConfigPath(t.cfg),
		"-UseMock", strconv.FormatBool(t.cfg.UseMock),
		"-UserOverrideFile", userFilePath,
		"-SessionFile", sessionFile,
	}
	if resumeSession := localStringArg(args, "resume_session"); resumeSession != "" {
		childArgs = append(childArgs, "-ResumeSessionFile", resumeSession)
	}
	processResult, err := t.runner.Run(ctx, localProcessRequest{
		Path:         agentExecutable,
		Args:         childArgs,
		Dir:          t.cfg.RepoRoot,
		Env:          localSubtaskEnvironment(t.cfg, localContext.Workspace, title),
		Timeout:      invocation.Timeout,
		StdoutWriter: streamFile,
		StderrWriter: streamFile,
	})
	if err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("run subtask agent: %w", err)), nil
	}
	if processResult.TimedOut {
		return localErrorResult(t.Name(), fmt.Errorf("subtask timed out after %s", invocation.Timeout)), nil
	}
	if processResult.ExitCode != 0 {
		return localSubtaskProcessError(sessionFile, processResult), nil
	}

	return localSubtaskResult(title, sessionFile), nil
}

func resolveLocalAgentExecutable(cfg *Config) (string, error) {
	if override := strings.TrimSpace(os.Getenv("AGENT_LOOP_PATH")); override != "" {
		return requireLocalAgentExecutable(override)
	}
	if executable, err := os.Executable(); err == nil {
		if path, statErr := requireLocalAgentExecutable(executable); statErr == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("agent executable is unavailable; start with pnpm dev or set AGENT_LOOP_PATH")
}

func requireLocalAgentExecutable(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("agent executable %q: %w", path, err)
	}
	if info.IsDir() || !info.Mode().IsRegular() {
		return "", fmt.Errorf("agent executable %q is not a regular file", path)
	}
	return path, nil
}

func localSubtaskUnavailableResult(err error) ToolResult {
	return ToolResult{
		Value: map[string]any{
			"tool":  "create_subtask",
			"ok":    false,
			"code":  "unavailable",
			"error": err.Error(),
		},
		IsError: true,
	}
}

func localSubtaskConfigPath(cfg *Config) string {
	if cfg != nil && strings.TrimSpace(cfg.ConfigPath) != "" {
		return cfg.ConfigPath
	}
	if cfg != nil && strings.TrimSpace(cfg.RepoRoot) != "" {
		return filepath.Join(cfg.RepoRoot, "config", "config.minimax.json")
	}
	return filepath.Join("config", "config.minimax.json")
}

func localSubtaskEnvironment(cfg *Config, workspace, title string) []string {
	repoRoot := ""
	if cfg != nil {
		repoRoot = cfg.RepoRoot
	}
	env := []string{
		"AGENT_REPO_ROOT=" + repoRoot,
		"WORKSPACE_DIR=" + workspace,
		"AGENT_RUN_KIND=subtask",
		"AGENT_SUBTASK_TITLE=" + title,
	}
	return append(env, localPptToolEnvironment(cfg)...)
}

func buildLocalSubtaskPrompt(cfg *Config, title, goal string, args map[string]any) (string, error) {
	taskParts := make([]string, 0, 16)
	if title != "" {
		taskParts = append(taskParts, "# Subtask: "+title)
	}
	taskParts = append(taskParts, "", "## Goal", goal, "", "## Todo", localStringArg(args, "todo"))
	for _, section := range []struct{ heading, key string }{
		{"Relevant Files", "relevant_files"},
		{"Criteria", "criteria"},
		{"Additional Info", "addition"},
	} {
		if value := localStringArg(args, section.key); value != "" {
			taskParts = append(taskParts, "", "## "+section.heading, value)
		}
	}
	if quickLook := localSubtaskQuickLook(localStringArg(args, "relevant_files"), cfg.RepoRoot, 4000); quickLook != "" {
		taskParts = append(taskParts, "", "## 关键规范速览", quickLook)
	}
	if localSubtaskIsResearch(title, goal, localStringArg(args, "todo")) {
		taskParts = append(taskParts, "", "## 收敛约束", "网络搜索(web_search) + 网页抓取(fetch_url) 合计最多 20 次；同一主题去重；信息足够即停止搜索，直接产出结论。")
	}
	taskContent := strings.Join(taskParts, "\n")

	if cfg.BuildSubtaskPrompt != nil {
		return cfg.BuildSubtaskPrompt(taskContent)
	}
	return taskContent + "\n\n请根据上面的被委派任务执行工作，完成后在 <subtask_result> 中输出结果。", nil
}

func localSubtaskIsResearch(parts ...string) bool {
	for _, part := range parts {
		for _, keyword := range []string{"调研", "深度研究", "研究", "资料", "搜集", "收集"} {
			if strings.Contains(part, keyword) {
				return true
			}
		}
	}
	return false
}

func localSubtaskQuickLook(relevantFiles, repoRoot string, budgetChars int) string {
	keywords := []string{"必须", "禁止", "不得", "硬约束", "字段", "校验", "输出", "产物", "绝对", "严禁", "只能"}
	var output []string
	used := 0
	for _, requested := range strings.FieldsFunc(relevantFiles, func(r rune) bool {
		return r == '\n' || r == '\r' || r == ' ' || r == ',' || r == '\t'
	}) {
		path := strings.TrimSpace(requested)
		if path == "" || !strings.HasSuffix(strings.ToLower(path), ".md") {
			continue
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoRoot, path)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		header := "> " + filepath.Base(path)
		if used+len([]rune(header)) > budgetChars {
			break
		}
		output = append(output, header)
		used += len([]rune(header))
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || (!strings.HasPrefix(line, "#") && !containsAnyString(line, keywords)) {
				continue
			}
			if used+len([]rune(line)) > budgetChars {
				break
			}
			output = append(output, line)
			used += len([]rune(line))
		}
	}
	return strings.Join(output, "\n")
}

func containsAnyString(value string, candidates []string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}

func localSubtaskSessionFile(parentSession, title string) string {
	sequence := atomic.AddUint64(&localSubtaskSequence, 1)
	if parentSession == "" {
		return filepath.Join(os.TempDir(), fmt.Sprintf("subtask_result_%d_%d.json", time.Now().UnixNano(), sequence))
	}
	subtasksDir := filepath.Join(filepath.Dir(parentSession), "subtasks")
	if err := os.MkdirAll(subtasksDir, 0o755); err != nil {
		return filepath.Join(os.TempDir(), fmt.Sprintf("subtask_result_%d_%d.json", time.Now().UnixNano(), sequence))
	}
	safeTitle := localSubtaskFilename(title)
	if safeTitle == "" {
		safeTitle = "subtask"
	}
	return filepath.Join(subtasksDir, fmt.Sprintf("%s-%d-%d.json", safeTitle, time.Now().UnixNano(), sequence))
}

func localSubtaskFilename(value string) string {
	var out strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_' || r == '-' || r == '.':
			out.WriteRune(r)
		case r == ' ':
			out.WriteRune('_')
		case r > 0x7f:
			out.WriteRune(r)
		default:
			out.WriteRune('_')
		}
	}
	name := strings.Trim(out.String(), "_.-")
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	return name
}

func localSubtaskProcessError(sessionFile string, result localProcessResult) ToolResult {
	logFile := sessionFile + ".log"
	logBody := result.Stdout + result.Stderr
	if err := os.WriteFile(logFile, []byte(logBody), 0o600); err != nil {
		logFile = ""
	}
	errorText := fmt.Sprintf("subtask agent exited with code %d", result.ExitCode)
	if logFile != "" {
		errorText += "; full log: " + logFile
	}
	return ToolResult{Value: map[string]any{"tool": "create_subtask", "ok": false, "error": errorText, "exit_code": result.ExitCode, "log": logFile}, IsError: true}
}

func localSubtaskResult(title, sessionFile string) ToolResult {
	value := map[string]any{"ok": true, "title": title, "session": sessionFile}
	raw, err := os.ReadFile(sessionFile)
	if err != nil {
		return ToolResult{Value: value}
	}
	var session struct {
		Messages []map[string]any `json:"messages"`
	}
	if json.Unmarshal(raw, &session) != nil {
		return ToolResult{Value: value}
	}
	var duration, promptTokens, completionTokens int64
	for _, message := range session.Messages {
		if message["role"] != "assistant" {
			continue
		}
		if value, ok := message["duration_ms"].(float64); ok {
			duration += int64(value)
		}
		if usage, ok := message["usage"].(map[string]any); ok {
			if value, ok := usage["prompt_tokens"].(float64); ok {
				promptTokens += int64(value)
			}
			if value, ok := usage["completion_tokens"].(float64); ok {
				completionTokens += int64(value)
			}
		}
	}
	value["agent_stats"] = map[string]any{
		"duration_ms": duration, "prompt_tokens": promptTokens, "completion_tokens": completionTokens,
	}
	if final := localSubtaskFinalDelivery(session.Messages); final != "" {
		// The full child conversation is already persisted in sessionFile. Return
		// only its final delivery to the parent so parallel PPT subtasks cannot
		// flood the main Agent context with their intermediate tool chatter.
		value["result"] = final
		value["messages"] = []map[string]any{{"role": "assistant", "content": final}}
	}
	return ToolResult{Value: value}
}

func localSubtaskFinalDelivery(messages []map[string]any) string {
	var report string
	for _, message := range messages {
		if message["role"] != "assistant" {
			continue
		}
		content, _ := message["content"].(string)
		if strings.Contains(content, "<subtask_result>") {
			report = content
			continue
		}
		if report == "" && strings.Contains(content, "<report>") {
			report = content
		}
	}
	if report != "" {
		return report
	}
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index]["role"] != "assistant" {
			continue
		}
		if content, _ := messages[index]["content"].(string); content != "" {
			return content
		}
	}
	return ""
}
