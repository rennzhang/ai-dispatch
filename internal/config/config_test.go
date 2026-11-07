package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const validConfig = `{
 "version":2,
 "defaults":{"provider":"omp","surface":"cli","model":"gpt-6.1-sol"},
 "providers":{"omp":{"executable":"/usr/local/bin/omp","surfaces":["cli"]}},
 "models":{"gpt-6.1-sol":{"aliases":["sol"],"targets":{
  "omp":{"cli":{"model":"magpie/codex/gpt-6.1-sol","origin":"scan","efforts":["high","xhigh"]}},
  "codex":{"app":{"model":"gpt-6.1-sol","origin":"manual","fast":true}}
 }}}
}`

func TestDefaultUsesOMPSolWithoutProviderVersion(t *testing.T) {
	cfg := Default()
	if cfg.Version != 2 || cfg.Defaults.Provider != "omp" || cfg.Defaults.Surface != "cli" || cfg.Defaults.Model != "gpt-6.1-sol" {
		t.Fatalf("cfg=%+v", cfg)
	}
	if cfg.UserFacingSummaryEnabled() {
		t.Fatal("summary must default off")
	}
}

func TestDecodeUserFacingSummary(t *testing.T) {
	for _, tc := range []struct {
		input   string
		enabled bool
	}{
		{`{}`, false}, {`{"user_facing_summary":false}`, false}, {`{"user_facing_summary":true}`, true},
	} {
		cfg, err := Decode([]byte(tc.input))
		if err != nil || cfg.UserFacingSummaryEnabled() != tc.enabled {
			t.Fatalf("input=%s cfg=%+v err=%v", tc.input, cfg, err)
		}
	}
}

func TestDecodeTargetCapabilities(t *testing.T) {
	cfg, err := Decode([]byte(validConfig))
	if err != nil {
		t.Fatal(err)
	}
	target := cfg.Models["gpt-6.1-sol"].Targets["omp"]["cli"]
	if !reflect.DeepEqual(target.Efforts, []string{"high", "xhigh"}) || target.Origin != "scan" {
		t.Fatalf("target=%+v", target)
	}
	app := cfg.Models["gpt-6.1-sol"].Targets["codex"]["app"]
	if app.Fast == nil || !*app.Fast {
		t.Fatalf("app=%+v", app)
	}
}

func TestDecodeRejectsInvalidSchema(t *testing.T) {
	for _, input := range []string{
		`null`, `[]`, `{"version":1}`, `{"version":99}`, `{"unknown":true}`,
		`{"user_facing_summary":"off"}`, `{"defaults":{"unknown":true}}`,
		`{"defaults":{"provider":"unsupported"}}`, `{"defaults":{"provider":"omp","surface":"app"}}`,
		`{"models":{"sol":[{"provider":"codex","model":"gpt-6.1-sol"}]}}`,
		`{"models":{"sol":{"targets":{"codex":{"app":"gpt-6.1-sol"}}}}}`,
		`{"models":{"sol":{"targets":{}}}}`,
		`{"models":{"sol":{"targets":{"codex":{"app":{"model":"x","extra":true}}}}}}`,
		`{"models":{"sol":{"targets":{"codex":{"app":{"model":"x","origin":"made-up"}}}}}}`,
		`{"models":{"sol":{"targets":{"omp":{"app":{"model":"x"}}}}}}`,
		`{"models":{"sol":{"targets":{"codex":{"cli":{"model":""}}}}}}`,
		`{"providers":{"codex":{"available":true}}}`,
		`{"providers":{"codex":{"executable":"codex","surfaces":["cli"]}}}`,
		`{"providers":{"codex":{"executable":"/bin/codex","surfaces":[]}}}`,
		`{"providers":{"codex":{"executable":"/bin/codex","surfaces":["cli","cli"]}}}`,
		`{"models":{"sol":{"targets":{"codex":{"cli":{"model":"x","efforts":["auto"]}}}}}}`,
		`{"models":{"sol":{"targets":{"codex":{"cli":{"model":"x","efforts":["ultra"]}}}}}}`,
		`{"models":{"sol":{"targets":{"codex":{"cli":{"model":"x","efforts":["high","high"]}}}}}}`,
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := Decode([]byte(input)); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDecodeRejectsAliasAndProviderCollisions(t *testing.T) {
	for _, models := range []string{
		`"codex":{"targets":{"codex":{"cli":{"model":"x"}}}}`,
		`"sol":{"aliases":["grok"],"targets":{"codex":{"cli":{"model":"x"}}}}`,
		`"sol":{"aliases":["SOL"],"targets":{"codex":{"cli":{"model":"x"}}}}`,
		`"sol":{"aliases":["shared"],"targets":{"codex":{"cli":{"model":"x"}}}},"other":{"aliases":["SHARED"],"targets":{"grok":{"cli":{"model":"y"}}}}`,
	} {
		if _, err := Decode([]byte(`{"models":{` + models + `}}`)); err == nil {
			t.Fatalf("expected collision: %s", models)
		}
	}
}

func TestManualOriginDefaultAndSaveRoundTrip(t *testing.T) {
	cfg, err := Decode([]byte(`{"models":{"sol":{"targets":{"codex":{"app":{"model":"gpt-6.1-sol"}}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models["sol"].Targets["codex"]["app"].Origin != "manual" {
		t.Fatal("handwritten targets must be protected")
	}
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("AI_DISPATCH_CONFIG", path)
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil || !reflect.DeepEqual(got, cfg) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v", info.Mode())
	}
}

func TestPreservesAliasesAfterAllDiscoveredTargetsDisappear(t *testing.T) {
	cfg, err := Decode([]byte(`{"models":{"gpt-6.1-sol":{"aliases":["sol"],"targets":{}}}}`))
	if err != nil || len(cfg.Models["gpt-6.1-sol"].Aliases) != 1 {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
}

func TestLoadDoesNotMigrateOrRewrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	input := []byte(`{"version":1,"models":{"sol":[{"provider":"codex","model":"gpt-6.1-sol"}]}}`)
	if err := os.WriteFile(path, input, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DISPATCH_CONFIG", path)
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "config migrate") {
		t.Fatalf("err=%v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(input) {
		t.Fatal("Load rewrote legacy config")
	}
}

func TestProviderDiagnosticsAreNotSerialized(t *testing.T) {
	cfg := Default()
	cfg.Providers["codex"] = ProviderStatus{Executable: "/bin/codex", Surfaces: []string{"cli"}}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"available"`, `"catalog_model_count"`, `"version":"1.2"`} {
		if strings.Contains(string(data), field) {
			t.Fatalf("diagnostic leaked: %s", data)
		}
	}
}
