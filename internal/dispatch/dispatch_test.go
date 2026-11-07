package dispatch

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestExecuteDisabledDoesNotRunProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "codex",
		Prompt:  "hello",
	})
	if result.OK || result.Status != contract.StatusDisabled || result.ProviderUsed != "codex" {
		t.Fatalf("result=%+v", result)
	}
}

func TestOpenCodeMissingBinaryIsConfigFailure(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_OPENCODE_BIN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", filepath.Join(t.TempDir(), "opencode.lock"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "opencode",
		Prompt:  "hello",
	})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("result=%+v", result)
	}
	if result.NextAction != contract.NextSwitchStrategy {
		t.Fatalf("next_action=%q", result.NextAction)
	}
}

func TestGrokMissingBinaryIsConfigFailure(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_GROK_BIN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
	})
	if result.OK || result.ProviderUsed != "grok" || result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("result=%+v", result)
	}
}

func TestExecuteRejectsUnsupportedTarget(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "unknown-model",
		Prompt:  "hello",
	})
	if result.ExitCode != 2 || result.NextAction != contract.NextFixInput {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "unsupported target") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestResumeInfersProviderFromRunstore(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	previous := contract.SuccessResult("hello")
	previous.RequestedTarget = "gpt5.5"
	previous.ProviderUsed = "codex"
	previous.ModelUsed = "gpt-5.5"
	previous.SessionID = "s1"
	if err := runstore.WriteResult(root, "run-1", previous); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "run-corrupt"), 0o755); err != nil {
		t.Fatal(err)
	}
	result := Execute(contract.DispatchRequest{
		Command:   "resume",
		SessionID: "s1",
		Prompt:    "continue",
	})
	if result.ProviderUsed != "codex" || result.RequestedTarget != "gpt5.5" {
		t.Fatalf("result=%+v", result)
	}
}

func TestGeminiRoutesToAntigravityProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "gemini",
		Prompt:  "hello",
	})
	if result.Status != contract.StatusDisabled || result.ProviderUsed != "antigravity" || result.RequestedTarget != "gemini" {
		t.Fatalf("result=%+v", result)
	}
}

func TestGrokRoutesToGrokProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
	})
	if result.Status != contract.StatusDisabled || result.ProviderUsed != "grok" || result.ModelUsed != "" {
		t.Fatalf("result=%+v", result)
	}
}

func TestOpenCodeProviderLockSerializesExecution(t *testing.T) {
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", filepath.Join(t.TempDir(), "opencode.lock"))
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	secondEntered := make(chan struct{}, 1)
	secondDone := make(chan struct{})

	go func() {
		withProviderExecutionLock(context.Background(), "opencode", func() execruntime.RunResult {
			close(firstEntered)
			<-releaseFirst
			return execruntime.RunResult{ExitCode: 0}
		})
		close(firstDone)
	}()
	<-firstEntered

	go func() {
		withProviderExecutionLock(context.Background(), "opencode", func() execruntime.RunResult {
			secondEntered <- struct{}{}
			return execruntime.RunResult{ExitCode: 0}
		})
		close(secondDone)
	}()

	select {
	case <-secondEntered:
		t.Fatal("second opencode execution entered before first released lock")
	case <-time.After(50 * time.Millisecond):
	}
	close(releaseFirst)
	<-firstDone
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("second opencode execution did not complete after lock release")
	}
}

func TestOpenCodeProviderLockReportsCancellationWithoutTimeout(t *testing.T) {
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", filepath.Join(t.TempDir(), "opencode.lock"))
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstDone := make(chan struct{})
	go func() {
		withProviderExecutionLock(context.Background(), "opencode", func() execruntime.RunResult {
			close(firstEntered)
			<-releaseFirst
			return execruntime.RunResult{ExitCode: 0}
		})
		close(firstDone)
	}()
	<-firstEntered

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := withProviderExecutionLock(ctx, "opencode", func() execruntime.RunResult {
		t.Fatal("canceled execution acquired the lock")
		return execruntime.RunResult{}
	})
	close(releaseFirst)
	<-firstDone
	if !result.Canceled || result.TimedOut || result.ExitCode != 130 {
		t.Fatalf("result=%+v", result)
	}
}

func TestCanceledTargetResultDoesNotRetryOrFallBack(t *testing.T) {
	target := routing.DispatchTarget{Requested: "opencode", Provider: "opencode", Model: "openrouter/x"}
	result := canceledTargetResult(target, 42)
	if result.OK || result.Status != contract.StatusError || result.ExitCode != 130 {
		t.Fatalf("result=%+v", result)
	}
	if result.FailureClass != nil || result.NextAction != contract.NextDone {
		t.Fatalf("canceled result must not be retryable: %+v", result)
	}
	if result.Stderr != "dispatch canceled" {
		t.Fatalf("canceled result must not claim who initiated cancellation: %+v", result)
	}
	if result.ProviderUsed != "opencode" || result.ModelUsed != "openrouter/x" || len(result.RouteSteps) != 1 {
		t.Fatalf("route metadata missing: %+v", result)
	}
}

