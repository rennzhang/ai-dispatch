package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	defaultOutputLimit = 8 << 20
	maximumOutputLimit = 64 << 20
	outputDrainGrace   = 250 * time.Millisecond
)

var errActivityTimeout = errors.New("activity timeout")

type CommandSpec struct {
	Args  []string
	CWD   string
	Env   []string
	Stdin []byte
}

type RunOptions struct {
	FixedTimeout    time.Duration
	ActivityTimeout time.Duration
	// OutputLimit bounds each of stdout and stderr independently. Zero uses the
	// delivery default; callers cannot disable or raise the hard safety bound.
	OutputLimit int
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
	Canceled        bool
	FixedTimeout    bool
	ActivityTimeout bool
	Error           string
	WaitError       string

	StdoutTruncated    bool
	StderrTruncated    bool
	StdoutDroppedBytes int64
	StderrDroppedBytes int64

	CleanupAttempted bool
	CleanupComplete  bool
	CleanupError     string
}

type processCleanup struct {
	Attempted bool
	Complete  bool
	Error     string
}

type processGroupController interface {
	Cancel() error
	Cleanup() processCleanup
}

type runningProcess struct {
	started       time.Time
	parentCtx     context.Context
	runCtx        context.Context
	commandCtx    context.Context
	commandCancel context.CancelCauseFunc
	cmd           *exec.Cmd
	group         processGroupController
	streams       *processStreams
	stdout        *captureWriter
	stderr        *captureWriter
	activity      <-chan struct{}
	options       RunOptions
}

type processCompletion struct {
	waitErr      error
	parentErr    error
	runErr       error
	commandCause error
}

func configureProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = processGroupAttr()
}

func RunProcess(ctx context.Context, spec CommandSpec, opts RunOptions, hooks StreamHooks) RunResult {
	started := time.Now()
	if len(spec.Args) == 0 {
		return RunResult{ExitCode: 2, Error: "empty command"}
	}

	runCtx := ctx
	var runCancel context.CancelFunc
	if opts.FixedTimeout > 0 {
		runCtx, runCancel = context.WithTimeout(ctx, opts.FixedTimeout)
	} else {
		runCtx, runCancel = context.WithCancel(ctx)
	}
	defer runCancel()

	commandCtx, commandCancel := context.WithCancelCause(runCtx)
	defer commandCancel(nil)

	cmd := exec.CommandContext(commandCtx, spec.Args[0], spec.Args[1:]...)
	cmd.Dir = spec.CWD
	cmd.Env = spec.Env
	configureProcessGroup(cmd)

	limit := opts.OutputLimit
	if limit <= 0 {
		limit = defaultOutputLimit
	} else if limit > maximumOutputLimit {
		limit = maximumOutputLimit
	}
	activity := make(chan struct{}, 16)
	hookMu := &sync.Mutex{}
	stdout := newCaptureWriter(limit, hooks.Stdout, hookMu, activity)
	stderr := newCaptureWriter(limit, hooks.Stderr, hookMu, activity)
	streams, err := newProcessStreams(spec.Stdin, stdout, stderr)
	if err != nil {
		return RunResult{ExitCode: 127, Error: err.Error()}
	}
	defer streams.Close()
	cmd.Stdin = streams.stdinReader
	cmd.Stdout = streams.stdoutWriter
	cmd.Stderr = streams.stderrWriter

	group := newProcessGroupController(cmd)
	cmd.Cancel = group.Cancel

	if err := cmd.Start(); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return canceledResult(started, nil, nil, err)
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			return timeoutResult(started, nil, nil, true, false, err)
		}
		return RunResult{ExitCode: 127, DurationMS: time.Since(started).Milliseconds(), Error: err.Error()}
	}
	streams.AfterStart(spec.Stdin)
	return (&runningProcess{
		started:       started,
		parentCtx:     ctx,
		runCtx:        runCtx,
		commandCtx:    commandCtx,
		commandCancel: commandCancel,
		cmd:           cmd,
		group:         group,
		streams:       streams,
		stdout:        stdout,
		stderr:        stderr,
		activity:      activity,
		options:       opts,
	}).wait()
}

