//go:build darwin || linux

package runtime

import (
	"context"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestProcessGroupHelper(t *testing.T) {
	if os.Getenv("AI_DISPATCH_PROCESS_GROUP_HELPER") != "1" {
		return
	}
	ignoreTerm := os.Getenv("AI_DISPATCH_PROCESS_GROUP_IGNORE_TERM") == "1"
	if ignoreTerm {
		signal.Ignore(syscall.SIGTERM)
	}
	child := exec.Command("sleep", "300")
	if ignoreTerm {
		child = exec.Command("sh", "-c", `trap '' TERM; printf ready > "$1"; while :; do :; done`, "sh", os.Getenv("AI_DISPATCH_PROCESS_GROUP_CHILD_READY_FILE"))
	}
	if os.Getenv("AI_DISPATCH_PROCESS_GROUP_DETACH") == "1" {
		configureProcessGroup(child)
	}
	if os.Getenv("AI_DISPATCH_PROCESS_GROUP_INHERIT_OUTPUT") == "1" {
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
	}
	if err := child.Start(); err != nil {
		os.Exit(2)
	}
	if ignoreTerm {
		deadline := time.Now().Add(2 * time.Second)
		for {
			if _, err := os.Stat(os.Getenv("AI_DISPATCH_PROCESS_GROUP_CHILD_READY_FILE")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				os.Exit(6)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	if err := os.WriteFile(os.Getenv("AI_DISPATCH_PROCESS_GROUP_PID_FILE"), []byte(strconv.Itoa(child.Process.Pid)), 0o600); err != nil {
		os.Exit(3)
	}
	if rootPIDFile := os.Getenv("AI_DISPATCH_PROCESS_GROUP_ROOT_PID_FILE"); rootPIDFile != "" {
		if err := os.WriteFile(rootPIDFile, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
			os.Exit(4)
		}
	}
	_, _ = os.Stdout.WriteString("VALID")
	if os.Getenv("AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT") == "1" {
		if rawExitAt := os.Getenv("AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT_AT"); rawExitAt != "" {
			exitAt, err := strconv.ParseInt(rawExitAt, 10, 64)
			if err != nil {
				os.Exit(5)
			}
			if delay := time.Until(time.Unix(0, exitAt)); delay > 0 {
				time.Sleep(delay)
			}
		} else if delay, _ := strconv.Atoi(os.Getenv("AI_DISPATCH_PROCESS_GROUP_ROOT_DELAY_MS")); delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		os.Exit(0)
	}
	_ = child.Wait()
	os.Exit(0)
}

func TestRunProcessInheritedFDDoesNotDelayRootCompletion(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	started := time.Now()
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
		Env: append(os.Environ(),
			"GORACE=atexit_sleep_ms=0",
			"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
			"AI_DISPATCH_PROCESS_GROUP_DETACH=1",
			"AI_DISPATCH_PROCESS_GROUP_INHERIT_OUTPUT=1",
			"AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT=1",
			"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+pidFile,
		),
	}, RunOptions{FixedTimeout: 2 * time.Second}, StreamHooks{})
	childPID := waitForPIDFile(t, pidFile)
	defer terminateTestProcess(childPID)

	if result.ExitCode != 0 || result.TimedOut || string(result.Stdout) != "VALID" {
		t.Fatalf("result=%+v stdout=%q", result, result.Stdout)
	}
	if time.Since(started) >= 1500*time.Millisecond {
		t.Fatalf("root completion waited on inherited descriptors: %s", time.Since(started))
	}
	if result.CleanupComplete || !strings.Contains(result.CleanupError, "output streams remained open") {
		t.Fatalf("detached resource was presented as proven cleanup: %+v", result)
	}
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("generic runtime killed adapter-owned detached child: %v", err)
	}
}

func TestRunProcessRootCompletionBeforeDeadlineStaysSuccessfulDuringDrain(t *testing.T) {
	const fixedTimeout = time.Second
	pidFile := t.TempDir() + "/child.pid"
	exitAt := time.Now().Add(800 * time.Millisecond)
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
		Env: append(os.Environ(),
			"GORACE=atexit_sleep_ms=0",
			"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
			"AI_DISPATCH_PROCESS_GROUP_DETACH=1",
			"AI_DISPATCH_PROCESS_GROUP_INHERIT_OUTPUT=1",
			"AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT=1",
			"AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT_AT="+strconv.FormatInt(exitAt.UnixNano(), 10),
			"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+pidFile,
		),
	}, RunOptions{FixedTimeout: fixedTimeout}, StreamHooks{})
	childPID := waitForPIDFile(t, pidFile)
	defer terminateTestProcess(childPID)

	if result.ExitCode != 0 || result.TimedOut || result.Canceled || string(result.Stdout) != "VALID" {
		t.Fatalf("root completion was reclassified after cleanup/drain: %+v", result)
	}
	if time.Duration(result.DurationMS)*time.Millisecond <= fixedTimeout {
		t.Fatalf("fixture did not cross deadline during drain: duration=%dms", result.DurationMS)
	}
	if result.CleanupComplete || !strings.Contains(result.CleanupError, "output streams remained open") {
		t.Fatalf("detached inherited descriptor was not reported: %+v", result)
	}
}

