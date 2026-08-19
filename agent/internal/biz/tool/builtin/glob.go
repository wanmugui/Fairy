package builtin

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

func NewLocalGlobTool(schema ToolDef) Tool {
	return NewLocalGlobToolWithConfig(schema, "")
}

func NewLocalGlobToolWithConfig(schema ToolDef, skillsRoot string) Tool {
	return NewLocalGlobToolWithConfigAndMemory(schema, skillsRoot, "")
}

func NewLocalGlobToolWithConfigAndMemory(schema ToolDef, skillsRoot, memoryRoot string) Tool {
	return newLocalStructuredTool("glob", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		args, err := decodeLocalToolArgs(invocation)
		if err != nil {
			return localErrorResult("glob", err), nil
		}
		pattern := localStringArg(args, "pattern")
		if pattern == "" {
			return localErrorResult("glob", fmt.Errorf("pattern is required")), nil
		}
		localContext, err := localToolContext(invocation)
		if err != nil {
			return localErrorResult("glob", err), nil
		}
		searchRoot := localStringArg(args, "path")
		if searchRoot == "" {
			searchRoot = "."
		}
		root, rootKind, err := resolveLocalReadablePathWithMemory(localContext.Workspace, skillsRoot, memoryRoot, searchRoot)
		if err != nil {
			return localErrorResult("glob", err), nil
		}
		matches, err := localGlobMatches(root, pattern)
		if err != nil {
			return localErrorResult("glob", err), nil
		}
		for index, match := range matches {
			matches[index] = localReadableResultPathWithMemory(rootKind, localContext.Workspace, skillsRoot, memoryRoot, match)
		}
		return ToolResult{Value: map[string]any{
			"ok":      true,
			"matches": matches,
			"count":   len(matches),
		}}, nil
	})
}

func localGlobMatches(root, pattern string) ([]string, error) {
	pattern = strings.TrimSpace(pattern)
	for _, prefix := range []string{"local://", "memory://", "knowledge://"} {
		if strings.HasPrefix(strings.ToLower(pattern), prefix) {
			pattern = pattern[len(prefix):]
			break
		}
	}
	pattern = filepath.ToSlash(pattern)
	if err := validateRelativeGlobPattern(pattern); err != nil {
		return nil, err
	}
	matches := make([]string, 0)
	if strings.Contains(pattern, "**") {
		wildcardIndex := strings.Index(pattern, "**")
		prefix := strings.Trim(pattern[:wildcardIndex], "/")
		suffix := strings.Trim(pattern[wildcardIndex+2:], "/")
		walkRoot := root
		if prefix != "" {
			var err error
			walkRoot, err = resolveLocalWorkspacePath(root, prefix)
			if err != nil {
				return nil, err
			}
		}
		if err := filepath.WalkDir(walkRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			relative, err := filepath.Rel(walkRoot, path)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			matched := suffix == "" || matchGlobPattern(suffix, relative)
			if matched {
				matches = append(matches, path)
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walk glob root: %w", err)
		}
	} else {
		globPattern := filepath.Join(root, filepath.FromSlash(pattern))
		globbed, err := filepath.Glob(globPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid glob pattern: %w", err)
		}
		for _, path := range globbed {
			if _, err := resolveLocalWorkspacePath(root, path); err != nil {
				return nil, err
			}
			matches = append(matches, path)
		}
	}
	sort.Strings(matches)
	return uniqueStrings(matches), nil
}

func validateRelativeGlobPattern(pattern string) error {
	if filepath.IsAbs(filepath.FromSlash(pattern)) {
		return fmt.Errorf("glob pattern must be relative to the requested path")
	}
	for _, part := range strings.Split(pattern, "/") {
		if part == ".." {
			return fmt.Errorf("glob pattern must not leave the requested path")
		}
	}
	return nil
}

func matchGlobPattern(pattern, path string) bool {
	if matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.FromSlash(path)); matched {
		return true
	}
	return filepath.Base(path) != path && func() bool {
		matched, _ := filepath.Match(filepath.FromSlash(pattern), filepath.Base(filepath.FromSlash(path)))
		return matched
	}()
}

func uniqueStrings(values []string) []string {
	if len(values) < 2 {
		return values
	}
	result := values[:1]
	for _, value := range values[1:] {
		if value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}
