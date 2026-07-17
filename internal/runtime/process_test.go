package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRunProcessSuccess(t *testing.T) {
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "printf hello"},
	}, RunOptions{}, StreamHooks{})
	if result.ExitCode != 0 || string(result.Stdout) != "hello" {
		t.Fatalf("unexpected result: %+v stdout=%q", result, result.Stdout)
	}
}

func TestRunProcessStderrExit(t *testing.T) {
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "printf nope >&2; exit 7"},
	}, RunOptions{}, StreamHooks{})
	if result.ExitCode != 7 || string(result.Stderr) != "nope" {
		t.Fatalf("unexpected result: %+v stderr=%q", result, result.Stderr)
	}
}

func TestRunProcessCleansProcessGroupAfterRootExits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("process-group fixture is unix-oriented")
	}
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	script := fmt.Sprintf("sleep 300 >/dev/null 2>&1 & echo $! > %q; exit 7", pidFile)
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", script},
		Env:  os.Environ(),
	}, RunOptions{}, StreamHooks{})
	if result.ExitCode != 7 {
		t.Fatalf("exit=%d stderr=%s", result.ExitCode, result.Stderr)
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("child process %d survived root exit", pid)
}

func TestRunProcessActivityTimeout(t *testing.T) {
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "sleep 2"},
	}, RunOptions{ActivityTimeout: 100 * time.Millisecond}, StreamHooks{})
	if !result.TimedOut || !result.ActivityTimeout || result.ExitCode != 124 {
		t.Fatalf("unexpected timeout result: %+v", result)
	}
}

func TestRunProcessHookReceivesOutput(t *testing.T) {
	var chunks []string
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "printf abc"},
	}, RunOptions{}, StreamHooks{Stdout: func(data []byte) {
		chunks = append(chunks, string(data))
	}})
	if result.ExitCode != 0 {
		t.Fatalf("unexpected exit: %+v", result)
	}
	if !strings.Contains(strings.Join(chunks, ""), "abc") {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestLargeOutputHelper(t *testing.T) {
	rawSize := os.Getenv("AI_DISPATCH_LARGE_OUTPUT_SIZE")
	if rawSize == "" {
		return
	}
	size, err := strconv.Atoi(rawSize)
	if err != nil || size < 0 {
		os.Exit(2)
	}
	chunk := strings.Repeat("x", 32<<10)
	for written := 0; written < size; {
		remaining := size - written
		if remaining < len(chunk) {
			chunk = chunk[:remaining]
		}
		n, writeErr := os.Stdout.WriteString(chunk)
		if writeErr != nil {
			os.Exit(3)
		}
		written += n
	}
	os.Exit(0)
}

func TestRunProcessBoundsStreamHookAtCaptureLimit(t *testing.T) {
	const (
		total = 2 << 20
		limit = 4 << 10
	)
	var hooked strings.Builder
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{os.Args[0], "-test.run=^TestLargeOutputHelper$"},
		Env: append(os.Environ(),
			"AI_DISPATCH_LARGE_OUTPUT_SIZE="+strconv.Itoa(total),
		),
	}, RunOptions{OutputLimit: limit}, StreamHooks{Stdout: func(data []byte) {
		_, _ = hooked.Write(data)
	}})
	if result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
	if hooked.Len() != limit || !strings.EqualFold(hooked.String(), strings.Repeat("x", limit)) {
		t.Fatalf("hook retained %d bytes, want %d", hooked.Len(), limit)
	}
	if len(result.Stdout) != limit || !result.StdoutTruncated || result.StdoutDroppedBytes != total-limit {
		t.Fatalf("capture state=%+v stdout_len=%d", result, len(result.Stdout))
	}
}

func TestRunProcessFixedTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell timeout fixture is unix-oriented")
	}
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "sleep 2"},
	}, RunOptions{FixedTimeout: 100 * time.Millisecond}, StreamHooks{})
	if !result.TimedOut || !result.FixedTimeout || result.ExitCode != 124 {
		t.Fatalf("unexpected fixed timeout result: %+v", result)
	}
}

