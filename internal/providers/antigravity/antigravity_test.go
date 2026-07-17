package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestBuildUsesAgyDriver(t *testing.T) {
	t.Setenv("AI_DISPATCH_AGY_GO_DRIVER", "/tmp/fake-agy-driver")
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{
			Requested: "antigravity",
			Provider:  "antigravity",
			Model:     "pro",
		},
		CWD:            "/tmp/project",
		SessionID:      "session-1",
		TimeoutSeconds: 42,
		ProviderOptions: map[string]string{
			"bin":  "/tmp/agy",
			"root": "/tmp/agy-root",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(spec.Args, " ")
	for _, want := range []string{
		"/tmp/fake-agy-driver",
		"--model pro",
		"--session-id session-1",
		"--project /tmp/project",
		"--print-timeout 42s",
		"--agy-bin /tmp/agy",
		"--agy-root /tmp/agy-root",
		"--prompt hello",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("args %q missing %q", got, want)
		}
	}
}

func TestBuildOmitsModelWhenEmpty(t *testing.T) {
	t.Setenv("AI_DISPATCH_AGY_GO_DRIVER", "/tmp/fake-agy-driver")
	spec, err := (Provider{}).Build(providers.BuildRequest{
		Prompt: "hello",
		Target: routing.DispatchTarget{Requested: "antigravity", Provider: "antigravity"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.Join(spec.Args, "\x00"), "--model") {
		t.Fatalf("empty model must not send --model: %#v", spec.Args)
	}
}

func TestResolveAntigravityEffortSameFamilyOnly(t *testing.T) {
	resetAgyModelsCacheForTest()
	t.Cleanup(resetAgyModelsCacheForTest)
	var calls atomic.Int32
	queryAgyModels = func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte("Gemini 3.5 Flash (Medium)\nGemini 3.5 Flash (High)\nGemini 3.5 Flash (Low)\nGemini 3.1 Pro (Low)\nGemini 3.1 Pro (High)\nClaude Sonnet 4.6 (Thinking)\n"), nil
	}
	t.Cleanup(func() { queryAgyModels = defaultQueryAgyModels })

	p := Provider{}
	auto := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "flash", Requested: contract.EffortAuto})
	if auto.Applied != contract.EffortAuto || auto.AppliedModel != defaultFlashLabel || auto.Fallback {
		t.Fatalf("auto=%+v", auto)
	}
	exact := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "flash", Requested: contract.EffortHigh})
	if exact.Applied != contract.EffortHigh || exact.AppliedModel != "Gemini 3.5 Flash (High)" || exact.Fallback {
		t.Fatalf("exact=%+v", exact)
	}
	missing := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "pro", Requested: contract.EffortMedium})
	if missing.Applied != contract.EffortAuto || !missing.Fallback || missing.AppliedModel != defaultProLabel {
		t.Fatalf("missing medium on pro family=%+v", missing)
	}
	noModel := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "", Requested: contract.EffortHigh})
	if noModel.Applied != contract.EffortAuto || !noModel.Fallback || noModel.AppliedModel != "" {
		t.Fatalf("no model=%+v", noModel)
	}
	xhigh := p.ResolveEffort(context.Background(), providers.EffortRequest{Model: "flash", Requested: contract.EffortXHigh})
	if xhigh.Applied != contract.EffortAuto || !xhigh.Fallback || xhigh.AppliedModel != defaultFlashLabel {
		t.Fatalf("xhigh=%+v", xhigh)
	}
	if calls.Load() < 1 {
		t.Fatal("expected models query")
	}
}

func TestResolveAntigravityEffortQueryFailure(t *testing.T) {
	resetAgyModelsCacheForTest()
	t.Cleanup(resetAgyModelsCacheForTest)
	queryAgyModels = func(ctx context.Context) ([]byte, error) {
		return nil, errors.New("agy models failed")
	}
	t.Cleanup(func() { queryAgyModels = defaultQueryAgyModels })
	res := Provider{}.ResolveEffort(context.Background(), providers.EffortRequest{Model: "flash", Requested: contract.EffortLow})
	if res.Applied != contract.EffortAuto || !res.Fallback || res.AppliedModel != defaultFlashLabel || !strings.Contains(res.Reason, "agy models failed") {
		t.Fatalf("res=%+v", res)
	}
}

