package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func NewLocalWriteFileTool(schema ToolDef) Tool {
	return newLocalStructuredTool("write_file", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		args, err := decodeLocalToolArgs(invocation)
		if err != nil {
			return localErrorResult("write_file", err), nil
		}
		filePath := localStringArg(args, "file_path", "path")
		if filePath == "" {
			return localErrorResult("write_file", fmt.Errorf("file_path is required")), nil
		}
		localContext, err := localToolContext(invocation)
		if err != nil {
			return localErrorResult("write_file", err), nil
		}
		fullPath, err := resolveLocalWorkspacePath(localContext.Workspace, filePath)
		if err != nil {
			return localErrorResult("write_file", err), nil
		}
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			return localErrorResult("write_file", fmt.Errorf("mkdir error: %w", err)), nil
		}
		content := localStringArg(args, "content")
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return localErrorResult("write_file", fmt.Errorf("write error: %w", err)), nil
		}
		result := map[string]any{"ok": true, "path": localRelativePath(localContext.Workspace, fullPath), "size": len(content)}
		if localBoolArg(args, "_truncated") {
			result["truncated"] = true
			result["written_bytes"] = len(content)
		}
		return ToolResult{Value: result}, nil
	})
}
