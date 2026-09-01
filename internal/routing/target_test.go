package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fableConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "fable": [
      { "provider": "claude", "model": "claude-fable-5" },
      { "provider": "cursor", "model": "claude-fable-5-thinking-high" }
    ],
    "kimi-k3": [
      { "provider": "cursor", "model": "kimi-k3" }
    ]
  },
  "providers": {}
}`

const grokConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "grok": [
      { "provider": "grok", "model": "grok-4.5" },
      { "provider": "opencode", "model": "openrouter/x-ai/grok-4.5" }
    ]
  },
  "providers": {}
}`

const grokExplicitConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "grok": [
      { "provider": "opencode", "model": "openrouter/x-ai/grok-4.5" }
    ]
  },
  "providers": {}
}`

const gptConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "gpt5.5": [
      { "provider": "opencode", "model": "openai/gpt-5.5" }
    ]
  },
  "providers": {}
}`

const codexOverrideConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "codex": [
      { "provider": "opencode", "model": "openai/gpt-5.5" }
    ]
  },
  "providers": {}
}`

const mimoConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "mimo-pro": [
      { "provider": "opencode", "model": "openrouter/xiaomi/mimo-v2.5-pro" },
      { "provider": "opencode", "model": "opencode/mimo-v2.5-free" }
    ]
  },
  "providers": {}
}`

const ambiguousConfig = `{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "one": [{ "provider": "claude", "model": "shared-id" }],
    "two": [{ "provider": "cursor", "model": "shared-id" }]
  },
  "providers": {}
}`