func (p *runningProcess) wait() RunResult {
	done := make(chan processCompletion, 1)
	go func() {
		waitErr := p.cmd.Wait()
		// Snapshot every classification input immediately when root Wait returns.
		// Cleanup and output drain may legitimately cross a later deadline.
		done <- processCompletion{
			waitErr:      waitErr,
			parentErr:    p.parentCtx.Err(),
			runErr:       p.runCtx.Err(),
			commandCause: context.Cause(p.commandCtx),
		}
	}()

	var timer *time.Timer
	var timerC <-chan time.Time
	if p.options.ActivityTimeout > 0 {
		timer = time.NewTimer(p.options.ActivityTimeout)
		timerC = timer.C
		defer timer.Stop()
	}

	var completion processCompletion
	for {
		select {
		case completion = <-done:
			goto completed
		case <-timerC:
			timerC = nil
			p.commandCancel(errActivityTimeout)
		case <-p.activity:
			if timer != nil && timerC != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(p.options.ActivityTimeout)
			}
		}
	}

completed:
	cleanup := p.group.Cleanup()
	drainComplete, drainErr := p.streams.FinishOutput(outputDrainGrace)
	if !drainComplete {
		cleanup.Complete = false
		cleanup.Error = joinErrors(cleanup.Error, drainErr)
	}

	var result RunResult
	switch {
	case errors.Is(completion.commandCause, errActivityTimeout):
		result = timeoutResult(p.started, p.stdout.Bytes(), p.stderr.Bytes(), false, true, completion.waitErr)
	case errors.Is(completion.parentErr, context.Canceled):
		result = canceledResult(p.started, p.stdout.Bytes(), p.stderr.Bytes(), completion.waitErr)
	case errors.Is(completion.runErr, context.DeadlineExceeded):
		result = timeoutResult(p.started, p.stdout.Bytes(), p.stderr.Bytes(), true, false, completion.waitErr)
	default:
		result = RunResult{
			Stdout:     p.stdout.Bytes(),
			Stderr:     p.stderr.Bytes(),
			ExitCode:   exitCode(completion.waitErr),
			DurationMS: time.Since(p.started).Milliseconds(),
		}
		if completion.waitErr != nil {
			result.Error = completion.waitErr.Error()
			result.WaitError = completion.waitErr.Error()
		}
	}
	applyCaptureState(&result, p.stdout, p.stderr)
	applyCleanupState(&result, cleanup)
	return result
}

func canceledResult(started time.Time, stdout []byte, stderr []byte, waitErr error) RunResult {
	result := RunResult{
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   130,
		DurationMS: time.Since(started).Milliseconds(),
		Canceled:   true,
		Error:      context.Canceled.Error(),
	}
	if waitErr != nil {
		result.WaitError = waitErr.Error()
	}
	return result
}

func timeoutResult(started time.Time, stdout []byte, stderr []byte, fixed bool, activity bool, waitErr error) RunResult {
	result := RunResult{
		Stdout:          stdout,
		Stderr:          stderr,
		ExitCode:        124,
		DurationMS:      time.Since(started).Milliseconds(),
		TimedOut:        true,
		FixedTimeout:    fixed,
		ActivityTimeout: activity,
	}
	if waitErr != nil {
		result.WaitError = waitErr.Error()
	}
	return result
}

func applyCaptureState(result *RunResult, stdout, stderr *captureWriter) {
	result.StdoutTruncated, result.StdoutDroppedBytes = stdout.Truncation()
	result.StderrTruncated, result.StderrDroppedBytes = stderr.Truncation()
}

func applyCleanupState(result *RunResult, cleanup processCleanup) {
	result.CleanupAttempted = cleanup.Attempted
	result.CleanupComplete = cleanup.Complete
	result.CleanupError = cleanup.Error
}

func joinErrors(left, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "":
		return left
	default:
		return left + "; " + right
	}
}

type captureWriter struct {
	mu       sync.Mutex
	buf      bytes.Buffer
	limit    int
	dropped  int64
	hook     func([]byte)
	hookMu   *sync.Mutex
	activity chan<- struct{}
}

func newCaptureWriter(limit int, hook func([]byte), hookMu *sync.Mutex, activity chan<- struct{}) *captureWriter {
	return &captureWriter{limit: limit, hook: hook, hookMu: hookMu, activity: activity}
}

