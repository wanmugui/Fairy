package builtin

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
)

// NewLocalEditFileTool edits a workspace file by replacing an exact text
// snippet. Inspired by dsh `tool-str-replace-editor`: it surfaces where the
// snippet matched (or would have matched) so the model can fix typos with a
// concrete line number rather than a bare "not found" message.
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
		oldText := firstNonEmpty(localStringArg(args, "old_text"), localStringArg(args, "old_string"))
		if filePath == "" {
			return localErrorResult("edit_file", fmt.Errorf("file_path is required")), nil
		}
		if oldText == "" {
			return localErrorResult("edit_file", fmt.Errorf("old_text is required")), nil
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
		newText := firstNonEmpty(localStringArg(args, "new_text"), localStringArg(args, "new_string"))
		replaceAll := localBoolArg(args, "replace_all") || localBoolArg(args, "all_occurrences")

		offsets := matchOffsets(content, oldText)
		switch {
		case len(offsets) == 0:
			return localErrorResult("edit_file", fmt.Errorf(
				"old_text not found in %s\n%s",
				filePath,
				nearestMatchHint(content, oldText),
			)), nil
		case len(offsets) > 1 && !replaceAll:
			lines := lineNumbersAt(content, offsets)
			return localErrorResult("edit_file", fmt.Errorf(
				"old_text matched %d locations at lines %v in %s; pass replace_all=true to replace every occurrence, or extend old_text with more surrounding lines to make it unique",
				len(offsets), lines, filePath,
			)), nil
		}

		var updated string
		count := 0
		if replaceAll {
			count = len(offsets)
			updated = strings.ReplaceAll(content, oldText, newText)
		} else {
			count = 1
			index := offsets[0]
			var buf bytes.Buffer
			buf.WriteString(content[:index])
			buf.WriteString(newText)
			buf.WriteString(content[index+len(oldText):])
			updated = buf.String()
		}

		if err := os.WriteFile(fullPath, []byte(updated), 0o644); err != nil {
			return localErrorResult("edit_file", fmt.Errorf("write error: %w", err)), nil
		}
		replacedAt := lineNumbersAt(content, offsets)
		if !replaceAll && len(replacedAt) > 0 {
			replacedAt = replacedAt[:1]
		}
		return ToolResult{Value: map[string]any{
			"ok":           true,
			"path":         localRelativePath(localContext.Workspace, fullPath),
			"replacements": count,
			"replaced_at":  replacedAt,
		}}, nil
	})
}

// matchOffsets returns the byte offsets where needle occurs in content. An
// empty needle yields no matches (avoiding pathological ReplaceAll behavior).
func matchOffsets(content, needle string) []int {
	if needle == "" {
		return nil
	}
	var offsets []int
	offset := 0
	for {
		idx := strings.Index(content[offset:], needle)
		if idx < 0 {
			return offsets
		}
		offsets = append(offsets, offset+idx)
		offset += idx + len(needle)
	}
}

// lineNumbersAt returns the 1-indexed line number for each byte offset.
func lineNumbersAt(content string, offsets []int) []int {
	lines := make([]int, len(offsets))
	line := 1
	cursor := 0
	for i, offset := range offsets {
		for cursor < offset {
			if content[cursor] == '\n' {
				line++
			}
			cursor++
		}
		lines[i] = line
	}
	return lines
}

// nearestMatchHint finds a small unique snippet (line containing the first
// occurrence of the longest token shared with needle) so the model can
// localize a typo without flooding the context. Empty when no token overlaps.
func nearestMatchHint(content, needle string) string {
	tokens := strings.Fields(needle)
	if len(tokens) == 0 {
		return ""
	}
	best := ""
	bestLine := 0
	for _, tok := range tokens {
		if len(tok) < 3 {
			continue
		}
		idx := strings.Index(content, tok)
		if idx < 0 {
			continue
		}
		line, text := lineAndTextAt(content, idx)
		if best == "" || len(tok) > len(best) {
			best = tok
			bestLine = line
			_ = text
		}
	}
	if best == "" {
		return ""
	}
	return fmt.Sprintf("(hint: similar token %q appears at line %d)", best, bestLine)
}

func lineAndTextAt(content string, offset int) (int, string) {
	line := 1
	lineStart := 0
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	lineEnd := strings.IndexByte(content[lineStart:], '\n')
	if lineEnd < 0 {
		lineEnd = len(content)
	} else {
		lineEnd += lineStart
	}
	return line, content[lineStart:lineEnd]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