func TestResolveProvider(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("codex", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "codex" || target.Model != "gpt-5.5" || target.Requested != "codex" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveRejectsColonRouteSyntax(t *testing.T) {
	for _, tc := range []struct {
		target string
		model  string
	}{
		{target: "cursor:opus5"},
		{target: "opus5:cursor"},
		{target: "cursor", model: "opus5:cursor"},
	} {
		if _, err := Resolve(tc.target, tc.model); err == nil || !strings.Contains(err.Error(), "colon route syntax is not supported") {
			t.Fatalf("Resolve(%q, %q) err=%v", tc.target, tc.model, err)
		}
	}
}

func TestResolveBareProviderUsesCLIDefault(t *testing.T) {
	isolateConfig(t)
	for _, tc := range []struct {
		target   string
		provider string
	}{
		{target: "codex", provider: "codex"},
		{target: "claude", provider: "claude"},
		{target: "cursor", provider: "cursor"},
		{target: "grok", provider: "grok"},
		{target: "opencode", provider: "opencode"},
		{target: "antigravity", provider: "antigravity"},
		{target: "gemini", provider: "antigravity"},
	} {
		got, err := Resolve(tc.target, "")
		if err != nil {
			t.Fatalf("Resolve(%q): %v", tc.target, err)
		}
		if got.Provider != tc.provider || got.Model != "" || got.Source != "provider" {
			t.Fatalf("Resolve(%q)=%+v want provider=%s empty model", tc.target, got, tc.provider)
		}
	}
}

func TestResolveRejectsFormerRegistryShortNames(t *testing.T) {
	isolateConfig(t)
	for _, target := range []string{"kimi", "qwen", "qwen37plus", "fable5-cursor", "cursor-fable5", "gpt5.5", "gpt-5.5", "mimo-openrouter-pro", "gemini-pro", "grok-fast", "openrouter/moonshotai/kimi-k2.7-code", "sonnet4.6"} {
		if _, err := Resolve(target, ""); err == nil || !strings.Contains(err.Error(), "unsupported target") {
			t.Fatalf("Resolve(%q) err=%v want unsupported target", target, err)
		}
	}
}

func TestRejectModelWithNonProviderTarget(t *testing.T) {
	writeConfig(t, fableConfig)
	if _, err := Resolve("fable", "other"); err == nil {
		t.Fatal("expected error")
	}
}

func TestResolveConfiguredShortNameCandidateChain(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("fable", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-fable-5" || target.Source != "config" || target.ModelKey != "fable" {
		t.Fatalf("target=%+v", target)
	}
	candidates := CandidateTargets(target)
	if len(candidates) != 2 || candidates[1].Provider != "cursor" || candidates[1].Model != "claude-fable-5-thinking-high" {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestResolveExactModelIDPinsSingleCandidate(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("claude-fable-5", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "claude-fable-5" || target.Source != "config" || target.ModelKey != "fable" {
		t.Fatalf("target=%+v", target)
	}
	if len(target.Candidates) != 0 {
		t.Fatalf("exact id must pin a single candidate, got %+v", target)
	}
}

func TestResolveKimiK3ConfigKey(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("kimi-k3", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "cursor" || target.Model != "kimi-k3" || target.Source != "config" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveExactModelIDIsCaseInsensitive(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("Kimi-K3", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "cursor" || target.Model != "kimi-k3" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveExactModelIDAmbiguousFails(t *testing.T) {
	writeConfig(t, ambiguousConfig)
	if _, err := Resolve("shared-id", ""); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveProviderUsesConfiguredShortName(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("cursor", "fable")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "cursor" || target.Model != "claude-fable-5-thinking-high" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderExactIDPinsMatchingCandidate(t *testing.T) {
	writeConfig(t, fableConfig)
	target, err := Resolve("cursor", "claude-fable-5-thinking-high")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "cursor" || target.Model != "claude-fable-5-thinking-high" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderRejectsExactIDOwnedByAnotherProvider(t *testing.T) {
	writeConfig(t, fableConfig)
	if _, err := Resolve("cursor", "claude-fable-5"); err == nil || !strings.Contains(err.Error(), "belongs to provider") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveProviderPassesThroughUnknownModelID(t *testing.T) {
	isolateConfig(t)
	target, err := Resolve("claude", "sonnet")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "sonnet" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderShortNameMissingCandidateFails(t *testing.T) {
	writeConfig(t, fableConfig)
	if _, err := Resolve("codex", "fable"); err == nil || !strings.Contains(err.Error(), "has no codex candidate") {
		t.Fatalf("err=%v", err)
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

func TestResolveGrokUsesConfiguredCandidateChain(t *testing.T) {
	writeConfig(t, grokConfig)
	target, err := Resolve("grok", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "grok" || target.Model != "grok-4.5" || target.Source != "config" || len(target.Candidates) != 2 {
		t.Fatalf("target=%+v", target)
	}
	candidates := CandidateTargets(target)
	if len(candidates) != 2 || candidates[1].Provider != "opencode" || candidates[1].Model != "openrouter/x-ai/grok-4.5" {
		t.Fatalf("candidates=%+v", candidates)
	}
}

func TestResolveGrokExplicitModelKeepsProviderSemantics(t *testing.T) {
	writeConfig(t, grokExplicitConfig)
	target, err := Resolve("grok", "grok-4.5")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "grok" || target.Model != "grok-4.5" || target.Source != "provider" || len(target.Candidates) != 0 {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveConfiguredModelOverridesProviderNameUnlessModelExplicit(t *testing.T) {
	writeConfig(t, codexOverrideConfig)
	target, err := Resolve("codex", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openai/gpt-5.5" || target.Source != "config" {
		t.Fatalf("target=%+v", target)
	}
	explicit, err := Resolve("codex", "gpt-5.5")
	if err != nil {
		t.Fatal(err)
	}
	if explicit.Provider != "codex" || explicit.Model != "gpt-5.5" || explicit.Source != "provider" {
		t.Fatalf("explicit=%+v", explicit)
	}
}

func TestResolveConfiguredShortNameCanReplaceFormerBuiltin(t *testing.T) {
	writeConfig(t, gptConfig)
	target, err := Resolve("gpt5.5", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openai/gpt-5.5" || target.Source != "config" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveConfiguredModelCandidates(t *testing.T) {
	writeConfig(t, mimoConfig)
	target, err := Resolve("mimo-pro", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || len(target.Candidates) != 2 {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderUsesConfiguredModelAlias(t *testing.T) {
	writeConfig(t, mimoConfig)
	target, err := Resolve("opencode", "mimo-pro")
	if err != nil {
		t.Fatal(err)
	}
	candidates := CandidateTargets(target)
	if target.Provider != "opencode" || target.Model != "openrouter/xiaomi/mimo-v2.5-pro" || len(candidates) != 2 {
		t.Fatalf("target=%+v candidates=%+v", target, candidates)
	}
}

func TestSupportedTargetsIncludesConfigModelsAndProvidersOnly(t *testing.T) {
	writeConfig(t, mimoConfig)
	targets := SupportedTargets()
	for _, target := range []string{"mimo-pro", "codex", "claude", "cursor", "grok", "gemini"} {
		if !contains(targets, target) {
			t.Fatalf("missing %q in targets=%v", target, targets)
		}
	}
	for _, target := range []string{"mimo-openrouter-pro", "kimi", "gemini-pro", "grok-fast", "gpt5.5"} {
		if contains(targets, target) {
			t.Fatalf("registry leftover %q still advertised: %v", target, targets)
		}
	}
}

func TestResolveBareProviderIgnoresUnrelatedBadConfigKey(t *testing.T) {
	writeConfig(t, "{\n  \"version\": 1,\n  \"claude_transport\": \"print\",\n  \"models\": {\n    \"broken\": [{\"provider\": \"not-a-provider\", \"model\": \"x\"}],\n    \"kimi-k3\": [{\"provider\": \"cursor\", \"model\": \"kimi-k3\"}]\n  },\n  \"providers\": {}\n}")
	target, err := Resolve("claude", "")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "claude" || target.Model != "" {
		t.Fatalf("target=%+v", target)
	}
}

func TestResolveProviderExactIDDisambiguatesAcrossProviders(t *testing.T) {
	writeConfig(t, ambiguousConfig)
	target, err := Resolve("cursor", "shared-id")
	if err != nil {
		t.Fatal(err)
	}
	if target.Provider != "cursor" || target.Model != "shared-id" || target.Source != "provider" {
		t.Fatalf("target=%+v", target)
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
