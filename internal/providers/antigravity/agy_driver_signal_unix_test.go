//go:build darwin || linux

package antigravity

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

func TestAgySettingsLockWaitHonorsCancellation(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"model":"Gemini 3.5 Flash (Medium)","other":true}`)
	writeLegacyRecoveryFixture(t, root, original, defaultProLabel, 0o600)
	lock, err := os.OpenFile(filepath.Join(root, "settings.json.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	if err := flockContext(context.Background(), lock); err != nil {
		t.Fatal(err)
	}
	defer funlock(lock)
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	err = recoverLegacyModelSwitchContext(ctx, root)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("recovery lock wait ignored cancellation: err=%v", err)
	}
	if _, statErr := os.Stat(legacyModelRecoveryJournalPath(root)); statErr != nil {
		t.Fatalf("journal changed while recovery was canceled: %v", statErr)
	}
}

func TestAgyDriverSIGTERMKillsChildProcessTreeWithoutSettings(t *testing.T) {
	root := t.TempDir()
	startedPath := filepath.Join(root, "started")
	childPIDPath := filepath.Join(root, "child-pid")
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
(
  trap '' TERM
  while :; do sleep 1; done
) &
child=$!
printf '%s\n' "$child" > "$FAKE_AGY_CHILD_PID"
: > "$FAKE_AGY_STARTED"
wait "$child"
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAgyDriverSignalHelper$")
	cmd.Env = append(os.Environ(),
		"AI_DISPATCH_TEST_AGY_SIGNAL_HELPER=1",
		"AI_DISPATCH_TEST_AGY_ROOT="+root,
		"AI_DISPATCH_TEST_AGY_BIN="+fakeAgy,
		"FAKE_AGY_STARTED="+startedPath,
		"FAKE_AGY_CHILD_PID="+childPIDPath,
	)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitForPath(t, startedPath, 2*time.Second)
	waitForPath(t, childPIDPath, 2*time.Second)
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("agy driver did not exit promptly after SIGTERM: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	childPIDData, err := os.ReadFile(childPIDPath)
	if err != nil {
		t.Fatal(err)
	}
	childPID, err := strconv.Atoi(strings.TrimSpace(string(childPIDData)))
	if err != nil {
		t.Fatalf("invalid fake agy child pid %q: %v", childPIDData, err)
	}
	waitForProcessExit(t, childPID, 2*time.Second)
	if _, err := os.Stat(filepath.Join(root, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("signal cancellation unexpectedly created settings.json: %v", err)
	}
}

func TestAgyDriverSignalHelper(t *testing.T) {
	if os.Getenv("AI_DISPATCH_TEST_AGY_SIGNAL_HELPER") != "1" {
		return
	}
	code := RunAgyDriverCLI([]string{
		"--agy-bin", os.Getenv("AI_DISPATCH_TEST_AGY_BIN"),
		"--agy-root", os.Getenv("AI_DISPATCH_TEST_AGY_ROOT"),
		"--model", "pro",
		"--prompt", "hello",
	}, os.Stdout, os.Stderr)
	os.Exit(code)
}

func waitForProcessExit(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		err := syscall.Kill(pid, syscall.Signal(0))
		if errors.Is(err, syscall.ESRCH) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d still exists after cancellation", pid)
}
