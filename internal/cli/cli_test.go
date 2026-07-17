package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

// TestMain ensures tests never trigger real provider CLI calls, regardless of
// the developer's shell environment. main.go sets the default to "on" for
// production, but tests must not hit the network.
func TestMain(m *testing.M) {
	os.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "off")
	os.Exit(m.Run())
}

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
	for _, forbidden := range []string{"home", "config_path", "runs_dir"} {
		if _, ok := payload[forbidden]; ok {
			t.Fatalf("doctor should not expose %s: %v", forbidden, payload)
		}
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
		{"preferences", "--help"},
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

func TestSendHelpDoesNotInitializeConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "--help"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("send --help should not create config, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "preferences.md")); !os.IsNotExist(err) {
		t.Fatalf("send --help should not create preferences, err=%v", err)
	}
}

func TestDoctorBadConfigReturnsNonZero(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{"claude_transport":"bad"}`)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"doctor", "--format", "json"}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected non-zero code stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["ok"] != false || payload["config_error"] == nil {
		t.Fatalf("payload=%v", payload)
	}
	if strings.Contains(stdout.String(), home) {
		t.Fatalf("doctor should not expose config path: %s", stdout.String())
	}
}

func TestDoctorJSONIsCompact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("ANTHROPIC_MODEL", "private-model-name")
	t.Setenv("ANTHROPIC_BASE_URL", "https://private.example.test")
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "private-alias": [
      { "provider": "opencode", "model": "private/provider-model" }
    ]
  },
  "providers": {
    "opencode": { "available": true, "version": "1.17.3", "catalog_model_count": 42 }
  }
}`)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"doctor", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	text := stdout.String()
	for _, forbidden := range []string{home, "private-model-name", "private.example.test", "private/provider-model", "private-alias", "1.17.3"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("doctor leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"model_alias_count":1`) || !strings.Contains(text, `"catalog_model_count":42`) {
		t.Fatalf("doctor should keep compact counts: %s", text)
	}
}

func TestLegacyConfigRegistryPathFailsClosed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "registry_path": "/private/old/registry.json",
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" }
    ]
  },
  "providers": {}
}`)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"models", "resolve", "mimo-pro"}, &stdout, &stderr)
	if code == 0 || !strings.Contains(stdout.String()+stderr.String(), "models.registry_path") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRetiredCallerFlagsFailClosed(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "codex", "hello", "--caller-env", "local", "--json-result"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stdout.String()+stderr.String(), "--caller-env was removed") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestInvalidFormatFails(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"doctor", "--format", "jsn"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--format must be text or json") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
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

func TestInitCreatesPreferences(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"init", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	preferences, err := os.ReadFile(filepath.Join(home, "preferences.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# ai-dispatch 偏好", "### 代码实现"} {
		if !strings.Contains(string(preferences), want) {
			t.Fatalf("preferences missing %q: %s", want, string(preferences))
		}
	}
	for _, privateDefault := range []string{"默认代码 review：用 opus", "默认实现：用 gpt5.5", "grok：实时检索"} {
		if strings.Contains(string(preferences), privateDefault) {
			t.Fatalf("preferences should not contain user-specific default %q: %s", privateDefault, string(preferences))
		}
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

func TestPreferencesPathAndShow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"preferences", "path"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != filepath.Join(home, "preferences.md") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(home, "preferences.md")); !os.IsNotExist(err) {
		t.Fatalf("preferences path should not create file, err=%v", err)
	}

	stdout.Reset()
	stderr.Reset()
	code = Main([]string{"preferences", "show"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "### 代码实现") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestGuideModelsPrintsRuntimeRegistryGuide(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Main([]string{"guide", "models"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	text := stdout.String()
	for _, want := range []string{"# 模型指南", "## Built-in registry targets", "gpt5.5", "mimo-openrouter-pro", "provider_used"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in guide:\n%s", want, text)
		}
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

func TestModelsResolveGrokJSON(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "grok": [
      { "provider": "grok", "model": "grok-4.5" },
      { "provider": "opencode", "model": "openrouter/x-ai/grok-4.5" }
    ]
  },
  "providers": {}
}`)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"models", "resolve", "grok"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["Provider"] != "grok" && payload["provider"] != "grok" {
		t.Fatalf("payload=%v", payload)
	}
	candidates, _ := payload["candidates"].([]any)
	if len(candidates) != 2 {
		t.Fatalf("expected configured grok candidate chain, payload=%v", payload)
	}
}

func TestProvidersScanTextIncludesGrok(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "grok")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'grok 0.2.93'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	var stdout, stderr bytes.Buffer
	code := Main([]string{"providers", "scan"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "grok: available (grok 0.2.93)") {
		t.Fatalf("stdout=%s", stdout.String())
	}
}

