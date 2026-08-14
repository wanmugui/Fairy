package local

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type recordingLocalProcessRunner struct {
	request localProcessRequest
	result  localProcessResult
	err     error
	inspect func(localProcessRequest)
}

func (r *recordingLocalProcessRunner) Run(_ context.Context, request localProcessRequest) (localProcessResult, error) {
	r.request = request
	if r.inspect != nil {
		r.inspect(request)
	}
	return r.result, r.err
}

func localExecuteTestConfig() *Config {
	return &Config{ToolRuntime: &ToolRuntimeConfig{Executables: LocalExecutableConfig{Python: "/custom/python"}}}
}

func localExecuteTestResolver() localExecutableResolver {
	return resolverForTest("darwin", nil, nil, map[string]bool{"/custom/python": true})
}

func TestLocalExecuteCodeRunsConfiguredPythonInWorkspace(t *testing.T) {
	workspace := t.TempDir()
	runner := &recordingLocalProcessRunner{
		result: localProcessResult{Stdout: `{"answer": 42}`, ExitCode: 0},
	}
	tool := newLocalExecuteCodeTool(ToolDef{}, localExecuteTestConfig(), localExecuteTestResolver(), runner)

	result, err := tool.Execute(context.Background(), ToolInvocation{
		Name:      "execute_code",
		Workspace: workspace,
		Args:      json.RawMessage(`{"code":"print('/mnt/data/result.json')"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError || result.Value["ok"] != true || result.Value["exit_code"] != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if got := result.Value["result"].(map[string]any)["answer"]; got != float64(42) {
		t.Fatalf("unexpected parsed JSON result: %#v", result.Value["result"])
	}
	if runner.request.Path != "/custom/python" || runner.request.Dir != workspace {
		t.Fatalf("unexpected process request: %#v", runner.request)
	}
	if info, statErr := os.Stat(filepath.Join(workspace, "result")); statErr != nil || !info.IsDir() {
		t.Fatalf("result directory was not created at the workspace root: err=%v", statErr)
	}
	if len(runner.request.Args) != 2 || runner.request.Args[0] != "-u" {
		t.Fatalf("unexpected Python args: %#v", runner.request.Args)
	}
	if _, err := os.Stat(runner.request.Args[1]); !os.IsNotExist(err) {
		t.Fatalf("temporary Python file was not cleaned up: %q err=%v", runner.request.Args[1], err)
	}
}

func TestLocalExecuteCodeMapsMntDataAndKeepsUtf8Script(t *testing.T) {
	workspace := t.TempDir()
	var script string
	runner := &recordingLocalProcessRunner{
		result: localProcessResult{Stdout: "ok", ExitCode: 0},
		inspect: func(request localProcessRequest) {
			raw, err := os.ReadFile(request.Args[len(request.Args)-1])
			if err != nil {
				t.Errorf("read temporary script: %v", err)
				return
			}
			script = string(raw)
		},
	}
	tool := newLocalExecuteCodeTool(ToolDef{}, localExecuteTestConfig(), localExecuteTestResolver(), runner)

	_, err := tool.Execute(context.Background(), ToolInvocation{
		Name:      "execute_code",
		Workspace: workspace,
		Args:      json.RawMessage(`{"code":"print('中文 /mnt/data/a.txt')"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(script, "# -*- coding: utf-8 -*-") || !strings.Contains(script, filepath.ToSlash(filepath.Join(workspace, "a.txt"))) {
		t.Fatalf("script did not preserve UTF-8 header and mapped path: %q", script)
	}
}

func TestReplaceMntDataReferencesMapsBareRootWithoutChangingLookalikes(t *testing.T) {
	workspace := t.TempDir()
	got := replaceMntDataReferences(`ls /mnt/data && print("/mnt/data/result") && echo /mnt/database`, workspace)
	if !strings.Contains(got, "ls "+filepath.ToSlash(workspace)+" &&") ||
		!strings.Contains(got, filepath.ToSlash(filepath.Join(workspace, "result"))) ||
		!strings.Contains(got, "/mnt/database") {
		t.Fatalf("unexpected replacement: %q", got)
	}
}

func TestReplaceLocalPathReferencesMapsProductionSkillsRoot(t *testing.T) {
	workspace := t.TempDir()
	skillsRoot := filepath.Join(t.TempDir(), "skills")
	got := replaceLocalPathReferences(`print("/skills/ppt-maker/SKILL.md")\nprint("/skills-not-a-root")`, workspace, skillsRoot)
	if !strings.Contains(got, filepath.ToSlash(filepath.Join(skillsRoot, "ppt-maker", "SKILL.md"))) ||
		strings.Contains(got, filepath.ToSlash(skillsRoot)+"-not-a-root") {
		t.Fatalf("unexpected replacement: %q", got)
	}
}

func TestLocalExecuteCodePreservesProcessFailureFields(t *testing.T) {
	runner := &recordingLocalProcessRunner{result: localProcessResult{
		Stdout:   "partial",
		Stderr:   "traceback",
		ExitCode: 2,
	}}
	tool := newLocalExecuteCodeTool(ToolDef{}, localExecuteTestConfig(), localExecuteTestResolver(), runner)

	result, err := tool.Execute(context.Background(), ToolInvocation{Workspace: t.TempDir(), Args: json.RawMessage(`{"code":"raise SystemExit(2)"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value["ok"] != false || result.Value["exit_code"] != 2 || result.Value["stdout"] != "partial" || result.Value["stderr"] != "traceback" {
		t.Fatalf("process failure fields were not preserved: %#v", result.Value)
	}
}

func TestLocalExecuteCodeReturnsUnavailableWithoutPython(t *testing.T) {
	resolver := resolverForTest("darwin", nil, nil, nil)
	runner := &recordingLocalProcessRunner{}
	tool := newLocalExecuteCodeTool(ToolDef{}, &Config{}, resolver, runner)

	result, err := tool.Execute(context.Background(), ToolInvocation{Workspace: t.TempDir(), Args: json.RawMessage(`{"code":"print(1)"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError || result.Value["code"] != "unavailable" || result.Value["tool"] != "execute_code" {
		t.Fatalf("unexpected unavailable result: %#v", result)
	}
	if runner.request.Path != "" {
		t.Fatal("runner should not start when Python is unavailable")
	}
}

func TestLocalExecuteCodeRejectsInvalidLanguageAndEmptyCode(t *testing.T) {
	tool := newLocalExecuteCodeTool(ToolDef{}, localExecuteTestConfig(), localExecuteTestResolver(), &recordingLocalProcessRunner{})
	for name, args := range map[string]string{
		"empty":    `{"code":""}`,
		"language": `{"code":"print(1)","language":"javascript"}`,
	} {
		result, err := tool.Execute(context.Background(), ToolInvocation{Workspace: t.TempDir(), Args: json.RawMessage(args)})
		if err != nil {
			t.Fatalf("%s: unexpected execution error: %v", name, err)
		}
		if !result.IsError || result.Value["tool"] != "execute_code" {
			t.Fatalf("%s: expected structured error, got %#v", name, result)
		}
	}
}

func TestLocalExecuteCodeReportsToolTimeout(t *testing.T) {
	runner := &recordingLocalProcessRunner{result: localProcessResult{ExitCode: -1, TimedOut: true}}
	tool := newLocalExecuteCodeTool(ToolDef{}, localExecuteTestConfig(), localExecuteTestResolver(), runner)

	result, err := tool.Execute(context.Background(), ToolInvocation{Workspace: t.TempDir(), Args: json.RawMessage(`{"code":"sleep(10)"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if result.Value["timed_out"] != true || result.Value["ok"] != false {
		t.Fatalf("unexpected timeout result: %#v", result.Value)
	}
	if runner.request.Timeout < 5*time.Second {
		t.Fatalf("unexpected default timeout: %v", runner.request.Timeout)
	}
}

func TestLocalExecuteCodePassesPptToolEnvironment(t *testing.T) {
	runner := &recordingLocalProcessRunner{result: localProcessResult{ExitCode: 0}}
	cfg := localExecuteTestConfig()
	cfg.PptTools = PptToolsConfig{
		BaseURL: "https://ppt.example/",
		APIPath: "/api/agent/tool_call",
		HostPin: "ppt.example=127.0.0.1",
	}
	tool := newLocalExecuteCodeTool(ToolDef{}, cfg, localExecuteTestResolver(), runner)

	_, err := tool.Execute(context.Background(), ToolInvocation{Workspace: t.TempDir(), Args: json.RawMessage(`{"code":"print('ok')"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"PPT_TOOL_API_BASE=https://ppt.example",
		"BACKEND_TOOL_BASE=https://ppt.example",
		"CREATIVE_RENDER_API_URL=https://ppt.example/api/agent/tool_call",
		"PPT_TOOL_HOST_IP=ppt.example=127.0.0.1",
		"CREATIVE_RENDER_HOST_IP=ppt.example=127.0.0.1",
	} {
		if !containsLocalEnv(runner.request.Env, want) {
			t.Fatalf("missing %q in environment: %#v", want, runner.request.Env)
		}
	}
}

func containsLocalEnv(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}
