package main

import (
	toolruntime "agentloop/agent/internal/biz/tool"
	"agentloop/agent/internal/biz/tool/httptool"
	"fmt"
	"sort"
	"strings"
)

type LocalToolBuilder func(ToolDef, *Config) (Tool, error)

type ToolFactory struct {
	LocalBuilders map[string]LocalToolBuilder
}

func NewToolFactory() *ToolFactory {
	return &ToolFactory{LocalBuilders: builtinLocalToolBuilders()}
}

func (f *ToolFactory) BuildRegistry(cfg *Config) (*ToolRegistry, error) {
	if cfg == nil {
		return nil, fmt.Errorf("build tool registry: config is nil")
	}
	if err := ensureToolRuntimeDefaults(cfg); err != nil {
		return nil, err
	}
	schemas, err := toolruntime.LoadSchemas(cfg.SchemasPath())
	if err != nil {
		return nil, fmt.Errorf("build tool registry: %w", err)
	}

	registry := NewToolRegistry()
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, schemaKey := range names {
		schema := schemas[schemaKey]
		name := strings.TrimSpace(schema.Name)
		if name == "" {
			name = schemaKey
		}
		if !strings.EqualFold(name, schemaKey) {
			return nil, fmt.Errorf("tool schema key %q does not match schema name %q", schemaKey, schema.Name)
		}
		if !isConfiguredToolEnabled(cfg.HTTPTools, name) {
			continue
		}

		override, hasOverride := configuredBackendOverride(cfg.ToolRuntime.Tools, name)
		if !hasOverride {
			return nil, fmt.Errorf("tool %q has no backend configuration", name)
		}
		backend := override.Backend
		definition := schema.ToolDef()

		var tool Tool
		switch backend {
		case BackendLocal:
			builder, ok := localBuilder(f.LocalBuilders, name)
			if !ok {
				return nil, fmt.Errorf("tool %q uses local backend but no LocalToolBuilder is registered", name)
			}
			tool, err = builder(definition, cfg)
			if err != nil {
				return nil, fmt.Errorf("build local tool %q: %w", name, err)
			}
		case BackendHTTP:
			if cfg.Gateway.Endpoint == "" {
				return nil, fmt.Errorf("tool %q uses http backend but unified tool service endpoint is missing", name)
			}
			tool = httptool.NewHTTPTool(name, cfg.Gateway.Endpoint, definition, cfg.Gateway.Timeout, cfg.Gateway.BearerToken, cfg.Gateway.Headers, cfg.ToolRuntime.RetryCount, cfg.Gateway.HostPin)
		default:
			return nil, fmt.Errorf("tool %q uses unsupported backend %q", name, backend)
		}

		if tool == nil {
			return nil, fmt.Errorf("tool %q backend %q returned nil implementation", name, backend)
		}
		if err := registry.Register(backend, tool); err != nil {
			return nil, fmt.Errorf("register tool %q: %w", name, err)
		}
	}
	return registry, nil
}

func isConfiguredToolEnabled(entries map[string]ToolEntry, name string) bool {
	for configuredName, entry := range entries {
		if strings.EqualFold(configuredName, name) {
			return entry.Enabled
		}
	}
	return true
}

func configuredBackendOverride(overrides map[string]ToolBackendOverride, name string) (ToolBackendOverride, bool) {
	for configuredName, override := range overrides {
		if strings.EqualFold(configuredName, name) {
			return override, true
		}
	}
	return ToolBackendOverride{}, false
}

func localBuilder(builders map[string]LocalToolBuilder, name string) (LocalToolBuilder, bool) {
	for configuredName, builder := range builders {
		if strings.EqualFold(configuredName, name) {
			return builder, true
		}
	}
	return nil, false
}
