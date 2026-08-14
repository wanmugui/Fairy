package local

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"testing"
)

func TestLocalEnvironmentSmoke(t *testing.T) {
	if os.Getenv("RUN_LOCAL_ENVIRONMENT_SMOKE") != "1" {
		t.Skip("set RUN_LOCAL_ENVIRONMENT_SMOKE=1 to exercise the user's installed Python and Shell")
	}
	workspace := t.TempDir()

	pythonTool := NewLocalExecuteCodeTool(ToolDef{}, &Config{})
	pythonResult, err := pythonTool.Execute(context.Background(), ToolInvocation{
		Workspace: workspace,
		Args:      json.RawMessage(`{"code":"import json; print(json.dumps({'smoke': 'ok'}))"}`),
	})
	if err != nil || pythonResult.IsError {
		t.Fatalf("Python smoke failed: result=%#v err=%v", pythonResult, err)
	}
	if pythonResult.Value["ok"] != true {
		t.Fatalf("Python smoke returned unsuccessful result: %#v", pythonResult.Value)
	}

	command := "printf smoke-ok"
	if runtime.GOOS == "windows" {
		command = "Write-Output smoke-ok"
	}
	bashTool := NewLocalBashTool(ToolDef{}, &Config{})
	bashResult, err := bashTool.Execute(context.Background(), ToolInvocation{
		Workspace: workspace,
		Args:      json.RawMessage(`{"command":"` + command + `"}`),
	})
	if err != nil || bashResult.IsError {
		t.Fatalf("Bash smoke failed: result=%#v err=%v", bashResult, err)
	}
	if bashResult.Value["result"] != "smoke-ok" {
		t.Fatalf("Bash smoke returned unexpected output: %#v", bashResult.Value)
	}
}

func TestLocalBrowserEnvironmentSmoke(t *testing.T) {
	if os.Getenv("RUN_LOCAL_BROWSER_SMOKE") != "1" {
		t.Skip("set RUN_LOCAL_BROWSER_SMOKE=1 to exercise the user's installed Chrome, Chromium or Edge")
	}
	tool := NewLocalHTMLToPNGTool(ToolDef{}, &Config{})
	result, err := tool.Execute(context.Background(), ToolInvocation{
		Workspace: t.TempDir(),
		Args:      json.RawMessage(`{"mode":"stateless","html_file_content":"<html><body style='margin:0'>phase3</body></html>","viewport_width":320,"viewport_height":180}`),
	})
	if err != nil || result.IsError {
		t.Fatalf("browser smoke failed: result=%#v err=%v", result, err)
	}
	if result.Value["ok"] != true || result.Value["width"] != 320 || result.Value["height"] != 180 {
		t.Fatalf("browser smoke returned unexpected result: %#v", result.Value)
	}
}
