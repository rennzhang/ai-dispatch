package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
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
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return RunResult{ExitCode: 1, Error: err.Error()}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return RunResult{ExitCode: 1, Error: err.Error()}
	}
	if err := cmd.Start(); err != nil {
		return RunResult{ExitCode: 127, Error: err.Error()}
	}

	activity := make(chan struct{}, 16)
	var outBuf, errBuf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(2)
	go copyStream(&wg, stdout, &outBuf, hooks.Stdout, activity)
	go copyStream(&wg, stderr, &errBuf, hooks.Stderr, activity)

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
			wg.Wait()
			result.ExitCode = exitCode(err)
			result.Stdout = outBuf.Bytes()
			result.Stderr = errBuf.Bytes()
			result.DurationMS = time.Since(started).Milliseconds()
			if err != nil && result.ExitCode == 0 {
				result.Error = err.Error()
			}
			return result
		case <-runCtx.Done():
			terminateProcessGroup(cmd.Process.Pid)
			wg.Wait()
			result = timeoutResult(started, outBuf.Bytes(), errBuf.Bytes(), true, false)
			return result
		case <-timerC:
			terminateProcessGroup(cmd.Process.Pid)
			wg.Wait()
			result = timeoutResult(started, outBuf.Bytes(), errBuf.Bytes(), false, true)
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

func copyStream(wg *sync.WaitGroup, reader io.Reader, buf *bytes.Buffer, hook func([]byte), activity chan<- struct{}) {
	defer wg.Done()
	tmp := make([]byte, 4096)
	for {
		n, err := reader.Read(tmp)
		if n > 0 {
			chunk := append([]byte(nil), tmp[:n]...)
			buf.Write(chunk)
			if hook != nil {
				hook(chunk)
			}
			select {
			case activity <- struct{}{}:
			default:
			}
		}
		if err != nil {
			return
		}
	}
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
