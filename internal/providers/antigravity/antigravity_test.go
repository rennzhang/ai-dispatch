package antigravity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestAgyDriverRunsFakeCLIAndRestoresSettings(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "brain"), 0o700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	originalSettings := `{"model":"Gemini 3.5 Flash (Medium)","other":true}`
	if err := os.WriteFile(settingsPath, []byte(originalSettings), 0o600); err != nil {
		t.Fatal(err)
	}
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/usr/bin/env bash
set -euo pipefail
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

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{"--agy-bin", fakeAgy, "--agy-root", root, "--model", "pro", "--project", root, "--prompt", "hello"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
	}
	if !strings.Contains(stdout.String(), "transcript response") {
		t.Fatalf("stdout=%s", stdout.String())
	}
	restored, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != originalSettings {
		t.Fatalf("settings not restored: %s", string(restored))
	}
}
