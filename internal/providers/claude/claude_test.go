package claude

import (
	"strings"
	"testing"
	"time"

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
		"--stream",
		"--transport\x00tmux",
		"--cwd\x00/tmp/work",
		"--timeout\x0040",
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

func containsEnv(env []string, value string) bool {
	for _, item := range env {
		if item == value {
			return true
		}
	}
	return false
}
