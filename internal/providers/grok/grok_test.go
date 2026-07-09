package grok

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

func TestBuildInlinePrompt(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"},
		CWD:    "/tmp/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "--output-format", "json", "--always-approve", "--cwd", "/tmp/project", "--model", "grok-4.5", "--single", "hello"}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%#v want=%#v", spec.Args, want)
	}
}

func TestBuildPromptFileDoesNotLeakPromptToArgv(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("secret prompt body"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := (Provider{}).Build(providers.BuildRequest{
		PromptFile: promptFile,
		Target:     routing.DispatchTarget{Requested: "grok-fast", Provider: "grok", Model: "grok-composer-2.5-fast"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if strings.Contains(joined, "secret prompt body") {
		t.Fatalf("prompt body leaked into argv: %#v", spec.Args)
	}
	if !containsArgPair(spec.Args, "--prompt-file", promptFile) {
		t.Fatalf("missing prompt-file arg: %#v", spec.Args)
	}
}

func TestBuildResume(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:    "continue",
		SessionID: "session-1",
		Target:    routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(spec.Args, "--resume", "session-1") {
		t.Fatalf("missing resume arg: %#v", spec.Args)
	}
}

func TestBuildProviderOptions(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"},
		ProviderOptions: map[string]string{
			"approval":   "default",
			"max-turns":  "1",
			"effort":     "low",
			"web-search": "off",
			"subagents":  "off",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{"--max-turns\x001", "--reasoning-effort\x00low", "--disable-web-search", "--no-subagents"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in args=%#v", want, spec.Args)
		}
	}
	if strings.Contains(joined, "--always-approve") {
		t.Fatalf("approval=default should not add --always-approve: %#v", spec.Args)
	}
}

func TestBuildForwardsProxyEnv(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	t.Setenv("HTTP_PROXY", "http://proxy.example.test:8080")
	t.Setenv("GROK_CLI_PROXY", "socks5://grok-proxy.example.test:1080")
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsEnv(spec.Env, "HTTP_PROXY=http://proxy.example.test:8080") {
		t.Fatalf("missing HTTP_PROXY in env: %#v", spec.Env)
	}
	if !containsEnv(spec.Env, "GROK_CLI_PROXY=socks5://grok-proxy.example.test:1080") {
		t.Fatalf("missing GROK_CLI_PROXY in env: %#v", spec.Env)
	}
}

func TestBuildRejectsBadProviderOption(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	_, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:          "hello",
		ProviderOptions: map[string]string{"max-turns": "0"},
	})
	if err == nil || !strings.Contains(err.Error(), "grok.max-turns") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildRejectsOpenRouterModel(t *testing.T) {
	bin := writeFakeGrok(t)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	_, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "openrouter/x-ai/grok-4.5"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot run OpenRouter") {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildMissingBinaryIsConfigFailureShape(t *testing.T) {
	t.Setenv("AI_DISPATCH_GROK_BIN", filepath.Join(t.TempDir(), "missing-grok"))
	_, err := (Provider{}).Build(providers.BuildRequest{})
	if err == nil {
		t.Fatal("expected missing binary error")
	}
	if !strings.Contains(err.Error(), "AI_DISPATCH_GROK_BIN override binary not found") || strings.Contains(err.Error(), "missing-grok") {
		t.Fatalf("unexpected error: %q", err.Error())
	}
}

func TestParseSuccessJSON(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Stdout:     []byte(`{"text":"OK","sessionId":"s1","requestId":"r1"}`),
		Stderr:     []byte("non fatal warning"),
		ExitCode:   0,
		DurationMS: 12,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"}})
	if !result.OK || result.Text != "OK" || result.SessionID != "s1" || result.ProviderUsed != "grok" {
		t.Fatalf("result=%+v", result)
	}
	if result.Stderr != "" || len(result.Warnings) != 1 {
		t.Fatalf("success should suppress stderr into warning: %+v", result)
	}
}

func TestParseMalformedJSONIsRuntimeFailure(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Stdout:     []byte("plain text despite json flag"),
		ExitCode:   0,
		DurationMS: 12,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureRuntime {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(strings.ToLower(result.Stderr), "malformed json") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseJSONMissingTextIsRuntimeFailure(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Stdout:     []byte(`{"sessionId":"s1"}`),
		ExitCode:   0,
		DurationMS: 12,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureRuntime {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "stdout_events=1") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseTimeout(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		TimedOut:        true,
		ActivityTimeout: true,
		ExitCode:        124,
	}, providers.BuildRequest{
		ActivityTimeoutSeconds: 7,
		Target:                 routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-4.5"},
	})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureTimeout {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "without provider activity") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseModelFailureIsConfig(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Stderr:   []byte("unknown model: grok-missing"),
		ExitCode: 1,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "grok", Provider: "grok", Model: "grok-missing"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("result=%+v", result)
	}
}

func TestRedactGrokDiagnostics(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	input := "\x1b[31murl=http://localhost/mcp?sc_token=secret123 path=" + home + "/.cursor/hooks.json\x1b[0m"
	got := redactGrokDiagnostics(input)
	if strings.Contains(got, "secret123") || strings.Contains(got, home) || strings.Contains(got, "\x1b") {
		t.Fatalf("not redacted: %q", got)
	}
}

func writeFakeGrok(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "grok")
	if err := os.WriteFile(path, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsArgPair(args []string, key string, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsEnv(env []string, want string) bool {
	for _, item := range env {
		if item == want {
			return true
		}
	}
	return false
}
