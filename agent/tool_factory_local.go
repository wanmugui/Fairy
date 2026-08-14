package main

import (
	"agentloop/agent/internal/biz/tool/builtin"
	localtool "agentloop/agent/internal/biz/tool/local"
	"fmt"
	"path/filepath"
	"strings"
)

// builtinLocalToolBuilders returns the local and builtin tools that run within
// this Agent process. Service-backed tools are constructed by the HTTP backend.
func builtinLocalToolBuilders() map[string]LocalToolBuilder {
	return map[string]LocalToolBuilder{
		"read_file": func(schema ToolDef, cfg *Config) (Tool, error) {
			return builtin.NewLocalReadFileToolWithConfig(schema, builtin.ReadFileToolConfig{
				SegmentReadMaxTokens: cfg.Tools.ReadFile.SegmentReadMaxTokens,
				SegmentReadMinTokens: cfg.Tools.ReadFile.SegmentReadMinTokens,
				MaxReadFileSizeBytes: cfg.Tools.ReadFile.MaxReadFileSizeBytes,
				SkillsRoot:           configuredSkillsRoot(cfg),
			}), nil
		},
		"write_file": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalWriteFileTool(schema), nil
		},
		"edit_file": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalEditFileTool(schema), nil
		},
		"glob": func(schema ToolDef, cfg *Config) (Tool, error) {
			return builtin.NewLocalGlobToolWithConfig(schema, configuredSkillsRoot(cfg)), nil
		},
		"get_current_time": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalTimeTool(schema), nil
		},
		"todolist_create": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalTodoCreateTool(schema), nil
		},
		"todolist_append": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalTodoAppendTool(schema), nil
		},
		"todolist_update": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalTodoUpdateTool(schema), nil
		},
		"list_todos": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalListTodosTool(schema), nil
		},
		"ask_user": func(schema ToolDef, _ *Config) (Tool, error) {
			return builtin.NewLocalAskUserTool(schema), nil
		},
		"create_subtask": func(schema ToolDef, cfg *Config) (Tool, error) {
			return localtool.NewLocalCreateSubtaskTool(schema, localToolConfig(cfg)), nil
		},
		"execute_code": func(schema ToolDef, cfg *Config) (Tool, error) {
			return localtool.NewLocalExecuteCodeTool(schema, localToolConfig(cfg)), nil
		},
		"bash": func(schema ToolDef, cfg *Config) (Tool, error) {
			return localtool.NewLocalBashTool(schema, localToolConfig(cfg)), nil
		},
		"html_to_png": func(schema ToolDef, cfg *Config) (Tool, error) {
			return localtool.NewLocalHTMLToPNGTool(schema, localToolConfig(cfg)), nil
		},
	}
}

func configuredSkillsRoot(cfg *Config) string {
	if cfg == nil || strings.TrimSpace(cfg.SkillsDir) == "" {
		return ""
	}
	return cfg.ResolvePath(cfg.SkillsDir)
}

func localToolConfig(cfg *Config) *localtool.Config {
	if cfg == nil {
		return &localtool.Config{}
	}
	return &localtool.Config{
		RepoRoot:   cfg.RepoRoot,
		ConfigPath: cfg.ConfigPath,
		SkillsRoot: configuredSkillsRoot(cfg),
		UseMock:    cfg.UseMock,
		PptTools: localtool.PptToolsConfig{
			BaseURL: cfg.Tools.PptTools.BaseUrl,
			APIPath: cfg.Tools.PptTools.ApiPath,
			HostPin: cfg.Tools.PptTools.HostPin,
		},
		ToolRuntime: &localtool.ToolRuntimeConfig{Executables: localtool.LocalExecutableConfig{
			Python:  cfg.ToolRuntime.Executables.Python,
			Shell:   cfg.ToolRuntime.Executables.Shell,
			Browser: cfg.ToolRuntime.Executables.Browser,
		}},
		BuildSubtaskPrompt: func(task string) (string, error) {
			return renderLocalSubtaskPrompt(cfg, task)
		},
	}
}

func renderLocalSubtaskPrompt(cfg *Config, task string) (string, error) {
	template := readTextFile(cfg.ResolvePath(filepath.Join("config", "locales", "subtask", "zh.md")))
	if template == "" {
		return task + "\n\n请根据上面的被委派任务执行工作，完成后在 <subtask_result> 中输出结果。", nil
	}
	vars := systemTemplateVars(cfg, buildSkillRegistryJSON(cfg.Skills))
	vars["Task"] = task
	rendered, err := renderJinja(template, vars)
	if err != nil {
		return "", fmt.Errorf("render subtask prompt: %w", err)
	}
	rendered = strings.TrimSpace(rendered)
	if rendered != "" {
		rendered += "\n\n"
	}
	return rendered + "请根据上面的被委派任务执行工作，完成后在 <subtask_result> 中输出结果。", nil
}
