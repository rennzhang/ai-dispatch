package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/routing"
)

func TestSelectedExecutionPreservesDeadlineAndMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	target := routing.DispatchTarget{Requested: "codex", Provider: "codex", Model: "gpt-6.1-sol", Surface: "cli"}
	calls := 0
	execute := func(got context.Context, _ contract.DispatchRequest, selected routing.DispatchTarget, _ Options) contract.ProviderResult {
		calls++
		if got != ctx {
			t.Fatal("execution lost request context")
		}
		<-got.Done()
		result := contract.SuccessResult("partial output")
		result.SessionID = "partial-session"
		result.Stderr = "provider timeout detail"
		result.Warnings = []string{"runtime_cleanup=incomplete"}
		ensureRouteMetadata(&result, selected, 30)
		return result
	}
	result := executeSelectedWith(ctx, contract.DispatchRequest{Command: "send"}, target, Options{}, execute)
	if calls != 1 || result.OK || result.Status != contract.StatusTimeout || result.ExitCode != 124 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	if result.Text != "partial output" || result.SessionID != "partial-session" || result.Stderr != "provider timeout detail" || len(result.Warnings) != 1 {
		t.Fatalf("lost metadata: %+v", result)
	}
	if result.Degraded || len(result.RouteSteps) != 1 {
		t.Fatalf("fabricated route: %+v", result)
	}
}

func TestSelectedFailureDoesNotTryAnotherEntry(t *testing.T) {
	for _, class := range []contract.FailureClass{contract.FailureQuota, contract.FailureRuntime, contract.FailureConfig, contract.FailureNetwork} {
		t.Run(string(class), func(t *testing.T) {
			calls := 0
			target := routing.DispatchTarget{Requested: "grok", Provider: "grok"}
			result := executeSelectedWith(context.Background(), contract.DispatchRequest{Command: "send"}, target, Options{}, func(_ context.Context, _ contract.DispatchRequest, selected routing.DispatchTarget, _ Options) contract.ProviderResult {
				calls++
				r := contract.ErrorResult(contract.StatusError, class, "provider failed", 1)
				ensureRouteMetadata(&r, selected, 1)
				return r
			})
			if calls != 1 || result.OK || result.Degraded || result.ProviderUsed != "grok" {
				t.Fatalf("calls=%d result=%+v", calls, result)
			}
		})
	}
}

func TestAppSurfaceKeepsCancelWithoutRequestDeadline(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	ctx, stop := executionContext(parent, contract.DispatchRequest{Surface: "app", TimeoutSeconds: 1})
	defer stop()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("app surface must not add a request deadline")
	}
	cancel()
	select {
	case <-ctx.Done():
	default:
		t.Fatal("cancel must still reach an app-surface wait")
	}
}

func TestCLISurfaceKeepsRequestDeadline(t *testing.T) {
	ctx, stop := executionContext(context.Background(), contract.DispatchRequest{TimeoutSeconds: 1})
	defer stop()
	if _, ok := ctx.Deadline(); !ok {
		t.Fatal("cli surface must keep --timeout")
	}
}

func TestStampDispatchPromptCoversInlineAndFile(t *testing.T) {
	inline := contract.DispatchRequest{Prompt: "hello"}
	if _, err := stampDispatchPrompt(&inline); err != nil {
		t.Fatal(err)
	}
	if inline.Prompt != "[ai-dispatch]\n\nhello" {
		t.Fatalf("prompt=%q", inline.Prompt)
	}
	again := inline
	if _, err := stampDispatchPrompt(&again); err != nil {
		t.Fatal(err)
	}
	if again.Prompt != inline.Prompt {
		t.Fatalf("double stamp=%q", again.Prompt)
	}

	original := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(original, []byte("from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	fileReq := contract.DispatchRequest{PromptFile: original, Prompt: "ignored"}
	cleanup, err := stampDispatchPrompt(&fileReq)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if fileReq.Prompt != "" || fileReq.PromptFile == original {
		t.Fatalf("req=%+v", fileReq)
	}
	stamped, err := os.ReadFile(fileReq.PromptFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(stamped) != "[ai-dispatch]\n\nfrom file" {
		t.Fatalf("stamped=%q", stamped)
	}
	raw, err := os.ReadFile(original)
	if err != nil || string(raw) != "from file" {
		t.Fatalf("original=%q err=%v", raw, err)
	}
}