func TestRunProcessCancellationAllowsGracefulCleanup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal fixture is unix-oriented")
	}
	marker := filepath.Join(t.TempDir(), "terminated")
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan struct{}, 1)
	resultCh := make(chan RunResult, 1)
	go func() {
		resultCh <- RunProcess(ctx, CommandSpec{
			Args: []string{"sh", "-c", `trap 'printf term > "$1"; exit 0' TERM; printf READY; while :; do :; done`, "sh", marker},
		}, RunOptions{}, StreamHooks{Stdout: func(data []byte) {
			if strings.Contains(string(data), "READY") {
				select {
				case ready <- struct{}{}:
				default:
				}
			}
		}})
	}()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal("process did not start")
	}
	cancel()
	result := <-resultCh
	if !result.Canceled || result.TimedOut || result.ExitCode != 130 {
		t.Fatalf("result=%+v", result)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "term" {
		t.Fatalf("marker=%q err=%v", data, err)
	}
}

func TestRunProcessPreCanceledContextDoesNotStart(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "started")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := RunProcess(ctx, CommandSpec{
		Args: []string{"sh", "-c", `printf started > "$1"`, "sh", marker},
	}, RunOptions{}, StreamHooks{})
	if !result.Canceled || result.TimedOut || result.ExitCode != 130 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("pre-canceled command started; marker err=%v", err)
	}
	if result.CleanupAttempted {
		t.Fatalf("cleanup should not claim a process was started: %+v", result)
	}
}

func TestRunProcessNormalizesSignalExitAndPreservesWaitError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal fixture is unix-oriented")
	}
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", "kill -TERM $$"},
	}, RunOptions{}, StreamHooks{})
	if result.ExitCode != 128+int(syscall.SIGTERM) {
		t.Fatalf("signal exit=%d, result=%+v", result.ExitCode, result)
	}
	if result.WaitError == "" || result.Error == "" {
		t.Fatalf("signal wait error was discarded: %+v", result)
	}
}

func TestRunProcessBoundsCapturedOutput(t *testing.T) {
	stdout := strings.Repeat("a", 100)
	stderr := strings.Repeat("b", 90)
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{"sh", "-c", `printf '%s' "$1"; printf '%s' "$2" >&2`, "sh", stdout, stderr},
	}, RunOptions{OutputLimit: 32}, StreamHooks{})
	if result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
	if string(result.Stdout) != strings.Repeat("a", 32) || string(result.Stderr) != strings.Repeat("b", 32) {
		t.Fatalf("stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
	if !result.StdoutTruncated || result.StdoutDroppedBytes != 68 {
		t.Fatalf("stdout truncation=%+v", result)
	}
	if !result.StderrTruncated || result.StderrDroppedBytes != 58 {
		t.Fatalf("stderr truncation=%+v", result)
	}
}

func TestRunProcessCancellationStress(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal fixture is unix-oriented")
	}
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ready := make(chan struct{}, 1)
		resultCh := make(chan RunResult, 1)
		go func() {
			resultCh <- RunProcess(ctx, CommandSpec{
				Args: []string{"sh", "-c", `trap 'exit 0' TERM; printf READY; while :; do :; done`},
			}, RunOptions{}, StreamHooks{Stdout: func(data []byte) {
				if strings.Contains(string(data), "READY") {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
			}})
		}()
		select {
		case <-ready:
		case <-time.After(3 * time.Second):
			cancel()
			t.Fatalf("iteration %d did not start", i)
		}
		cancel()
		select {
		case result := <-resultCh:
			if !result.Canceled || result.ExitCode != 130 || !result.CleanupComplete {
				t.Fatalf("iteration %d result=%+v", i, result)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("iteration %d did not stop", i)
		}
	}
}
