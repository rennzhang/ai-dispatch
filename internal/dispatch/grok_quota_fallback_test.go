package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestGrokQuotaFallsBackToConfiguredOpenCodeCandidate(t *testing.T) {
	home := t.TempDir()
	binDir := t.TempDir()
	grokBin := filepath.Join(t.TempDir(), "grok")
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(home, "config.json"))
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	t.Setenv("AI_DISPATCH_OPENCODE_LOCK_PATH", filepath.Join(t.TempDir(), "opencode.lock"))
	t.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	t.Setenv("AI_DISPATCH_GROK_BIN", grokBin)
	t.Setenv("PATH", binDir)
	writeConfig(t, filepath.Join(home, "config.json"), `{
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
	writeFakeGrokQuota(t, grokBin)
	writeFakeOpenCode(t, filepath.Join(binDir, "opencode"))

	result := Execute(contract.DispatchRequest{
		Command: "send",
		Target:  "grok",
		Prompt:  "hello",
	})
	if !result.OK || result.ProviderUsed != "opencode" || result.ModelUsed != "openrouter/x-ai/grok-4.5" {
		t.Fatalf("result=%+v", result)
	}
	if !result.Degraded || !strings.Contains(result.DegradeReason, "quota") {
		t.Fatalf("degrade fields missing: %+v", result)
	}
	if len(result.RouteSteps) != 2 || result.RouteSteps[0].Status != contract.StatusQuota || result.RouteSteps[1].Status != contract.StatusSuccess {
		t.Fatalf("route steps=%+v", result.RouteSteps)
	}
}

func writeFakeGrokQuota(t *testing.T, path string) {
	t.Helper()
	script := `#!/bin/sh
echo 'HTTP 402 Payment Required: usage balance exhausted' >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}
