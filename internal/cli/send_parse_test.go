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
	if req.TimeoutSeconds != 1800 {
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
