package codex

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

func TestBuildCodexArgsAutoDoesNotHardcodeHigh(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortAuto})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--json", "--model", "gpt-5.5", "hello"}
	if len(spec.Args) != len(want) {
		t.Fatalf("args=%#v", spec.Args)
	}
	for i := range want {
		if spec.Args[i] != want[i] {
			t.Fatalf("args=%#v", spec.Args)
		}
	}
	joined := strings.Join(spec.Args, "\x00")
	if strings.Contains(joined, "model_reasoning_effort") {
		t.Fatalf("auto effort must not set model_reasoning_effort: %#v", spec.Args)
	}
}

func TestBuildCodexArgsExactEffort(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.6", Provider: "codex", Model: "gpt-5.6"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortXHigh})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--json", "-c", `model_reasoning_effort="xhigh"`, "--model", "gpt-5.6", "hello"}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%#v want=%#v", spec.Args, want)
	}
}

func TestResolveCodexEffort(t *testing.T) {
	p := Provider{}
	auto := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6", Requested: contract.EffortAuto})
	if auto.Applied != contract.EffortAuto || auto.Fallback {
		t.Fatalf("auto=%+v", auto)
	}
	exact := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6", Requested: contract.EffortXHigh})
	if exact.Applied != contract.EffortXHigh || exact.Fallback {
		t.Fatalf("exact=%+v", exact)
	}
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra"} {
		for _, level := range []contract.Effort{contract.EffortLow, contract.EffortMedium, contract.EffortHigh, contract.EffortXHigh, contract.EffortMax} {
			got := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: model, Requested: level})
			if got.Applied != level || got.Fallback {
				t.Fatalf("model %s exact %s=%+v", model, level, got)
			}
		}
	}
	gpt55Max := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.5", Requested: contract.EffortMax})
	if gpt55Max.Applied != contract.EffortAuto || !gpt55Max.Fallback {
		t.Fatalf("gpt-5.5 max must fall back to auto: %+v", gpt55Max)
	}
	forged := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "foo-gpt5-model", Requested: contract.EffortHigh})
	if forged.Applied != contract.EffortAuto || !forged.Fallback {
		t.Fatalf("forged gpt5 id must fall back to auto: %+v", forged)
	}
	for _, level := range []contract.Effort{contract.EffortNone, contract.EffortMinimal} {
		got := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6-sol", Requested: level})
		if got.Applied != contract.EffortAuto || !got.Fallback {
			t.Fatalf("lowest level %s must fall back to auto: %+v", level, got)
		}
	}
	luna := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6-luna", Requested: contract.EffortHigh})
	if luna.Applied != contract.EffortAuto || !luna.Fallback {
		t.Fatalf("unexposed luna must fall back to auto: %+v", luna)
	}
	unknown := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "o3", Requested: contract.EffortHigh})
	if unknown.Applied != contract.EffortAuto || !unknown.Fallback {
		t.Fatalf("unknown=%+v", unknown)
	}
}

func TestBuildCodexPromptFileUsesStdin(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("prompt from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := Provider{}.Build(providers.BuildRequest{PromptFile: promptFile, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Args[len(spec.Args)-1]; got != "-" {
		t.Fatalf("args=%#v", spec.Args)
	}
	if string(spec.Stdin) != "prompt from file" {
		t.Fatalf("stdin=%q", spec.Stdin)
	}
	for _, arg := range spec.Args {
		if arg == "prompt from file" {
			t.Fatalf("prompt leaked into args: %#v", spec.Args)
		}
	}
}

func TestBuildCodexRejectsOpenRouterModel(t *testing.T) {
	target := routing.DispatchTarget{Requested: "bad", Provider: "codex", Model: "openrouter/xiaomi/mimo-v2.5-pro"}
	_, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err == nil || !strings.Contains(err.Error(), "cannot run OpenRouter model") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseCodexJSONStream(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:     []byte("{\"type\":\"thread.started\",\"thread_id\":\"s1\"}\n{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"hello\"}}\n"),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target})
	if !result.OK || result.Text != "hello" || result.SessionID != "s1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseCodexJSONStreamKeepsOnlyLatestAgentMessageAsFinal(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout: []byte(
			"{\"type\":\"thread.started\",\"thread_id\":\"s1\"}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"reading files\"}}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"running checks\"}}\n" +
				"{\"type\":\"item.completed\",\"item\":{\"type\":\"agent_message\",\"text\":\"final answer\"}}\n",
		),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target})
	if !result.OK || result.Text != "final answer" || result.SessionID != "s1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseCodexUsageLimitWithLoaderWarningsAsQuota(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout: []byte(
			"{\"type\":\"thread.started\",\"thread_id\":\"s1\"}\n" +
				"{\"type\":\"error\",\"text\":\"You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 7:30 PM.\"}\n",
		),
		Stderr:   []byte("2026-06-16T10:12:01Z WARN codex_core_skills::loader: ignoring interface.icon_small\n"),
		ExitCode: 1,
	}, providers.BuildRequest{Target: target})
	if result.OK || result.Status != contract.StatusQuota || result.FailureClass == nil || *result.FailureClass != contract.FailureQuota {
		t.Fatalf("result=%+v", result)
	}
	if result.NextAction != contract.NextSwitchAccount {
		t.Fatalf("next_action=%q", result.NextAction)
	}
	if result.Stderr == "" || result.Stderr == "2026-06-16T10:12:01Z WARN codex_core_skills::loader: ignoring interface.icon_small\n" {
		t.Fatalf("stderr was not cleaned: %q", result.Stderr)
	}
}
