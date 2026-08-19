package cursor

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

func TestBuildInlinePrompt(t *testing.T) {
	bin := writeFakeCursor(t)
	t.Setenv("AI_DISPATCH_CURSOR_BIN", bin)
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
		CWD:    "/tmp/project",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{bin, "--print", "--output-format", "stream-json", "--trust", "--model", "claude-fable-5-thinking-high", "--workspace", "/tmp/project"}
	if strings.Join(spec.Args, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("args=%#v want=%#v", spec.Args, want)
	}
	if string(spec.Stdin) != "hello" {
		t.Fatalf("stdin=%q want hello", string(spec.Stdin))
	}
}

func TestBuildApprovalOptions(t *testing.T) {
	bin := writeFakeCursor(t)
	t.Setenv("AI_DISPATCH_CURSOR_BIN", bin)
	target := routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"}

	always, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"approval": "always"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArg(always.Args, "--force") {
		t.Fatalf("approval=always missing --force: %#v", always.Args)
	}

	def, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"approval": "default"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if containsArg(def.Args, "--force") {
		t.Fatalf("approval=default must not add --force: %#v", def.Args)
	}

	bad, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"approval": "bogus"},
	})
	if err == nil || !strings.Contains(err.Error(), "cursor.approval must be always or default") {
		t.Fatalf("bogus approval err=%v", err)
	}
	if bad.Args != nil {
		t.Fatalf("bogus approval should return no command: %#v", bad.Args)
	}
}

func TestBuildPromptFileErrorDoesNotLeakPath(t *testing.T) {
	bin := writeFakeCursor(t)
	t.Setenv("AI_DISPATCH_CURSOR_BIN", bin)
	missing := filepath.Join(t.TempDir(), "no-such-prompt.txt")
	_, err := (Provider{}).Build(providers.BuildRequest{
		PromptFile: missing,
		Target:     routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), missing) || strings.Contains(err.Error(), t.TempDir()) {
		t.Fatalf("prompt file error leaked absolute path: %v", err)
	}
}

func TestBuildPromptFileDoesNotLeakPromptToArgv(t *testing.T) {
	bin := writeFakeCursor(t)
	t.Setenv("AI_DISPATCH_CURSOR_BIN", bin)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("secret prompt body"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := (Provider{}).Build(providers.BuildRequest{
		PromptFile: promptFile,
		Target:     routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if strings.Contains(joined, "secret prompt body") {
		t.Fatalf("prompt body leaked into argv: %#v", spec.Args)
	}
	if string(spec.Stdin) != "secret prompt body" {
		t.Fatalf("stdin=%q want prompt body", string(spec.Stdin))
	}
}

func TestBuildResume(t *testing.T) {
	bin := writeFakeCursor(t)
	t.Setenv("AI_DISPATCH_CURSOR_BIN", bin)
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt:    "continue",
		SessionID: "session-1",
		Target:    routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(spec.Args, "--resume", "session-1") {
		t.Fatalf("missing resume arg: %#v", spec.Args)
	}
}

func TestBuildBinaryMissing(t *testing.T) {
	t.Setenv("AI_DISPATCH_CURSOR_BIN", filepath.Join(t.TempDir(), "missing-cursor-agent"))
	_, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "binary not found") {
		t.Fatalf("err=%v want binary not found", err)
	}
}

func TestResolveCursorEffort(t *testing.T) {
	p := Provider{}
	auto := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "claude-fable-5-thinking-high", Requested: contract.EffortAuto})
	if auto.Applied != contract.EffortAuto || auto.Fallback {
		t.Fatalf("auto=%+v", auto)
	}
	explicit := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "claude-fable-5-thinking-high", Requested: contract.EffortHigh})
	if !explicit.Fallback || explicit.Applied != contract.EffortAuto {
		t.Fatalf("explicit=%+v", explicit)
	}
}

func TestResolveCursorFast(t *testing.T) {
	p := Provider{}
	res := p.ResolveFast(context.Background(), providers.FastRequest{Model: "claude-fable-5-thinking-high", Requested: true})
	if !res.Fallback || res.Applied {
		t.Fatalf("fast=%+v", res)
	}
}

