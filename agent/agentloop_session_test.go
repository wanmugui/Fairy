package main

import (
	"os"
	"path/filepath"
	"testing"

	"agentloop/agent/internal/biz/tool/httptool"
)

// TestSaveSessionPreservesSessionID ?? SaveSession ?????????
// httptool ????? session_id ???
func TestSaveSessionPreservesSessionID(t *testing.T) {
	dir := t.TempDir()
	sessionFile := filepath.Join(dir, "chat.json")
	const sid = "11111111-1111-4111-8111-111111111111"
	if err := os.WriteFile(sessionFile, []byte(`{"messages":[],"model":"m","session_id":"`+sid+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSession(sessionFile, nil, "m2"); err != nil {
		t.Fatal(err)
	}
	if got := httptool.LoadSessionID(sessionFile); got != sid {
		t.Fatalf("SaveSession dropped session_id: got %q want %q", got, sid)
	}
}
