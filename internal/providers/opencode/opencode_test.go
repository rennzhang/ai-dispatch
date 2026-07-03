package opencode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestBuildOpenCodeArgs(t *testing.T) {
	bin := fakeOpenCodeBin(t)
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "run", "--format", "json", "--title", "ai-dispatch", "--pure", "--auto", "--model", "openrouter/x", "hello"}
	if len(spec.Args) != len(want) {
		t.Fatalf("args=%#v", spec.Args)
	}
	for i := range want {
		if spec.Args[i] != want[i] {
			t.Fatalf("args=%#v", spec.Args)
		}
	}
}

func TestBuildOpenCodeArgsSupportsDefaultFormat(t *testing.T) {
	bin := fakeOpenCodeBin(t)
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"format": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "run", "--format", "default", "--title", "ai-dispatch", "--pure", "--auto", "--model", "openrouter/x", "hello"}
	if len(spec.Args) != len(want) {
		t.Fatalf("args=%#v", spec.Args)
	}
	for i := range want {
		if spec.Args[i] != want[i] {
			t.Fatalf("args=%#v", spec.Args)
		}
	}
}

func TestBuildOpenCodePromptFileUsesAttachment(t *testing.T) {
	fakeOpenCodeBin(t)
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("prompt from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := Provider{}.Build(providers.BuildRequest{PromptFile: promptFile, Target: target})
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range spec.Args {
		if arg == "prompt from file" {
			t.Fatalf("prompt file content leaked into opencode args: %#v", spec.Args)
		}
	}
	joined := strings.Join(spec.Args, "\x00")
	if !strings.Contains(joined, "--file\x00"+promptFile) {
		t.Fatalf("prompt file not attached: %#v", spec.Args)
	}
}

func TestBuildOpenCodeUsesUserInstallPathWhenPATHMisses(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".opencode", "bin", "opencode")
	if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_OPENCODE_BIN", "")
	t.Setenv("HOME", home)
	t.Setenv("PATH", t.TempDir())

	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Args[0] != bin {
		t.Fatalf("bin=%q args=%#v", bin, spec.Args)
	}
}

func TestBuildOpenCodeFailsBeforeRunWhenBinaryMissing(t *testing.T) {
	t.Setenv("AI_DISPATCH_OPENCODE_BIN", "")
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PATH", t.TempDir())
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	_, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err == nil || !strings.Contains(err.Error(), "opencode binary not found") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildOpenCodeOverrideFailureDoesNotLeakPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "private", "opencode")
	t.Setenv("AI_DISPATCH_OPENCODE_BIN", missing)
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	_, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "AI_DISPATCH_OPENCODE_BIN override") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseOpenCodeStream(t *testing.T) {
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:     []byte("{\"type\":\"text\",\"sessionID\":\"s1\",\"part\":{\"text\":\"hello\"}}\n{\"type\":\"completed\",\"sessionID\":\"s1\"}\n"),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target})
	if !result.OK || result.Text != "hello" || result.SessionID != "s1" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseOpenCodeNoResultIncludesEventSummary(t *testing.T) {
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:   []byte("{\"type\":\"session.updated\",\"sessionID\":\"s1\"}\n{\"type\":\"tool.finished\",\"sessionID\":\"s1\"}\n"),
		ExitCode: 0,
	}, providers.BuildRequest{Target: target})
	if result.OK {
		t.Fatalf("result=%+v", result)
	}
	for _, want := range []string{"stdout_events=2", "last_event=tool.finished", "exit_code=0"} {
		if !strings.Contains(result.Stderr, want) {
			t.Fatalf("stderr=%q missing %q", result.Stderr, want)
		}
	}
}

func fakeOpenCodeBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "opencode")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_OPENCODE_BIN", bin)
	return bin
}

func TestParseOpenCodeStreamMessagePartEvents(t *testing.T) {
	target := routing.DispatchTarget{Requested: "openrouter/x", Provider: "opencode", Model: "openrouter/x"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout: []byte(
			"{\"type\":\"session.updated\",\"sessionID\":\"s2\"}\n" +
				"{\"type\":\"message.part.delta\",\"sessionID\":\"s2\",\"part\":{\"type\":\"text\",\"delta\":{\"text\":\"hel\"}}}\n" +
				"{\"type\":\"message.part.delta\",\"sessionID\":\"s2\",\"part\":{\"type\":\"text\",\"text\":\"lo\"}}\n",
		),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target})
	if !result.OK || result.Text != "hello" || result.SessionID != "s2" {
		t.Fatalf("result=%+v", result)
	}
}
