package setup

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

func TestParseOMPModelsKeepsSelectorsAndFiltersUnsupportedCapabilities(t *testing.T) {
	models, err := parseOMPModels([]byte(`{"models":[
	 {"provider":"magpie","id":"codex/gpt-6.1-sol","selector":"magpie/codex/gpt-6.1-sol","kind":"chat","thinking":["low","high","ultra","high"]},
	 {"provider":"magpie","id":"image","selector":"magpie/image","kind":"image"},
	 {"provider":"custom","id":"gpt-6.1-sol","kind":"chat"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Key != "gpt-6.1-sol" || models[0].ID != "magpie/codex/gpt-6.1-sol" {
		t.Fatalf("models=%+v", models)
	}
	if !reflect.DeepEqual(models[0].Efforts, []string{"low", "high"}) {
		t.Fatalf("efforts=%v", models[0].Efforts)
	}
	if models[1].Key == models[0].Key {
		t.Fatal("unconfirmed namespace incorrectly merged")
	}
	if _, err := parseOMPModels([]byte(`{"models":[]}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := parseOMPModels([]byte(`{"unexpected":[]}`)); err == nil {
		t.Fatal("malformed output accepted as empty catalog")
	}
}

func TestTextCatalogsAcceptNativeRowsAndRejectHelp(t *testing.T) {
	cases := []struct {
		provider, raw string
		ids           []string
	}{
		{"grok", "Default model: grok-4.7\nAvailable models:\n * grok-4.7 (default)\n - grok-4.7-build-fast\n", []string{"grok-4.7", "grok-4.7-build-fast"}},
		{"antigravity", "gemini-3.8-flash-high\tGemini 3.8 Flash (High)\n", []string{"gemini-3.8-flash-high"}},
		{"cursor", "Available models:\nclaude-fable-5 - Fable\ngpt-6.1-sol - Sol\n", []string{"claude-fable-5", "gpt-6.1-sol"}},
		{"cursor", `{"models":[{"id":"gpt-6.1-sol"}]}`, []string{"gpt-6.1-sol"}},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			models, err := parseTextModels(tc.provider, []byte(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			var ids []string
			for _, model := range models {
				ids = append(ids, model.ID)
			}
			if !reflect.DeepEqual(ids, tc.ids) {
				t.Fatalf("ids=%v want=%v", ids, tc.ids)
			}
		})
	}
	for _, raw := range []string{"Usage: grok models [OPTIONS]", "Authentication required", "version 1.2.3", "*"} {
		if _, err := parseTextModels("grok", []byte(raw)); err == nil {
			t.Fatalf("accepted noncatalog %q", raw)
		}
	}
}

func testModel(id, origin string) config.ModelEntry {
	return config.ModelEntry{Targets: map[string]map[string]config.ModelTarget{
		"omp": {"cli": {Model: id, Origin: origin}},
	}}
}

func scanFixture(status string, models ...discoveredModel) map[string]discoveredProvider {
	return map[string]discoveredProvider{"omp": {
		Name: "omp", Models: map[string][]discoveredModel{"cli": models},
		Report: ProviderScan{Executable: "/test/omp", Status: "available", Surfaces: map[string]SurfaceScan{"cli": {Status: status, ModelCount: len(models)}}},
	}}
}

func TestMergeDiscoveryPreservesManualDefaultsAndAliases(t *testing.T) {
	cfg := config.Default()
	cfg.Defaults.Model = "custom"
	manual := testModel("manual-model", "manual")
	manual.Aliases = []string{"my-model"}
	cfg.Models["custom"] = manual
	scanned := testModel("old-model", "scan")
	scanned.Aliases = []string{"keep-alias"}
	cfg.Models["old"] = scanned
	cfg.Models["gone"] = testModel("gone-model", "scan")
	discovered := scanFixture("success", discoveredModel{Key: "new", ID: "old-model", Efforts: []string{"high"}}, discoveredModel{Key: "fresh", ID: "fresh-model"})
	report := ScanReport{}
	mergeDiscovery(&cfg, discovered, &report)
	if cfg.Defaults.Model != "custom" || cfg.Models["custom"].Targets["omp"]["cli"].Origin != "manual" {
		t.Fatal("manual settings overwritten")
	}
	if _, ok := cfg.Models["gone"]; ok {
		t.Fatal("vanished scanned model retained")
	}
	if !reflect.DeepEqual(cfg.Models["old"].Aliases, []string{"keep-alias"}) || cfg.Models["old"].Targets["omp"]["cli"].Model != "old-model" {
		t.Fatalf("scanned alias identity lost: %+v", cfg.Models["old"])
	}
	if cfg.Models["fresh"].Targets["omp"]["cli"].Origin != "scan" {
		t.Fatal("discovered model absent")
	}
}

func TestMergeDiscoveryFailureRetainsOldTargetsAndDistinctSources(t *testing.T) {
	cfg := config.Default()
	cfg.Models["sol"] = testModel("magpie/codex/gpt-6.1-sol", "scan")
	report := ScanReport{}
	mergeDiscovery(&cfg, scanFixture("failed"), &report)
	if cfg.Models["sol"].Targets["omp"]["cli"].Model == "" || len(report.Warnings) != 1 {
		t.Fatal("failed scan erased old catalog or hid failure")
	}
	cfg.Models["different"] = testModel("manual-source", "manual")
	addDiscoveredModel(cfg.Models, "omp", "cli", discoveredModel{Key: "different", ID: "other-source"})
	if cfg.Models["different"].Targets["omp"]["cli"].Model != "manual-source" {
		t.Fatal("source collision overwritten")
	}
	if cfg.Models["omp/other-source"].Targets["omp"]["cli"].Model != "other-source" {
		t.Fatal("distinct source lost")
	}
}

func TestScanDryRunDoesNotWriteAndApplyUsesLatestConfig(t *testing.T) {
	t.Setenv("AI_DISPATCH_HOME", t.TempDir())
	cfg := config.Default()
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config.ConfigPath())
	discovered := scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol"})
	report, err := applyDiscovery(discovered, true)
	if err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(config.ConfigPath())
	if !bytes.Equal(before, after) || len(report.Config.Models) != 1 {
		t.Fatal("dry-run mutated config or omitted candidate")
	}
	cfg.Defaults.Model = "new-user-choice"
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := applyDiscovery(discovered, false); err != nil {
		t.Fatal(err)
	}
	written, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if written.Defaults.Model != "new-user-choice" {
		t.Fatal("scan overwrote concurrent defaults")
	}
	data, _ := os.ReadFile(config.ConfigPath())
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	provider := raw["providers"].(map[string]any)["omp"].(map[string]any)
	if _, ok := provider["version"]; ok {
		t.Fatal("CLI version persisted")
	}
}

func TestConfigWriterLockSerializesUpdates(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	entered, release, second := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() { _ = WithConfigLock(func() error { close(entered); <-release; return nil }) }()
	<-entered
	go func() { _ = WithConfigLock(func() error { close(second); return nil }) }()
	select {
	case <-second:
		t.Fatal("writer lock did not exclude second writer")
	case <-time.After(40 * time.Millisecond):
	}
	close(release)
	select {
	case <-second:
	case <-time.After(time.Second):
		t.Fatal("writer lock was not released")
	}
}

func TestDiscoveryUsesConfiguredExecutableAndOnlyCatalogCommands(t *testing.T) {
	t.Setenv("AI_DISPATCH_GROK_BIN", "")
	bin := filepath.Join(t.TempDir(), "custom-grok")
	script := "#!/bin/sh\ncase \"$1\" in\n--version) echo grok-test ;;\nmodels) printf 'Available models:\\n - grok-4.7\\n' ;;\n*) exit 64 ;;\nesac\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	item := discoverProvider("grok", false, bin)
	if item.Report.Executable != bin || item.Report.Surfaces["cli"].Status != "success" || len(item.Models["cli"]) != 1 {
		t.Fatalf("configured discovery=%+v", item)
	}
	missing := filepath.Join(t.TempDir(), "missing-grok")
	item = discoverProvider("grok", false, missing)
	if item.Report.Executable != missing || item.Report.Status != "unavailable" || item.Report.Surfaces["cli"].Status != "failed" {
		t.Fatalf("invalid config silently fell back to another installation: %+v", item)
	}
}
