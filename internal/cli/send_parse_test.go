package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptFileIsValidatedBeforeProviderExecution(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing.md")
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "--prompt-file", missing, "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["next_action"] != "fix_input" {
		t.Fatalf("payload=%v", payload)
	}
	joined := stdout.String() + stderr.String()
	if strings.Contains(joined, missing) {
		t.Fatalf("prompt-file absolute path leaked into output: %s", joined)
	}
}

func TestCwdIsValidatedWithoutLeakingAbsolutePath(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	missing := filepath.Join(t.TempDir(), "missing-dir")
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "--cwd", missing, "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	joined := stdout.String() + stderr.String()
	if strings.Contains(joined, missing) {
		t.Fatalf("cwd absolute path leaked into output: %s", joined)
	}
}

func TestPromptFileCanExceedInlinePromptLimit(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 25_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "--prompt-file", path, "--json-result"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "disabled" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestStdinPromptIsAccepted(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := MainWithInput([]string{"send", "codex", "--json-result", "--wrap-up"}, &stdout, &stderr, strings.NewReader("hello from stdin"))
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "disabled" || payload["provider_used"] != "codex" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestSendJSONResultPrintsUserFacingNoticeAndKeepsStdoutPure(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--wrap-up"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "[ai-dispatch") || !strings.Contains(summary, "codex") {
		t.Fatalf("payload=%v", payload)
	}
	hint, _ := payload["agent_hint"].(string)
	if !strings.Contains(hint, "You MUST paste the dispatch result user_facing_summary") {
		t.Fatalf("missing agent_hint: %v", payload)
	}
	if !strings.Contains(stderr.String(), summary) || !strings.Contains(stderr.String(), "You MUST paste the dispatch result user_facing_summary into the final reply to the user verbatim") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestSendParseErrorJSONIncludesUserFacingSummary(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "--json-result", "--wrap-up"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "Failed: input error") {
		t.Fatalf("payload=%v", payload)
	}
	if !strings.Contains(stderr.String(), "ai-dispatch") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestModelsResolveJSONDoesNotPrintUserFacingNotice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"models", "resolve", "not-a-real-model-xyz", "--format", "json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected resolve failure, stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String()+stderr.String(), "[ai-dispatch") {
		t.Fatalf("models resolve should not print user-facing wrap-up: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
}

func TestInvalidTimeoutWithJSONResultStillWritesJSON(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--timeout", "nope", "--json-result", "--wrap-up"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout should stay JSON: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "Failed: input error") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestStreamProgressDoesNotPrintPlaintextNotice(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--stream-progress", "--wrap-up"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stream-progress stderr should stay NDJSON, got %s", stderr.String())
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "[ai-dispatch") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestDefaultActivityTimeoutIsDisabledForAllProviders(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"mimo-openrouter-pro", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 0 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
	}
	req, _, err = parseSend("send", []string{"gpt5.5", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 0 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
	}
	req, _, err = parseSend("send", []string{"--activity-timeout", "0", "mimo-openrouter-pro", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 0 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
	}
}

func TestDefaultFixedTimeoutIsSet(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"gpt5.5", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.TimeoutSeconds != 7200 {
		t.Fatalf("timeout=%d", req.TimeoutSeconds)
	}
	req, _, err = parseSend("send", []string{"--timeout", "0", "gpt5.5", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.TimeoutSeconds != 0 {
		t.Fatalf("timeout=%d", req.TimeoutSeconds)
	}
}

func TestInvalidProviderOptFails(t *testing.T) {
	var stderr bytes.Buffer
	_, _, err := parseSend("send", []string{"gpt5.5", "hello", "--provider-opt", "claude.transprot=pty"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider option") {
		t.Fatalf("err=%v", err)
	}
}

func TestCursorProviderOptWhitelist(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"cursor-fable5", "hello", "--provider-opt", "cursor.approval=always"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ProviderOpts == nil || req.ProviderOpts["cursor"]["approval"] != "always" {
		t.Fatalf("req=%+v", req)
	}

	_, _, err = parseSend("send", []string{"cursor-fable5", "hello", "--provider-opt", "cursor.effort=high"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider option") {
		t.Fatalf("err=%v", err)
	}
}

func TestUnknownInterspersedFlagFails(t *testing.T) {
	var stderr bytes.Buffer
	_, _, err := parseSend("send", []string{"gpt5.5", "hello", "--provder-opt", "claude.transport=pty"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unknown flag: --provder-opt") {
		t.Fatalf("err=%v", err)
	}
}

func TestExplicitDoubleDashAllowsDashPrefixedPrompt(t *testing.T) {
	var stderr bytes.Buffer
	req, jsonResult, err := parseSend("send", []string{"gpt5.5", "--", "--fallback"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.Target != "gpt5.5" || req.Prompt != "--fallback" || jsonResult {
		t.Fatalf("req=%+v json=%v", req, jsonResult)
	}
}

func TestValidInterspersedFlagsStillParse(t *testing.T) {
	var stderr bytes.Buffer
	req, jsonResult, err := parseSend("send", []string{"gpt5.5", "hello", "--timeout", "7", "--json-result"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.Target != "gpt5.5" || req.Prompt != "hello" || req.TimeoutSeconds != 7 || !jsonResult {
		t.Fatalf("req=%+v json=%v", req, jsonResult)
	}
}

func TestGrokProviderOptsParse(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{
		"grok", "hello",
		"--provider-opt", "grok.max-turns=1",
		"--provider-opt", "grok.web-search=off",
	}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ProviderOpts["grok"]["max-turns"] != "1" || req.ProviderOpts["grok"]["web-search"] != "off" {
		t.Fatalf("provider opts=%v", req.ProviderOpts)
	}
}

func TestEffortParseOmittedIsAuto(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"gpt5.5", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.Effort != "auto" {
		t.Fatalf("effort=%q", req.Effort)
	}
	req, _, err = parseSend("send", []string{"gpt5.5", "hello", "--effort", "auto"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.Effort != "auto" {
		t.Fatalf("explicit auto effort=%q", req.Effort)
	}
}

func TestEffortParseAllLevels(t *testing.T) {
	var stderr bytes.Buffer
	for _, level := range []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"} {
		req, _, err := parseSend("send", []string{"gpt5.5", "hello", "--effort", level}, &stderr)
		if err != nil {
			t.Fatalf("level %s err=%v", level, err)
		}
		if string(req.Effort) != level {
			t.Fatalf("level %s effort=%q", level, req.Effort)
		}
	}
}

func TestEffortParseRejectsInvalid(t *testing.T) {
	var stderr bytes.Buffer
	_, _, err := parseSend("send", []string{"gpt5.5", "hello", "--effort", "ultra"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "unsupported effort") {
		t.Fatalf("err=%v", err)
	}
}

func TestGrokEffortProviderOptMigrationError(t *testing.T) {
	var stderr bytes.Buffer
	_, _, err := parseSend("send", []string{"grok", "hello", "--provider-opt", "grok.effort=low"}, &stderr)
	if err == nil || err.Error() != "grok.effort was removed; use --effort" {
		t.Fatalf("err=%v", err)
	}
}

func TestEffortInterspersedFlag(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"gpt5.5", "hello", "--effort", "high", "--json-result"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.Effort != "high" || req.Target != "gpt5.5" || req.Prompt != "hello" {
		t.Fatalf("req=%+v", req)
	}
}

func TestFastInterspersedFlag(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"gpt5.6-luna", "hello", "--fast", "--json-result"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if !req.Fast || req.Target != "gpt5.6-luna" || req.Prompt != "hello" {
		t.Fatalf("req=%+v", req)
	}
}

func TestCWDValidation(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "hi", "--cwd", t.TempDir() + "/missing", "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(payload["stderr"].(string), "--cwd") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestRejectsAmbiguousDashPrompt(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "-", "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "ambiguous") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestExplicitPromptIgnoresInheritedStdin(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := MainWithInput([]string{"send", "codex", "hello", "--json-result"}, &stdout, &stderr, strings.NewReader("stdin"))
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "disabled" || payload["provider_used"] != "codex" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestDefaultOmitsUserFacingSummary(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["user_facing_summary"]; ok {
		t.Fatalf("default should omit wrap-up: %v", payload)
	}
	if _, ok := payload["agent_hint"]; ok {
		t.Fatalf("default should omit agent_hint: %v", payload)
	}
}

func TestNoWrapUpFlagDisablesUserFacingSummary(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--no-wrap-up"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["user_facing_summary"]; ok {
		t.Fatalf("wrap-up should be omitted: %v", payload)
	}
	if _, ok := payload["agent_hint"]; ok {
		t.Fatalf("agent_hint should be omitted: %v", payload)
	}
	if strings.Contains(stderr.String(), "[ai-dispatch") || strings.Contains(stderr.String(), "You MUST paste") {
		t.Fatalf("stderr leaked wrap-up: %s", stderr.String())
	}
}

func TestConfigDisablesUserFacingSummary(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"claude_transport":"print","user_facing_summary":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", path)
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["user_facing_summary"]; ok {
		t.Fatalf("wrap-up should be omitted: %v", payload)
	}
}

func TestConfigEnablesUserFacingSummary(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"claude_transport":"print","user_facing_summary":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", path)
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "[ai-dispatch") {
		t.Fatalf("payload=%v", payload)
	}
	hint, _ := payload["agent_hint"].(string)
	if !strings.Contains(hint, "You MUST paste the dispatch result user_facing_summary") {
		t.Fatalf("missing agent_hint: %v", payload)
	}
}

func TestWrapUpFlagOverridesConfigOff(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"claude_transport":"print","user_facing_summary":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", path)
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--wrap-up"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "[ai-dispatch") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestWrapUpAndNoWrapUpConflict(t *testing.T) {
	var stderr bytes.Buffer
	_, _, err := parseSend("send", []string{"codex", "hello", "--wrap-up", "--no-wrap-up"}, &stderr)
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("err=%v", err)
	}
}

func TestNoWrapUpOverridesConfigTrue(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"claude_transport":"print","user_facing_summary":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", path)
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--no-wrap-up"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["user_facing_summary"]; ok {
		t.Fatalf("--no-wrap-up should omit wrap-up: %v", payload)
	}
	if strings.Contains(stderr.String(), "[ai-dispatch") || strings.Contains(stderr.String(), "You MUST paste") {
		t.Fatalf("stderr leaked wrap-up: %s", stderr.String())
	}
}

func TestStreamProgressInputErrorKeepsStderrNDJSON(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "--json-result", "--stream-progress", "--wrap-up"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stream-progress stderr should stay NDJSON, got %s", stderr.String())
		}
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	summary, _ := payload["user_facing_summary"].(string)
	if !strings.Contains(summary, "Failed: input error") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestStreamProgressFirstRunKeepsStderrNDJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("AI_DISPATCH_PREFERENCES", filepath.Join(home, "preferences.md"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", filepath.Join(home, "runs"))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--json-result", "--stream-progress", "--wrap-up"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected provider failure, stdout=%s", stdout.String())
	}
	if strings.Contains(stderr.String(), "首次调用") || strings.Contains(stderr.String(), "配置初始化完成") {
		t.Fatalf("first-run plaintext leaked into stream-progress stderr: %s", stderr.String())
	}
	for _, line := range strings.Split(strings.TrimSpace(stderr.String()), "\n") {
		if line == "" {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stream-progress stderr should stay NDJSON, got %s", stderr.String())
		}
	}
}
