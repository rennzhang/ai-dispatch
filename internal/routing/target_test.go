package routing

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const routeConfig = `{
 "version":2,
 "defaults":{"provider":"omp","surface":"cli","model":"gpt-6.1-sol"},
 "models":{
  "gpt-6.1-sol":{"aliases":["sol","gpt6.1-sol"],"targets":{
   "omp":{"cli":{"model":"magpie/codex/gpt-6.1-sol","origin":"scan","efforts":["low","high"]}},
   "codex":{"cli":{"model":"gpt-6.1-sol","origin":"manual","efforts":["high"]},"app":{"model":"gpt-6.1-sol","origin":"scan","efforts":["high","xhigh"],"fast":true}}
  }},
  "grok-4.7":{"aliases":["grok4.7"],"targets":{
   "grok":{"cli":{"model":"grok-4.7","origin":"scan"}},
   "omp":{"cli":{"model":"magpie/grok/grok-4.7","origin":"scan"}}
  }},
  "fable":{"targets":{
   "claude":{"cli":{"model":"claude-fable-5","origin":"manual"}},
   "cursor":{"cli":{"model":"claude-fable-5-thinking-high","origin":"manual"}}
  }},
  "native-other":{"targets":{"grok":{"cli":{"model":"shared-id","origin":"scan"}}}}
 },
 "providers":{
  "omp":{"executable":"/opt/bin/omp","surfaces":["cli"]},
  "codex":{"executable":"/app/bin/codex","surfaces":["cli","app"]},
  "grok":{"executable":"/opt/bin/grok","surfaces":["cli"]}
 }
}`

func TestResolveSelectionMatrix(t *testing.T) {
	writeConfig(t, routeConfig)
	for _, tc := range []struct {
		name, target, model, surface, provider, id, resolvedSurface, source string
		verified                                                            bool
	}{
		{name: "default", provider: "omp", id: "magpie/codex/gpt-6.1-sol", resolvedSurface: "cli", source: "default", verified: true},
		{name: "default named model", target: "sol", provider: "omp", id: "magpie/codex/gpt-6.1-sol", resolvedSurface: "cli", source: "config", verified: true},
		{name: "default model flag", model: "sol", provider: "omp", id: "magpie/codex/gpt-6.1-sol", resolvedSurface: "cli", source: "default", verified: true},
		{name: "explicit omp", target: "omp", model: "sol", provider: "omp", id: "magpie/codex/gpt-6.1-sol", resolvedSurface: "cli", source: "provider", verified: true},
		{name: "codex app", target: "codex", model: "sol", surface: "app", provider: "codex", id: "gpt-6.1-sol", resolvedSurface: "app", source: "provider", verified: true},
		{name: "codex cli", target: "codex", model: "sol", provider: "codex", id: "gpt-6.1-sol", resolvedSurface: "cli", source: "provider", verified: true},
		{name: "grok explicit", target: "grok", model: "grok4.7", provider: "grok", id: "grok-4.7", resolvedSurface: "cli", source: "provider", verified: true},
		{name: "grok provider native default", target: "grok", provider: "grok", resolvedSurface: "cli", source: "provider"},
		{name: "omp provider native default", target: "omp", provider: "omp", resolvedSurface: "cli", source: "provider"},
		{name: "codex provider native default", target: "codex", provider: "codex", resolvedSurface: "cli", source: "provider"},
		{name: "unknown explicit native ID", target: "grok", model: "grok-future", provider: "grok", id: "grok-future", resolvedSurface: "cli", source: "provider"},
		{name: "raw ID listed only elsewhere", target: "codex", model: "shared-id", provider: "codex", id: "shared-id", resolvedSurface: "cli", source: "provider"},
		{name: "raw exact for default", target: "magpie/codex/gpt-6.1-sol", provider: "omp", id: "magpie/codex/gpt-6.1-sol", resolvedSurface: "cli", source: "config", verified: true},
		{name: "case insensitive alias", target: "codex", model: "SOL", surface: "app", provider: "codex", id: "gpt-6.1-sol", resolvedSurface: "app", source: "provider", verified: true},
		{name: "unlisted provider default", target: "claude", provider: "claude", resolvedSurface: "cli", source: "provider"},
		{name: "gemini provider alias", target: "gemini", model: "pro", provider: "antigravity", id: "pro", resolvedSurface: "cli", source: "provider"},
		{name: "native ID colon preserved", target: "omp", model: "local/model:latest", provider: "omp", id: "local/model:latest", resolvedSurface: "cli", source: "provider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveSurface(tc.target, tc.model, tc.surface)
			if err != nil {
				t.Fatal(err)
			}
			if got.Provider != tc.provider || got.Model != tc.id || got.Surface != tc.resolvedSurface || got.Source != tc.source || got.ModelListed != tc.verified {
				t.Fatalf("got=%+v", got)
			}
			if got.Requested != tc.target {
				t.Fatalf("request/fallback metadata=%+v", got)
			}
			if tc.provider == "omp" && got.Executable != "/opt/bin/omp" {
				t.Fatalf("path not preserved: %+v", got)
			}
		})
	}
}

func TestResolveDoesNotSwitchEntryForMissingMapping(t *testing.T) {
	writeConfig(t, routeConfig)
	for _, tc := range []struct{ target, model, surface, contains string }{
		{"fable", "", "", "has no omp/cli target"},
		{"codex", "fable", "", "has no codex/cli target"},
		{"grok", "sol", "", "has no grok/cli target"},
		{"grok", "grok-4.7", "app", "only supported by codex"},
		{"", "sol", "app", "only supported by codex"},
		{"sol", "other", "", "cannot combine"},
		{"made-up", "", "", "unsupported target"},
		{"codex:sol", "", "", "colon route syntax"},
		{"codex", "sol", "web", "surface must be"},
	} {
		_, err := ResolveSurface(tc.target, tc.model, tc.surface)
		if err == nil || !strings.Contains(err.Error(), tc.contains) {
			t.Fatalf("ResolveSurface(%q,%q,%q) err=%v", tc.target, tc.model, tc.surface, err)
		}
	}
}