func TestResolveAntigravityCustomBinFallsBackWithoutQuery(t *testing.T) {
	resetAgyModelsCacheForTest()
	t.Cleanup(resetAgyModelsCacheForTest)
	var calls atomic.Int32
	queryAgyModels = func(ctx context.Context) ([]byte, error) {
		calls.Add(1)
		return []byte("Gemini 3.5 Flash (High)\n"), nil
	}
	t.Cleanup(func() { queryAgyModels = defaultQueryAgyModels })
	res := Provider{}.ResolveEffort(context.Background(), providers.EffortRequest{
		Model:           "flash",
		Requested:       contract.EffortHigh,
		ProviderOptions: map[string]string{"bin": "/custom/agy"},
	})
	if res.Applied != contract.EffortAuto || !res.Fallback || res.AppliedModel != defaultFlashLabel {
		t.Fatalf("res=%+v", res)
	}
	if !strings.Contains(res.Reason, "custom antigravity.bin") {
		t.Fatalf("reason=%q", res.Reason)
	}
	if calls.Load() != 0 {
		t.Fatalf("custom bin must not query default catalog, calls=%d", calls.Load())
	}
	auto := Provider{}.ResolveEffort(context.Background(), providers.EffortRequest{
		Model:           "pro",
		Requested:       contract.EffortAuto,
		ProviderOptions: map[string]string{"bin": "/custom/agy"},
	})
	if auto.Applied != contract.EffortAuto || auto.Fallback || auto.AppliedModel != defaultProLabel {
		t.Fatalf("auto custom bin=%+v", auto)
	}
}

func TestAgyDriverOmitsModelWhenEmpty(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "agy-args")
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "${FAKE_AGY_ARGS}"
log_file=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    --print) prompt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sid="22222222-2222-2222-2222-222222222222"
echo "Created conversation ${sid}" > "${log_file}"
mkdir -p "${AI_DISPATCH_AGY_APPDATA_DIR}/conversations"
touch "${AI_DISPATCH_AGY_APPDATA_DIR}/conversations/${sid}.pb"
mkdir -p "${AI_DISPATCH_AGY_APPDATA_DIR}/brain/${sid}/.system_generated/logs"
cat > "${AI_DISPATCH_AGY_APPDATA_DIR}/brain/${sid}/.system_generated/logs/transcript.jsonl" <<'JSONL'
{"source":"MODEL","type":"PLANNER_RESPONSE","content":"default model response"}
JSONL
echo "stdout response"
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_AGY_APPDATA_DIR", root)
	t.Setenv("FAKE_AGY_ARGS", argsPath)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{"--agy-bin", fakeAgy, "--agy-root", root, "--project", root, "--prompt", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(args), "--model\n") {
		t.Fatalf("empty model must not pass --model to agy: %s", args)
	}
}

func TestParseAgyDriverStream(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Stdout:   []byte(`{"event":"session_start","session_id":"abc","model":"Gemini 3.1 Pro (High)"}` + "\n" + `{"event":"done","session_id":"abc","model":"Gemini 3.1 Pro (High)","text":"done text"}` + "\n"),
		ExitCode: 0,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "antigravity", Provider: "antigravity", Model: "pro"}})
	if !result.OK || result.Text != "done text" || result.SessionID != "abc" || result.ModelUsed != "Gemini 3.1 Pro (High)" {
		t.Fatalf("result=%+v", result)
	}
}

