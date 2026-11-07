package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/setup"
)

// CLI tests exercise command behavior, not local installations, accounts or UI.
// Provider integration is tested separately through the real adapters.
func fakeCLIScan(opts setup.ScanOptions) (setup.ScanReport, error) {
	cfg, err := config.Load()
	if err != nil {
		return setup.ScanReport{}, err
	}
	cfg.Providers["omp"] = config.ProviderStatus{Executable: "/test/bin/omp", Surfaces: []string{"cli"}}
	cfg.Providers["codex"] = config.ProviderStatus{Executable: "/test/bin/codex", Surfaces: []string{"cli", "app"}}
	cfg.Providers["grok"] = config.ProviderStatus{Executable: "/test/bin/grok", Surfaces: []string{"cli"}}
	if _, ok := cfg.Models["gpt-6.1-sol"]; !ok {
		cfg.Models["gpt-6.1-sol"] = config.ModelEntry{Aliases: []string{"sol"}, Targets: map[string]map[string]config.ModelTarget{
			"omp":   {"cli": {Model: "magpie/codex/gpt-6.1-sol", Origin: "scan", Efforts: []string{"low", "high"}}},
			"codex": {"cli": {Model: "gpt-6.1-sol", Origin: "scan"}, "app": {Model: "gpt-6.1-sol", Origin: "scan", Efforts: []string{"high", "xhigh"}}},
		}}
	}
	report := setup.ScanReport{Config: cfg, DryRun: opts.DryRun, Providers: map[string]setup.ProviderScan{
		"omp":   {Executable: "/test/bin/omp", Status: "available", Surfaces: map[string]setup.SurfaceScan{"cli": {Status: "success", ModelCount: 1}}},
		"codex": {Executable: "/test/bin/codex", Status: "available", Surfaces: map[string]setup.SurfaceScan{"cli": {Status: "success", ModelCount: 1}, "app": {Status: "success", ModelCount: 1, GUISelection: "not_checked"}}},
		"grok":  {Executable: "/test/bin/grok", Status: "available", Surfaces: map[string]setup.SurfaceScan{"cli": {Status: "success", ModelCount: 1}}},
	}}
	if !opts.DryRun {
		err = config.Save(cfg)
	}
	return report, err
}

func fakeCLIInit(opts setup.Options) (setup.Result, error) {
	scan := opts.ScanProviders
	opts.ScanProviders = false
	result, err := setup.Run(opts)
	if err != nil || !scan {
		return result, err
	}
	report, err := fakeCLIScan(setup.ScanOptions{})
	result.Config = report.Config
	return result, err
}

func TestProvidersScanPreviewPreservesConfigAndForwardsOptions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	path := filepath.Join(home, "config.json")
	input := `{"version":2,"models":{},"providers":{}}`
	writeCLIConfig(t, path, input)
	before, _ := os.ReadFile(path)
	original := scanLocalProviders
	t.Cleanup(func() { scanLocalProviders = original })
	calls := 0
	scanLocalProviders = func(opts setup.ScanOptions) (setup.ScanReport, error) {
		calls++
		if !opts.Refresh || !opts.DryRun {
			t.Fatalf("opts=%+v", opts)
		}
		return fakeCLIScan(opts)
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"providers", "scan", "--dry-run", "--refresh", "--format", "json"}, &stdout, &stderr)
	if code != 0 || calls != 1 {
		t.Fatalf("code=%d calls=%d stdout=%s stderr=%s", code, calls, stdout.String(), stderr.String())
	}
	var report setup.ScanReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Config.Providers["codex"].Executable != "/test/bin/codex" || report.Providers["codex"].Surfaces["app"].GUISelection != "not_checked" {
		t.Fatalf("report=%+v", report)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("scan preview changed config")
	}
}

func TestProvidersScanFailureIsReportedWithoutOverwritingConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	path := filepath.Join(home, "config.json")
	writeCLIConfig(t, path, `{"version":2}`)
	before, _ := os.ReadFile(path)
	original := scanLocalProviders
	t.Cleanup(func() { scanLocalProviders = original })
	scanLocalProviders = func(setup.ScanOptions) (setup.ScanReport, error) {
		return setup.ScanReport{}, errors.New("catalog unavailable")
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"providers", "scan", "--format", "json"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "catalog unavailable") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("failed scan changed config")
	}
}

