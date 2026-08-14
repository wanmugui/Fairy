package builtin

import (
	"agentloop/agent/internal/biz/tool/shared"
	"context"
	"path/filepath"
)

// LocalToolContext contains the filesystem context shared by in-process tools.
// A local tool must resolve every user-provided path relative to Workspace.
type LocalToolContext = shared.ToolContext

type localStructuredTool struct {
	name   string
	schema ToolDef
	run    func(context.Context, ToolInvocation) (ToolResult, error)
}

func newLocalStructuredTool(name string, schema ToolDef, run func(context.Context, ToolInvocation) (ToolResult, error)) Tool {
	return &localStructuredTool{name: name, schema: schema, run: run}
}

func (t *localStructuredTool) Name() string {
	return t.name
}

func (t *localStructuredTool) Schema() ToolDef {
	return t.schema
}

func (t *localStructuredTool) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	return t.run(ctx, invocation)
}

func localToolContext(invocation ToolInvocation) (LocalToolContext, error) {
	return shared.ContextForInvocation(invocation)
}

func decodeLocalToolArgs(invocation ToolInvocation) (map[string]any, error) {
	return shared.DecodeArgs(invocation)
}

func resolveLocalWorkspacePath(workspace, requested string) (string, error) {
	return shared.ResolveWorkspacePath(workspace, requested)
}

func resolveLocalReadablePath(workspace, skillsRoot, requested string) (string, shared.ReadablePathRoot, error) {
	return shared.ResolveReadablePath(workspace, skillsRoot, requested)
}

func localRelativePath(workspace, fullPath string) string {
	return shared.RelativePath(workspace, fullPath)
}

func localReadableResultPath(root shared.ReadablePathRoot, workspace, skillsRoot, fullPath string) string {
	if root != shared.ReadablePathSkills {
		return localRelativePath(workspace, fullPath)
	}
	relative, err := filepath.Rel(skillsRoot, fullPath)
	if err != nil || relative == "." {
		return "local:///skills"
	}
	return "local:///skills/" + filepath.ToSlash(relative)
}

func localErrorResult(toolName string, err error) ToolResult {
	return shared.ErrorResult(toolName, err)
}

func localStringArg(args map[string]any, keys ...string) string {
	return shared.StringArg(args, keys...)
}

func localBoolArg(args map[string]any, key string) bool {
	return shared.BoolArg(args, key)
}

func localIntArg(args map[string]any, key string, defaultValue int) int {
	return shared.IntArg(args, key, defaultValue)
}

func writeJSONFileAtomically(path string, value any) error {
	return shared.WriteJSONFileAtomically(path, value)
}
