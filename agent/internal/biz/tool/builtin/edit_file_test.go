package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalEditFileReplacesFirstOrAllOccurrences(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "edit.txt")
	if err := os.WriteFile(path, []byte("a a a"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := executeLocalFileTool(t, NewLocalEditFileTool(localFileTestSchema("edit_file")), workspace, `{"file_path":"edit.txt","old_string":"a","new_string":"b"}`)
	if result.IsError {
		t.Fatalf("unexpected first replacement error: %#v", result.Value)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "b a a" || result.Value["replacements"] != 1 {
		t.Fatalf("unexpected first replacement: content=%q result=%#v", raw, result.Value)
	}
	result = executeLocalFileTool(t, NewLocalEditFileTool(localFileTestSchema("edit_file")), workspace, `{"file_path":"edit.txt","old_string":"a","new_string":"c","all_occurrences":true}`)
	if result.IsError {
		t.Fatalf("unexpected all replacement error: %#v", result.Value)
	}
	raw, _ = os.ReadFile(path)
	if string(raw) != "b c c" || result.Value["replacements"] != 2 {
		t.Fatalf("unexpected all replacement: content=%q result=%#v", raw, result.Value)
	}
}
