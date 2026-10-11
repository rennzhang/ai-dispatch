package config

import (
	"strings"
	"testing"
)

func TestUserFacingSummaryEnabledDefault(t *testing.T) {
	cfg := Default()
	if cfg.UserFacingSummaryEnabled() {
		t.Fatal("missing user_facing_summary should default off")
	}
}

func TestDecodeUserFacingSummary(t *testing.T) {
	cfg, err := decodeConfig([]byte(`{"version":1,"claude_transport":"print"}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserFacingSummaryEnabled() || cfg.UserFacingSummary != nil {
		t.Fatalf("cfg=%+v", cfg)
	}

	cfg, err = decodeConfig([]byte(`{"user_facing_summary":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UserFacingSummaryEnabled() {
		t.Fatal("false should disable wrap-up")
	}

	cfg, err = decodeConfig([]byte(`{"user_facing_summary":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.UserFacingSummaryEnabled() {
		t.Fatal("true should keep wrap-up on")
	}

	_, err = decodeConfig([]byte(`{"user_facing_summary":"off"}`))
	if err == nil || !strings.Contains(err.Error(), "user_facing_summary must be a boolean") {
		t.Fatalf("err=%v", err)
	}
}

func TestDecodeModelRouteEffortsAndFast(t *testing.T) {
	cfg, err := decodeConfig([]byte(`{
  "version": 1,
  "claude_transport": "print",
  "models": {
    "sol": [{"provider":"codex","model":"gpt-5.6-sol","efforts":["high","xhigh"],"fast":true}]
  }
}`))
	if err != nil {
		t.Fatal(err)
	}
	normalized, err := normalize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	route := normalized.Models["sol"][0]
	if len(route.Efforts) != 2 || route.Efforts[0] != "high" || route.Fast == nil || !*route.Fast {
		t.Fatalf("route=%+v", route)
	}
}

func TestNormalizeRejectsAutoEffortInList(t *testing.T) {
	_, err := normalize(Config{
		Version:         1,
		ClaudeTransport: "print",
		Models: ModelsConfig{
			"sol": {{Provider: "codex", Model: "gpt-5.6-sol", Efforts: []string{"auto", "high"}}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "must not include auto") {
		t.Fatalf("err=%v", err)
	}
}
