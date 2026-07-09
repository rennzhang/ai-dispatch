package setup

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFilterOpenCodeModelsCountsOnlyConfiguredProviders(t *testing.T) {
	models := []string{
		"opencode/grok-code",
		"openrouter/anthropic/claude-opus-4",
		"google/gemini-2.5-pro",
		"openai/gpt-5.1-codex",
	}

	got := filterOpenCodeModels(models, map[string]bool{
		"opencode":   true,
		"openrouter": true,
	})

	want := []string{
		"opencode/grok-code",
		"openrouter/anthropic/claude-opus-4",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestFilterOpenCodeModelsAuthMissingFallsBackToBuiltInOnly(t *testing.T) {
	models := []string{
		"opencode/grok-code",
		"openrouter/anthropic/claude-opus-4",
		"google/gemini-2.5-pro",
	}

	got := filterOpenCodeModels(models, map[string]bool{"opencode": true})

	want := []string{"opencode/grok-code"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}

func TestExplicitBinaryOverrideDoesNotFallback(t *testing.T) {
	temp := t.TempDir()
	fallback := filepath.Join(temp, "fallback")
	if err := os.WriteFile(fallback, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(temp, "missing-bin")
	t.Setenv("AI_DISPATCH_TEST_BIN", missing)

	got, err := resolveBinary(providerSpec{
		binary:      "definitely-missing-ai-dispatch-test",
		envOverride: "AI_DISPATCH_TEST_BIN",
		fallbacks:   []string{fallback},
	})
	if err == nil {
		t.Fatalf("expected explicit override failure, got binary %s", got)
	}
	msg := sanitizeProbeError(err)
	if strings.Contains(msg, temp) || strings.Contains(msg, missing) {
		t.Fatalf("sanitized error leaked path: %q", msg)
	}
	if !strings.Contains(msg, "AI_DISPATCH_TEST_BIN override") {
		t.Fatalf("unexpected error: %q", msg)
	}
}

func TestProbeOneKnowsGrok(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "grok")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 'grok 0.2.93'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_GROK_BIN", bin)
	status, ok := ProbeOne("grok", false)
	if !ok {
		t.Fatal("grok provider not registered")
	}
	if !status.Available || status.Version != "grok 0.2.93" {
		t.Fatalf("status=%+v", status)
	}
}
