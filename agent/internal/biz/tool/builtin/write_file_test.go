package builtin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalWriteFileCreatesParentAndReturnsRelativePath(t *testing.T) {
	workspace := t.TempDir()
	result := executeLocalFileTool(t, NewLocalWriteFileTool(localFileTestSchema("write_file")), workspace, `{"file_path":"nested/out.txt","content":"hello"}`)
	if result.IsError {
		t.Fatalf("unexpected error result: %#v", result.Value)
	}
	if got, want := result.Value["path"], "nested/out.txt"; got != want {
		t.Fatalf("unexpected result path: got %#v want %q", got, want)
	}
	raw, err := os.ReadFile(filepath.Join(workspace, "nested", "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello" {
		t.Fatalf("unexpected file content: %q", raw)
	}
}

func TestLocalWriteFileAllowsBundledSkillPath(t *testing.T) {
	// Post-loosen: bundled-skill paths are writable when the agent supplies an
	// absolute /skills target. The skill-vs-workspace distinction is now
	// informational, not a containment boundary.
	result := executeLocalFileTool(t, NewLocalWriteFileTool(localFileTestSchema("write_file")), t.TempDir(), `{"file_path":"local:///skills/ppt-maker/SKILL.md","content":"changed"}`)
	if result.IsError {
		t.Fatalf("expected bundled skill write to succeed, got %#v", result.Value)
	}
}
