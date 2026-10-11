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

func TestBuildCodexArgsFastUsesPriorityServiceTier(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.6-luna", Provider: "codex", Model: "gpt-5.6-luna"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Effort: contract.EffortMax, Fast: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--json", "-c", `model_reasoning_effort="max"`, "-c", `service_tier="priority"`, "--model", "gpt-5.6-luna", "hello"}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%#v want=%#v", spec.Args, want)
	}
}

func TestResolveCodexEffort(t *testing.T) {
	p := Provider{}
	declared := []string{"low", "medium", "high", "xhigh", "max"}
	auto := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6", Requested: contract.EffortAuto, DeclaredEfforts: declared})
	if auto.Applied != contract.EffortAuto || auto.Fallback {
		t.Fatalf("auto=%+v", auto)
	}
	exact := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6", Requested: contract.EffortXHigh, DeclaredEfforts: declared})
	if exact.Applied != contract.EffortXHigh || exact.Fallback {
		t.Fatalf("exact=%+v", exact)
	}
	for _, model := range []string{"gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		for _, level := range []contract.Effort{contract.EffortLow, contract.EffortMedium, contract.EffortHigh, contract.EffortXHigh, contract.EffortMax} {
			got := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: model, Requested: level, DeclaredEfforts: declared})
			if got.Applied != level || got.Fallback {
				t.Fatalf("model %s exact %s=%+v", model, level, got)
			}
		}
	}
	undeclared := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6-sol", Requested: contract.EffortHigh})
	if undeclared.Applied != contract.EffortAuto || !undeclared.Fallback || !strings.Contains(undeclared.Reason, "not declared in config") {
		t.Fatalf("undeclared must fall back: %+v", undeclared)
	}
	outside := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "gpt-5.6-sol", Requested: contract.EffortNone, DeclaredEfforts: declared})
	if outside.Applied != contract.EffortAuto || !outside.Fallback {
		t.Fatalf("outside list must fall back: %+v", outside)
	}
	if got := SuggestEffortLevels("gpt-5.6-sol"); len(got) == 0 {
		t.Fatal("suggest hint should still list known gpt-5.6 efforts")
	}
}

func TestResolveCodexFast(t *testing.T) {
	p := Provider{}
	yes := true
	for _, model := range []string{"gpt-5.5", "gpt-5.6-sol", "gpt-5.6-terra", "gpt-5.6-luna"} {
		got := p.ResolveFast(context.Background(), providers.FastRequest{Model: model, Requested: true, DeclaredFast: &yes})
		if !got.Applied || got.Fallback {
			t.Fatalf("model %s fast=%+v", model, got)
		}
	}
	undeclared := p.ResolveFast(context.Background(), providers.FastRequest{Model: "gpt-5.6-sol", Requested: true})
	if undeclared.Applied || !undeclared.Fallback || !strings.Contains(undeclared.Reason, "does not declare fast") {
		t.Fatalf("undeclared=%+v", undeclared)
	}
	if !SuggestFast("gpt-5.6-luna") {
		t.Fatal("suggest hint should still mark known gpt-5.6 fast")
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