func TestResolveExactNativeIDCannotBeBlockedByOtherEntry(t *testing.T) {
	writeConfig(t, `{"models":{"gpt-6.1-sol":{"targets":{"codex":{"cli":{"model":"gpt-6.1-sol"}}}}}}`)
	got, err := Resolve("grok", "gpt-6.1-sol")
	if err != nil || got.Provider != "grok" || got.Model != "gpt-6.1-sol" || got.ModelListed {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestResolveSurfaceCapabilitiesRemainEntrySpecific(t *testing.T) {
	writeConfig(t, routeConfig)
	cli, err := ResolveSurface("codex", "sol", "cli")
	if err != nil {
		t.Fatal(err)
	}
	app, err := ResolveSurface("codex", "sol", "app")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cli.Efforts, []string{"high"}) || cli.Fast != nil || !reflect.DeepEqual(app.Efforts, []string{"high", "xhigh"}) || app.Fast == nil || !*app.Fast {
		t.Fatalf("cli=%+v app=%+v", cli, app)
	}
}

func TestResolveUsesConfiguredDefaultSurfaceOnlyWhenEntryOmitted(t *testing.T) {
	writeConfig(t, strings.Replace(routeConfig, `"defaults":{"provider":"omp","surface":"cli"`, `"defaults":{"provider":"codex","surface":"app"`, 1))
	got, err := Resolve("sol", "")
	if err != nil || got.Provider != "codex" || got.Surface != "app" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	explicit, err := Resolve("codex", "sol")
	if err != nil || explicit.Surface != "cli" {
		t.Fatalf("explicit=%+v err=%v", explicit, err)
	}
}

func TestResolveRejectsLegacyWithoutRewriting(t *testing.T) {
	writeConfig(t, `{"version":1,"models":{"codex":[{"provider":"omp","model":"x"}]}}`)
	if _, err := Resolve("codex", ""); err == nil || !strings.Contains(err.Error(), "config migrate") {
		t.Fatalf("err=%v", err)
	}
}

func TestConfiguredTargetsIncludesNamesAndAliases(t *testing.T) {
	writeConfig(t, routeConfig)
	targets := ConfigModelTargets()
	for _, name := range []string{"sol", "gpt6.1-sol", "grok4.7"} {
		if !contains(targets, name) {
			t.Fatalf("missing %s: %v", name, targets)
		}
	}
}

func TestMissingConfigStillAllowsExplicitEntryAndNativeID(t *testing.T) {
	isolateConfig(t)
	got, err := Resolve("codex", "gpt-future")
	if err != nil || got.Provider != "codex" || got.Model != "gpt-future" || got.Executable != "" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestMissingDefaultModelRequiresDiscoveryButExplicitIDStillPassesThrough(t *testing.T) {
	isolateConfig(t)
	if _, err := Resolve("", ""); err == nil || !strings.Contains(err.Error(), "run init or providers scan") {
		t.Fatalf("err=%v", err)
	}
	got, err := Resolve("", "gpt-6.1-sol")
	if err != nil || got.Provider != "omp" || got.Model != "gpt-6.1-sol" || got.ModelListed {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	for _, provider := range []string{"codex", "grok", "omp"} {
		got, err := Resolve(provider, "")
		if err != nil || got.Provider != provider || got.Model != "" {
			t.Fatalf("got=%+v err=%v", got, err)
		}
	}
}

func TestPreservedAliasWithoutDiscoveredTargetCannotBeExecuted(t *testing.T) {
	writeConfig(t, `{"models":{"gpt-6.1-sol":{"aliases":["sol"],"targets":{}}}}`)
	if _, err := Resolve("omp", "sol"); err == nil || !strings.Contains(err.Error(), "no omp/cli target") {
		t.Fatalf("err=%v", err)
	}
}

func TestNativeIDDoesNotArbitrarilyChooseConflictingCapabilities(t *testing.T) {
	writeConfig(t, `{"models":{
 "one":{"targets":{"codex":{"cli":{"model":"shared-id","efforts":["high"]}}}},
 "two":{"targets":{"codex":{"cli":{"model":"shared-id","efforts":["low"]}}}}
}}`)
	if _, err := Resolve("codex", "shared-id"); err == nil || !strings.Contains(err.Error(), "ambiguous capabilities") {
		t.Fatalf("err=%v", err)
	}
	got, err := Resolve("codex", "one")
	if err != nil || !reflect.DeepEqual(got.Efforts, []string{"high"}) {
		t.Fatalf("got=%+v err=%v", got, err)
	}
}

func TestRepeatedNativeIDWithSameCapabilitiesIsNotAmbiguous(t *testing.T) {
	writeConfig(t, `{"models":{
 "one":{"targets":{"codex":{"cli":{"model":"shared-id","efforts":["high"],"origin":"manual"}}}},
 "two":{"targets":{"codex":{"cli":{"model":"shared-id","efforts":["high"],"origin":"scan"}}}}
}}`)
	got, err := Resolve("codex", "shared-id")
	if err != nil || got.Model != "shared-id" || !got.ModelListed || !reflect.DeepEqual(got.Efforts, []string{"high"}) {
		t.Fatalf("got=%+v err=%v", got, err)
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
