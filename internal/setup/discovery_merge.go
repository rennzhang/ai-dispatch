package setup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/providerinfo"
)

// WithConfigLock serializes all read/modify/write configuration commands.
// The permanent empty lock file is not configuration or a second catalog.
func WithConfigLock(update func() error) error {
	path := config.ConfigPath() + ".lock"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			return err
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("config is being updated by another command")
		}
		time.Sleep(20 * time.Millisecond)
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	return update()
}

func applyDiscovery(discovered map[string]discoveredProvider, dryRun bool) (ScanReport, error) {
	report := discoveryReport(discovered, dryRun)
	apply := func() error {
		// Discovery can take seconds. Re-read after acquiring the writer lock,
		// so another scan's or migration's defaults/manual entries survive.
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		mergeDiscovery(&cfg, discovered, &report)
		if err := config.Validate(cfg); err != nil {
			return err
		}
		report.Config = cfg
		if dryRun {
			return nil
		}
		return config.Save(cfg)
	}
	var err error
	if dryRun {
		err = apply()
	} else {
		err = WithConfigLock(apply)
	}
	return report, err
}

func discoveryReport(discovered map[string]discoveredProvider, dryRun bool) ScanReport {
	report := ScanReport{Providers: map[string]ProviderScan{}, DryRun: dryRun}
	for name, item := range discovered {
		report.Providers[name] = item.Report
	}
	return report
}

func mergeDiscovery(cfg *config.Config, discovered map[string]discoveredProvider, report *ScanReport) {
	confirmed := map[string]bool{}
	if report.Providers == nil {
		report.Providers = map[string]ProviderScan{}
	}
	if cfg.Providers == nil {
		cfg.Providers = config.ProvidersConfig{}
	}
	if cfg.Models == nil {
		cfg.Models = config.ModelsConfig{}
	}
	for _, name := range providerinfo.Names() {
		item, ok := discovered[name]
		if !ok {
			continue
		}
		if scanPathChanged(cfg, item, report) {
			continue
		}
		for surface, models := range item.Models {
			for index := range models {
				if identity := sharedIdentity(name, models[index].ID); identity != "" {
					confirmed[identity] = true
				}
				if key := existingModelKey(cfg.Models, name, surface, models[index].ID); key != "" {
					models[index].Key = key
				}
			}
		}
		if item.Report.Executable == "" {
			delete(cfg.Providers, name)
		} else {
			cfg.Providers[name] = discoveredStatus(item)
		}
		for surface, status := range item.Report.Surfaces {
			if status.Status == "success" || status.Status == "not_found" {
				removeScannedTargets(cfg.Models, name, surface)
			}
			if status.Status == "failed" || status.Status == "unsupported" {
				report.Warnings = append(report.Warnings, name+"/"+surface+": "+status.Status+"; existing model entries retained")
			}
		}
		for _, surface := range []string{"cli", "app"} {
			models := item.Models[surface]
			sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
			for _, model := range models {
				if err := addDiscoveredModel(cfg.Models, name, surface, model); err != nil {
					report.Warnings = append(report.Warnings, name+"/"+surface+": "+err.Error())
				}
			}
		}
	}
	consolidateSharedModels(cfg.Models, report, confirmed)
}

func scanPathChanged(cfg *config.Config, item discoveredProvider, report *ScanReport) bool {
	current := cfg.Providers[item.Name].Executable
	if !item.SnapshotKnown || current == item.ConfiguredExecutable {
		return false
	}
	stale := item.Report
	stale.Executable, stale.Status = current, "stale"
	for surface := range stale.Surfaces {
		stale.Surfaces[surface] = SurfaceScan{Status: "stale", Error: "config_changed_during_scan"}
	}
	report.Providers[item.Name] = stale
	report.Warnings = append(report.Warnings, item.Name+": executable changed during scan; results skipped, run scan again")
	return true
}

func discoveredStatus(item discoveredProvider) config.ProviderStatus {
	status := config.ProviderStatus{Executable: item.Report.Executable}
	for _, surface := range []string{"cli", "app"} {
		if result, ok := item.Report.Surfaces[surface]; ok && result.Status != "not_found" {
			status.Surfaces = append(status.Surfaces, surface)
		}
	}
	if item.Report.Status == "unavailable" {
		status.Error = "executable_check_failed"
	}
	return status
}

func removeScannedTargets(models config.ModelsConfig, provider, surface string) {
	for key, entry := range models {
		targets := entry.Targets[provider]
		if target, ok := targets[surface]; ok && target.Origin == "scan" {
			delete(targets, surface)
			if len(targets) == 0 {
				delete(entry.Targets, provider)
			}
			// Keep user aliases, even when their catalog target disappears.
			if len(entry.Targets) == 0 && len(entry.Aliases) == 0 {
				delete(models, key)
			} else {
				models[key] = entry
			}
		}
	}
}

func addDiscoveredModel(models config.ModelsConfig, provider, surface string, model discoveredModel) error {
	key := existingModelKey(models, provider, surface, model.ID)
	if key == "" {
		key = model.Key
	}
	entry := models[key]
	current, occupied := entry.Targets[provider][surface]
	if aliasClaimsKey(models, key) || (occupied && current.Model != model.ID) {
		// A logical model can have several source variants through the same
		// CLI. One target must not overwrite another; keep a distinct key.
		key = provider + "/" + model.ID
		entry = models[key]
	}
	if aliasClaimsKey(models, key) {
		return fmt.Errorf("catalog key conflicts with a manual alias; model entry skipped")
	}
	if current, ok := entry.Targets[provider][surface]; ok && current.Model != model.ID {
		return fmt.Errorf("catalog key conflicts with an existing native target; model entry skipped")
	}
	if entry.Targets == nil {
		entry.Targets = map[string]map[string]config.ModelTarget{}
	}
	if entry.Targets[provider] == nil {
		entry.Targets[provider] = map[string]config.ModelTarget{}
	}
	if current, ok := entry.Targets[provider][surface]; ok && current.Origin != "scan" {
		return nil
	}
	entry.Targets[provider][surface] = config.ModelTarget{Model: model.ID, Origin: "scan", Efforts: model.Efforts}
	models[key] = entry
	return nil
}

func aliasClaimsKey(models config.ModelsConfig, key string) bool {
	for _, entry := range models {
		for _, alias := range entry.Aliases {
			if strings.EqualFold(alias, key) {
				return true
			}
		}
	}
	return false
}

func existingModelKey(models config.ModelsConfig, provider, surface, id string) string {
	keys := make([]string, 0, len(models))
	for key := range models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if target, ok := models[key].Targets[provider][surface]; ok && target.Model == id {
			return key
		}
	}
	return ""
}