func TestParseAgyEmptyOutputIsConfigDiagnostic(t *testing.T) {
	result := (Provider{}).Parse(runtime.RunResult{
		Error:    "agy completed without output",
		ExitCode: 1,
	}, providers.BuildRequest{Target: routing.DispatchTarget{Requested: "gemini-pro", Provider: "antigravity", Model: "pro"}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != "config" {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "verify agy login") {
		t.Fatalf("stderr=%q", result.Stderr)
	}
}

func TestAgyDriverRunsWithoutSettingsAndPassesResolvedModel(t *testing.T) {
	root := t.TempDir()
	argsPath := filepath.Join(root, "agy-args")
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$@" > "${FAKE_AGY_ARGS}"
log_file=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    --project) project="$2"; shift 2 ;;
    --print) prompt="$2"; shift 2 ;;
    *) shift ;;
  esac
done
sid="11111111-1111-1111-1111-111111111111"
echo "Created conversation ${sid}" > "${log_file}"
echo 'selected model override to backend: label="Gemini 3.1 Pro (High)"' >> "${log_file}"
mkdir -p "${AI_DISPATCH_AGY_APPDATA_DIR}/conversations"
touch "${AI_DISPATCH_AGY_APPDATA_DIR}/conversations/${sid}.pb"
mkdir -p "${AI_DISPATCH_AGY_APPDATA_DIR}/brain/${sid}/.system_generated/logs"
cat > "${AI_DISPATCH_AGY_APPDATA_DIR}/brain/${sid}/.system_generated/logs/transcript.jsonl" <<'JSONL'
{"source":"MODEL","type":"PLANNER_RESPONSE","content":"transcript response"}
JSONL
echo "stdout response"
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_AGY_APPDATA_DIR", root)
	t.Setenv("FAKE_AGY_ARGS", argsPath)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{"--agy-bin", fakeAgy, "--agy-root", root, "--model", "pro", "--project", root, "--prompt", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "transcript response") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(args), "--model\n"+defaultProLabel+"\n") {
		t.Fatalf("agy did not receive the resolved model label: %s", args)
	}
	for _, name := range []string{"settings.json", "settings.json.lock", legacyModelRecoveryJournalName} {
		if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("driver unexpectedly created legacy settings state %s: %v", name, err)
		}
	}
}

func TestResolveAgyBinaryExplicitFailureDoesNotFallbackOrLeakPath(t *testing.T) {
	root := t.TempDir()
	fallback := filepath.Join(root, ".local", "bin", "agy")
	if err := os.MkdirAll(filepath.Dir(fallback), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "private", "missing-agy")
	t.Setenv("HOME", root)
	t.Setenv("PATH", t.TempDir())
	_, err := resolveAgyBinary(missing)
	if err == nil {
		t.Fatal("expected explicit binary failure")
	}
	if strings.Contains(err.Error(), missing) || !strings.Contains(err.Error(), "agy binary override") {
		t.Fatalf("err=%v", err)
	}
}

func TestRecoverLegacyModelSwitchClearsAlreadyRestoredJournal(t *testing.T) {
	root := t.TempDir()
	settingsPath := filepath.Join(root, "settings.json")
	original := []byte(`{"model":"Gemini 3.5 Flash (Medium)","other":true}`)
	writeLegacyRecoveryJournal(t, root, original, 0o600)
	if err := os.WriteFile(settingsPath, original, 0o640); err != nil {
		t.Fatal(err)
	}
	err := recoverLegacyModelSwitchContext(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Fatalf("already restored settings changed: %s", restored)
	}
	if _, err := os.Stat(legacyModelRecoveryJournalPath(root)); !os.IsNotExist(err) {
		t.Fatalf("recovery journal still exists: %v", err)
	}
}

