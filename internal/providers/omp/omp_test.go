package omp

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func fakeOMPBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "omp")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_OMP_BIN", bin)
	return bin
}

func TestBuildOMPArgsAutoOmitsThinking(t *testing.T) {
	bin := fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "deepseek", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortAuto})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{bin, "--print", "--mode", "json", "--auto-approve", "--model", "opencode-go/deepseek-v4.1-flash"}
	for i, part := range wantPrefix {
		if i >= len(spec.Args) || spec.Args[i] != part {
			t.Fatalf("args=%#v", spec.Args)
		}
	}
	joined := strings.Join(spec.Args, "\x00")
	if strings.Contains(joined, "--thinking") {
		t.Fatalf("auto must omit --thinking: %#v", spec.Args)
	}
	if string(spec.Stdin) != "hello" {
		t.Fatalf("stdin=%q", string(spec.Stdin))
	}
	if strings.Contains(joined, "hello") {
		t.Fatalf("prompt leaked into argv: %#v", spec.Args)
	}
}

func TestBuildOMPArgsExactThinkingAndNoneMapsOff(t *testing.T) {
	bin := fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "deepseek", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortHigh})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if !strings.Contains(joined, "--thinking\x00high") {
		t.Fatalf("args=%#v", spec.Args)
	}
	if spec.Args[0] != bin {
		t.Fatalf("bin=%q", spec.Args[0])
	}

	none, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortNone})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(none.Args, "\x00"), "--thinking\x00off") {
		t.Fatalf("none should map to off: %#v", none.Args)
	}
}

func TestBuildOMPApprovalDefaultSkipsAutoApprove(t *testing.T) {
	fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "omp", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"approval": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "--auto-approve") {
		t.Fatalf("default approval should skip auto-approve: %#v", spec.Args)
	}
}

func TestBuildOMPResumeAndCWD(t *testing.T) {
	fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "omp", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:    "follow up",
		Target:    target,
		SessionID: "01a12a51-bc10-7000-8b82-d63795425090",
		CWD:       "/tmp/workspace",
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if !strings.Contains(joined, "--resume\x0001a12a51-bc10-7000-8b82-d63795425090") {
		t.Fatalf("resume missing: %#v", spec.Args)
	}
	if !strings.Contains(joined, "--cwd\x00/tmp/workspace") {
		t.Fatalf("cwd missing: %#v", spec.Args)
	}
}

func TestBuildOMPPromptFileUsesStdin(t *testing.T) {
	fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "omp", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("secret prompt body"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := Provider{}.Build(providers.BuildRequest{PromptFile: promptFile, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if string(spec.Stdin) != "secret prompt body" {
		t.Fatalf("stdin=%q", string(spec.Stdin))
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "secret prompt body") {
		t.Fatalf("prompt leaked into argv: %#v", spec.Args)
	}
}

func TestBuildOMPFailsWhenBinaryMissing(t *testing.T) {
	t.Setenv("AI_DISPATCH_OMP_BIN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	target := routing.DispatchTarget{Requested: "omp", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	_, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err == nil || !strings.Contains(err.Error(), "omp binary not found") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveOMPEffortFromDeclared(t *testing.T) {
	p := Provider{}
	declared := []string{"low", "high", "max"}
	exact := p.ResolveEffort(context.Background(), providers.EffortRequest{
		Model: "opencode-go/deepseek-v4.1-flash", Requested: contract.EffortHigh, DeclaredEfforts: declared,
	})
	if exact.Applied != contract.EffortHigh || exact.Fallback {
		t.Fatalf("exact=%+v", exact)
	}
	outside := p.ResolveEffort(context.Background(), providers.EffortRequest{
		Model: "opencode-go/deepseek-v4.1-flash", Requested: contract.EffortMedium, DeclaredEfforts: declared,
	})
	if outside.Applied != contract.EffortAuto || !outside.Fallback {
		t.Fatalf("outside=%+v", outside)
	}
	undeclared := p.ResolveEffort(context.Background(), providers.EffortRequest{
		Model: "opencode-go/deepseek-v4.1-flash", Requested: contract.EffortHigh,
	})
	if undeclared.Applied != contract.EffortAuto || !undeclared.Fallback || !strings.Contains(undeclared.Reason, "not declared in config") {
		t.Fatalf("undeclared=%+v", undeclared)
	}
}

func TestSuggestOMPEffortLevels(t *testing.T) {
	if got := SuggestEffortLevels("opencode-go/deepseek-v4.1-flash"); len(got) != 3 {
		t.Fatalf("suggest=%v", got)
	}
	if got := SuggestEffortLevels("opencode-go/deepseek-v4-pro"); len(got) != 3 {
		t.Fatalf("suggest pro=%v", got)
	}
	if got := SuggestEffortLevels("unknown-model"); got != nil {
		t.Fatalf("unknown should be nil: %v", got)
	}
}

func TestParseOMPStream(t *testing.T) {
	stdout := strings.Join([]string{
		`{"type":"session","version":3,"id":"sess-123","timestamp":"2026-10-11T09:36:02.064Z","cwd":"/tmp"}`,
		`{"type":"message_end","message":{"role":"assistant","content":[{"type":"thinking","thinking":"plan"},{"type":"text","text":"pong"}],"provider":"opencode-go","model":"deepseek-v4.1-flash","usage":{"input":10,"output":2,"cacheRead":1,"cacheWrite":0}}}`,
		`{"type":"turn_end","message":{"role":"assistant","content":[{"type":"text","text":"pong"}]}}`,
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout), ExitCode: 0, DurationMS: 12}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "deepseek", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"},
	})
	if !result.OK || result.Text != "pong" || result.SessionID != "sess-123" {
		t.Fatalf("result=%+v", result)
	}
	if result.ModelUsed != "opencode-go/deepseek-v4.1-flash" {
		t.Fatalf("model=%q", result.ModelUsed)
	}
	if result.ProviderUsed != "omp" || result.Usage == nil || result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 2 {
		t.Fatalf("usage/provider=%+v usage=%+v", result, result.Usage)
	}
}

func TestBuildOMPRequiresPrompt(t *testing.T) {
	fakeOMPBin(t)
	target := routing.DispatchTarget{Requested: "omp", Provider: "omp", Model: "opencode-go/deepseek-v4.1-flash"}
	_, err := Provider{}.Build(providers.BuildRequest{Target: target})
	if err == nil || !strings.Contains(err.Error(), "requires a prompt") {
		t.Fatalf("err=%v", err)
	}
}