func TestParseSuccess(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout: []byte(strings.Join([]string{
			`{"type":"system","subtype":"init","session_id":"abc-123","model":"claude-fable-5-thinking-high"}`,
			`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"O"}]},"session_id":"abc-123"}`,
			`{"type":"tool_call","subtype":"started","tool_call":{"readToolCall":{"args":{"path":"README.md"}}},"session_id":"abc-123"}`,
			`{"type":"result","subtype":"success","is_error":false,"duration_ms":12042,"result":"OK","session_id":"abc-123","usage":{"inputTokens":10,"outputTokens":2,"cacheReadTokens":5,"cacheWriteTokens":0}}`,
		}, "\n") + "\n"),
		ExitCode:   0,
		DurationMS: 12042,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if !result.OK || result.Status != contract.StatusSuccess {
		t.Fatalf("result=%+v", result)
	}
	if result.Text != "OK" {
		t.Fatalf("text=%q", result.Text)
	}
	if result.SessionID != "abc-123" {
		t.Fatalf("session=%q", result.SessionID)
	}
	if result.ModelUsed != "claude-fable-5-thinking-high" {
		t.Fatalf("model=%q", result.ModelUsed)
	}
	if result.Usage == nil || result.Usage.InputTokens != 10 || result.Usage.OutputTokens != 2 {
		t.Fatalf("usage=%+v", result.Usage)
	}
}

func TestParseModelFromResultOverridesRequest(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   []byte(`{"type":"result","subtype":"success","is_error":false,"duration_ms":1,"result":"OK","model":"claude-fable-5-max","session_id":"abc-123"}` + "\n"),
		ExitCode: 0,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if !result.OK {
		t.Fatalf("result=%+v", result)
	}
	if result.ModelUsed != "claude-fable-5-max" {
		t.Fatalf("model=%q want result model", result.ModelUsed)
	}
}

func TestParseErrorClassification(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   []byte(`{"type":"result","subtype":"error","is_error":true,"result":"No models available for this account."}` + "\n"),
		Stderr:   []byte(""),
		ExitCode: 1,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}
	if result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("failure=%v want config", result.FailureClass)
	}
}

func TestParseIsErrorTextPromotedToStderr(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   []byte(`{"type":"result","subtype":"error","is_error":true,"result":"Workspace Trust Required. Please run interactively."}` + "\n"),
		Stderr:   []byte(""),
		ExitCode: 1,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}
	if !strings.Contains(result.Stderr, "Workspace Trust Required") {
		t.Fatalf("stderr=%q want promoted error text", result.Stderr)
	}
}

func TestParseStderrRedactsWorkspace(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   nil,
		Stderr:   []byte("Error reading /Volumes/secret/project/.cursor: denied"),
		ExitCode: 1,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
		CWD:    "/Volumes/secret/project",
	})
	if strings.Contains(result.Stderr, "/Volumes/secret/project") {
		t.Fatalf("stderr leaked workspace path: %q", result.Stderr)
	}
	if !strings.Contains(result.Stderr, "<workspace>") {
		t.Fatalf("stderr=%q want <workspace> placeholder", result.Stderr)
	}
}

func TestParseJSONErrorRedactsWorkspace(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   []byte(`{"type":"result","subtype":"error","is_error":true,"result":"Error reading /Volumes/secret/project/.cursor: denied"}` + "\n"),
		Stderr:   []byte(""),
		ExitCode: 1,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
		CWD:    "/Volumes/secret/project",
	})
	if strings.Contains(result.Stderr, "/Volumes/secret/project") {
		t.Fatalf("JSON error stderr leaked workspace path: %q", result.Stderr)
	}
	if strings.Contains(result.Text, "/Volumes/secret/project") {
		t.Fatalf("JSON error text leaked workspace path: %q", result.Text)
	}
	if !strings.Contains(result.Stderr, "<workspace>") {
		t.Fatalf("stderr=%q want <workspace> placeholder", result.Stderr)
	}
}

func TestParseTimeout(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:       nil,
		Stderr:       nil,
		ExitCode:     124,
		TimedOut:     true,
		FixedTimeout: true,
		DurationMS:   30000,
	}, providers.BuildRequest{
		Target:                 routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
		TimeoutSeconds:         30,
		ActivityTimeoutSeconds: 0,
	})
	if result.OK {
		t.Fatalf("expected timeout: %+v", result)
	}
	if result.FailureClass == nil || *result.FailureClass != contract.FailureTimeout {
		t.Fatalf("failure=%v want timeout", result.FailureClass)
	}
}

func TestParseEmptyNoResult(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   []byte(`{"type":"result","subtype":"success","is_error":false,"result":""}` + "\n"),
		ExitCode: 0,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}
	if !strings.Contains(result.Stderr, "Cursor returned no successful result") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseStderrFailureShape(t *testing.T) {
	p := Provider{}
	result := p.Parse(runtime.RunResult{
		Stdout:   nil,
		Stderr:   []byte("Error: model claude-fable-5-unknown is not available for your account"),
		ExitCode: 1,
	}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "cursor-fable5", Provider: "cursor", Model: "claude-fable-5-thinking-high"},
	})
	if result.OK {
		t.Fatalf("expected failure: %+v", result)
	}
	if result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("failure=%v want config", result.FailureClass)
	}
	if !strings.Contains(result.Stderr, "not available for your account") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func writeFakeCursor(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "cursor-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return bin
}

func containsArgPair(args []string, key string, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func containsArg(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
