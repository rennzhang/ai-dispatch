package setup

import (
	"reflect"
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
