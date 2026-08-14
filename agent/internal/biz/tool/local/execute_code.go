package local

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type localExecuteCodeTool struct {
	schema   ToolDef
	cfg      *Config
	resolver localExecutableResolver
	runner   localProcessRunner
}

func NewLocalExecuteCodeTool(schema ToolDef, cfg *Config) Tool {
	return newLocalExecuteCodeTool(schema, cfg, newLocalExecutableResolver(), osLocalProcessRunner{})
}

func newLocalExecuteCodeTool(schema ToolDef, cfg *Config, resolver localExecutableResolver, runner localProcessRunner) Tool {
	if runner == nil {
		runner = osLocalProcessRunner{}
	}
	return &localExecuteCodeTool{schema: schema, cfg: cfg, resolver: resolver, runner: runner}
}

func (t *localExecuteCodeTool) Name() string {
	return "execute_code"
}

func (t *localExecuteCodeTool) Schema() ToolDef {
	return t.schema
}

func (t *localExecuteCodeTool) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	code := localStringArg(args, "code", "command")
	if strings.TrimSpace(code) == "" {
		return localErrorResult(t.Name(), fmt.Errorf("code is required")), nil
	}
	if language := localStringArg(args, "language", "lang"); language != "" && !strings.EqualFold(language, "python") && !strings.EqualFold(language, "py") {
		return localErrorResult(t.Name(), fmt.Errorf("unsupported language: %s", language)), nil
	}
	localContext, err := localToolContext(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}

	python, err := t.resolver.resolvePython(toolRuntimeExecutables(t.cfg).Python)
	if err != nil {
		return localUnavailableResult(t.Name(), err), nil
	}

	code = replaceLocalPathReferences(code, localContext.Workspace, t.cfg.SkillsRoot)
	tmp, err := os.CreateTemp("", "agent-execute-code-*.py")
	if err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("create temporary Python file: %w", err)), nil
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return localErrorResult(t.Name(), fmt.Errorf("protect temporary Python file: %w", err)), nil
	}
	header := "# -*- coding: utf-8 -*-\n" +
		"import sys, io\n" +
		"sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding=\"utf-8\")\n" +
		"sys.stderr = io.TextIOWrapper(sys.stderr.buffer, encoding=\"utf-8\")\n\n"
	if _, err := tmp.WriteString(header + code); err != nil {
		_ = tmp.Close()
		return localErrorResult(t.Name(), fmt.Errorf("write temporary Python file: %w", err)), nil
	}
	if err := tmp.Close(); err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("close temporary Python file: %w", err)), nil
	}

	resultDir := filepath.Join(localContext.Workspace, "result")
	if err := os.MkdirAll(resultDir, 0o755); err != nil {
		return localErrorResult(t.Name(), fmt.Errorf("create result directory: %w", err)), nil
	}
	timeout := localToolTimeout(invocation, localIntArg(args, "timeout", 300), 5*time.Second, 10*time.Minute)
	processArgs := append([]string{}, python.PrefixArgs...)
	processArgs = append(processArgs, "-u", tmpPath)
	processResult, err := t.runner.Run(ctx, localProcessRequest{
		Path:    python.Path,
		Args:    processArgs,
		Dir:     localContext.Workspace,
		Env:     localPptToolEnvironment(t.cfg),
		Timeout: timeout,
	})
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	stdout := strings.TrimSpace(processResult.Stdout)
	stderr := strings.TrimSpace(processResult.Stderr)
	parsedResult := any(stdout)
	if stdout != "" {
		var parsed any
		if json.Unmarshal([]byte(stdout), &parsed) == nil {
			parsedResult = parsed
		}
	}
	ok := processResult.ExitCode == 0 && stderr == "" && !processResult.TimedOut
	value := map[string]any{
		"tool":      t.Name(),
		"ok":        ok,
		"exit_code": processResult.ExitCode,
		"stdout":    stdout,
		"stderr":    stderr,
		"result":    parsedResult,
	}
	if processResult.TimedOut {
		value["error"] = fmt.Sprintf("timeout after %s (use short-polling and print progress so partial output survives)", timeout)
		value["timed_out"] = true
	}
	return ToolResult{Value: value, IsError: !ok}, nil
}

func localToolTimeout(invocation ToolInvocation, requestedSeconds int, minimum, maximum time.Duration) time.Duration {
	if requestedSeconds <= 0 {
		requestedSeconds = int(minimum / time.Second)
	}
	timeout := time.Duration(requestedSeconds) * time.Second
	if timeout < minimum {
		timeout = minimum
	}
	if timeout > maximum {
		timeout = maximum
	}
	if invocation.Timeout > 0 && invocation.Timeout < timeout {
		timeout = invocation.Timeout
	}
	return timeout
}

func replaceMntDataReferences(value, workspace string) string {
	return replaceLocalPathReferences(value, workspace, "")
}

// replaceLocalPathReferences maps the two virtual filesystem roots described
// by the production tool contract before a local shell or Python process sees
// them. It intentionally accepts only those roots: other absolute paths stay
// untouched and are not treated as workspace paths.
func replaceLocalPathReferences(value, workspace, skillsRoot string) string {
	replacements := []struct {
		logicalRoot string
		replacement string
	}{
		{logicalRoot: "/mnt/data", replacement: filepath.ToSlash(workspace)},
	}
	if strings.TrimSpace(skillsRoot) != "" {
		replacements = append(replacements, struct {
			logicalRoot string
			replacement string
		}{logicalRoot: "/skills", replacement: filepath.ToSlash(skillsRoot)})
	}

	var result strings.Builder
	last := 0
	for searchFrom := 0; searchFrom < len(value); {
		index, logicalRoot, replacement := nextLocalPathReference(value, searchFrom, replacements)
		if index < 0 {
			break
		}
		end := index + len(logicalRoot)
		if isLocalPathBoundaryBefore(value, index) && isLocalPathBoundaryAfter(value, end) {
			result.WriteString(value[last:index])
			result.WriteString(replacement)
			last = end
		}
		searchFrom = end
	}
	if last == 0 {
		return value
	}
	result.WriteString(value[last:])
	return result.String()
}

func nextLocalPathReference(value string, searchFrom int, replacements []struct {
	logicalRoot string
	replacement string
}) (int, string, string) {
	bestIndex := -1
	var bestRoot, bestReplacement string
	for _, candidate := range replacements {
		relative := strings.Index(value[searchFrom:], candidate.logicalRoot)
		if relative < 0 {
			continue
		}
		index := searchFrom + relative
		if bestIndex < 0 || index < bestIndex {
			bestIndex, bestRoot, bestReplacement = index, candidate.logicalRoot, candidate.replacement
		}
	}
	return bestIndex, bestRoot, bestReplacement
}

func isLocalPathBoundaryBefore(value string, index int) bool {
	return index == 0 || strings.ContainsRune(" \t\r\n\"'=(:,;|&<>{[", rune(value[index-1]))
}

func isLocalPathBoundaryAfter(value string, index int) bool {
	return index == len(value) || strings.ContainsRune("/ \t\r\n\"'()[]{};,|&<>", rune(value[index]))
}
