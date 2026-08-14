package main

import (
	toolruntime "agentloop/agent/internal/biz/tool"
	"agentloop/agent/internal/biz/tool/httptool"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type factoryHTTPToolRequest struct {
	ToolCallID string `json:"tool_call_id"`
	ToolName   string `json:"tool_name"`
	Arguments  string `json:"arguments"`
}

func newFactoryHTTPTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	return server
}

func writeFactorySchemas(t *testing.T, repoRoot string, names ...string) string {
	t.Helper()
	schemas := make(map[string]toolruntime.Schema, len(names))
	for _, name := range names {
		schemas[name] = toolruntime.Schema{
			Name:        name,
			Description: "schema for " + name,
			Parameters:  map[string]any{"type": "object"},
		}
	}
	raw, err := json.Marshal(schemas)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repoRoot, "config", "schemas.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func factoryTestConfig(t *testing.T, names ...string) *Config {
	t.Helper()
	repoRoot := t.TempDir()
	schemasPath := writeFactorySchemas(t, repoRoot, names...)
	return &Config{
		RepoRoot:     repoRoot,
		ToolsSchemas: schemasPath,
		HTTPTools:    make(map[string]ToolEntry),
		Gateway: GatewayConfig{
			Endpoint: "http://127.0.0.1:18080/api/agent/tool_call",
			Timeout:  2,
		},
	}
}

func localFactoryBuilder(name string) LocalToolBuilder {
	return func(schema ToolDef, cfg *Config) (Tool, error) {
		return NewLocalToolFunc(name, schema, func(ctx context.Context, inv ToolInvocation) (map[string]any, error) {
			return map[string]any{"tool": name}, nil
		}), nil
	}
}

func TestNewToolFactoryRegistersBuiltinLocalBuilders(t *testing.T) {
	factory := NewToolFactory()
	for _, name := range []string{
		"read_file",
		"write_file",
		"edit_file",
		"glob",
		"get_current_time",
		"todolist_create",
		"todolist_append",
		"todolist_update",
		"list_todos",
		"ask_user",
		"create_subtask",
		"execute_code",
		"bash",
		"html_to_png",
	} {
		if _, ok := factory.LocalBuilders[name]; !ok {
			t.Fatalf("builtin local builder %q is missing", name)
		}
	}
}

func TestBuildToolRegistryUsesBuiltinLocalBackend(t *testing.T) {
	cfg := factoryTestConfig(t, "read_file")
	cfg.ToolRuntime = &ToolRuntimeConfig{
		Tools: map[string]ToolBackendOverride{"read_file": {Backend: BackendLocal}},
	}

	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if backend, ok := registry.GetBackend("read_file"); !ok || backend != BackendLocal {
		t.Fatalf("unexpected backend: %q found=%v", backend, ok)
	}
	if tool, ok := registry.Get("read_file"); !ok {
		t.Fatal("read_file was not registered")
	} else if tool.Name() != "read_file" {
		t.Fatalf("unexpected local tool: %T %s", tool, tool.Name())
	}
}

func TestBuildToolRegistryReadsSkillFromConfiguredSkillsDirectory(t *testing.T) {
	cfg := factoryTestConfig(t, "read_file")
	cfg.SkillsDir = "skills"
	cfg.ToolRuntime = &ToolRuntimeConfig{
		Tools: map[string]ToolBackendOverride{"read_file": {Backend: BackendLocal}},
	}
	skillPath := filepath.Join(cfg.RepoRoot, cfg.SkillsDir, "ppt-maker", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# PPT maker"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("read_file")
	if !ok {
		t.Fatal("read_file was not registered")
	}
	result, err := tool.Execute(context.Background(), ToolInvocation{
		Name:      "read_file",
		Workspace: filepath.Join(cfg.RepoRoot, "workspace"),
		Args:      json.RawMessage(`{"file_path":"local:///skills/ppt-maker/SKILL.md"}`),
	})
	if err != nil || result.IsError {
		t.Fatalf("configured read_file could not read skill: result=%#v err=%v", result, err)
	}
}

func TestBuildToolRegistryDoesNotResolveLocalEnvironmentAtStartup(t *testing.T) {
	cfg := factoryTestConfig(t, "bash", "execute_code", "html_to_png")
	cfg.ToolRuntime = &ToolRuntimeConfig{
		Executables: LocalExecutableConfig{
			Python:  "/missing/python",
			Shell:   "/missing/shell",
			Browser: "/missing/browser",
		},
		Tools: map[string]ToolBackendOverride{
			"bash":         {Backend: BackendLocal},
			"execute_code": {Backend: BackendLocal},
			"html_to_png":  {Backend: BackendLocal},
		},
	}

	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		t.Fatalf("local environment must be resolved lazily at invocation time: %v", err)
	}
	for _, name := range []string{"bash", "execute_code", "html_to_png"} {
		if backend, ok := registry.GetBackend(name); !ok || backend != BackendLocal {
			t.Fatalf("unexpected backend for %s: %q found=%v", name, backend, ok)
		}
	}
}