func TestCanceledRunKeepsParsedMetadataAndSurfacesRuntimeState(t *testing.T) {
	p, ok := providerFor("codex")
	if !ok {
		t.Fatal("codex provider missing")
	}
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	run := execruntime.RunResult{
		Stdout: []byte("{\"type\":\"thread.started\",\"thread_id\":\"session-partial\"}\n" +
			"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"partial answer\"}}\n"),
		ExitCode:           130,
		DurationMS:         42,
		Canceled:           true,
		Error:              context.Canceled.Error(),
		CleanupAttempted:   true,
		CleanupComplete:    false,
		CleanupError:       "child process remained alive",
		StdoutTruncated:    true,
		StdoutDroppedBytes: 17,
		StderrTruncated:    true,
		StderrDroppedBytes: 9,
	}
	result := providerResultFromRun(p, run, providers.BuildRequest{Target: target}, target, providers.EffortAuto(contract.EffortAuto, target.Model), providers.FastStandard(false))
	if result.Text != "partial answer" || result.SessionID != "session-partial" {
		t.Fatalf("captured provider metadata was lost: %+v", result)
	}
	if result.OK || result.Status != contract.StatusError || result.ExitCode != 130 || result.NextAction != contract.NextDone || result.FailureClass != nil || result.Stderr != "dispatch canceled" {
		t.Fatalf("cancel contract=%+v", result)
	}
	if len(result.RouteSteps) != 1 || result.RouteSteps[0].Status != contract.StatusError {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
	warnings := strings.Join(result.Warnings, "\n")
	for _, expected := range []string{
		"runtime_cleanup=incomplete error=child process remained alive",
		"runtime_stdout_truncated=true dropped_bytes=17",
		"runtime_stderr_truncated=true dropped_bytes=9",
	} {
		if !strings.Contains(warnings, expected) {
			t.Fatalf("missing warning %q in %q", expected, warnings)
		}
	}
}

func TestExecuteWithOptionsCancellationPreservesCapturedProviderMetadata(t *testing.T) {
	root := t.TempDir()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	readyFile := filepath.Join(root, "ready")
	script := `#!/bin/sh
printf '%s\n' '{"type":"thread.started","thread_id":"full-chain-session"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"captured before cancel"}}'
printf ready > "` + readyFile + `"
trap 'exit 0' TERM INT
while :; do /bin/sleep 1; done
`
	if err := os.WriteFile(filepath.Join(binDir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(root, "missing-config.json"))

	ctx, cancel := context.WithCancel(context.Background())
	resultCh := make(chan contract.ProviderResult, 1)
	go func() {
		resultCh <- ExecuteWithOptions(contract.DispatchRequest{
			Command:        "send",
			Target:         "codex",
			Model:          "gpt-5.5",
			Prompt:         "hello",
			TimeoutSeconds: 0,
		}, Options{Context: ctx})
	}()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(readyFile); err == nil {
			break
		}
		if time.Now().After(deadline) {
			cancel()
			t.Fatal("fake codex did not publish captured output")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()

	select {
	case result := <-resultCh:
		if result.Text != "captured before cancel" || result.SessionID != "full-chain-session" {
			t.Fatalf("captured metadata was overwritten after cancellation: %+v", result)
		}
		if result.OK || result.ExitCode != 130 || result.NextAction != contract.NextDone || result.FailureClass != nil || result.Stderr != "dispatch canceled" {
			t.Fatalf("cancel contract=%+v", result)
		}
		if strings.Contains(strings.Join(result.Warnings, "\n"), "runtime_cleanup=complete") {
			t.Fatalf("normal cleanup must not be a warning: %v", result.Warnings)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("canceled dispatch did not return")
	}
}

func TestCompletedCleanupDoesNotCreateWarning(t *testing.T) {
	result := contract.SuccessResult("ok")
	applyRuntimeWarnings(&result, execruntime.RunResult{CleanupAttempted: true, CleanupComplete: true})
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings=%v", result.Warnings)
	}
}

func TestCanceledExecutionDoesNotStartOrFallBack(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_GROK_BIN", filepath.Join(t.TempDir(), "missing-grok"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := ExecuteWithOptions(contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
	}, Options{Context: ctx})
	if result.ExitCode != 130 || result.Stderr != "dispatch canceled" || result.ProviderUsed != "grok" {
		t.Fatalf("result=%+v", result)
	}
	if result.Degraded || len(result.RouteSteps) != 1 {
		t.Fatalf("canceled execution must not start or fall back: %+v", result)
	}
}

func TestOpenCodeProviderFileLockPreservesCancellationAndDeadline(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.lock")
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", path)
	release, err := acquireFileLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()

	cancelCtx, cancel := context.WithCancel(context.Background())
	cancelResult := make(chan execruntime.RunResult, 1)
	go func() {
		cancelResult <- withProviderExecutionLock(cancelCtx, "opencode", func() execruntime.RunResult {
			t.Error("canceled execution acquired the file lock")
			return execruntime.RunResult{}
		})
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	if result := <-cancelResult; !result.Canceled || result.TimedOut || result.ExitCode != 130 {
		t.Fatalf("cancel result=%+v", result)
	}

	deadlineCtx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	result := withProviderExecutionLock(deadlineCtx, "opencode", func() execruntime.RunResult {
		t.Error("deadline execution acquired the file lock")
		return execruntime.RunResult{}
	})
	if !result.TimedOut || !result.FixedTimeout || result.Canceled || result.ExitCode != 124 {
		t.Fatalf("deadline result=%+v", result)
	}
	if result.DurationMS < 15 {
		t.Fatalf("deadline lock wait duration was not preserved: %+v", result)
	}
}

func TestNonOpenCodeProviderDoesNotUseOpenCodeLock(t *testing.T) {
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", filepath.Join(t.TempDir(), "opencode.lock"))
	withProviderExecutionLock(context.Background(), "opencode", func() execruntime.RunResult {
		done := make(chan struct{})
		go func() {
			withProviderExecutionLock(context.Background(), "codex", func() execruntime.RunResult {
				close(done)
				return execruntime.RunResult{ExitCode: 0}
			})
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("non-opencode provider was blocked by opencode lock")
		}
		return execruntime.RunResult{ExitCode: 0}
	})
}

func TestResumeTargetResolvesModelAlias(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	previous := contract.SuccessResult("hello")
	previous.RequestedTarget = "gpt5.5"
	previous.ProviderUsed = "codex"
	previous.ModelUsed = "gpt-5.5"
	previous.SessionID = "s1"
	if err := runstore.WriteResult(root, "run-1", previous); err != nil {
		t.Fatal(err)
	}
	result := Execute(contract.DispatchRequest{
		Command:   "resume",
		Target:    "codex",
		SessionID: "s1",
		Prompt:    "continue",
	})
	if result.ProviderUsed != "codex" || result.RequestedTarget != "codex" {
		t.Fatalf("result=%+v", result)
	}
}

func TestResumeRejectsProviderMismatch(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	previous := contract.SuccessResult("hello")
	previous.RequestedTarget = "gpt5.5"
	previous.ProviderUsed = "codex"
	previous.ModelUsed = "gpt-5.5"
	previous.SessionID = "s1"
	if err := runstore.WriteResult(root, "run-1", previous); err != nil {
		t.Fatal(err)
	}
	result := Execute(contract.DispatchRequest{
		Command:   "resume",
		Target:    "claude",
		SessionID: "s1",
		Prompt:    "continue",
	})
	if result.ExitCode != 2 || !strings.Contains(result.Stderr, "belongs to provider") {
		t.Fatalf("result=%+v", result)
	}
}

func TestResumeSessionProviderAcceptsProviderAlias(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	previous := contract.SuccessResult("hello")
	previous.RequestedTarget = "gemini-pro"
	previous.ProviderUsed = "antigravity"
	previous.ModelUsed = "pro"
	previous.SessionID = "s1"
	if err := runstore.WriteResult(root, "run-1", previous); err != nil {
		t.Fatal(err)
	}
	result := Execute(contract.DispatchRequest{
		Command:         "resume",
		SessionProvider: "gemini",
		SessionID:       "s1",
		Prompt:          "continue",
	})
	if result.ExitCode == 2 || result.ProviderUsed != "antigravity" {
		t.Fatalf("result=%+v", result)
	}
}

func writeConfig(t *testing.T, path string, data string) {
	t.Helper()
	// Legacy fixture notation is converted explicitly before exercising the v2 runtime.
	if cfg, _, err := config.Migrate([]byte(data)); err == nil {
		encoded, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		data = string(encoded)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeFakeOpenCode(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
set -eu
if [ "${1:-}" = "--version" ]; then
  echo "opencode 1.0.0"
  exit 0
fi
model=""
prev=""
for arg in "$@"; do
  if [ "$prev" = "--model" ]; then
    model="$arg"
    break
  fi
  prev="$arg"
done
if [ "$model" = "openrouter/xiaomi/mimo-v2.5-pro" ]; then
  echo "OpenRouter: This model is not available in your region" >&2
  exit 1
fi
echo '{"result":"OK","sessionID":"s1"}'
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
