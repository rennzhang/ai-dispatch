package runtime

import (
	"context"
	"runtime"
	"strings"
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
