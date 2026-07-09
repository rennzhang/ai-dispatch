package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestExecuteDisabledDoesNotRunProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "gpt5.5",
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
	if result.Status != contract.StatusDisabled || result.ProviderUsed != "grok" || result.ModelUsed != "grok-4.5" {
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
		Target:    "gpt5.5",
		SessionID: "s1",
		Prompt:    "continue",
	})
	if result.ProviderUsed != "codex" || result.RequestedTarget != "gpt5.5" {
		t.Fatalf("result=%+v", result)
	}
}

func TestResumeRejectsProviderMismatch(t *testing.T) {
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

func TestRuntimeFailureDoesNotTryNextCandidate(t *testing.T) {
	failure := contract.FailureRuntime
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "opencode",
		FailureClass: &failure,
	}
	if shouldTryNextCandidate(contract.DispatchRequest{Command: "send"}, result) {
		t.Fatalf("runtime failures should not auto degrade")
	}
}

func TestConfigFailureCanTryNextCandidate(t *testing.T) {
	failure := contract.FailureConfig
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "opencode",
		FailureClass: &failure,
	}
	if !shouldTryNextCandidate(contract.DispatchRequest{Command: "send"}, result) {
		t.Fatalf("config failures should try the next configured candidate")
	}
}

func TestExecuteFallsBackAcrossConfiguredCandidates(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("PATH", binDir)
	writeConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ]
  },
  "providers": {}
}`)
	writeFakeOpenCode(t, filepath.Join(binDir, "opencode"))

	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "mimo-pro",
		Prompt:  "hello",
	})
	if !result.OK || result.ProviderUsed != "opencode" || result.ModelUsed != "opencode/mimo-v2.5-free" {
		t.Fatalf("result=%+v", result)
	}
	if !result.Degraded || !strings.Contains(result.DegradeReason, "openrouter/xiaomi/mimo-v2.5-pro") {
		t.Fatalf("degrade fields missing: %+v", result)
	}
	if len(result.RouteSteps) != 2 || result.RouteSteps[0].Status == contract.StatusSuccess || result.RouteSteps[1].Status != contract.StatusSuccess {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
}

func TestGrokConfiguredRouteFallsBackToOpenCode(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_GROK_BIN", filepath.Join(t.TempDir(), "missing-grok"))
	t.Setenv("PATH", binDir)
	writeConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "grok": [
      { "provider": "grok", "model": "grok-4.5" },
      { "provider": "opencode", "model": "openrouter/x-ai/grok-4.5" }
    ]
  },
  "providers": {}
}`)
	writeFakeOpenCode(t, filepath.Join(binDir, "opencode"))

	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
	})
	if !result.OK || result.ProviderUsed != "opencode" || result.ModelUsed != "openrouter/x-ai/grok-4.5" {
		t.Fatalf("result=%+v", result)
	}
	if !result.Degraded || !strings.Contains(result.DegradeReason, "grok:grok-4.5") {
		t.Fatalf("degrade fields missing: %+v", result)
	}
	if len(result.RouteSteps) != 2 || result.RouteSteps[0].Provider != "grok" || result.RouteSteps[1].Provider != "opencode" {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
}

func writeConfig(t *testing.T, path string, data string) {
	t.Helper()
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
