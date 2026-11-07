package config

import (
	"strings"
	"testing"
)

func TestMigratePreservesIDsCapabilitiesAliasesAndNoProviderBinding(t *testing.T) {
	cfg, report, err := Migrate([]byte(`{"version":1,"models":{
 "gpt6.1-sol":[{"provider":"codex","model":"gpt-6.1-sol","efforts":["high","xhigh"],"fast":true}],
 "sol":[{"provider":"codex","model":"gpt-6.1-sol","efforts":["high","xhigh"],"fast":true}],
 "fable":[{"provider":"claude","model":"claude-fable-5"},{"provider":"cursor","model":"claude-fable-5-thinking-high"}]
 },"providers":{"codex":{"available":true,"version":"1.0"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if report.FromVersion != 1 || report.ToVersion != 2 || len(report.Conflicts) != 0 || len(report.Warnings) < 2 {
		t.Fatalf("report=%+v", report)
	}
	target := cfg.Models["gpt-6.1-sol"].Targets["codex"]["cli"]
	if target.Model != "gpt-6.1-sol" || target.Origin != "manual" || len(target.Efforts) != 2 || target.Fast == nil || !*target.Fast {
		t.Fatalf("target=%+v", target)
	}
	if len(cfg.Models["gpt-6.1-sol"].Aliases) != 2 || len(cfg.Models["fable"].Targets) != 2 {
		t.Fatalf("models=%+v", cfg.Models)
	}
	if len(cfg.Providers) != 0 {
		t.Fatalf("legacy diagnostics cannot become installed entries: %+v", cfg.Providers)
	}
	if _, ok := cfg.Models["gpt-6.1-sol"].Targets["codex"]["app"]; ok {
		t.Fatal("migration invented app compatibility")
	}
}

func TestMigrateReservedProviderAlias(t *testing.T) {
	cfg, report, err := Migrate([]byte(`{"version":1,"models":{"grok":[{"provider":"grok","model":"grok-4.7"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.Models["grok"]; ok {
		t.Fatal("provider remains shadowed")
	}
	if cfg.Models["grok-4.7"].Targets["grok"]["cli"].Model != "grok-4.7" || len(report.Warnings) == 0 {
		t.Fatalf("cfg=%+v report=%+v", cfg, report)
	}
}

func TestMigrateRejectsLossySameEntryCandidates(t *testing.T) {
	_, report, err := Migrate([]byte(`{"version":1,"models":{"mimo":[{"provider":"opencode","model":"a"},{"provider":"opencode","model":"b"}]}}`))
	if err == nil || len(report.Conflicts) != 1 || !strings.Contains(report.Conflicts[0], "split them") {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestMigrateRejectsMissingModelUnsupportedProviderAndUnknownFields(t *testing.T) {
	for _, input := range []string{
		`{"version":1,"models":{"x":[{"provider":"codex"}]}}`,
		`{"version":1,"models":{"x":[{"provider":"made-up","model":"x"}]}}`,
		`{"version":1,"models":{"x":[]}}`,
		`{"version":1,"unknown":true}`,
	} {
		if _, _, err := Migrate([]byte(input)); err == nil {
			t.Fatalf("expected conflict: %s", input)
		}
	}
}

func TestMigrateV2IsValidationOnly(t *testing.T) {
	cfg, report, err := Migrate([]byte(validConfig))
	if err != nil || cfg.Version != 2 || report.FromVersion != 2 || len(report.Warnings) != 0 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}

func TestMigrateReportsConflictingNativeCapabilitiesAcrossLogicalModels(t *testing.T) {
	_, report, err := Migrate([]byte(`{"version":1,"models":{
 "combined":[{"provider":"codex","model":"shared-id","efforts":["high"]},{"provider":"grok","model":"grok-4.7"}],
 "single":[{"provider":"codex","model":"shared-id","efforts":["low"]}]
}}`))
	if err == nil || len(report.Conflicts) == 0 || !strings.Contains(strings.Join(report.Conflicts, " "), "conflicting capabilities") {
		t.Fatalf("report=%+v err=%v", report, err)
	}
}
