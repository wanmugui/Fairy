package main

import (
	"strings"
	"testing"
)

func TestExtractInlineToolCallsParsesTextCall(t *testing.T) {
	content := "<report>接下来我需要修改文件。\n```typescript\nfunctions.read_file({\"file_path\": \"local://frontend/dist/index.html\"})\n```\n</report>"
	calls, cleaned := extractInlineToolCalls(content)
	if len(calls) != 1 {
		t.Fatalf("expected 1 inline call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read_file" {
		t.Fatalf("unexpected tool name: %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, "local://frontend/dist/index.html") {
		t.Fatalf("unexpected args: %q", calls[0].Function.Arguments)
	}
	if strings.Contains(cleaned, "functions.read_file") {
		t.Fatalf("cleaned content still contains pseudo call: %q", cleaned)
	}
	if !strings.Contains(cleaned, "<report>") || !strings.Contains(cleaned, "</report>") {
		t.Fatalf("<report> must remain untouched, got: %q", cleaned)
	}
}

func TestExtractInlineToolCallsSkipsProse(t *testing.T) {
	calls, cleaned := extractInlineToolCalls("我建议先读取文件。")
	if len(calls) != 0 {
		t.Fatalf("unexpected calls: %#v", calls)
	}
	if cleaned != "我建议先读取文件。" {
		t.Fatalf("prose changed: %q", cleaned)
	}
}
