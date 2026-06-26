package routing

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveProvider(t *testing.T) {
	target, err := Resolve("codex", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Requested != "codex" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveCodexDefaultsToGPT55(t *testing.T) {
	target, err := Resolve("codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Requested != "codex" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveAlias(t *testing.T) {
	target, err := Resolve("gpt5.5", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" {
		t.Fatalf("target=%+v", target)
	}
}

func TestRejectModelWithModelTarget(t *testing.T) {
	if _, err := Resolve("gpt-5.5", "other"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveRegistryAlias(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	target, err := Resolve("mimo", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveGeminiRegistryTarget(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	target, err := Resolve("gemini-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveClaudeRegistryTargetUsesCLIModelAlias(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	target, err := Resolve("sonnet4.6", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "sonnet" || target.Source != "registry" || target.ActualID != "claude-sonnet-4-6" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveClaudeActualIDTargetPreservesExplicitModelID(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	target, err := Resolve("claude-opus-4-7", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-opus-4-7" || target.Source != "registry" || target.ModelKey != "opus4.7" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderExplicitActualModelIDPreservesModel(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	target, err := Resolve("claude", "claude-opus-4-7")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-opus-4-7" || target.Source != "provider" || target.ModelKey != "opus4.7" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveAntigravityProvider(t *testing.T) {
	target, err := Resolve("antigravity", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Requested != "antigravity" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveGeminiProviderAlias(t *testing.T) {
	target, err := Resolve("gemini", "pro")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Requested != "gemini" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveGeminiGoogleModel(t *testing.T) {
	target, err := Resolve("google/gemini-3.1-pro-preview", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "antigravity" || target.Model != "pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func TestSupportedTargetsIncludesGeminiAliases(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	targets := SupportedTargets()
	if !contains(targets, "mimo-v2.5-pro") {
		t.Fatalf("targets=%v", targets)
	}
	if !contains(targets, "gemini-pro") || !contains(targets, "gemini-flash") {
		t.Fatalf("gemini aliases should be visible: %v", targets)
	}
	if !contains(targets, "mimo") {
		t.Fatalf("registry aliases should be visible: %v", targets)
	}
}

func TestResolveUsesConfiguredRegistry(t *testing.T) {
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", writeRegistry(t))
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "missing-config.json"))
	data := `{
  "models": [
    {
      "key": "mimo-v2.5-pro",
      "actualModelId": "openrouter/xiaomi/mimo-v2.5-pro",
      "provider": "opencode",
      "dispatchRunner": "opencode",
      "dispatchModel": "openrouter/xiaomi/mimo-v2.5-pro",
      "aliases": ["mimo"]
    }
  ]
}`
	registryPath := filepath.Join(t.TempDir(), "registry.json")
	if err := os.WriteFile(registryPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_MODEL_REGISTRY", registryPath)

	target, err := Resolve("mimo", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || target.Source != "registry" {
		t.Fatalf("target=%+v", target)
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeRegistry(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/registry.json"
	data := `{
  "models": [
	    {
	      "key": "opus4.7",
	      "actualModelId": "claude-opus-4-7",
	      "provider": "claude",
	      "dispatchRunner": "claude",
	      "dispatchModel": "opus",
	      "aliases": ["opus"]
	    },
	    {
	      "key": "sonnet4.6",
	      "actualModelId": "claude-sonnet-4-6",
	      "provider": "claude",
	      "dispatchRunner": "claude",
	      "dispatchModel": "sonnet",
	      "aliases": ["sonnet"]
	    },
	    {
	      "key": "gemini-pro",
	      "actualModelId": "google/gemini-3.1-pro-preview",
	      "provider": "gemini",
      "dispatchRunner": "antigravity",
      "dispatchModel": "pro",
      "aliases": ["geminipro"]
    },
    {
      "key": "mimo-v2.5-pro",
      "actualModelId": "openrouter/xiaomi/mimo-v2.5-pro",
      "provider": "opencode",
      "dispatchRunner": "opencode",
      "dispatchModel": "openrouter/xiaomi/mimo-v2.5-pro",
      "aliases": ["mimo"]
    }
  ]
}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