func TestRunProcessCleansOwnedGroupAfterQuickRootExit(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
		Env: append(os.Environ(),
			"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
			"AI_DISPATCH_PROCESS_GROUP_INHERIT_OUTPUT=1",
			"AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT=1",
			"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+pidFile,
		),
	}, RunOptions{}, StreamHooks{})
	childPID := waitForPIDFile(t, pidFile)
	defer terminateTestProcess(childPID)
	if result.ExitCode != 0 || string(result.Stdout) != "VALID" {
		t.Fatalf("result=%+v stdout=%q", result, result.Stdout)
	}
	if !result.CleanupAttempted || !result.CleanupComplete || result.CleanupError != "" {
		t.Fatalf("owned group cleanup not proven: %+v", result)
	}
	waitForProcessExit(t, childPID)
}

func TestRunProcessCancellationKillsOwnedProcessGroup(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan RunResult, 1)
	go func() {
		resultCh <- RunProcess(ctx, CommandSpec{
			Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
			Env: append(os.Environ(),
				"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
				"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+pidFile,
			),
		}, RunOptions{}, StreamHooks{})
	}()
	childPID := waitForPIDFile(t, pidFile)
	defer terminateTestProcess(childPID)
	cancel()
	result := <-resultCh
	if !result.Canceled || result.TimedOut || result.ExitCode != 130 {
		t.Fatalf("result=%+v", result)
	}
	if !result.CleanupComplete || result.CleanupError != "" {
		t.Fatalf("cleanup=%+v", result)
	}
	waitForProcessExit(t, childPID)
}

func TestRunProcessEscalatesIgnoredTERMToBoundedKillStress(t *testing.T) {
	for i := 0; i < 6; i++ {
		func() {
			dir := t.TempDir()
			childPIDFile := dir + "/child.pid"
			rootPIDFile := dir + "/root.pid"
			childReadyFile := dir + "/child.ready"
			ctx, cancel := context.WithCancel(context.Background())
			resultCh := make(chan RunResult, 1)
			go func() {
				resultCh <- RunProcess(ctx, CommandSpec{
					Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
					Env: append(os.Environ(),
						"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
						"AI_DISPATCH_PROCESS_GROUP_IGNORE_TERM=1",
						"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+childPIDFile,
						"AI_DISPATCH_PROCESS_GROUP_ROOT_PID_FILE="+rootPIDFile,
						"AI_DISPATCH_PROCESS_GROUP_CHILD_READY_FILE="+childReadyFile,
					),
				}, RunOptions{}, StreamHooks{})
			}()
			childPID := waitForPIDFile(t, childPIDFile)
			rootPID := waitForPIDFile(t, rootPIDFile)
			defer func() {
				if childPID > 0 {
					terminateTestProcess(childPID)
				}
				if rootPID > 0 {
					terminateTestProcess(rootPID)
				}
			}()
			cancelAt := time.Now()
			cancel()
			result := <-resultCh
			elapsed := time.Since(cancelAt)
			if !result.Canceled || result.TimedOut || result.ExitCode != 130 {
				t.Fatalf("iteration %d result=%+v", i, result)
			}
			if !result.CleanupComplete || result.CleanupError != "" {
				t.Fatalf("iteration %d cleanup=%+v", i, result)
			}
			if elapsed < processTermGrace-100*time.Millisecond {
				t.Fatalf("iteration %d exited before SIGKILL escalation: %s", i, elapsed)
			}
			if elapsed > processTermGrace+processKillGrace+time.Second {
				t.Fatalf("iteration %d escalation was not bounded: %s", i, elapsed)
			}
			waitForProcessExit(t, rootPID)
			waitForProcessExit(t, childPID)
			rootPID = 0
			childPID = 0
		}()
	}
}

func TestRunProcessDoesNotKillDetachedProcessGroup(t *testing.T) {
	pidFile := t.TempDir() + "/child.pid"
	result := RunProcess(context.Background(), CommandSpec{
		Args: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"},
		Env: append(os.Environ(),
			"AI_DISPATCH_PROCESS_GROUP_HELPER=1",
			"AI_DISPATCH_PROCESS_GROUP_DETACH=1",
			"AI_DISPATCH_PROCESS_GROUP_ROOT_EXIT=1",
			"AI_DISPATCH_PROCESS_GROUP_PID_FILE="+pidFile,
		),
	}, RunOptions{}, StreamHooks{})
	childPID := waitForPIDFile(t, pidFile)
	defer terminateTestProcess(childPID)
	if result.ExitCode != 0 {
		t.Fatalf("result=%+v", result)
	}
	if err := syscall.Kill(childPID, 0); err != nil {
		t.Fatalf("detached process group was signaled: %v", err)
	}
}

func waitForPIDFile(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
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
	t.Fatal("helper did not write child pid")
	return 0
}

func waitForProcessExit(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d survived owned-group cleanup", pid)
}

func terminateTestProcess(pid int) {
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
