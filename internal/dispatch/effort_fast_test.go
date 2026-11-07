package dispatch

import (
	"context"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/providers/grok"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecuteWithOptionsNormalizesEmptyEffort(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	result := ExecuteWithOptions(contract.DispatchRequest{
		Command: "send",
		Target:  "codex",
		Prompt:  "hello",
	}, Options{})
	if result.RequestedEffort != contract.EffortAuto || result.AppliedEffort != contract.EffortAuto {
		t.Fatalf("result effort fields=%+v", result)
	}
	if result.EffortFallbackReason != "" {
		t.Fatalf("auto should not set fallback reason: %+v", result)
	}
	if result.Degraded {
		t.Fatalf("effort auto must not set degraded: %+v", result)
	}
}

func TestCanceledBeforeExecutionFillsRouteStepEffort(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := ExecuteWithOptions(contract.DispatchRequest{
		Command: "send",
		Target:  "codex",
		Prompt:  "hello",
		Effort:  contract.EffortHigh,
	}, Options{Context: ctx})
	if result.OK || result.ExitCode != 130 || result.Stderr != "dispatch canceled" {
		t.Fatalf("result=%+v", result)
	}
	if result.RequestedEffort != contract.EffortHigh || result.AppliedEffort != contract.EffortAuto {
		t.Fatalf("top-level effort=%+v", result)
	}
	if len(result.RouteSteps) != 1 {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
	if result.RouteSteps[0].AppliedEffort != contract.EffortAuto {
		t.Fatalf("route step effort must be filled at completion: %+v", result.RouteSteps[0])
	}
	if result.RouteSteps[0].EffortFallbackReason != "" {
		t.Fatalf("pre-exec cancel is not effort fallback: %+v", result.RouteSteps[0])
	}
}

func TestCompleteDispatchResultDoesNotOverwriteCandidateEffort(t *testing.T) {
	result := contract.SuccessResult("ok")
	result.RequestedEffort = contract.EffortHigh
	result.AppliedEffort = contract.EffortHigh
	result.RouteSteps = []contract.RouteStep{
		{Provider: "grok", Model: "composer", AppliedEffort: contract.EffortAuto, EffortFallbackReason: "unsupported"},
		{Provider: "grok", Model: "grok-4.5", AppliedEffort: contract.EffortHigh},
		{Provider: "codex", Model: "gpt-5.5"}, // empty applied should be filled from top-level
	}
	got := completeDispatchResult(contract.DispatchRequest{Effort: contract.EffortHigh}, Options{}, result)
	if got.RouteSteps[0].AppliedEffort != contract.EffortAuto || got.RouteSteps[0].EffortFallbackReason != "unsupported" {
		t.Fatalf("must preserve first candidate effort: %+v", got.RouteSteps[0])
	}
	if got.RouteSteps[1].AppliedEffort != contract.EffortHigh || got.RouteSteps[1].EffortFallbackReason != "" {
		t.Fatalf("must preserve final candidate effort: %+v", got.RouteSteps[1])
	}
	if got.RouteSteps[2].AppliedEffort != contract.EffortHigh {
		t.Fatalf("empty step should inherit top-level applied: %+v", got.RouteSteps[2])
	}
}

func TestEffortFallbackDoesNotSetDegradedOrRetry(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_GROK_BIN", writeFakeGrokOK(t))
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var builds int
	result := executeSelectedWith(context.Background(), contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
		Effort:  contract.EffortXHigh,
	}, routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"}, Options{},
		func(ctx context.Context, req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
			builds++
			p := grok.Provider{}
			resolution := p.ResolveEffort(ctx, providers.EffortRequest{Model: target.Model, Requested: req.Effort})
			buildTarget := target
			buildTarget.Model = resolution.AppliedModel
			spec, err := p.Build(providers.BuildRequest{
				Prompt: req.Prompt,
				Target: buildTarget,
				Effort: resolution.Applied,
			})
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(spec.Args, "\x00")
			if strings.Contains(joined, "--reasoning-effort") {
				t.Fatalf("fallback must not pass unsupported effort: %#v", spec.Args)
			}
			run := execruntime.RunResult{
				Stdout:     []byte("{\"type\":\"text\",\"data\":\"OK\"}\n{\"type\":\"end\",\"sessionId\":\"s1\"}\n"),
				ExitCode:   0,
				DurationMS: 5,
			}
			result := p.Parse(run, providers.BuildRequest{Target: buildTarget, Effort: resolution.Applied})
			ensureRouteMetadata(&result, buildTarget, run.DurationMS)
			applyEffortResolution(&result, resolution)
			return result
		})
	if builds != 1 {
		t.Fatalf("effort fallback must execute provider once, builds=%d", builds)
	}
	if !result.OK || result.RequestedEffort != contract.EffortXHigh || result.AppliedEffort != contract.EffortAuto {
		t.Fatalf("result=%+v", result)
	}
	if result.EffortFallbackReason == "" || result.Degraded {
		t.Fatalf("expected effort fallback without degraded: %+v", result)
	}
	if len(result.RouteSteps) != 1 || result.RouteSteps[0].AppliedEffort != contract.EffortAuto {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
}

func TestAllProvidersResolveSharedFastIntent(t *testing.T) {
	yes := true
	tests := []struct {
		provider string
		model    string
		declared *bool
		applied  bool
	}{
		{provider: "codex", model: "gpt-5.6-luna", declared: &yes, applied: true},
		{provider: "claude", model: "claude-haiku-4-5", declared: nil, applied: false},
		{provider: "opencode", model: "openrouter/xiaomi/mimo-v2.5-pro", declared: nil, applied: false},
		{provider: "grok", model: "grok-4.5", declared: nil, applied: false},
		{provider: "antigravity", model: "gemini-3-flash", declared: nil, applied: false},
	}
	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			provider, ok := providerFor(tc.provider)
			if !ok {
				t.Fatalf("provider %q not registered", tc.provider)
			}
			got := provider.ResolveFast(context.Background(), providers.FastRequest{Model: tc.model, Requested: true, DeclaredFast: tc.declared})
			if !got.Requested || got.Applied != tc.applied {
				t.Fatalf("resolution=%+v", got)
			}
			if tc.applied && (got.Fallback || got.Reason != "") {
				t.Fatalf("supported fast must be exact: %+v", got)
			}
			if !tc.applied && (!got.Fallback || !strings.Contains(got.Reason, "standard speed")) {
				t.Fatalf("unsupported fast must be explicit fallback: %+v", got)
			}
		})
	}
}