func (w *captureWriter) Write(p []byte) (int, error) {
	var hookChunk []byte
	w.mu.Lock()
	remaining := w.limit - w.buf.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		hookChunk = append([]byte(nil), p[:remaining]...)
		_, _ = w.buf.Write(hookChunk)
	}
	if remaining < len(p) {
		w.dropped += int64(len(p) - remaining)
	}
	w.mu.Unlock()

	select {
	case w.activity <- struct{}{}:
	default:
	}
	if w.hook != nil && len(hookChunk) > 0 {
		w.hookMu.Lock()
		w.hook(hookChunk)
		w.hookMu.Unlock()
	}
	return len(p), nil
}

func (w *captureWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func (w *captureWriter) Truncation() (bool, int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.dropped > 0, w.dropped
}

type processStreams struct {
	stdinReader  *os.File
	stdinWriter  *os.File
	stdoutReader *os.File
	stdoutWriter *os.File
	stderrReader *os.File
	stderrWriter *os.File
	stdoutDone   chan error
	stderrDone   chan error
	closeOnce    sync.Once
}

func newProcessStreams(stdin []byte, stdout, stderr io.Writer) (*processStreams, error) {
	streams := &processStreams{
		stdoutDone: make(chan error, 1),
		stderrDone: make(chan error, 1),
	}
	var err error
	if len(stdin) > 0 {
		streams.stdinReader, streams.stdinWriter, err = os.Pipe()
		if err != nil {
			streams.Close()
			return nil, fmt.Errorf("create stdin pipe: %w", err)
		}
	}
	streams.stdoutReader, streams.stdoutWriter, err = os.Pipe()
	if err != nil {
		streams.Close()
		return nil, fmt.Errorf("create stdout pipe: %w", err)
	}
	streams.stderrReader, streams.stderrWriter, err = os.Pipe()
	if err != nil {
		streams.Close()
		return nil, fmt.Errorf("create stderr pipe: %w", err)
	}
	go copyOutput(streams.stdoutReader, stdout, streams.stdoutDone)
	go copyOutput(streams.stderrReader, stderr, streams.stderrDone)
	return streams, nil
}

func copyOutput(reader *os.File, writer io.Writer, done chan<- error) {
	_, err := io.Copy(writer, reader)
	done <- err
}

func (s *processStreams) AfterStart(stdin []byte) {
	closeFile(&s.stdoutWriter)
	closeFile(&s.stderrWriter)
	closeFile(&s.stdinReader)
	if s.stdinWriter != nil {
		writer := s.stdinWriter
		go func() {
			_, _ = writer.Write(stdin)
			_ = writer.Close()
		}()
	}
}

func (s *processStreams) FinishOutput(grace time.Duration) (bool, string) {
	// If the root exited without consuming stdin, release our writer even when
	// an adapter-owned detached process inherited the read end.
	closeFile(&s.stdinWriter)
	timer := time.NewTimer(grace)
	defer timer.Stop()
	stdoutDone := s.stdoutDone
	stderrDone := s.stderrDone
	for stdoutDone != nil || stderrDone != nil {
		select {
		case err := <-stdoutDone:
			if err != nil && !errors.Is(err, os.ErrClosed) {
				return false, "stdout capture failed: " + err.Error()
			}
			stdoutDone = nil
		case err := <-stderrDone:
			if err != nil && !errors.Is(err, os.ErrClosed) {
				return false, "stderr capture failed: " + err.Error()
			}
			stderrDone = nil
		case <-timer.C:
			closeFile(&s.stdoutReader)
			closeFile(&s.stderrReader)
			return false, "output streams remained open after process-group cleanup"
		}
	}
	return true, ""
}

func (s *processStreams) Close() {
	s.closeOnce.Do(func() {
		closeFile(&s.stdinReader)
		closeFile(&s.stdinWriter)
		closeFile(&s.stdoutReader)
		closeFile(&s.stdoutWriter)
		closeFile(&s.stderrReader)
		closeFile(&s.stderrWriter)
	})
}

func closeFile(file **os.File) {
	if *file == nil {
		return
	}
	_ = (*file).Close()
	*file = nil
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok {
			if status.Signaled() {
				return 128 + int(status.Signal())
			}
			if code := status.ExitStatus(); code >= 0 {
				return code
			}
		}
	}
	return 1
}
