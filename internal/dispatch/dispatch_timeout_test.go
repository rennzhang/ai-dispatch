package dispatch

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/routing"
)

func TestDispatchTimeoutBudgetIsSharedAcrossFallbackCandidates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	target := routing.DispatchTarget{
		Requested: "chain",
		Candidates: []routing.RouteCandidate{
			{Provider: "codex", Model: "gpt-5.5"},
			{Provider: "claude", Model: "sonnet"},
		},
	}
	calls := 0
	seenContexts := []context.Context{}
	execute := func(gotCtx context.Context, _ contract.DispatchRequest, candidate routing.DispatchTarget, _ Options) contract.ProviderResult {
		calls++
		seenContexts = append(seenContexts, gotCtx)
		if calls == 1 {
			time.Sleep(25 * time.Millisecond)
		} else {
			<-gotCtx.Done()
		}
		result := contract.ErrorResult(contract.StatusError, contract.FailureConfig, "fake candidate failure", 3)
		if calls == 2 {
			result.Text = "partial fallback output"
			result.SessionID = "partial-session"
			result.Stderr = "provider timeout detail"
			result.Warnings = []string{"runtime_cleanup=incomplete error=fake cleanup failure"}
		}
		ensureRouteMetadata(&result, candidate, 0)
		return result
	}

	started := time.Now()
	result := executeCandidatesWith(ctx, contract.DispatchRequest{Command: "send"}, target, Options{}, execute)
	if calls != 2 {
		t.Fatalf("calls=%d result=%+v", calls, result)
	}
	for _, seen := range seenContexts {
		if seen != ctx {
			t.Fatal("fallback received a fresh context instead of the dispatch-wide budget")
		}
	}
	if result.Status != contract.StatusTimeout || result.FailureClass == nil || *result.FailureClass != contract.FailureTimeout || result.ExitCode != 124 {
		t.Fatalf("result=%+v", result)
	}
	if result.Text != "partial fallback output" || result.SessionID != "partial-session" || !strings.Contains(strings.Join(result.Warnings, "\n"), "runtime_cleanup=incomplete") {
		t.Fatalf("timeout normalization lost parsed metadata: %+v", result)
	}
	if result.Stderr != "provider timeout detail" {
		t.Fatalf("timeout normalization lost provider diagnostic: %+v", result)
	}
	if result.DurationMS < 40 || len(result.RouteSteps) != 2 || result.RouteSteps[1].DurationMS != result.DurationMS {
		t.Fatalf("timeout duration did not include the active fallback attempt: %+v", result)
	}
	if time.Since(started) > 250*time.Millisecond {
		t.Fatalf("fallback exceeded shared timeout: %s", time.Since(started))
	}
}

func TestFirstCandidateTimeoutDoesNotCreatePhantomFallbackRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	target := routing.DispatchTarget{
		Requested: "chain",
		Candidates: []routing.RouteCandidate{
			{Provider: "codex", Model: "gpt-5.5"},
			{Provider: "claude", Model: "sonnet"},
			{Provider: "opencode", Model: "openrouter/x"},
		},
	}
	calls := 0
	execute := func(gotCtx context.Context, _ contract.DispatchRequest, candidate routing.DispatchTarget, _ Options) contract.ProviderResult {
		calls++
		if calls > 1 {
			t.Fatalf("unexecuted fallback candidate was invoked: %+v", candidate)
		}
		<-gotCtx.Done()
		result := contract.ErrorResult(contract.StatusError, contract.FailureConfig, "fake failure after deadline", 3)
		ensureRouteMetadata(&result, candidate, 40)
		return result
	}

	result := executeCandidatesWith(ctx, contract.DispatchRequest{Command: "send"}, target, Options{}, execute)
	if calls != 1 {
		t.Fatalf("calls=%d", calls)
	}
	if result.Status != contract.StatusTimeout || result.ProviderUsed != "codex" {
		t.Fatalf("result=%+v", result)
	}
	if result.Degraded || result.DegradeReason != "" {
		t.Fatalf("timeout before fallback must not claim a route switch: %+v", result)
	}
	if len(result.RouteSteps) != 1 || result.RouteSteps[0].Provider != "codex" || len(result.RouteTrace) != 1 {
		t.Fatalf("phantom route metadata: trace=%v steps=%+v", result.RouteTrace, result.RouteSteps)
	}
}

func TestFallbackPreservesRuntimeWarningsFromEveryCandidate(t *testing.T) {
	target := routing.DispatchTarget{
		Requested: "chain",
		Candidates: []routing.RouteCandidate{
			{Provider: "codex", Model: "gpt-5.5"},
			{Provider: "claude", Model: "sonnet"},
		},
	}
	calls := 0
	execute := func(_ context.Context, _ contract.DispatchRequest, candidate routing.DispatchTarget, _ Options) contract.ProviderResult {
		calls++
		if calls == 1 {
			result := contract.ErrorResult(contract.StatusError, contract.FailureConfig, "fake config failure", 3)
			result.Warnings = []string{"runtime_cleanup=incomplete error=fake cleanup failure"}
			ensureRouteMetadata(&result, candidate, 1)
			return result
		}
		result := contract.SuccessResult("ok")
		result.Warnings = []string{"runtime_stderr_truncated=true dropped_bytes=12"}
		ensureRouteMetadata(&result, candidate, 1)
		return result
	}
	result := executeCandidatesWith(context.Background(), contract.DispatchRequest{Command: "send"}, target, Options{}, execute)
	warnings := strings.Join(result.Warnings, "\n")
	for _, expected := range []string{"runtime_cleanup=incomplete error=fake cleanup failure", "runtime_stderr_truncated=true dropped_bytes=12"} {
		if !strings.Contains(warnings, expected) {
			t.Fatalf("missing %q in warnings=%v", expected, result.Warnings)
		}
	}
}
