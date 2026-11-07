package setup

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

func TestConfirmedSolIdentityJoinsNativeAndCanonicalEntriesWithoutLosingManualCapabilities(t *testing.T) {
	cfg := config.Default()
	fast, standard := true, false
	omp := testModel("magpie/codex/gpt-6.1-sol", "manual")
	omp.Aliases = []string{"my-sol"}
	omp.Targets["omp"]["cli"] = config.ModelTarget{Model: "magpie/codex/gpt-6.1-sol", Origin: "manual", Efforts: []string{"high"}, Fast: &standard}
	cfg.Models["magpie/codex/gpt-6.1-sol"] = omp
	cfg.Models["gpt-6.1-sol"] = config.ModelEntry{Aliases: []string{"gpt6.1-sol"}, Targets: map[string]map[string]config.ModelTarget{
		"codex": {"app": {Model: "gpt-6.1-sol", Origin: "manual", Efforts: []string{"high", "xhigh"}, Fast: &fast}},
	}}
	report := ScanReport{}
	mergeDiscovery(&cfg, scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol", Efforts: []string{"low", "high"}}), &report)
	if len(cfg.Models) != 1 {
		t.Fatalf("same model split across entries: %+v", cfg.Models)
	}
	entry := cfg.Models["gpt-6.1-sol"]
	if got := entry.Targets["omp"]["cli"]; got.Origin != "manual" || !reflect.DeepEqual(got.Efforts, []string{"high"}) || got.Fast == nil || *got.Fast {
		t.Fatalf("manual OMP capability lost: %+v", got)
	}
	if got := entry.Targets["codex"]["app"]; got.Fast == nil || !*got.Fast || !reflect.DeepEqual(got.Efforts, []string{"high", "xhigh"}) {
		t.Fatalf("manual App capability lost: %+v", got)
	}
	for _, want := range []string{"my-sol", "gpt6.1-sol", "magpie/codex/gpt-6.1-sol"} {
		found := false
		for _, alias := range entry.Aliases {
			if alias == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("old public name %q lost: %v", want, entry.Aliases)
		}
	}
	if err := config.Validate(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestSharedIdentityConflictDoesNotDiscardManualDeclarations(t *testing.T) {
	cfg := config.Default()
	first := testModel("magpie/codex/gpt-6.1-sol", "manual")
	second := testModel("magpie/codex/gpt-6.1-sol", "manual")
	first.Targets["omp"]["cli"] = config.ModelTarget{Model: "magpie/codex/gpt-6.1-sol", Origin: "manual", Efforts: []string{"high"}}
	second.Targets["omp"]["cli"] = config.ModelTarget{Model: "magpie/codex/gpt-6.1-sol", Origin: "manual", Efforts: []string{"low"}}
	cfg.Models["first"], cfg.Models["second"] = first, second
	report := ScanReport{}
	mergeDiscovery(&cfg, scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol"}), &report)
	if len(cfg.Models) != 2 || len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "conflicting manual capabilities") {
		t.Fatalf("manual conflict hidden: models=%+v warnings=%v", cfg.Models, report.Warnings)
	}
}

func TestExecutableChangedDuringScanSkipsStaleCatalog(t *testing.T) {
	cfg := config.Default()
	cfg.Providers["omp"] = config.ProviderStatus{Executable: "/new/omp", Surfaces: []string{"cli"}}
	cfg.Models["user-model"] = testModel("user-source", "manual")
	discovered := scanFixture("success", discoveredModel{Key: "stale", ID: "old-source"})
	item := discovered["omp"]
	item.ConfiguredExecutable, item.SnapshotKnown, item.Report.Executable = "/old/omp", true, "/old/omp"
	discovered["omp"] = item
	report := discoveryReport(discovered, false)
	mergeDiscovery(&cfg, discovered, &report)
	if cfg.Providers["omp"].Executable != "/new/omp" || len(cfg.Models) != 1 {
		t.Fatalf("stale discovery overwrote latest config: %+v", cfg)
	}
	if report.Providers["omp"].Status != "stale" || len(report.Warnings) != 1 {
		t.Fatalf("stale result hidden: %+v", report)
	}
}

func TestInitCommitsOnceAgainstLatestConfigAndForceRejectsConcurrentUpdate(t *testing.T) {
	t.Setenv("AI_DISPATCH_HOME", t.TempDir())
	cfg := config.Default()
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	_, snapshot, err := initializationSnapshot(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Defaults.Model = "concurrent-user-choice"
	cfg.Models["custom"] = testModel("user-model", "manual")
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(config.ConfigPath())
	discovered := scanFixture("success", discoveredModel{Key: "fresh", ID: "newly-listed"})
	if _, _, err := initializeConfig(Options{Force: true, ScanProviders: true}, snapshot, discovered); err == nil {
		t.Fatal("force overwrote concurrent edit")
	}
	after, _ := os.ReadFile(config.ConfigPath())
	if !bytes.Equal(before, after) {
		t.Fatal("failed force init partially wrote config")
	}
	written, report, err := initializeConfig(Options{ScanProviders: true}, snapshot, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if written.Defaults.Model != "concurrent-user-choice" || written.Models["custom"].Targets["omp"]["cli"].Model != "user-model" || report == nil {
		t.Fatalf("normal init discarded concurrent settings: %+v", written)
	}
}

func TestCatalogCommandBoundsInheritedPipeAndRunningDescendants(t *testing.T) {
	for _, wait := range []bool{false, true} {
		t.Run(map[bool]string{false: "inherited-pipe", true: "running-tree"}[wait], func(t *testing.T) {
			bin := filepath.Join(t.TempDir(), "catalog")
			script := "#!/bin/sh\n/bin/sleep 30 &\nprintf '{}\\n'\n"
			if wait {
				script += "wait\n"
			}
			if err := os.WriteFile(bin, []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancel()
			started := time.Now()
			_, err := catalogCommand(ctx, bin)
			if err == nil {
				t.Fatal("incomplete command/tree reported successful catalog")
			}
			if elapsed := time.Since(started); elapsed > 2500*time.Millisecond {
				t.Fatalf("catalog command exceeded bound: %v", elapsed)
			}
		})
	}
}

func TestCatalogListingDoesNotClaimInferenceAccess(t *testing.T) {
	item := discoveredProvider{Models: map[string][]discoveredModel{}, Report: ProviderScan{Surfaces: map[string]SurfaceScan{}}}
	setSurface(&item, "cli", []discoveredModel{{Key: "listed", ID: "listed"}}, nil)
	if item.Report.Surfaces["cli"].Availability != "listed_not_tested" {
		t.Fatal("catalog listing represented as tested access")
	}
	got := filterOpenCodeModels([]string{"openrouter/private-model", "custom/my-model", "opencode/free-model"}, map[string]bool{"custom": true, "opencode": true})
	if !reflect.DeepEqual(got, []string{"custom/my-model", "opencode/free-model"}) {
		t.Fatalf("unconfigured catalog source included: %v", got)
	}
}

func TestDiscoveredKeyCannotInvalidateManualAlias(t *testing.T) {
	cfg := config.Default()
	entry := testModel("manual-source", "manual")
	entry.Aliases = []string{"gpt-6.1-sol"}
	cfg.Models["custom"] = entry
	report := ScanReport{}
	mergeDiscovery(&cfg, scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol"}), &report)
	if err := config.Validate(cfg); err != nil {
		t.Fatalf("discovery invalidated manual aliases: %v", err)
	}
	if cfg.Models["custom"].Aliases[0] != "gpt-6.1-sol" || cfg.Models["omp/magpie/codex/gpt-6.1-sol"].Targets["omp"]["cli"].Model == "" {
		t.Fatalf("manual alias or distinct catalog entry lost: %+v", cfg.Models)
	}
}

func TestFreshInitAddsSolAliasOnlyAfterConfirmedDiscovery(t *testing.T) {
	t.Setenv("AI_DISPATCH_HOME", t.TempDir())
	discovered := scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol"})
	cfg, _, err := initializeConfig(Options{ScanProviders: true}, nil, discovered)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Models["gpt-6.1-sol"].Aliases, []string{"sol"}) {
		t.Fatalf("fresh init missing Sol alias: %+v", cfg.Models)
	}
	if cfg.Defaults.Model != "gpt-6.1-sol" || cfg.Models["gpt-6.1-sol"].Targets["omp"]["cli"].Model != "magpie/codex/gpt-6.1-sol" {
		t.Fatalf("fresh defaults and discovery disagree: %+v", cfg)
	}
	missing := config.Default()
	report := ScanReport{}
	mergeDiscovery(&missing, scanFixture("failed"), &report)
	if len(missing.Models) != 0 {
		t.Fatalf("failed discovery invented a default model or alias: %+v", missing.Models)
	}
}

func TestSolAliasDoesNotOverrideUserKeyOrAlias(t *testing.T) {
	for _, occupiedByKey := range []bool{true, false} {
		cfg := config.Default()
		manual := testModel("manual-choice", "manual")
		if occupiedByKey {
			cfg.Models["Sol"] = manual
		} else {
			manual.Aliases = []string{"Sol"}
			cfg.Models["custom"] = manual
		}
		report := ScanReport{}
		mergeDiscovery(&cfg, scanFixture("success", discoveredModel{Key: "gpt-6.1-sol", ID: "magpie/codex/gpt-6.1-sol"}), &report)
		for _, alias := range cfg.Models["gpt-6.1-sol"].Aliases {
			if strings.EqualFold(alias, "sol") {
				t.Fatal("scan took over user-owned Sol name")
			}
		}
		if err := config.Validate(cfg); err != nil {
			t.Fatal(err)
		}
	}
}