func TestRecoverLegacyModelSwitchPreservesUnverifiableAppliedState(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"model":"Gemini 3.5 Flash (Medium)","other":true}`)
	writeLegacyRecoveryFixture(t, root, original, defaultProLabel, 0o600)
	settingsPath := filepath.Join(root, "settings.json")
	applied, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	err := recoverLegacyModelSwitchContext(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("expected unverifiable legacy recovery to fail closed, got %v", err)
	}
	current, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, applied) {
		t.Fatalf("unverifiable applied settings were overwritten: %s", current)
	}
	if _, statErr := os.Stat(legacyModelRecoveryJournalPath(root)); statErr != nil {
		t.Fatalf("unverifiable recovery journal was not preserved: %v", statErr)
	}
}

func TestRecoverLegacyModelSwitchPreservesConflictingUserChange(t *testing.T) {
	root := t.TempDir()
	original := []byte(`{"model":"Gemini 3.5 Flash (Medium)","other":true}`)
	writeLegacyRecoveryFixture(t, root, original, defaultProLabel, 0o600)
	settingsPath := filepath.Join(root, "settings.json")
	userChange := []byte(`{"model":"Gemini 3.1 Pro (Low)","other":"changed after crash"}`)
	if err := os.WriteFile(settingsPath, userChange, 0o600); err != nil {
		t.Fatal(err)
	}
	err := recoverLegacyModelSwitchContext(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "cannot verify") {
		t.Fatalf("expected explicit recovery conflict, got %v", err)
	}
	current, readErr := os.ReadFile(settingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !bytes.Equal(current, userChange) {
		t.Fatalf("conflicting user settings were overwritten: %s", current)
	}
	if _, statErr := os.Stat(legacyModelRecoveryJournalPath(root)); statErr != nil {
		t.Fatalf("conflicting recovery journal was not preserved: %v", statErr)
	}
}

func writeLegacyRecoveryFixture(t *testing.T, root string, original []byte, appliedLabel string, mode os.FileMode) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(original, &payload); err != nil {
		t.Fatal(err)
	}
	payload["model"] = appliedLabel
	applied, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeLegacyRecoveryJournal(t, root, original, mode)
	if err := os.WriteFile(filepath.Join(root, "settings.json"), applied, mode); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyRecoveryJournal(t *testing.T, root string, original []byte, mode os.FileMode) {
	t.Helper()
	journalData, err := json.Marshal(legacyModelRecoveryJournal{Version: 1, Original: original, Mode: uint32(mode.Perm())})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyModelRecoveryJournalPath(root), journalData, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestAgyResumeWithoutTranscriptGrowthDoesNotReuseOldAnswer(t *testing.T) {
	root := t.TempDir()
	sessionID := "11111111-1111-1111-1111-111111111111"
	if err := os.MkdirAll(filepath.Dir(conversationPath(root, sessionID)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conversationPath(root, sessionID), []byte("conversation"), 0o600); err != nil {
		t.Fatal(err)
	}
	transcript := transcriptPath(root, sessionID)
	if err := os.MkdirAll(filepath.Dir(transcript), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcript, []byte(`{"source":"MODEL","type":"PLANNER_RESPONSE","content":"old answer"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
log_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
: > "$log_file"
printf '%s\n' 'old answer'
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{
		"--agy-bin", fakeAgy,
		"--agy-root", root,
		"--session-id", sessionID,
		"--model", "flash",
		"--prompt", "continue",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("resume without transcript growth must fail: stdout=%s", stdout.String())
	}
	if strings.Contains(stdout.String(), "old answer") {
		t.Fatalf("old answer was reused: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "completed without output") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestAgyOuterCancellationStopsWithoutSettings(t *testing.T) {
	root := t.TempDir()
	startedPath := filepath.Join(root, "started")
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
: > "$FAKE_AGY_STARTED"
exec sleep 30
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_AGY_STARTED", startedPath)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runAgyDriverContext(ctx, agyDriverConfig{
			Prompt:  "hello",
			Model:   "pro",
			AgyBin:  fakeAgy,
			AgyRoot: root,
		}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	waitForPath(t, startedPath, 2*time.Second)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected canceled agy run to fail")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("agy driver did not stop promptly after cancellation")
	}
	if _, err := os.Stat(filepath.Join(root, "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("canceled driver unexpectedly created settings.json: %v", err)
	}
}

func waitForPath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("path did not appear: %s", path)
}
