package claude

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestBuildClaudeAPIArgs(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude-sonnet", Provider: "claude", Model: "sonnet"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		SessionID:       "s1",
		ProviderOptions: map[string]string{"transport": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := []string{"claude", "-p", "--setting-sources", "user,project", "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose", "--resume", "s1", "--model", "sonnet", "hello"}
	if len(spec.Args) != len(wantPrefix) {
		t.Fatalf("args=%#v", spec.Args)
	}
	for i := range wantPrefix {
		if spec.Args[i] != wantPrefix[i] {
			t.Fatalf("args=%#v", spec.Args)
		}
	}
	if len(spec.Stdin) != 0 {
		t.Fatalf("stdin=%q", spec.Stdin)
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "--effort") {
		t.Fatalf("auto effort must not pass --effort: %#v", spec.Args)
	}
}

func TestBuildClaudePrintAndPTYNeverPassEffort(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"}
	printSpec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		Effort:          contract.EffortLow,
		ProviderOptions: map[string]string{"transport": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(printSpec.Args, "\x00"), "--effort") {
		t.Fatalf("print must never pass --effort: %#v", printSpec.Args)
	}
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_GO_DRIVER", "go-pty-driver-test")
	ptySpec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		Effort:          contract.EffortHigh,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(ptySpec.Args, "\x00"), "--effort") {
		t.Fatalf("pty must never pass --effort: %#v", ptySpec.Args)
	}
}

func TestResolveClaudeEffortAlwaysFallsBackUntilCLIVerified(t *testing.T) {
	p := Provider{}
	auto := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "opus", Requested: contract.EffortAuto})
	if auto.Applied != contract.EffortAuto || auto.Fallback || auto.AppliedModel != "opus" {
		t.Fatalf("auto=%+v", auto)
	}
	for _, level := range []contract.Effort{
		contract.EffortNone, contract.EffortMinimal, contract.EffortLow,
		contract.EffortMedium, contract.EffortHigh, contract.EffortXHigh, contract.EffortMax,
	} {
		for _, model := range []string{"opus", "sonnet", "claude-opus-4-7", ""} {
			got := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: model, Requested: level})
			if got.Applied != contract.EffortAuto || !got.Fallback {
				t.Fatalf("model %q level %s must fall back to auto: %+v", model, level, got)
			}
			if !strings.Contains(got.Reason, "Claude Code CLI") {
				t.Fatalf("reason must mention Claude Code CLI: %q", got.Reason)
			}
			if got.AppliedModel != model {
				t.Fatalf("applied model changed: %+v", got)
			}
		}
	}
}

