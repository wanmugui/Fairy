package local

import (
	"agentloop/agent/internal/biz/tool/shared"
	"strings"
)

type localInvocationContext = shared.ToolContext

func localToolContext(invocation ToolInvocation) (localInvocationContext, error) {
	return shared.ContextForInvocation(invocation)
}

func decodeLocalToolArgs(invocation ToolInvocation) (map[string]any, error) {
	return shared.DecodeArgs(invocation)
}

func resolveLocalWorkspacePath(workspace, requested string) (string, error) {
	return shared.ResolveWorkspacePath(workspace, requested)
}

func localStringArg(args map[string]any, keys ...string) string {
	return shared.StringArg(args, keys...)
}

func localIntArg(args map[string]any, key string, defaultValue int) int {
	return shared.IntArg(args, key, defaultValue)
}

func localErrorResult(toolName string, err error) ToolResult {
	return shared.ErrorResult(toolName, err)
}

// localPptToolEnvironment provides the compatibility variables consumed by
// PPT skill scripts. The old process toolloader used to inject them; Local
// tools execute the scripts directly, so the injection belongs here instead.
func localPptToolEnvironment(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.PptTools.BaseURL), "/")
	if baseURL == "" {
		return nil
	}
	apiPath := strings.TrimSpace(cfg.PptTools.APIPath)
	if apiPath != "" && !strings.HasPrefix(apiPath, "/") {
		apiPath = "/" + apiPath
	}
	env := []string{
		"PPT_TOOL_API_BASE=" + baseURL,
		"BACKEND_TOOL_BASE=" + baseURL,
	}
	if apiPath != "" {
		env = append(env, "CREATIVE_RENDER_API_URL="+baseURL+apiPath)
	}
	if hostPin := strings.TrimSpace(cfg.PptTools.HostPin); hostPin != "" {
		env = append(env,
			"PPT_TOOL_HOST_IP="+hostPin,
			"CREATIVE_RENDER_HOST_IP="+hostPin,
		)
	}
	return env
}
