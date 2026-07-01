package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

func TestDoctorJSON(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"doctor", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != true || payload["runtime"] != "go" || payload["provider_execution"] != "disabled_by_default" {
		t.Fatalf("payload=%v", payload)
	}
	if _, ok := payload["claude_env"].(map[string]any); !ok {
		t.Fatalf("missing claude_env payload=%v", payload)
	}
	if _, ok := payload["config"].(map[string]any); !ok {
		t.Fatalf("missing config payload=%v", payload)
	}
}

func TestDoctorJSONReportsEnabledProviderExecution(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	var stdout, stderr bytes.Buffer
	code := Main([]string{"doctor", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["provider_execution"] != "enabled" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestHelpFlagsExitCleanly(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	cases := [][]string{
		{"--help"},
		{"config", "--help"},
		{"send", "--help"},
		{"guide", "--help"},
		{"resume", "--help"},
		{"doctor", "--help"},
		{"models", "--help"},
		{"models", "resolve", "--help"},
		{"runs", "list", "--help"},
		{"runs", "failures", "--help"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		code := Main(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
		if strings.Contains(stderr.String(), "flag: help requested") {
			t.Fatalf("args=%v stderr=%s", args, stderr.String())
		}
	}
}

func TestInitConfigDoesNotExposeTrustedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"init", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stdout.String(), "trusted_workspace") {
		t.Fatalf("init output should not expose trusted_workspace: %s", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(home, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "trusted_workspace") {
		t.Fatalf("config should not expose trusted_workspace: %s", string(data))
	}
}

func TestConfigPathAndShow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"config", "path"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != filepath.Join(home, "config.json") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"config", "show"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"claude_transport": "print"`) || strings.Contains(stdout.String(), "trusted_workspace") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestGuideModelsPrintsBundledReference(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"guide", "models"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{"# 模型路由", "## 默认路由", "gpt5.5", "mimo", "provider_used"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in guide:\n%s", want, text)
		}
	}
}

func TestSanitizedClaudeProviderConfig(t *testing.T) {
	model, baseURL, hasAPIBackend := sanitizedClaudeProviderConfig(`{"env":{"ANTHROPIC_MODEL":"z-ai/glm-5.2","ANTHROPIC_BASE_URL":"https://openrouter.ai/api","IGNORED_CREDENTIAL_FIELD":"redacted"}}`)
	if model != "z-ai/glm-5.2" || baseURL != "https://openrouter.ai/api" || !hasAPIBackend {
		t.Fatalf("model=%q baseURL=%q hasAPIBackend=%v", model, baseURL, hasAPIBackend)
	}
}

func TestRetiredPositionalSendFailsClosed(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"gpt5.5", "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "unknown command: gpt5.5") {
		t.Fatalf("stderr=%s", stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "" {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestRetiredTransportFlagFailsClosed(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "claude", "hi", "--transport", "pty", "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["next_action"] != "fix_input" || !strings.Contains(payload["stderr"].(string), "--transport was removed") {
		t.Fatalf("payload=%v", payload)
	}
}

func TestModelsResolveJSON(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"models", "resolve", "gpt5.5"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["Provider"] != "codex" && payload["provider"] != "codex" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestModelsListsRegistryAliases(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeCLIRegistry(t))
	var stdout, stderr bytes.Buffer
	code := Main([]string{"models", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload struct {
		Targets []string `json:"targets"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"mimo-v2.5-pro", "mimo", "kimi"} {
		if !containsString(payload.Targets, target) {
			t.Fatalf("missing %q in targets=%v", target, payload.Targets)
		}
	}
}

func TestPromptFileIsValidatedBeforeProviderExecution(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "--prompt-file", t.TempDir() + "/missing.md", "--json-result"}, &stdout, &stderr)
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
}

func TestPromptFileCanExceedInlinePromptLimit(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	path := filepath.Join(t.TempDir(), "prompt.md")
	if err := os.WriteFile(path, []byte(strings.Repeat("x", 25_000)), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "gpt5.5", "--prompt-file", path, "--json-result"}, &stdout, &stderr)
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
	code := MainWithInput([]string{"send", "gpt5.5", "--json-result"}, &stdout, &stderr, strings.NewReader("hello from stdin"))
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

func TestDefaultActivityTimeoutUsesLongerOpenCodeWindow(t *testing.T) {
	var stderr bytes.Buffer
	req, _, err := parseSend("send", []string{"mimo", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 300 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
	}
	req, _, err = parseSend("send", []string{"gpt5.5", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 180 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
	}
	req, _, err = parseSend("send", []string{"--activity-timeout", "0", "mimo", "hello"}, &stderr)
	if err != nil {
		t.Fatal(err)
	}
	if req.ActivityTimeoutSeconds != 0 {
		t.Fatalf("activity_timeout=%d", req.ActivityTimeoutSeconds)
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
	code := MainWithInput([]string{"send", "gpt5.5", "hello", "--json-result"}, &stdout, &stderr, strings.NewReader("stdin"))
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

func TestRunsList(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunsListStatusFilter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	success := contract.SuccessResult("ok")
	success.RequestedTarget = "gpt5.5"
	success.ProviderUsed = "codex"
	if err := runstore.WriteResult(root, "run-success", success); err != nil {
		t.Fatal(err)
	}
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResult(root, "run-quota", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--status", "quota"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListTaskNameAndFailureClassFilters(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	success := contract.SuccessResult("ok")
	success.RequestedTarget = "gpt5.5"
	success.ProviderUsed = "codex"
	if err := runstore.WriteResultWithTask(root, "run-success", "review-r1", success); err != nil {
		t.Fatal(err)
	}
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResultWithTask(root, "run-quota", "review-r2", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--task-name", "review-r*", "--failure-class", "quota"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" || records[0]["task_name"] != "review-r2" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListAcceptsRelativeSince(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResultWithTask(root, "run-quota", "review-r2", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--since", "7d"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListSummarizesWithoutResultBody(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	result := contract.ErrorResult(contract.StatusError, contract.FailureRuntime, "SECRET STDERR BODY", 1)
	result.Text = "SECRET ASSISTANT BODY"
	result.ProviderUsed = "codex"
	result.ModelUsed = "gpt-5.5"
	result.RequestedTarget = "gpt5.5"
	if err := runstore.WriteResultWithTask(root, "run-secret", "review-r1", result); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "SECRET") || strings.Contains(stdout.String(), "\"result\"") || strings.Contains(stdout.String(), "\"text\"") || strings.Contains(stdout.String(), "\"stderr\"") {
		t.Fatalf("run list leaked full result: %s", stdout.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-secret" || records[0]["stderr_bytes"] == nil {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsFailuresSummarizesWithoutText(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	failure := contract.ErrorResult(contract.StatusError, contract.FailureRuntime, "\x1b[91mError:\x1b[0m Unexpected error database is locked", 1)
	failure.RequestedTarget = "kimi"
	failure.ProviderUsed = "opencode"
	failure.ModelUsed = "openrouter/moonshotai/kimi-k2.7-code"
	failure.Text = "SECRET ASSISTANT BODY"
	if err := runstore.WriteResultWithTask(root, "run-db", "review-r1", failure); err != nil {
		t.Fatal(err)
	}
	degraded := contract.SuccessResult("SECRET FALLBACK BODY")
	degraded.RequestedTarget = "sonnet4.6"
	degraded.ProviderUsed = "codex"
	degraded.ModelUsed = "gpt-5.5"
	degraded.Degraded = true
	degraded.DegradeReason = "claude failed with error/config; switched to codex:gpt-5.5"
	if err := runstore.WriteResultWithTask(root, "run-degraded", "review-r2", degraded); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "failures", "--since", "7d", "--limit", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "SECRET") {
		t.Fatalf("failure summary leaked result text: %s", stdout.String())
	}
	var payload struct {
		Total         int             `json:"total"`
		Returned      int             `json:"returned"`
		ByFingerprint map[string]int  `json:"by_fingerprint"`
		Records       []failureRecord `json:"records"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 2 || payload.Returned != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.ByFingerprint["database_locked"] != 1 || payload.ByFingerprint["degraded"] != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if len(payload.Records) != 1 || payload.Records[0].Fingerprint == "" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestParseSinceRelativeDurations(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got=%s", got)
	}
	got, err = parseSince("24h", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got=%s", got)
	}
}

func TestResumeRequiresExplicitSessionID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	result := contract.SuccessResult("ok")
	result.RequestedTarget = "gpt5.5"
	result.ProviderUsed = "codex"
	result.ModelUsed = "gpt-5.5"
	result.SessionID = "sid-1"
	if err := runstore.WriteResult(root, "run-1", result); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"resume", "sid-1", "continue this task", "--task-name", "resume-r2", "--json-result"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["next_action"] != "fix_input" || !strings.Contains(payload["stderr"].(string), "--session-id is required") {
		t.Fatalf("payload=%v", payload)
	}
}

func writeCLIRegistry(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.json")
	data := `{
  "models": [
    {
      "key": "mimo-v2.5-pro",
      "actualModelId": "openrouter/xiaomi/mimo-v2.5-pro",
      "provider": "opencode",
      "dispatchRunner": "opencode",
      "dispatchModel": "openrouter/xiaomi/mimo-v2.5-pro",
      "aliases": ["mimo"]
    },
    {
      "key": "kimi-k2.7-code",
      "actualModelId": "openrouter/moonshotai/kimi-k2.7-code",
      "provider": "opencode",
      "dispatchRunner": "opencode",
      "dispatchModel": "openrouter/moonshotai/kimi-k2.7-code",
      "aliases": ["kimi"]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
