package runtime

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

type CommandSpec struct {
	Args  []string
	CWD   string
	Env   []string
	Stdin []byte
}

type RunOptions struct {
	FixedTimeout    time.Duration
	ActivityTimeout time.Duration
}

type StreamHooks struct {
	Stdout func([]byte)
	Stderr func([]byte)
}

type RunResult struct {
	Stdout          []byte
	Stderr          []byte
	ExitCode        int
	DurationMS      int64
	TimedOut        bool
	FixedTimeout    bool
	ActivityTimeout bool
	Error           string
}

func RunProcess(ctx context.Context, spec CommandSpec, opts RunOptions, hooks StreamHooks) RunResult {
	started := time.Now()
	if len(spec.Args) == 0 {
		return RunResult{ExitCode: 2, Error: "empty command"}
	}

	runCtx := ctx
	var cancel context.CancelFunc
	if opts.FixedTimeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.FixedTimeout)
	} else {
		runCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	cmd := exec.CommandContext(runCtx, spec.Args[0], spec.Args[1:]...)
	cmd.Dir = spec.CWD
	cmd.Env = spec.Env
	cmd.SysProcAttr = processGroupAttr()
	if len(spec.Stdin) > 0 {
		cmd.Stdin = bytes.NewReader(spec.Stdin)
	}

	activity := make(chan struct{}, 16)
	stdout := &captureWriter{hook: hooks.Stdout, activity: activity}
	stderr := &captureWriter{hook: hooks.Stderr, activity: activity}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: 127, Error: err.Error()}
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	var timer *time.Timer
	var timerC <-chan time.Time
	if opts.ActivityTimeout > 0 {
		timer = time.NewTimer(opts.ActivityTimeout)
		timerC = timer.C
		defer timer.Stop()
	}

	result := RunResult{}
	for {
		select {
		case err := <-done:
			result.ExitCode = exitCode(err)
			result.Stdout = stdout.Bytes()
			result.Stderr = stderr.Bytes()
			result.DurationMS = time.Since(started).Milliseconds()
			if err != nil && result.ExitCode == 0 {
				result.Error = err.Error()
			}
			return result
		case <-runCtx.Done():
			terminateProcessGroup(cmd.Process.Pid)
			_ = cmd.Process.Kill()
			<-done
			result = timeoutResult(started, stdout.Bytes(), stderr.Bytes(), true, false)
			return result
		case <-timerC:
			terminateProcessGroup(cmd.Process.Pid)
			_ = cmd.Process.Kill()
			<-done
			result = timeoutResult(started, stdout.Bytes(), stderr.Bytes(), false, true)
			return result
		case <-activity:
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(opts.ActivityTimeout)
			}
		}
	}
}

type captureWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	hook     func([]byte)
	activity chan<- struct{}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	chunk := append([]byte(nil), p...)
	w.mu.Lock()
	_, _ = w.buf.Write(chunk)
	w.mu.Unlock()
	if w.hook != nil {
		w.hook(chunk)
	}
	select {
	case w.activity <- struct{}{}:
	default:
	}
	return len(p), nil
}

func (w *captureWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func timeoutResult(started time.Time, stdout []byte, stderr []byte, fixed bool, activity bool) RunResult {
	return RunResult{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        124,
		DurationMS:      time.Since(started).Milliseconds(),
		TimedOut:        true,
		FixedTimeout:    fixed,
		ActivityTimeout: activity,
	}
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			return status.ExitStatus()
		}
	}
	return 1
}
