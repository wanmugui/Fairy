package main

import (
	"agentloop/agent/internal/dtypes"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Message is the standard chat message format
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Usage      *UsageInfo `json:"usage,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
	RealMs     int64      `json:"real_ms,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type UsageInfo struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// APIResponse from LLM
type APIResponse struct {
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	Usage      *UsageInfo `json:"usage,omitempty"`
	FinishStop string     `json:"finish_stop,omitempty"`
	DurationMs int64      `json:"duration_ms,omitempty"`
}

// Request to LLM API
type APIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

type ToolDef = dtypes.ToolDef

// RealAPIResponse is the raw response from the external API
type RealAPIResponse struct {
	Choices []struct {
		Message struct {
			Content   string     `json:"content"`
			ToolCalls []ToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *UsageInfo `json:"usage"`
}

func CallLLM(cfg *APIConfig, messages []Message, toolDefs []ToolDef) (*APIResponse, error) {
	start := time.Now()

	// Strip usage/duration_ms from messages before sending (API rejects them)
	cleanMsgs := make([]Message, len(messages))
	for i, m := range messages {
		cm := Message{Role: m.Role, Content: m.Content}
		if len(m.ToolCalls) > 0 {
			cm.ToolCalls = m.ToolCalls
		}
		if m.ToolCallID != "" {
			cm.ToolCallID = m.ToolCallID
		}
		if m.Name != "" {
			cm.Name = m.Name
		}
		cleanMsgs[i] = cm
	}

	reqBody := APIRequest{
		Model:       cfg.Model,
		Messages:    cleanMsgs,
		MaxTokens:   cfg.MaxTokens,
		Temperature: cfg.Temperature,
	}
	if len(toolDefs) > 0 {
		reqBody.Tools = toolDefs
	}

	bodyJSON, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	client := &http.Client{Timeout: time.Duration(cfg.TimeoutSec) * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	apiURL := strings.TrimRight(cfg.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequest("POST", apiURL, bytes.NewReader(bodyJSON))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+cfg.APIKey)

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		// Debug aid: dump the failing request to a file so the exact message
		// sequence can be inspected (role/content head per message).
		func() {
			defer func() { _ = recover() }()
			var sb strings.Builder
			sb.WriteString("URL: " + apiURL + "\n")
			sb.WriteString("STATUS: " + fmt.Sprint(resp.StatusCode) + "\n")
			sb.WriteString("RESP: " + string(respBody) + "\n")
			sb.WriteString("--- MESSAGES ---\n")
			for i, m := range cleanMsgs {
				head := m.Content
				if len(head) > 300 {
					head = head[:300]
				}
				tc := ""
				if len(m.ToolCalls) > 0 {
					names := []string{}
					for _, t := range m.ToolCalls {
						names = append(names, t.Function.Name)
					}
					tc = " tool_calls=" + strings.Join(names, ",")
				}
				sb.WriteString(fmt.Sprintf("[%d] role=%s%s content=%q\n", i, m.Role, tc, head))
			}
			_ = os.WriteFile(filepath.Join(os.TempDir(), "apiclient_debug_fail.txt"), []byte(sb.String()), 0644)
		}()
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	duration := time.Since(start).Milliseconds()

	var realResp RealAPIResponse
	if err := json.Unmarshal(respBody, &realResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(realResp.Choices) == 0 {
		return nil, fmt.Errorf("empty choices")
	}

	// Fix: Clotho API returns empty string for arguments when no params,
	// but rejects empty arguments on re-submission. Default to "{}".
	for i := range realResp.Choices[0].Message.ToolCalls {
		if realResp.Choices[0].Message.ToolCalls[i].Function.Arguments == "" {
			realResp.Choices[0].Message.ToolCalls[i].Function.Arguments = "{}"
		}
	}

	choice := realResp.Choices[0]
	result := &APIResponse{
		Content:    choice.Message.Content,
		ToolCalls:  choice.Message.ToolCalls,
		Usage:      realResp.Usage,
		DurationMs: duration,
		FinishStop: choice.FinishReason,
	}
	return result, nil
}
