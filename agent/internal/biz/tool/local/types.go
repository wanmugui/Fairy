package local

import "agentloop/agent/internal/dtypes"

type Tool = dtypes.Tool
type ToolDef = dtypes.ToolDef
type ToolInvocation = dtypes.ToolInvocation
type ToolResult = dtypes.ToolResult

type LocalExecutableConfig struct {
	Python  string
	Shell   string
	Browser string
}

type ToolRuntimeConfig struct {
	Executables LocalExecutableConfig
}

// PptToolsConfig contains the endpoint settings that PPT skill scripts read
// from their process environment.
type PptToolsConfig struct {
	BaseURL string
	APIPath string
	HostPin string
}

// Config contains only the process-local settings required by environment
// tools. Prompt rendering remains owned by the Agent package.
type Config struct {
	RepoRoot           string
	ConfigPath         string
	SkillsRoot         string
	UseMock            bool
	BashPolicy         BashPolicy
	ToolRuntime        *ToolRuntimeConfig
	PptTools           PptToolsConfig
	BuildSubtaskPrompt func(task string) (string, error)
}
