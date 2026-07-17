//go:build !windows

package claude

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPTYDriverSignalsCleanTmuxSession(t *testing.T) {
	for name, sig := range map[string]syscall.Signal{"SIGINT": syscall.SIGINT, "SIGTERM": syscall.SIGTERM, "SIGHUP": syscall.SIGHUP} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			logPath := filepath.Join(root, "tmux.log")
			writeFakeTmux(t, root)
			cmd := exec.Command(os.Args[0], "-test.run=^TestPTYDriverSignalHelper$")
			cmd.Env = append(os.Environ(),
				"AI_DISPATCH_TEST_PTY_SIGNAL_HELPER=1",
				"AI_DISPATCH_TEST_PTY_ROOT="+root,
				"AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS=0",
				"FAKE_TMUX_LOG="+logPath,
				"PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"),
			)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			waitForFileContains(t, logPath, "new-session", 2*time.Second)
			if err := cmd.Process.Signal(sig); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				_ = cmd.Process.Kill()
				t.Fatalf("PTY driver did not exit promptly after %s: %s", name, stderr.String())
			}
			waitForFileContains(t, logPath, "kill-session", 2*time.Second)
		})
	}
}

func TestPTYDriverSignalHelper(t *testing.T) {
	if os.Getenv("AI_DISPATCH_TEST_PTY_SIGNAL_HELPER") != "1" {
		return
	}
	root := os.Getenv("AI_DISPATCH_TEST_PTY_ROOT")
	code := RunPTYDriverCLI([]string{
		"--transport", "tmux",
		"--cwd", root,
		"--startup-wait", "0",
		"--timeout", "0",
		"--input", "hello",
		"--session-id", "11111111-1111-4111-8111-111111111111",
		"--claude-base-dir", filepath.Join(root, ".claude"),
		"--", "fake-claude",
	}, os.Stdout, os.Stderr)
	os.Exit(code)
}

func TestPTYDriverCanceledBlockedCaptureCleansTmuxSession(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	statePath := filepath.Join(root, "tmux.state")
	pidPath := filepath.Join(root, "capture.pid")
	writeFakeTmux(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_TMUX_STATE", statePath)
	t.Setenv("FAKE_TMUX_BLOCK_COMMAND", "capture-pane")
	t.Setenv("FAKE_TMUX_BLOCK_PID_FILE", pidPath)
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "0")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runGoPTYDriverContext(ctx, ptyDriverConfig{
			CWD:                 root,
			Timeout:             time.Minute,
			Input:               "hello",
			SessionID:           "blocked-capture-session",
			ClaudeBaseDir:       filepath.Join(root, ".claude"),
			StartupWait:         time.Minute,
			StartupReadyPattern: "never-ready",
			Command:             []string{"fake-claude"},
		}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	waitForFileContains(t, logPath, "capture-pane", 2*time.Second)
	blockedPID := waitForPIDFile(t, pidPath, 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked capture did not stop after cancellation")
	}
	waitForFileContains(t, logPath, "kill-session", 2*time.Second)
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fake tmux session was left behind: %v", err)
	}
	waitForProcessExit(t, blockedPID, 2*time.Second)
}

func TestPTYDriverCanceledBlockedStartCleansPartiallyCreatedSession(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	statePath := filepath.Join(root, "tmux.state")
	pidPath := filepath.Join(root, "start.pid")
	writeFakeTmux(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_TMUX_STATE", statePath)
	t.Setenv("FAKE_TMUX_BLOCK_AFTER_NEW_SESSION", "1")
	t.Setenv("FAKE_TMUX_BLOCK_PID_FILE", pidPath)
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "0")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runGoPTYDriverContext(ctx, ptyDriverConfig{
			CWD:           root,
			Timeout:       time.Minute,
			Input:         "hello",
			SessionID:     "blocked-start-session",
			ClaudeBaseDir: filepath.Join(root, ".claude"),
			Command:       []string{"fake-claude"},
		}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	waitForFileContains(t, logPath, "new-session", 2*time.Second)
	blockedPID := waitForPIDFile(t, pidPath, 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocked start did not stop after cancellation")
	}
	waitForFileContains(t, logPath, "kill-session", 2*time.Second)
	if _, err := os.Stat(statePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partially created fake tmux session was left behind: %v", err)
	}
	waitForProcessExit(t, blockedPID, 2*time.Second)
}

func TestTmuxCaptureHasAnInternalCommandDeadline(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	pidPath := filepath.Join(root, "capture.pid")
	tmuxPath := writeFakeTmux(t, root)
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_TMUX_BLOCK_COMMAND", "capture-pane")
	t.Setenv("FAKE_TMUX_BLOCK_PID_FILE", pidPath)
	client := newTmuxClient(tmuxPath)
	client.captureTimeout = 80 * time.Millisecond

	started := time.Now()
	_, err := client.capture(context.Background(), "session")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("capture exceeded its command deadline: %s", elapsed)
	}
	waitForProcessExit(t, waitForPIDFile(t, pidPath, time.Second), time.Second)
}

func TestTmuxCleanupBlockedKillIsBoundedAndTransparent(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	statePath := filepath.Join(root, "tmux.state")
	pidPath := filepath.Join(root, "kill.pid")
	tmuxPath := writeFakeTmux(t, root)
	if err := os.WriteFile(statePath, []byte("active\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_TMUX_STATE", statePath)
	t.Setenv("FAKE_TMUX_BLOCK_COMMAND", "kill-session")
	t.Setenv("FAKE_TMUX_BLOCK_PID_FILE", pidPath)
	client := newTmuxClient(tmuxPath)
	client.commandTimeout = 80 * time.Millisecond
	client.cleanupTimeout = 120 * time.Millisecond

	started := time.Now()
	err := client.cleanupSession("session")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cleanup error was hidden: %v", err)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("cleanup exceeded its independent deadline: %s", elapsed)
	}
	waitForFileContains(t, logPath, "kill-session", time.Second)
	waitForProcessExit(t, waitForPIDFile(t, pidPath, time.Second), time.Second)
	if _, statErr := os.Stat(statePath); statErr != nil {
		t.Fatalf("blocked fake kill should leave detectable session state, stat=%v", statErr)
	}
}

func TestPTYDriverPreservesMainErrorWhenCleanupAlsoFails(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	statePath := filepath.Join(root, "tmux.state")
	writeFakeTmux(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("FAKE_TMUX_STATE", statePath)
	t.Setenv("FAKE_TMUX_FAIL_COMMAND", "kill-session")
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "0")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var stdout bytes.Buffer
	go func() {
		done <- runGoPTYDriverContext(ctx, ptyDriverConfig{
			CWD:           root,
			Timeout:       time.Minute,
			Input:         "hello",
			SessionID:     "cleanup-failure-session",
			ClaudeBaseDir: filepath.Join(root, ".claude"),
			Command:       []string{"fake-claude"},
		}, &stdout, &bytes.Buffer{})
	}()
	waitForFileContains(t, statePath, "active", 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("main cancellation was overwritten: %v", err)
		}
		if !strings.Contains(err.Error(), "kill-session") {
			t.Fatalf("cleanup failure was hidden: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("driver did not return after cancellation and cleanup failure")
	}
	if !strings.Contains(stdout.String(), `"event":"warning"`) || !strings.Contains(stdout.String(), "kill-session") {
		t.Fatalf("cleanup warning was not emitted: %s", stdout.String())
	}
}

func waitForPIDFile(t *testing.T, path string, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil {
			pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if parseErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("PID file %s was not populated", path)
	return 0
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d is still alive", pid)
}