func TestBuildToolRegistryUsesExplicitHTTPBackend(t *testing.T) {
	cfg := factoryTestConfig(t, "remote")
	cfg.ToolRuntime = &ToolRuntimeConfig{
		Tools: map[string]ToolBackendOverride{
			"remote": {Backend: BackendHTTP},
		},
		RetryCount: 2,
	}

	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if backend, ok := registry.GetBackend("remote"); !ok || backend != BackendHTTP {
		t.Fatalf("unexpected backend: %q found=%v", backend, ok)
	}
	tool, ok := registry.Get("remote")
	if !ok {
		t.Fatal("remote tool was not registered")
	}
	if _, ok := tool.(*httptool.HTTPTool); !ok {
		t.Fatalf("expected HTTPTool, got %T", tool)
	}
}

func TestBuildToolRegistryRoutesEnabledServiceToolToHTTPGateway(t *testing.T) {
	server := newFactoryHTTPTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request factoryHTTPToolRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if request.ToolName != "web_search" || request.ToolCallID != "call_http" || request.Arguments != `{"query":"cross platform"}` {
			t.Fatalf("unexpected gateway request: %#v", request)
		}
		_, _ = w.Write([]byte(`{"tool_call_id":"call_http","result":"{\"ok\":true,\"mock\":true}"}`))
	}))
	defer server.Close()

	cfg := factoryTestConfig(t, "web_search")
	cfg.Gateway.Endpoint = server.URL
	cfg.HTTPTools["web_search"] = ToolEntry{Enabled: true}
	cfg.ToolRuntime = &ToolRuntimeConfig{
		Tools: map[string]ToolBackendOverride{
			"web_search": {Backend: BackendHTTP},
		},
	}

	registry, err := NewToolFactory().BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if backend, ok := registry.GetBackend("web_search"); !ok || backend != BackendHTTP {
		t.Fatalf("enabled unified tool service must select HTTP: backend=%q found=%v", backend, ok)
	}
	tool, ok := registry.Get("web_search")
	if !ok {
		t.Fatal("web_search was not registered")
	}
	result, err := tool.Execute(context.Background(), ToolInvocation{CallID: "call_http", Args: json.RawMessage(`{"query":"cross platform"}`)})
	if err != nil || result.IsError || result.Value["mock"] != true {
		t.Fatalf("unexpected HTTP gateway result: result=%#v err=%v", result, err)
	}
}

func TestBuildToolRegistryDoesNotExposeDisabledTool(t *testing.T) {
	cfg := factoryTestConfig(t, "disabled")
	cfg.HTTPTools["disabled"] = ToolEntry{Enabled: false}
	cfg.ToolRuntime = &ToolRuntimeConfig{Tools: map[string]ToolBackendOverride{"disabled": {Backend: BackendLocal}}}
	factory := NewToolFactory()
	factory.LocalBuilders["disabled"] = localFactoryBuilder("disabled")

	registry, err := factory.BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Get("disabled"); ok || len(registry.ListSchemas()) != 0 {
		t.Fatalf("disabled tool was exposed: %#v", registry.ListNames())
	}
}

func TestBuildToolRegistryFailsWhenHTTPEndpointMissing(t *testing.T) {
	cfg := factoryTestConfig(t, "remote")
	cfg.Gateway.Endpoint = ""
	cfg.ToolRuntime = &ToolRuntimeConfig{Tools: map[string]ToolBackendOverride{"remote": {Backend: BackendHTTP}}}
	_, err := NewToolFactory().BuildRegistry(cfg)
	if err == nil || !strings.Contains(err.Error(), "endpoint") || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("unexpected missing endpoint error: %v", err)
	}
}

func TestBuildToolRegistryUsesRegisteredLocalBuilder(t *testing.T) {
	cfg := factoryTestConfig(t, "local")
	cfg.ToolRuntime = &ToolRuntimeConfig{Tools: map[string]ToolBackendOverride{"local": {Backend: BackendLocal}}}
	factory := NewToolFactory()
	factory.LocalBuilders["local"] = localFactoryBuilder("local")

	registry, err := factory.BuildRegistry(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tool, ok := registry.Get("LOCAL")
	if !ok {
		t.Fatal("local tool was not registered")
	}
	if _, ok := tool.(*LocalToolFunc); !ok {
		t.Fatalf("expected LocalToolFunc, got %T", tool)
	}
}

func TestBuildToolRegistryRejectsMissingBackendConfiguration(t *testing.T) {
	cfg := factoryTestConfig(t, "unconfigured")
	cfg.ToolRuntime = &ToolRuntimeConfig{Tools: map[string]ToolBackendOverride{}}
	_, err := NewToolFactory().BuildRegistry(cfg)
	if err == nil || !strings.Contains(err.Error(), "unconfigured") || !strings.Contains(err.Error(), "backend configuration") {
		t.Fatalf("unexpected missing backend error: %v", err)
	}
}
