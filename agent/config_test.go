package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeConfigForTest(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadConfigCreatesExplicitToolRouterForOldConfig(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{"workspace_dir": "workspace"})
	cfg, err := LoadConfig(filepath.Dir(path), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolRuntime == nil {
		t.Fatal("expected tool runtime defaults")
	}
	if len(cfg.ToolRuntime.Tools) != 0 {
		t.Fatalf("unexpected default routes: %#v", cfg.ToolRuntime.Tools)
	}
	if cfg.ToolRuntime.RetryCount != 1 {
		t.Fatalf("unexpected default retry count: %d", cfg.ToolRuntime.RetryCount)
	}
}

func TestLoadConfigReadsExplicitToolBackend(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{
		"tool_runtime": map[string]any{
			"tools": map[string]any{
				"read_file": map[string]any{"backend": "local"},
			},
			"retry_count": 3,
		},
	})
	cfg, err := LoadConfig(filepath.Dir(path), path)
	if err != nil {
		t.Fatal(err)
	}
	override, ok := cfg.ToolRuntime.Tools["read_file"]
	if !ok || override.Backend != BackendLocal {
		t.Fatalf("unexpected tool override: %#v", override)
	}
	if cfg.ToolRuntime.RetryCount != 3 {
		t.Fatalf("unexpected retry count: %d", cfg.ToolRuntime.RetryCount)
	}
}

func TestLoadConfigRejectsUnknownBackend(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{
		"tool_runtime": map[string]any{"tools": map[string]any{"read_file": map[string]any{"backend": "powershell"}}},
	})
	if _, err := LoadConfig(filepath.Dir(path), path); err == nil {
		t.Fatal("expected unknown backend to be rejected")
	}
}

func TestLoadConfigDefaultsHTTPRetryCount(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{
		"tool_runtime": map[string]any{},
	})
	cfg, err := LoadConfig(filepath.Dir(path), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolRuntime.RetryCount != 1 {
		t.Fatalf("expected retry count 1, got %d", cfg.ToolRuntime.RetryCount)
	}
}

func TestLoadConfigPreservesReflectionAndToolAPISettings(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{
		"reflection": map[string]any{"enabled": true},
		"tools": map[string]any{
			"fetchUrl": map[string]any{
				"summaryModelName":    "summary-model",
				"summaryModelBaseUrl": "https://summary.example/v1",
			},
			"readFile": map[string]any{
				"maxReadFileSizeBytes": 4096,
			},
		},
	})
	cfg, err := LoadConfig(filepath.Dir(path), path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Reflection.Enabled {
		t.Fatal("expected reflection configuration to be preserved")
	}
	if cfg.Tools.FetchUrl.SummaryModelName != "summary-model" || cfg.Tools.FetchUrl.SummaryModelBaseUrl != "https://summary.example/v1" {
		t.Fatalf("unexpected fetch URL settings: %#v", cfg.Tools.FetchUrl)
	}
	if cfg.Tools.ReadFile.MaxReadFileSizeBytes != 4096 {
		t.Fatalf("unexpected read file settings: %#v", cfg.Tools.ReadFile)
	}
}

func TestLoadConfigReadsLocalExecutables(t *testing.T) {
	path := writeConfigForTest(t, map[string]any{
		"tool_runtime": map[string]any{
			"executables": map[string]any{
				"python":  "/custom/python",
				"shell":   "/custom/shell",
				"browser": "/custom/browser",
			},
		},
	})
	cfg, err := LoadConfig(filepath.Dir(path), path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ToolRuntime.Executables.Python != "/custom/python" ||
		cfg.ToolRuntime.Executables.Shell != "/custom/shell" ||
		cfg.ToolRuntime.Executables.Browser != "/custom/browser" {
		t.Fatalf("unexpected local executable config: %#v", cfg.ToolRuntime.Executables)
	}
}

func TestDefaultConfigsUseLocalEnvironmentAdapters(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, relativePath := range []string{"config/config.json", "config/config.minimax.json"} {
		t.Run(filepath.Base(relativePath), func(t *testing.T) {
			cfg, err := LoadConfig(repoRoot, relativePath)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"bash", "create_subtask", "execute_code", "html_to_png"} {
				override, ok := configuredBackendOverride(cfg.ToolRuntime.Tools, name)
				if !ok || override.Backend != BackendLocal {
					t.Fatalf("%s must use the local backend in %s: %#v", name, relativePath, override)
				}
			}
			if cfg.ToolRuntime.Executables != (LocalExecutableConfig{}) {
				t.Fatalf("shared config must not contain user-specific executable paths: %#v", cfg.ToolRuntime.Executables)
			}
		})
	}
}

func TestDefaultConfigsRouteAllSchemasWithoutLegacyExecutables(t *testing.T) {
	repoRoot, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	for _, relativePath := range []string{"config/config.json", "config/config.minimax.json"} {
		t.Run(filepath.Base(relativePath), func(t *testing.T) {
			cfg, err := LoadConfig(repoRoot, relativePath)
			if err != nil {
				t.Fatal(err)
			}
			registry, err := NewToolFactory().BuildRegistry(cfg)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range registry.ListNames() {
				backend, ok := registry.GetBackend(name)
				if !ok {
					t.Fatalf("schema %q is not registered", name)
				}
				if backend != BackendLocal && backend != BackendHTTP {
					t.Fatalf("schema %q has unsupported backend %q in %s", name, backend, relativePath)
				}
			}
		})
	}
}
