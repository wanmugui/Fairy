package builtin

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func NewLocalEditFileTool(schema ToolDef) Tool {
	return newLocalStructuredTool("edit_file", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		args, err := decodeLocalToolArgs(invocation)
		if err != nil {
			return localErrorResult("edit_file", err), nil
		}
		filePath := localStringArg(args, "file_path", "path")
		oldString := localStringArg(args, "old_string")
		if filePath == "" {
			return localErrorResult("edit_file", fmt.Errorf("file_path is required")), nil
		}
		if oldString == "" {
			return localErrorResult("edit_file", fmt.Errorf("old_string is required")), nil
		}
		localContext, err := localToolContext(invocation)
		if err != nil {
			return localErrorResult("edit_file", err), nil
		}
		fullPath, err := resolveLocalWorkspacePath(localContext.Workspace, filePath)
		if err != nil {
			return localErrorResult("edit_file", err), nil
		}
		data, err := os.ReadFile(fullPath)
		if err != nil {
			return localErrorResult("edit_file", fmt.Errorf("read error: %w", err)), nil
		}
		content := string(data)
		newString := localStringArg(args, "new_string")
		count := 1
		var updated string
		if localBoolArg(args, "all_occurrences") {
			count = strings.Count(content, oldString)
			updated = strings.ReplaceAll(content, oldString, newString)
		} else {
			index := strings.Index(content, oldString)
			if index < 0 {
				return localErrorResult("edit_file", fmt.Errorf("old_string not found in %s", filePath)), nil
			}
			updated = content[:index] + newString + content[index+len(oldString):]
		}
		if count == 0 {
			return localErrorResult("edit_file", fmt.Errorf("old_string not found in %s", filePath)), nil
		}
		if err := os.WriteFile(fullPath, []byte(updated), 0o644); err != nil {
			return localErrorResult("edit_file", fmt.Errorf("write error: %w", err)), nil
		}
		return ToolResult{Value: map[string]any{
			"ok":           true,
			"path":         localRelativePath(localContext.Workspace, fullPath),
			"replacements": count,
		}}, nil
	})
}
