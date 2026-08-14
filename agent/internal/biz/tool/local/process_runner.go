package local

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"
)

type localProcessRequest struct {
	Path               string
	Args               []string
	Dir                string
	Env                []string
	Timeout            time.Duration
	SuccessFile        string
	SuccessFileMinSize int64
	StdoutWriter       io.Writer
	StderrWriter       io.Writer
}

type localProcessResult struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	TimedOut        bool
	CompletedByFile bool
}

type localProcessRunner interface {
	Run(context.Context, localProcessRequest) (localProcessResult, error)
}

type osLocalProcessRunner struct{}

func (osLocalProcessRunner) Run(ctx context.Context, request localProcessRequest) (localProcessResult, error) {
	if err := ctx.Err(); err != nil {
		return localProcessResult{}, err
	}
	if request.Path == "" {
		return localProcessResult{}, fmt.Errorf("process executable is required")
	}

	commandCtx := ctx
	cancel := func() {}
	if request.Timeout > 0 {
		commandCtx, cancel = context.WithTimeout(ctx, request.Timeout)
	}
	defer cancel()

	cmd := exec.CommandContext(commandCtx, request.Path, request.Args...)
	cmd.Dir = request.Dir
	if len(request.Env) > 0 {
		cmd.Env = append(os.Environ(), request.Env...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if request.StdoutWriter != nil {
		cmd.Stdout = io.MultiWriter(&stdout, request.StdoutWriter)
	}
	if request.StderrWriter != nil {
		cmd.Stderr = io.MultiWriter(&stderr, request.StderrWriter)
	}

	if request.SuccessFile == "" {
		err := cmd.Run()
		return finishLocalProcess(request.Path, ctx, commandCtx, err, stdout.String(), stderr.String())
	}
	if err := cmd.Start(); err != nil {
		return localProcessResult{}, fmt.Errorf("start process %q: %w", request.Path, err)
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	lastSize := int64(-1)
	stableSamples := 0
	for {
		select {
		case err := <-done:
			// Screenshot-style tools may write a complete output file and then
			// let their browser process exit non-zero. The caller validates the
			// file itself, so a completed success file is the authoritative signal
			// unless the parent context or timeout already stopped the command.
			if ctx.Err() == nil && commandCtx.Err() == nil && completedLocalSuccessFile(request) {
				return localProcessResult{
					Stdout:          stdout.String(),
					Stderr:          stderr.String(),
					ExitCode:        0,
					CompletedByFile: true,
				}, nil
			}
			return finishLocalProcess(request.Path, ctx, commandCtx, err, stdout.String(), stderr.String())
		case <-ticker.C:
			info, statErr := os.Stat(request.SuccessFile)
			if statErr != nil || !info.Mode().IsRegular() || info.Size() < localSuccessFileMinimumSize(request) {
				lastSize = -1
				stableSamples = 0
				continue
			}
			if info.Size() == lastSize {
				stableSamples++
			} else {
				lastSize = info.Size()
				stableSamples = 0
			}
			if stableSamples < 1 {
				continue
			}
			_ = cmd.Process.Kill()
			<-done
			return localProcessResult{
				Stdout:          stdout.String(),
				Stderr:          stderr.String(),
				ExitCode:        0,
				CompletedByFile: true,
			}, nil
		case <-commandCtx.Done():
			<-done
			if ctxErr := ctx.Err(); ctxErr != nil {
				return localProcessResult{Stdout: stdout.String(), Stderr: stderr.String()}, ctxErr
			}
			return localProcessResult{
				Stdout:   stdout.String(),
				Stderr:   stderr.String(),
				ExitCode: -1,
				TimedOut: true,
			}, nil
		}
	}
}

func completedLocalSuccessFile(request localProcessRequest) bool {
	info, err := os.Stat(request.SuccessFile)
	return err == nil && info.Mode().IsRegular() && info.Size() >= localSuccessFileMinimumSize(request)
}

func localSuccessFileMinimumSize(request localProcessRequest) int64 {
	if request.SuccessFileMinSize > 0 {
		return request.SuccessFileMinSize
	}
	return 1
}

func finishLocalProcess(path string, ctx, commandCtx context.Context, err error, stdout, stderr string) (localProcessResult, error) {
	result := localProcessResult{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: 0,
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return result, ctxErr
	}
	if errors.Is(commandCtx.Err(), context.DeadlineExceeded) {
		result.ExitCode = -1
		result.TimedOut = true
		return result, nil
	}
	if err == nil {
		return result, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	return result, fmt.Errorf("start process %q: %w", path, err)
}
