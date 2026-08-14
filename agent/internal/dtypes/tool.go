package dtypes

import (
	"context"
	"encoding/json"
	"time"
)

// ToolDef is the OpenAI-compatible function declaration supplied to the LLM.
type ToolDef struct {
	Type     string      `json:"type"`
	Function interface{} `json:"function"`
}

// ToolInvocation is the platform-neutral input supplied to a tool execution.
type ToolInvocation struct {
	Index       int
	CallID      string
	Name        string
	Args        json.RawMessage
	Timeout     time.Duration
	Workspace   string
	SessionFile string
	Metadata    map[string]string
}

// ToolResult is the normalized result returned by every tool backend.
type ToolResult struct {
	Value        map[string]any
	IsError      bool
	WaitingReply bool
	UpstreamCode int
}

type ToolBackend string

const (
	BackendLocal ToolBackend = "local"
	BackendHTTP  ToolBackend = "http"
)

type ToolInvocationResult struct {
	Index  int
	CallID string
	Name   string
	Result ToolResult
	Err    error
}

func (r ToolResult) JSON() ([]byte, error) {
	if r.Value == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(r.Value)
}

// Tool is the common execution contract for builtin, local and HTTP tools.
type Tool interface {
	Name() string
	Schema() ToolDef
	Execute(context.Context, ToolInvocation) (ToolResult, error)
}