func TestBuildClaudeAPIPromptFileUsesStdin(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"}
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("prompt from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := Provider{}.Build(providers.BuildRequest{
		PromptFile:      promptFile,
		Target:          target,
		ProviderOptions: map[string]string{"transport": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(spec.Stdin) != "prompt from file" {
		t.Fatalf("stdin=%q", spec.Stdin)
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "prompt from file") {
		t.Fatalf("prompt leaked into args: %#v", spec.Args)
	}
}

func TestBuildClaudeDoesNotOverrideBackendModelForRegistryAlias(t *testing.T) {
	t.Setenv("ANTHROPIC_MODEL", "openrouter/anthropic/claude-opus-4")
	target := routing.DispatchTarget{
		Requested: "sonnet4.6",
		Provider:  "claude",
		Model:     "sonnet",
		Source:    "registry",
		ActualID:  "claude-sonnet-4-6",
	}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"transport": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if strings.Contains(joined, "--model") {
		t.Fatalf("registry alias should not override backend model: %#v", spec.Args)
	}
}

func TestBuildClaudeAPIArgsUsesOpusAlias(t *testing.T) {
	target := routing.DispatchTarget{Requested: "opus4.7", Provider: "claude", Model: "opus"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		ProviderOptions: map[string]string{"transport": "api"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	if !strings.Contains(joined, "--model\x00opus") {
		t.Fatalf("args=%#v", spec.Args)
	}
	if strings.Contains(joined, "claude-opus-4-7") {
		t.Fatalf("args must use opus alias, got %#v", spec.Args)
	}
}

func TestBuildClaudePTYArgs(t *testing.T) {
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_GO_DRIVER", "go-pty-driver-test")
	target := routing.DispatchTarget{Requested: "claude-sonnet", Provider: "claude", Model: "sonnet"}
	spec, err := Provider{}.Build(providers.BuildRequest{
		Prompt:          "hello",
		Target:          target,
		CWD:             "/tmp/work",
		SessionID:       "sess-1",
		TimeoutSeconds:  45,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(spec.Args, "\x00")
	for _, want := range []string{
		"go-pty-driver-test",
		"--transport\x00tmux",
		"--cwd\x00/tmp/work",
		"--timeout\x0040",
		"--session-id\x00sess-1",
		"--input\x00hello",
		"--\x00env\x00CLAUDE_SESSION_ID=sess-1\x00AI_DISPATCH_CLAUDE_SESSION_ID=sess-1\x00claude",
		"--resume\x00sess-1",
		"--model\x00sonnet",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("args missing %q: %#v", want, spec.Args)
		}
	}
	if !containsEnv(spec.Env, "CLAUDE_SESSION_ID=sess-1") ||
		!containsEnv(spec.Env, "AI_DISPATCH_CLAUDE_SESSION_ID=sess-1") {
		t.Fatalf("env=%#v", spec.Env)
	}
}

func TestBuildClaudePTYPromptFileAndUnlimitedTimeout(t *testing.T) {
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_GO_DRIVER", "go-pty-driver-test")
	promptFile := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(promptFile, []byte("prompt from file"), 0o600); err != nil {
		t.Fatal(err)
	}
	spec, err := Provider{}.Build(providers.BuildRequest{
		PromptFile:      promptFile,
		Target:          routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"},
		TimeoutSeconds:  0,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !containsArgPair(spec.Args, "--input-file", promptFile) {
		t.Fatalf("missing prompt file: %#v", spec.Args)
	}
	if !containsArgPair(spec.Args, "--timeout", "0") {
		t.Fatalf("timeout=0 must reach driver unchanged: %#v", spec.Args)
	}
	sessionIDs := argValues(spec.Args, "--session-id")
	if len(sessionIDs) != 2 || sessionIDs[0] == "" || sessionIDs[0] != sessionIDs[1] {
		t.Fatalf("generated session id must match in driver and Claude command: %#v", spec.Args)
	}
	sessionID := sessionIDs[0]
	if !strings.Contains(strings.Join(spec.Args, "\x00"), "--\x00env\x00CLAUDE_SESSION_ID="+sessionID+"\x00AI_DISPATCH_CLAUDE_SESSION_ID="+sessionID+"\x00claude") ||
		!strings.Contains(strings.Join(spec.Args, "\x00"), "--session-id\x00"+sessionID) {
		t.Fatalf("generated session id not pinned in Claude command: %#v", spec.Args)
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "prompt from file") {
		t.Fatalf("prompt leaked into argv: %#v", spec.Args)
	}
}

func TestParseClaudeResult(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude"}
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:     []byte("{\"type\":\"result\",\"session_id\":\"s1\",\"model\":\"sonnet\",\"result\":\"hello\"}\n"),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target, ProviderOptions: map[string]string{"transport": "api"}})
	if !result.OK || result.Text != "hello" || result.SessionID != "s1" || result.ModelUsed != "sonnet" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseClaudeStreamFallsBackToAssistantTextWhenResultIsEmpty(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude"}
	stdout := strings.Join([]string{
		`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"hello from assistant"}]}}`,
		`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":"final assistant text"}]}}`,
		`{"type":"result","subtype":"success","is_error":false,"session_id":"s1","model":"deepseek/deepseek-v4-pro","result":""}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:     []byte(stdout),
		ExitCode:   0,
		DurationMS: 10,
	}, providers.BuildRequest{Target: target, ProviderOptions: map[string]string{"transport": "api"}})
	if !result.OK || result.Text != "final assistant text" || result.SessionID != "s1" || result.ModelUsed != "deepseek/deepseek-v4-pro" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseClaudeAPIErrorResultPreservesDiagnosticText(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"}
	stdout := strings.Join([]string{
		`{"type":"result","subtype":"error","is_error":true,"session_id":"s1","model":"sonnet","result":"API Error: 403 The request is prohibited due to a violation of provider Terms Of Service."}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:   []byte(stdout),
		ExitCode: 1,
	}, providers.BuildRequest{Target: target, ProviderOptions: map[string]string{"transport": "api"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != "config" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "403") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseClaudeAPIErrorTextWithoutIsErrorPreservesDiagnosticText(t *testing.T) {
	target := routing.DispatchTarget{Requested: "sonnet4.6", Provider: "claude", Model: "sonnet"}
	stdout := strings.Join([]string{
		`{"type":"assistant","session_id":"s1","message":{"role":"assistant","content":[{"type":"text","text":"API Error: 403 The request is prohibited due to provider Terms Of Service. · Please run /login"}]}}`,
		`{"type":"result","subtype":"success","session_id":"s1","model":"sonnet","result":""}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{
		Stdout:   []byte(stdout),
		ExitCode: 1,
	}, providers.BuildRequest{Target: target, ProviderOptions: map[string]string{"transport": "api"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != "config" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "403") || strings.Contains(result.Stderr, "returned no successful result") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseClaudePTYDoneResult(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"}
	stdout := strings.Join([]string{
		`{"event":"session_start","session_id":"s-pty"}`,
		`{"event":"assistant_text","text":"draft"}`,
		`{"event":"done","session_id":"s-pty","duration_ms":12,"response_text":"final answer","termination_reason":"done"}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout), ExitCode: 0, DurationMS: 20}, providers.BuildRequest{
		Target:          target,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if !result.OK || result.Text != "final answer" || result.SessionID != "s-pty" || result.DurationMS != 12 {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseClaudePTYAPIErrorTextIsFailure(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"}
	stdout := strings.Join([]string{
		`{"event":"session_start","session_id":"s-pty"}`,
		`{"event":"assistant_text","text":"Please run /login · API Error: 403 The request is prohibited due to a violation of provider Terms Of Service."}`,
		`{"event":"done","session_id":"s-pty","duration_ms":12,"response_text":"Please run /login · API Error: 403 The request is prohibited due to a violation of provider Terms Of Service.","termination_reason":"done"}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout), ExitCode: 0, DurationMS: 20}, providers.BuildRequest{
		Target:          target,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if result.OK || result.Status != "error" || result.FailureClass == nil || *result.FailureClass != "config" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "403") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestParseClaudePTYInterruptedPrompt(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude"}
	stdout := `{"event":"done","termination_reason":"interrupted_prompt","duration_ms":10}` + "\n"
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout), ExitCode: 0, DurationMS: 20}, providers.BuildRequest{
		Target:          target,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if result.OK || !strings.Contains(result.Stderr, "claude.transport=print") {
		t.Fatalf("result=%+v", result)
	}
}

func TestClaudeAutoTransportUsesPrintWhenAPIEnvExists(t *testing.T) {
	for _, env := range []map[string]string{
		{"ANTHROPIC_BASE_URL": "https://openrouter.ai/api"},
		{"ANTHROPIC_MODEL": "deepseek/deepseek-v4-pro"},
		{"ANTHROPIC_API_KEY": "sk-test"},
		{"ANTHROPIC_AUTH_TOKEN": "token-test"},
	} {
		if got := defaultTransportForEnv(env); got != "print" {
			t.Fatalf("env=%v transport=%q", env, got)
		}
	}
}

func TestClaudeAutoTransportUsesPTYWithoutAPIBackend(t *testing.T) {
	for _, env := range []map[string]string{
		nil,
		{},
		{"OTHER": "value"},
	} {
		if got := defaultTransportForEnv(env); got != "pty" {
			t.Fatalf("env=%v transport=%q", env, got)
		}
	}
}

func TestClaudeExplicitTransportOverridesAuto(t *testing.T) {
	cases := map[string]string{"api": "print", "print": "print", "pty": "pty"}
	for value, want := range cases {
		req := providers.BuildRequest{ProviderOptions: map[string]string{"transport": value}}
		if got := effectiveTransport(req); got != want {
			t.Fatalf("value=%q transport=%q want=%q", value, got, want)
		}
	}
}

func TestParseClaudePTYNonZeroWithoutStderrIsActionable(t *testing.T) {
	target := routing.DispatchTarget{Requested: "claude", Provider: "claude"}
	result := Provider{}.Parse(runtime.RunResult{ExitCode: 1, DurationMS: 20}, providers.BuildRequest{
		Target:          target,
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if result.OK || !strings.Contains(result.Stderr, "tmux -V") {
		t.Fatalf("result=%+v", result)
	}
}

func TestExtractPaneAssistantTextAfterCurrentPrompt(t *testing.T) {
	pane := strings.Join([]string{
		"❯ Reply exactly: OLD",
		"⏺ OLD",
		"❯ Reply exactly: NEW",
		"⏺ NEW",
		"✻ Worked for 2s",
	}, "\n")
	if got := extractPaneAssistantText(pane, "Reply exactly: NEW"); got != "NEW" {
		t.Fatalf("got=%q", got)
	}
}

func TestExtractPaneAssistantTextIgnoresProgress(t *testing.T) {
	pane := strings.Join([]string{
		"❯ Read ./secret.txt",
		"⏺ Reading 1 file… (ctrl+o to expand)",
	}, "\n")
	if got := extractPaneAssistantText(pane, "Read ./secret.txt"); got != "" {
		t.Fatalf("got=%q", got)
	}
}

func TestExtractPaneAssistantTextTrimsThinkingFooter(t *testing.T) {
	pane := strings.Join([]string{
		"❯ Reply exactly: MARKER",
		"⏺ MARKER Thought for 1s",
	}, "\n")
	if got := extractPaneAssistantText(pane, "Reply exactly: MARKER"); got != "MARKER" {
		t.Fatalf("got=%q", got)
	}
}

func TestClaudePTYSessionTTLDefaultsAndDisable(t *testing.T) {
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "")
	if got := claudePTYSessionTTL(); got != 6*time.Hour {
		t.Fatalf("default ttl=%s", got)
	}
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "0")
	if got := claudePTYSessionTTL(); got != 0 {
		t.Fatalf("disabled ttl=%s", got)
	}
}

func TestIsClaudePTYSessionName(t *testing.T) {
	for _, name := range []string{"ai-dispatch-claude-123", "claude-pty-123"} {
		if !isClaudePTYSessionName(name) {
			t.Fatalf("expected %s to be treated as claude pty session", name)
		}
	}
	if isClaudePTYSessionName("manual-claude") {
		t.Fatalf("manual sessions must not be cleaned")
	}
}

func TestClaudePTYUnlimitedRunCleansTmuxOnCancellation(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	writeFakeTmux(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	t.Setenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS", "0")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	var stdout bytes.Buffer
	go func() {
		done <- runGoPTYDriverContext(ctx, ptyDriverConfig{
			CWD:                 root,
			Timeout:             0,
			Input:               "hello",
			SessionID:           "11111111-1111-4111-8111-111111111111",
			ClaudeBaseDir:       filepath.Join(root, ".claude"),
			StartupWait:         0,
			StartupReadyPattern: "❯",
			Command:             []string{"fake-claude"},
		}, &stdout, &bytes.Buffer{})
	}()
	waitForFileContains(t, logPath, "new-session", 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "canceled") {
			t.Fatalf("err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("PTY driver did not stop after cancellation")
	}
	waitForFileContains(t, logPath, "kill-session", 2*time.Second)
	if !strings.Contains(stdout.String(), `"event":"session_start"`) || !strings.Contains(stdout.String(), `"session_id":"11111111-1111-4111-8111-111111111111"`) {
		t.Fatalf("known session was not emitted before transcript creation: %s", stdout.String())
	}
}

func TestFindClaudeSessionFileDoesNotGuessWhenExactSessionIsMissing(t *testing.T) {
	base := t.TempDir()
	project := filepath.Join(base, "projects", strings.ReplaceAll("/tmp/work", "/", "-"))
	if err := os.MkdirAll(project, 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(project, "other-session.jsonl")
	if err := os.WriteFile(other, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findClaudeSessionFile(base, "/tmp/work", "wanted-session", time.Now().Add(-time.Minute)); got != "" {
		t.Fatalf("missing exact session must not fall back to concurrent file: %q", got)
	}
}

func writeFakeTmux(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "tmux")
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FAKE_TMUX_LOG"
state="${FAKE_TMUX_STATE:-${FAKE_TMUX_LOG}.state}"
if [ "${FAKE_TMUX_BLOCK_COMMAND:-}" = "${1:-}" ]; then
  if [ -n "${FAKE_TMUX_BLOCK_PID_FILE:-}" ]; then
    printf '%s\n' "$$" > "$FAKE_TMUX_BLOCK_PID_FILE"
  fi
  trap '' TERM
  while :; do :; done
fi
if [ "${FAKE_TMUX_FAIL_COMMAND:-}" = "${1:-}" ]; then
  printf 'forced %s failure\n' "${1:-}" >&2
  exit 42
fi
case "${1:-}" in
  list-sessions) exit 1 ;;
  new-session)
    printf 'active\n' > "$state"
    if [ "${FAKE_TMUX_BLOCK_AFTER_NEW_SESSION:-}" = "1" ]; then
      if [ -n "${FAKE_TMUX_BLOCK_PID_FILE:-}" ]; then
        printf '%s\n' "$$" > "$FAKE_TMUX_BLOCK_PID_FILE"
      fi
      trap '' TERM
      while :; do :; done
    fi
    ;;
  has-session) [ -f "$state" ] ;;
  kill-session) rm -f "$state" ;;
  capture-pane) printf '❯\n' ;;
esac
exit 0
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func waitForFileContains(t *testing.T, path string, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(path)
	t.Fatalf("%s did not contain %q: %s", path, want, data)
}

func containsEnv(env []string, value string) bool {
	for _, item := range env {
		if item == value {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key string, value string) bool {
	return argValue(args, key) == value
}

func argValue(args []string, key string) string {
	values := argValues(args, key)
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func argValues(args []string, key string) []string {
	values := []string{}
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key {
			values = append(values, args[i+1])
		}
	}
	return values
}
