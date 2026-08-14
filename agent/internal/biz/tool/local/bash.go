package local

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type localBashTool struct {
	schema   ToolDef
	cfg      *Config
	resolver localExecutableResolver
	runner   localProcessRunner
}

// localBashPythonEnvironment makes a bare `python` command inside a skill use
// the same direct Python executable selected for execute_code. This keeps
// existing skill commands portable without rewriting each command line.
// Launcher-style executables such as Windows `py -3` have no stable directory
// to prepend, so they deliberately retain the shell's normal PATH behavior.
func localBashPythonEnvironment(resolver localExecutableResolver, cfg *Config) []string {
	env := localPptToolEnvironment(cfg)
	python, err := resolver.resolvePython(toolRuntimeExecutables(cfg).Python)
	if err != nil || len(python.PrefixArgs) > 0 {
		return env
	}
	dir := filepath.Dir(python.Path)
	if dir == "." || strings.TrimSpace(dir) == "" {
		return env
	}
	return append(env, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func NewLocalBashTool(schema ToolDef, cfg *Config) Tool {
	return newLocalBashTool(schema, cfg, newLocalExecutableResolver(), osLocalProcessRunner{})
}

func newLocalBashTool(schema ToolDef, cfg *Config, resolver localExecutableResolver, runner localProcessRunner) Tool {
	if runner == nil {
		runner = osLocalProcessRunner{}
	}
	return &localBashTool{schema: schema, cfg: cfg, resolver: resolver, runner: runner}
}

func (t *localBashTool) Name() string {
	return "bash"
}

func (t *localBashTool) Schema() ToolDef {
	return t.schema
}

func (t *localBashTool) Execute(ctx context.Context, invocation ToolInvocation) (ToolResult, error) {
	if err := ctx.Err(); err != nil {
		return ToolResult{}, err
	}
	args, err := decodeLocalToolArgs(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	command := localStringArg(args, "command")
	if strings.TrimSpace(command) == "" {
		return localErrorResult(t.Name(), fmt.Errorf("command is required")), nil
	}
	localContext, err := localToolContext(invocation)
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	workingDir := localContext.Workspace
	if requested := localStringArg(args, "working_dir", "workdir"); strings.TrimSpace(requested) != "" {
		workingDir, err = resolveLocalWorkspacePath(localContext.Workspace, requested)
		if err != nil {
			return localErrorResult(t.Name(), err), nil
		}
		info, statErr := os.Stat(workingDir)
		if statErr != nil {
			return localErrorResult(t.Name(), fmt.Errorf("working directory is unavailable: %w", statErr)), nil
		}
		if !info.IsDir() {
			return localErrorResult(t.Name(), fmt.Errorf("working directory is not a directory: %s", requested)), nil
		}
	}

	shell, err := t.resolver.resolveShell(toolRuntimeExecutables(t.cfg).Shell)
	if err != nil {
		return localUnavailableResult(t.Name(), err), nil
	}
	command = replaceLocalPathReferences(command, localContext.Workspace, t.cfg.SkillsRoot)
	timeout := localToolTimeout(invocation, localIntArg(args, "timeout", 120), 5*time.Second, 10*time.Minute)
	processArgs := append([]string{}, shell.PrefixArgs...)
	processArgs = append(processArgs, command)
	processResult, err := t.runner.Run(ctx, localProcessRequest{
		Path:    shell.Path,
		Args:    processArgs,
		Dir:     workingDir,
		Env:     localBashPythonEnvironment(t.resolver, t.cfg),
		Timeout: timeout,
	})
	if err != nil {
		return localErrorResult(t.Name(), err), nil
	}
	stdout := strings.TrimSpace(processResult.Stdout)
	stderr := strings.TrimSpace(processResult.Stderr)
	ok := processResult.ExitCode == 0 && !processResult.TimedOut
	value := map[string]any{
		"tool":      t.Name(),
		"ok":        ok,
		"exit_code": processResult.ExitCode,
		"stdout":    stdout,
		"stderr":    stderr,
		"result":    stdout,
	}
	if processResult.TimedOut {
		value["error"] = fmt.Sprintf("timeout after %s (use short-polling and print progress so partial output survives)", timeout)
		value["timed_out"] = true
	} else if !ok {
		// The agent-loop event stream uses this field as its user-visible error
		// summary. Without it, a normal shell exit such as `ls missing-dir`
		// was reduced to the unhelpful "tool returned an error", even though
		// stdout/stderr and the actual exit status were available in the result.
		value["error"] = fmt.Sprintf("command exited with code %d", processResult.ExitCode)
	}
	return ToolResult{Value: value, IsError: !ok}, nil
}
