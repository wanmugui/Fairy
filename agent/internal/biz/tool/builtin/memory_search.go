package builtin

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

type memoryFile struct {
	Path    string
	Content string
}

type memoryHit struct {
	Path    string `json:"path"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Score   int    `json:"score"`
}

func NewLocalMemorySearchTool(schema ToolDef, memoryRoot string) Tool {
	return newLocalStructuredTool("memory_search", schema, func(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
		if err := ctx.Err(); err != nil {
			return ToolResult{}, err
		}
		args, err := decodeLocalToolArgs(invocation)
		if err != nil {
			return localErrorResult("memory_search", err), nil
		}
		query := strings.TrimSpace(localStringArg(args, "query"))
		limit := localIntArg(args, "limit", 8)
		if limit <= 0 {
			limit = 8
		}
		if strings.TrimSpace(memoryRoot) == "" {
			return localErrorResult("memory_search", fmt.Errorf("memory directory is not configured")), nil
		}

		var files []memoryFile
		_ = filepath.WalkDir(memoryRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".md" && ext != ".txt" && ext != ".jsonl" {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			content := string(data)
			if len(content) > 8000 {
				content = content[:8000]
			}
			relative, relErr := filepath.Rel(memoryRoot, path)
			if relErr != nil {
				return nil
			}
			files = append(files, memoryFile{Path: "memory://" + filepath.ToSlash(relative), Content: content})
			return nil
		})

		hits := make([]memoryHit, 0, len(files))
		for _, file := range files {
			score := memorySearchScore(query, file.Path, file.Content)
			if score <= 0 {
				continue
			}
			hits = append(hits, memoryHit{
				Path:    file.Path,
				Title:   memoryTitle(file.Path, file.Content),
				Snippet: memorySnippet(file.Content),
				Score:   score,
			})
		}
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
		if len(hits) > limit {
			hits = hits[:limit]
		}
		return ToolResult{Value: map[string]any{
			"query":   query,
			"results": hits,
			"count":   len(hits),
		}}, nil
	})
}

func memorySearchScore(query, path, content string) int {
	score := 0
	if query != "" && strings.Contains(content, query) {
		score += 5
	}
	if query != "" && strings.Contains(path, query) {
		score += 3
	}
	lowerContent := strings.ToLower(content)
	lowerPath := strings.ToLower(path)
	for _, term := range strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		term = strings.ToLower(term)
		if term == "" {
			continue
		}
		score += strings.Count(lowerContent, term)
		if strings.Contains(lowerPath, term) {
			score += 2
		}
	}
	return score
}

func memoryTitle(path, content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trimmed, "# "))
		}
	}
	return filepath.Base(path)
}

func memorySnippet(content string) string {
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if len(trimmed) > 200 {
			trimmed = trimmed[:200]
		}
		return trimmed
	}
	if len(content) > 200 {
		return content[:200]
	}
	return content
}