func TestModelsListsRegistryAliases(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" }
    ]
  },
  "providers": {}
}`)
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
	for _, target := range []string{"mimo-pro", "mimo-openrouter-pro", "kimi"} {
		if !containsString(payload.Targets, target) {
			t.Fatalf("missing %q in targets=%v", target, payload.Targets)
		}
	}
	if containsString(payload.Targets, "mimo") {
		t.Fatalf("ambiguous mimo alias should not be listed: %v", payload.Targets)
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

func TestFirstRunSetupSummary(t *testing.T) {
	home := t.TempDir()
	runsRoot := filepath.Join(home, "runs")
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_RUNS_DIR", runsRoot)
	// Reset process-level state to simulate a fresh process.
	lastSetupResult = nil

	var stdout, stderr bytes.Buffer
	code := MainWithInput([]string{"send", "gpt5.5", "--json-result"}, &stdout, &stderr, strings.NewReader("hello"))
	// Provider execution is disabled in tests, so send fails — but config
	// setup and first-run injection should still occur.
	if code == 0 {
		t.Fatalf("expected non-zero exit code, got code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	// Verify setup progress on stderr. First send creates config state
	// only; provider probing is reserved for init/providers scan.
	stderrStr := stderr.String()
	for _, want := range []string{"首次调用", "配置初始化完成", "继续执行任务"} {
		if !strings.Contains(stderrStr, want) {
			t.Errorf("stderr missing %q\ngot:\n%s", want, stderrStr)
		}
	}
	if strings.Contains(stderrStr, "Provider 探测") {
		t.Errorf("first send should not scan providers\ngot:\n%s", stderrStr)
	}

	// Verify JSON result has first_run + first_run_setup fields.
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON: %v\nstdout=%s", err, stdout.String())
	}
	if payload["first_run"] != true {
		t.Errorf("first_run = %v, want true", payload["first_run"])
	}
	hint, _ := payload["first_run_hint"].(string)
	if !strings.Contains(hint, "preferences.md") {
		t.Errorf("first_run_hint = %q, want preferences.md mention", hint)
	}
	boot, ok := payload["first_run_setup"].(map[string]any)
	if !ok {
		t.Fatalf("first_run_setup field missing or wrong type: %v", payload["first_run_setup"])
	}
	for _, key := range []string{"initialized_at", "home_dir", "config_path", "preferences_path", "claude_transport"} {
		if _, ok := boot[key]; !ok {
			t.Errorf("first_run_setup.%s missing", key)
		}
	}
	if _, ok := boot["providers"]; ok {
		t.Errorf("first send setup should not include provider scan: %v", boot["providers"])
	}
	var cliResult contract.ProviderResult
	if err := json.Unmarshal(stdout.Bytes(), &cliResult); err != nil {
		t.Fatal(err)
	}
	records, err := runstore.List(runsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Result == nil {
		t.Fatalf("stored records=%+v", records)
	}
	if !reflect.DeepEqual(*records[0].Result, cliResult) {
		t.Fatalf("stored result must equal final CLI result\nstored=%+v\ncli=%+v", *records[0].Result, cliResult)
	}
}

func TestSecondRunNoSetup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)

	// First call triggers config setup + first-run hint.
	lastSetupResult = nil
	var stdout1, stderr1 bytes.Buffer
	MainWithInput([]string{"send", "gpt5.5", "--json-result"}, &stdout1, &stderr1, strings.NewReader("hello"))

	// Second call should not trigger setup or hint.
	lastSetupResult = nil
	var stdout2, stderr2 bytes.Buffer
	MainWithInput([]string{"send", "gpt5.5", "--json-result"}, &stdout2, &stderr2, strings.NewReader("hello"))

	stderrStr := stderr2.String()
	for _, unwanted := range []string{"首次调用", "配置初始化完成", "Provider 探测"} {
		if strings.Contains(stderrStr, unwanted) {
			t.Errorf("second run stderr should not contain %q\ngot:\n%s", unwanted, stderrStr)
		}
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout2.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON: %v\nstdout=%s", err, stdout2.String())
	}
	if _, ok := payload["first_run"]; ok {
		t.Errorf("second run should not have first_run field, got %v", payload["first_run"])
	}
	if _, ok := payload["first_run_setup"]; ok {
		t.Errorf("second run should not have first_run_setup field, got %v", payload["first_run_setup"])
	}
}

func TestInitDoesNotQueueFirstRunHint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	lastSetupResult = nil

	var initOut, initErr bytes.Buffer
	code := Main([]string{"init", "--format", "json"}, &initOut, &initErr)
	if code != 0 {
		t.Fatalf("init code=%d stdout=%s stderr=%s", code, initOut.String(), initErr.String())
	}

	lastSetupResult = nil
	var stdout, stderr bytes.Buffer
	MainWithInput([]string{"send", "gpt5.5", "--json-result"}, &stdout, &stderr, strings.NewReader("hello"))

	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal JSON: %v\nstdout=%s", err, stdout.String())
	}
	if _, ok := payload["first_run"]; ok {
		t.Errorf("send after explicit init should not repeat first_run: %v", payload["first_run"])
	}
	if strings.Contains(stderr.String(), "首次调用") {
		t.Errorf("send after explicit init should not print first-use message: %s", stderr.String())
	}
}

func writeCLIConfig(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
