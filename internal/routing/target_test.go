package routing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("codex", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Requested != "codex" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveCodexDefaultsToGPT55(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Requested != "codex" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveBuiltinAlias(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("gpt5.5", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestRejectModelWithModelTarget(t *testing.T) {
	isolateConfig(t)
	if _, err := Resolve("gpt-5.5", "other"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveBuiltinSpecificMiMoTarget(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("mimo-openrouter-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveDoesNotBindAmbiguousMiMoAlias(t *testing.T) {
	isolateConfig(t)
	if _, err := Resolve("mimo", ""); err == nil {
		t.Fatal("expected ambiguous mimo alias to be unsupported without config.models")
	}
}

func TestResolveGeminiRegistryTarget(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("gemini-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveClaudeRegistryTargetUsesCLIModelAlias(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("sonnet4.6", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "sonnet" || target.Source != "registry" || target.ActualID != "claude-sonnet-4-6" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveClaudeActualIDTargetPreservesExplicitModelID(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("claude-opus-4-7", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-opus-4-7" || target.Source != "registry" || target.ModelKey != "opus4.7" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderExplicitActualModelIDPreservesModel(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("claude", "claude-opus-4-7")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-opus-4-7" || target.Source != "provider" || target.ModelKey != "opus4.7" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveAntigravityProvider(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("antigravity", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Requested != "antigravity" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveGeminiProviderAlias(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("gemini", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Requested != "gemini" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveGeminiGoogleModel(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("google/gemini-3.1-pro-preview", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveConfiguredModelOverridesBuiltin(t *testing.T) {
	writeConfig(t, `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "gpt5.5": [
      { "provider": "opencode", "model": "openai/gpt-5.5" }
    ]
  },
  "providers": {}
}`)
	target, err := Resolve("gpt5.5", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openai/gpt-5.5" || target.Source != "config" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveConfiguredModelCandidates(t *testing.T) {
	writeConfig(t, `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ]
  },
  "providers": {}
}`)
	target, err := Resolve("mimo-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || len(target.Candidates) != 2 {
		t.Fatalf("target=%+v", target)
	}
	candidates := CandidateTargets(target)
	if len(candidates) != 2 || candidates[1].Model != "opencode/mimo-v2.5-free" {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestResolveProviderUsesConfiguredModelAlias(t *testing.T) {
	writeConfig(t, `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" }
    ]
  },
  "providers": {}
}`)
	target, err := Resolve("opencode", "mimo-pro")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderPreservesConfiguredCandidateChain(t *testing.T) {
	writeConfig(t, `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ]
  },
  "providers": {}
}`)
	target, err := Resolve("opencode", "mimo-pro")
	if err != nil {
		t.Fatal(err)
	}
	candidates := CandidateTargets(target)
	if len(candidates) != 2 || candidates[1].Model != "opencode/mimo-v2.5-free" {
		t.Fatalf("target=%+v candidates=%+v", target, candidates)
	}
}

func TestSupportedTargetsIncludesConfigModelsAndBuiltins(t *testing.T) {
	writeConfig(t, `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" }
    ]
  },
  "providers": {}
}`)
	targets := SupportedTargets()
	for _, target := range []string{"mimo-pro", "mimo-openrouter-pro", "mimo-opencode-free", "gemini-pro"} {
		if !contains(targets, target) {
			t.Fatalf("missing %q in targets=%v", target, targets)
		}
	}
	if contains(targets, "mimo") {
		t.Fatalf("ambiguous mimo alias should not be advertised: %v", targets)
	}
}

func isolateConfig(t *testing.T) {
	t.Helper()
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
}

func writeConfig(t *testing.T, data string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_CONFIG", path)
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
