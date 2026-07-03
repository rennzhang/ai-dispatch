package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestBuildCodexArgs(t *testing.T) {
	target := routing.DispatchTarget{Requested: "gpt5.5", Provider: "codex", Model: "gpt-5.5"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "exec", "--dangerously-bypass-approvals-and-sandbox", "--json", "-c", `model_reasoning_effort="high"`, "--model", "gpt-5.5", "hello"}
	if len(spec.Args) != len(want) {
		t.Fatalf("args=%#v", spec.Args)
	}
	for i := range want {
		if spec.Args[i] != want[i] {
			t.Fatalf("args=%#v", spec.Args)
		}
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
