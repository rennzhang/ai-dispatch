package setup

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/providerinfo"
	"github.com/rennzhang/ai-dispatch/internal/providers/codex"
)

type ScanOptions struct {
	Refresh bool
	DryRun  bool
}

type SurfaceScan struct {
	Status       string `json:"status"`
	ModelCount   int    `json:"model_count"`
	Error        string `json:"error,omitempty"`
	GUISelection string `json:"gui_selection,omitempty"`
	Availability string `json:"availability,omitempty"`
}

type ProviderScan struct {
	Executable string                 `json:"executable,omitempty"`
	Status     string                 `json:"status"`
	Surfaces   map[string]SurfaceScan `json:"surfaces"`
}

type ScanReport struct {
	Config    config.Config           `json:"config"`
	Providers map[string]ProviderScan `json:"providers"`
	Warnings  []string                `json:"warnings,omitempty"`
	DryRun    bool                    `json:"dry_run"`
}

type discoveredModel struct {
	Key     string
	ID      string
	Efforts []string
}

type discoveredProvider struct {
	Name                 string
	Report               ProviderScan
	Models               map[string][]discoveredModel
	ConfiguredExecutable string
	SnapshotKnown        bool
}

// Scan only reads executable and model catalogs. It never sends a prompt,
// installs a CLI, or changes another provider's configuration.
func Scan(opts ScanOptions) (ScanReport, error) {
	cfg, err := config.Load()
	if err != nil {
		return ScanReport{}, err
	}
	discovered := collectDiscovery(cfg, opts.Refresh)
	return applyDiscovery(discovered, opts.DryRun)
}

func collectDiscovery(cfg config.Config, refresh bool) map[string]discoveredProvider {
	results := make(chan discoveredProvider, len(providerinfo.Names()))
	for _, name := range providerinfo.Names() {
		go func(name string) { results <- discoverProvider(name, refresh, cfg.Providers[name].Executable) }(name)
	}
	discovered := make(map[string]discoveredProvider)
	for range providerinfo.Names() {
		item := <-results
		discovered[item.Name] = item
	}
	return discovered
}

func discoverProvider(name string, refresh bool, configured ...string) discoveredProvider {
	item := discoveredProvider{Name: name, Models: map[string][]discoveredModel{}, Report: ProviderScan{
		Status: "not_found", Surfaces: map[string]SurfaceScan{"cli": {Status: "not_found"}},
	}}
	if len(configured) > 0 {
		item.SnapshotKnown, item.ConfiguredExecutable = true, configured[0]
	}
	bin, err := providerinfo.ResolveBinary(name, configured...)
	if err != nil && len(configured) > 0 && configured[0] != "" {
		item.Report.Executable = configured[0]
		item.Report.Status = "unavailable"
		item.Report.Surfaces["cli"] = SurfaceScan{Status: "failed", Error: "configured_executable_unavailable"}
	}
	if err == nil {
		item.Report.Executable = bin
		item.Report.Status = "available"
		if _, err := runVersion(bin); err != nil {
			item.Report.Status = "unavailable"
			item.Report.Surfaces["cli"] = SurfaceScan{Status: "failed", Error: "executable_check_failed"}
		} else {
			discoverCLISurface(&item, bin, refresh)
		}
	}
	if name == "codex" {
		discoverDesktopSurface(&item)
	}
	return item
}

func discoverCLISurface(item *discoveredProvider, bin string, refresh bool) {
	if item.Name == "claude" {
		item.Report.Surfaces["cli"] = SurfaceScan{Status: "unsupported", Error: "no_model_catalog_interface"}
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	models, err := readModels(ctx, item.Name, bin, refresh)
	setSurface(item, "cli", models, err)
}

func discoverDesktopSurface(item *discoveredProvider) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	bin, err := codex.DesktopExecutable(ctx)
	if err != nil {
		if errors.Is(err, codex.ErrDesktopNotInstalled) {
			item.Report.Surfaces["app"] = SurfaceScan{Status: "not_found", Error: "desktop_not_installed"}
			return
		}
		// Lookup errors also include permission failures or ambiguous runtime
		// registrations. They do not prove that the application is absent.
		item.Report.Surfaces["app"] = SurfaceScan{Status: "failed", Error: "desktop_lookup_failed"}
		if item.Report.Executable == "" {
			item.Report.Status = "unknown"
		}
		return
	}
	if item.Report.Executable == "" {
		item.Report.Executable = bin
		item.Report.Status = "available"
	}
	models, err := codex.DiscoverCLIModels(ctx, bin)
	setSurface(item, "app", fromCodexModels(models), err)
	status := item.Report.Surfaces["app"]
	status.GUISelection = "not_checked"
	item.Report.Surfaces["app"] = status
}

func setSurface(item *discoveredProvider, surface string, models []discoveredModel, err error) {
	if err != nil {
		item.Report.Surfaces[surface] = SurfaceScan{Status: "failed", Error: "model_catalog_failed"}
		return
	}
	item.Report.Surfaces[surface] = SurfaceScan{Status: "success", ModelCount: len(models), Availability: "listed_not_tested"}
	item.Models[surface] = models
}

func readModels(ctx context.Context, name, bin string, refresh bool) ([]discoveredModel, error) {
	switch name {
	case "codex":
		models, err := codex.DiscoverCLIModels(ctx, bin)
		return fromCodexModels(models), err
	case "opencode":
		models, err := listOpenCodeModels(bin, refresh)
		out := make([]discoveredModel, 0, len(models))
		for _, id := range models {
			out = append(out, discoveredModel{Key: nativeModelKey(name, id), ID: id})
		}
		return out, err
	case "omp":
		raw, err := catalogCommand(ctx, bin, "models", "--json", "--no-extensions")
		if err != nil {
			return nil, err
		}
		return parseOMPModels(raw)
	case "grok", "cursor", "antigravity":
		raw, err := catalogCommand(ctx, bin, "models")
		if err != nil {
			return nil, err
		}
		return parseTextModels(name, raw)
	default:
		return nil, fmt.Errorf("model catalog unsupported")
	}
}

func fromCodexModels(models []codex.DesktopModel) []discoveredModel {
	out := make([]discoveredModel, 0, len(models))
	for _, model := range models {
		out = append(out, discoveredModel{Key: nativeModelKey("codex", model.ID), ID: model.ID, Efforts: supportedEfforts(model.Efforts)})
	}
	return out
}

func supportedEfforts(values []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, value := range values {
		if value == "off" {
			value = "none"
		}
		switch value {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
			if !seen[value] {
				out = append(out, value)
				seen[value] = true
			}
		}
	}
	return out
}

// These are explicit identities, not a generic namespace-stripping heuristic.
// Other source IDs remain distinct until an adapter or the user confirms them.
var sharedIdentities = map[string]string{
	"omp:magpie/codex/gpt-6.1-sol":   "gpt-6.1-sol",
	"omp:magpie/codex/gpt-6-astra":   "gpt-6-astra",
	"omp:magpie/codex/gpt-6.1-terra": "gpt-6.1-terra",
	"omp:magpie/grok/grok-4.7":       "grok-4.7",
}

func nativeModelKey(provider, id string) string {
	if key := sharedIdentities[provider+":"+id]; key != "" {
		return key
	}
	if provider == "codex" || provider == "grok" {
		return id
	}
	return provider + "/" + strings.TrimSpace(id)
}
