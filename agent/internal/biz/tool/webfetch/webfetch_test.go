package webfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"agentloop/agent/internal/dtypes"
)

func TestWebFetchReadsPlainText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("hello world\n"))
	}))
	defer server.Close()

	tool := NewTool(dtypes.ToolDef{})
	result, _ := tool.Execute(context.Background(), invocationFor(t, `{"url":"`+server.URL+`"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %#v", result.Value)
	}
	if got := result.Value["text"]; got != "hello world\n" {
		t.Fatalf("unexpected body: %#v", got)
	}
}

func TestWebFetchStripsHTML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<!DOCTYPE html><html><body><h1>Title</h1><p>Hello <b>world</b>.</p><script>alert(1)</script></body></html>`))
	}))
	defer server.Close()

	tool := NewTool(dtypes.ToolDef{})
	result, _ := tool.Execute(context.Background(), invocationFor(t, `{"url":"`+server.URL+`"}`))
	if result.IsError {
		t.Fatalf("unexpected error: %#v", result.Value)
	}
	text, _ := result.Value["text"].(string)
	if !strings.Contains(text, "Title") || !strings.Contains(text, "Hello world.") {
		t.Fatalf("expected prose in output, got %q", text)
	}
	if strings.Contains(text, "alert(1)") {
		t.Fatalf("script body should be stripped, got %q", text)
	}
}

func TestWebFetchRejectsNonHTTPURL(t *testing.T) {
	tool := NewTool(dtypes.ToolDef{})
	result, _ := tool.Execute(context.Background(), invocationFor(t, `{"url":"file:///etc/passwd"}`))
	if !result.IsError {
		t.Fatalf("expected error for non-http url, got %#v", result.Value)
	}
}

func TestWebFetchReportsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer server.Close()
	tool := NewTool(dtypes.ToolDef{})
	result, _ := tool.Execute(context.Background(), invocationFor(t, `{"url":"`+server.URL+`"}`))
	if !result.IsError {
		t.Fatalf("expected error for 500, got %#v", result.Value)
	}
}

// invocationFor builds a ToolInvocation for tests. The ToolFactory is not
// involved — Execute only needs CallID/Args populated.
func invocationFor(t *testing.T, args string) dtypes.ToolInvocation {
	t.Helper()
	return dtypes.ToolInvocation{
		CallID: "test",
		Name:   "web_fetch",
		Args:   []byte(args),
	}
}