func TestConfigMigrationPreviewAndApplyPreserveOriginal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	path := filepath.Join(home, "config.json")
	legacy := []byte(`{"version":1,"models":{"sol":[{"provider":"codex","model":"gpt-6.1-sol","efforts":["high"],"fast":true}]},"providers":{}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"config", "migrate", "--format", "json"}, {"config", "migrate", "--dry-run", "--format", "json"}} {
		var stdout, stderr bytes.Buffer
		code := Main(args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
		var payload struct {
			Applied bool                   `json:"applied"`
			Config  config.Config          `json:"config"`
			Report  config.MigrationReport `json:"report"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload.Applied || payload.Config.Version != 2 || payload.Report.FromVersion != 1 {
			t.Fatalf("payload=%+v", payload)
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(legacy, after) {
			t.Fatal("migration preview rewrote source")
		}
		backups, _ := filepath.Glob(path + ".before-v2-*.bak")
		if len(backups) != 0 {
			t.Fatalf("preview created backup: %v", backups)
		}
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"config", "migrate", "--apply", "--format", "json"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var applied struct {
		Applied bool   `json:"applied"`
		Backup  string `json:"backup"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Backup == "" {
		t.Fatalf("payload=%+v", applied)
	}
	backup, err := os.ReadFile(applied.Backup)
	if err != nil || !bytes.Equal(backup, legacy) {
		t.Fatalf("backup=%s err=%v", backup, err)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	target := cfg.Models["gpt-6.1-sol"].Targets["codex"]["cli"]
	if target.Model != "gpt-6.1-sol" || target.Fast == nil || !*target.Fast || !reflect.DeepEqual(target.Efforts, []string{"high"}) {
		t.Fatalf("target=%+v", target)
	}
	info, err := os.Stat(applied.Backup)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("backup permissions info=%v err=%v", info, err)
	}
}

func TestConfigMigrationConflictDoesNotApplyOrCreateBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	path := filepath.Join(home, "config.json")
	legacy := []byte(`{"version":1,"models":{"mimo":[{"provider":"opencode","model":"a"},{"provider":"opencode","model":"b"}]}}`)
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"config", "migrate", "--apply", "--format", "json"}, &stdout, &stderr)
	if code != 1 || !strings.Contains(stdout.String(), "conflicts") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	after, _ := os.ReadFile(path)
	backups, _ := filepath.Glob(path + ".before-v2-*.bak")
	if !bytes.Equal(legacy, after) || len(backups) != 0 {
		t.Fatalf("conflict changed files: backups=%v after=%s", backups, after)
	}
}

func TestDefaultSendPromptFileUsesConfiguredOMPSol(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	t.Setenv("AI_DISPATCH_RUNS_DIR", filepath.Join(home, "runs"))
	if _, err := fakeCLIScan(setup.ScanOptions{}); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.Join(home, "prompt.txt")
	if err := os.WriteFile(prompt, []byte("Reply exactly OK"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Main([]string{"send", "--prompt-file", prompt, "--json-result"}, &stdout, &stderr)
	if code != 3 {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["status"] != "disabled" || payload["provider_used"] != "omp" || payload["model_used"] != "magpie/codex/gpt-6.1-sol" || payload["surface"] != "cli" {
		t.Fatalf("payload=%v", payload)
	}
}

func TestModelsResolveExplicitAppAndUnlistedNativeID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	if _, err := fakeCLIScan(setup.ScanOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args                     []string
		provider, model, surface string
		listed                   bool
	}{
		{[]string{"models", "resolve", "codex", "--model", "sol", "--surface", "app"}, "codex", "gpt-6.1-sol", "app", true},
		{[]string{"models", "resolve"}, "omp", "magpie/codex/gpt-6.1-sol", "cli", true},
		{[]string{"models", "resolve", "grok", "--model", "future-model:preview"}, "grok", "future-model:preview", "cli", false},
	} {
		var stdout, stderr bytes.Buffer
		code := Main(tc.args, &stdout, &stderr)
		if code != 0 {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", tc.args, code, stdout.String(), stderr.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
			t.Fatal(err)
		}
		if payload["provider"] != tc.provider || payload["model"] != tc.model || payload["surface"] != tc.surface || payload["model_listed"] != tc.listed {
			t.Fatalf("payload=%v", payload)
		}
	}
}

func TestModelCommandsRejectInvalidConfigInsteadOfEmptyDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AI_DISPATCH_HOME", home)
	writeCLIConfig(t, filepath.Join(home, "config.json"), `{"version":2,"unknown_field":true}`)
	for _, args := range [][]string{{"models", "--format", "json"}, {"guide", "models"}} {
		var stdout, stderr bytes.Buffer
		if code := Main(args, &stdout, &stderr); code == 0 || !strings.Contains(stdout.String()+stderr.String(), "unknown field") {
			t.Fatalf("args=%v code=%d stdout=%s stderr=%s", args, code, stdout.String(), stderr.String())
		}
	}
}
