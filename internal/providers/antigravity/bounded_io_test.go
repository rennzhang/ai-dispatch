package antigravity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestAgyDriverBoundsLargeChildStreamsAndLogWithWarnings(t *testing.T) {
	root := t.TempDir()
	stdoutSource := filepath.Join(root, "large-stdout")
	stderrSource := filepath.Join(root, "large-stderr")
	logSource := filepath.Join(root, "large-log")
	stdoutTail := "STDOUT_FINAL_TAIL"
	stderrTail := "STDERR_FINAL_TAIL"
	if err := os.WriteFile(stdoutSource, []byte("STDOUT_HEAD"+strings.Repeat("o", agyStdoutHeadLimitBytes+agyStdoutTailLimitBytes)+stdoutTail), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stderrSource, []byte("STDERR_HEAD"+strings.Repeat("e", agyStderrHeadLimitBytes+agyStderrTailLimitBytes)+stderrTail), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionID := "11111111-1111-1111-1111-111111111111"
	logText := "Created conversation " + sessionID + "\n" + strings.Repeat("log filler\n", (agyLogHeadLimitBytes+agyLogTailLimitBytes)/10) + `selected model override to backend: label="Gemini 3.1 Pro (High)"` + "\n"
	if err := os.WriteFile(logSource, []byte(logText), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
log_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat "$FAKE_AGY_LOG_SOURCE" > "$log_file"
cat "$FAKE_AGY_STDOUT_SOURCE"
cat "$FAKE_AGY_STDERR_SOURCE" >&2
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AGY_LOG_SOURCE", logSource)
	t.Setenv("FAKE_AGY_STDOUT_SOURCE", stdoutSource)
	t.Setenv("FAKE_AGY_STDERR_SOURCE", stderrSource)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{"--agy-bin", fakeAgy, "--agy-root", root, "--model", "pro", "--prompt", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), stdoutTail) || !strings.Contains(stderr.String(), stderrTail) {
		t.Fatalf("final tails were not retained: stdout_tail=%v stderr_tail=%v", strings.Contains(stdout.String(), stdoutTail), strings.Contains(stderr.String(), stderrTail))
	}
	result := (Provider{}).Parse(runtime.RunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "antigravity", Provider: "antigravity", Model: "pro"},
	})
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	for _, want := range []string{"child stdout", "child stderr", "agy log"} {
		if !warningsContain(result.Warnings, want) {
			t.Fatalf("missing %q warning: %#v", want, result.Warnings)
		}
	}
}

func TestAgyDriverBoundsLargeResumeTranscriptAndKeepsFinalAnswer(t *testing.T) {
	root := t.TempDir()
	sessionID := "22222222-2222-2222-2222-222222222222"
	if err := os.MkdirAll(filepath.Dir(conversationPath(root, sessionID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conversationPath(root, sessionID), []byte("conversation"), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := transcriptPath(root, sessionID)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"source":"MODEL","type":"PLANNER_RESPONSE","content":"old answer"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	appendSource := filepath.Join(root, "transcript-append")
	fillerLine := `{"source":"USER","type":"MESSAGE","content":"filler"}` + "\n"
	finalAnswer := "TRANSCRIPT_FINAL_ANSWER"
	appendText := strings.Repeat(fillerLine, agyTranscriptReadLimitBytes/len(fillerLine)+100) + `{"source":"MODEL","type":"PLANNER_RESPONSE","content":"` + finalAnswer + `"}` + "\n"
	if err := os.WriteFile(appendSource, []byte(appendText), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
log_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
: > "$log_file"
cat "$FAKE_AGY_TRANSCRIPT_APPEND" >> "$FAKE_AGY_TRANSCRIPT"
printf '%s\n' 'stale stdout answer'
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AGY_TRANSCRIPT_APPEND", appendSource)
	t.Setenv("FAKE_AGY_TRANSCRIPT", transcript)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{
		"--agy-bin", fakeAgy,
		"--agy-root", root,
		"--session-id", sessionID,
		"--model", "flash",
		"--prompt", "continue",
	}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	result := (Provider{}).Parse(runtime.RunResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: 0}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "antigravity", Provider: "antigravity", Model: "flash"},
	})
	if !result.OK || result.Text != finalAnswer || strings.Contains(result.Text, "stale stdout") {
		t.Fatalf("result=%+v", result)
	}
	if !warningsContain(result.Warnings, "transcript growth") {
		t.Fatalf("transcript truncation was silent: %#v", result.Warnings)
	}
}

func warningsContain(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, needle) {
			return true
		}
	}
	return false
}
