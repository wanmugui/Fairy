package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalEditFileReplacesFirstOrAllOccurrences(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "edit.txt")
	if err := os.WriteFile(path, []byte("a a a"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Non-unique old_text without replace_all must surface the ambiguity so
	// the model can either retry with more context or pass replace_all=true.
	result := executeLocalFileTool(t, NewLocalEditFileTool(localFileTestSchema("edit_file")), workspace, `{"file_path":"edit.txt","old_text":"a","new_text":"b"}`)
	if !result.IsError {
		t.Fatalf("expected ambiguous old_text to error, got %#v", result.Value)
	}
	if !strings.Contains(stringifyEditResult(result.Value), "matched 3 locations") {
		t.Fatalf("expected match-count hint in error, got %#v", result.Value)
	}
	// Unique old_text replaces exactly once.
	result = executeLocalFileTool(t, NewLocalEditFileTool(localFileTestSchema("edit_file")), workspace, `{"file_path":"edit.txt","old_text":"a a","new_text":"b"}`)
	if result.IsError {
		t.Fatalf("unexpected unique-replacement error: %#v", result.Value)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "b a" || result.Value["replacements"] != 1 {
		t.Fatalf("unexpected first replacement: content=%q result=%#v", raw, result.Value)
	}
	// replace_all=true replaces every occurrence.
	result = executeLocalFileTool(t, NewLocalEditFileTool(localFileTestSchema("edit_file")), workspace, `{"file_path":"edit.txt","old_text":"a","new_text":"c","replace_all":true}`)
	if result.IsError {
		t.Fatalf("unexpected all replacement error: %#v", result.Value)
	}
	raw, _ = os.ReadFile(path)
	// After the unique replacement the file is "b a", so one "a" remains.
	if string(raw) != "b c" || result.Value["replacements"] != 1 {
		t.Fatalf("unexpected all replacement: content=%q result=%#v", raw, result.Value)
	}
}

func stringifyEditResult(v map[string]any) string {
	if v == nil {
		return ""
	}
	if err, ok := v["error"].(string); ok {
		return err
	}
	return ""
}
