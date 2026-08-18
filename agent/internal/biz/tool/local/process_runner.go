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

	// Detach, when true, returns immediately after Start() with a populated
	// PID; stdout/stderr are streamed into OutputPath/ErrorPath and the caller's
	// notification channel receives the final exit error (nil on success).
	Detach        bool
	OutputPath    string
	ErrorPath     string
	JobID         string
	OnComplete    chan<- jobCompletion
	CancelProcess func() error
}

type jobCompletion struct {
	JobID    string
	PID      int
	ExitCode int
	Err      error
}

type localProcessResult struct {
	Stdout          string
	Stderr          string
	ExitCode        int
	TimedOut        bool
	CompletedByFile bool

	// Populated only when Detach=true. The caller polls the corresponding
	// job_* tool to read the streaming files or to kill the process.
	Detached bool
	PID      int
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

	if request.Detach {
		return startLocalDetachedProcess(cmd, request)
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

// startLocalDetachedProcess launches the command and returns immediately with
// the PID. stdout/stderr stream into OutputPath/ErrorPath so the bash_job tool
// can tail them; OnComplete fires once when the process exits or is killed.
func startLocalDetachedProcess(cmd *exec.Cmd, request localProcessRequest) (localProcessResult, error) {
	if request.OutputPath == "" || request.ErrorPath == "" {
		return localProcessResult{}, fmt.Errorf("detach requires OutputPath and ErrorPath")
	}
	outFile, err := os.Create(request.OutputPath)
	if err != nil {
		return localProcessResult{}, fmt.Errorf("create stdout file: %w", err)
	}
	errFile, err := os.Create(request.ErrorPath)
	if err != nil {
		outFile.Close()
		return localProcessResult{}, fmt.Errorf("create stderr file: %w", err)
	}
	cmd.Stdout = outFile
	cmd.Stderr = errFile

	if err := cmd.Start(); err != nil {
		outFile.Close()
		errFile.Close()
		return localProcessResult{}, fmt.Errorf("start detached process %q: %w", request.Path, err)
	}
	pid := 0
	if cmd.Process != nil {
		pid = cmd.Process.Pid
	}

	if request.CancelProcess != nil {
		registerDetachedJob(request.JobID, cmd, outFile, errFile, request.CancelProcess)
	} else {
		registerDetachedJob(request.JobID, cmd, outFile, errFile, func() error { return cmd.Process.Kill() })
	}

	completion := request.OnComplete
	if completion != nil {
		go func() {
			waitErr := cmd.Wait()
			outFile.Close()
			errFile.Close()
			exit := 0
			if waitErr != nil {
				var exitErr *exec.ExitError
				if errors.As(waitErr, &exitErr) {
					exit = exitErr.ExitCode()
				} else {
					exit = -1
				}
			}
			unregisterDetachedJob(request.JobID)
			select {
			case completion <- jobCompletion{JobID: request.JobID, PID: pid, ExitCode: exit, Err: waitErr}:
			default:
			}
		}()
	}

	return localProcessResult{Detached: true, PID: pid}, nil
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
