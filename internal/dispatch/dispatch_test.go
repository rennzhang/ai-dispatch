package dispatch

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestExecuteDisabledDoesNotRunProvider(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
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

func TestExecuteRejectsUnsupportedTarget(t *testing.T) {
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
	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "gemini",
		Prompt:  "hello",
	})
	if result.Status != contract.StatusDisabled || result.ProviderUsed != "antigravity" || result.RequestedTarget != "gemini" {
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
		withProviderExecutionLock("opencode", func() execruntime.RunResult {
			close(firstEntered)
			<-releaseFirst
			return execruntime.RunResult{ExitCode: 0}
		})
		close(firstDone)
	}()
	<-firstEntered

	go func() {
		withProviderExecutionLock("opencode", func() execruntime.RunResult {
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
	withProviderExecutionLock("opencode", func() execruntime.RunResult {
		done := make(chan struct{})
		go func() {
			withProviderExecutionLock("codex", func() execruntime.RunResult {
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

func TestClaudeConfigFailureIsAutoDegradable(t *testing.T) {
	failure := contract.FailureConfig
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "claude",
		FailureClass: &failure,
	}
	if !shouldAutoDegrade(contract.DispatchRequest{Command: "send"}, target("claude"), result) {
		t.Fatalf("expected claude config failure to auto degrade")
	}
}

func TestClaudeRuntimeFailureIsNotAutoDegradable(t *testing.T) {
	failure := contract.FailureRuntime
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "claude",
		FailureClass: &failure,
	}
	if shouldAutoDegrade(contract.DispatchRequest{Command: "send"}, target("claude"), result) {
		t.Fatalf("runtime failures should not auto degrade")
	}
}

func TestAutoDegradeCanBeDisabled(t *testing.T) {
	t.Setenv("AI_DISPATCH_AUTO_DEGRADE", "off")
	failure := contract.FailureConfig
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "claude",
		FailureClass: &failure,
	}
	if shouldAutoDegrade(contract.DispatchRequest{Command: "send"}, target("claude"), result) {
		t.Fatalf("auto degrade should respect AI_DISPATCH_AUTO_DEGRADE=off")
	}
}

func target(provider string) routing.DispatchTarget {
	return routing.DispatchTarget{Requested: provider, Provider: provider}
}