func TestFastFallbackDoesNotSetDegradedOrRetry(t *testing.T) {
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_GROK_BIN", writeFakeGrokOK(t))
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var builds int
	result := executeSelectedWith(context.Background(), contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
		Fast:    true,
	}, routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"}, Options{},
		func(ctx context.Context, req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
			builds++
			p := grok.Provider{}
			resolution := p.ResolveFast(ctx, providers.FastRequest{Model: target.Model, Requested: req.Fast})
			spec, err := p.Build(providers.BuildRequest{
				Prompt: req.Prompt,
				Target: target,
				Fast:   resolution.Applied,
			})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.Join(spec.Args, "\x00"), "--fast") {
				t.Fatalf("fallback must not pass unsupported fast flag: %#v", spec.Args)
			}
			run := execruntime.RunResult{Stdout: []byte("{\"type\":\"text\",\"data\":\"OK\"}\n{\"type\":\"end\",\"sessionId\":\"s1\"}\n"), ExitCode: 0, DurationMS: 5}
			parsed := p.Parse(run, providers.BuildRequest{Target: target, Fast: resolution.Applied})
			ensureRouteMetadata(&parsed, target, run.DurationMS)
			applyFastResolution(&parsed, resolution)
			return parsed
		})
	if builds != 1 {
		t.Fatalf("fast fallback must execute provider once, builds=%d", builds)
	}
	if !result.OK || !result.RequestedFast || result.AppliedFast {
		t.Fatalf("result=%+v", result)
	}
	if result.FastFallbackReason == "" || result.Degraded {
		t.Fatalf("expected explicit fast fallback without route degradation: %+v", result)
	}
	for _, warning := range result.Warnings {
		if strings.Contains(strings.ToLower(warning), "fast") {
			t.Fatalf("fast capability fallback must not pollute warnings: %v", result.Warnings)
		}
	}
	if len(result.RouteSteps) != 1 || result.RouteSteps[0].AppliedFast || result.RouteSteps[0].FastFallbackReason == "" {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
}

func TestSupportsFastSelectedEntry(t *testing.T) {
	yes := true
	if !SupportsFast(context.Background(), routing.DispatchTarget{
		Requested: "gpt5.6-luna",
		Provider:  "codex",
		Model:     "gpt-5.6-luna",
		Fast:      &yes,
	}) {
		t.Fatal("codex luna with declared fast must support fast")
	}
	if SupportsFast(context.Background(), routing.DispatchTarget{
		Requested: "gpt5.6-luna",
		Provider:  "codex",
		Model:     "gpt-5.6-luna",
	}) {
		t.Fatal("undeclared fast must not support fast")
	}
	if SupportsFast(context.Background(), routing.DispatchTarget{
		Requested: "grok",
		Provider:  "grok",
		Model:     "grok-4.5",
	}) {
		t.Fatal("grok must not support fast")
	}
	if SupportsFast(context.Background(), routing.DispatchTarget{
		Requested: "multi",
		Provider:  "grok",
		Model:     "grok-4.5",
	}) {
		t.Fatal("SupportsFast must describe the selected entry only")
	}
}

func writeFakeGrokOK(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nprintf '%s\\n' '{\"type\":\"text\",\"data\":\"OK\"}' '{\"type\":\"end\",\"sessionId\":\"s1\"}'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
