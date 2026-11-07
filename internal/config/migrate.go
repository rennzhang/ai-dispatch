package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

type MigrationReport struct {
	FromVersion int      `json:"from_version"`
	ToVersion   int      `json:"to_version"`
	Warnings    []string `json:"warnings"`
	Conflicts   []string `json:"conflicts"`
}

type legacyRoute struct {
	Provider string   `json:"provider"`
	Model    string   `json:"model,omitempty"`
	Efforts  []string `json:"efforts,omitempty"`
	Fast     *bool    `json:"fast,omitempty"`
}

type legacyProvider struct {
	Available         bool   `json:"available"`
	Version           string `json:"version,omitempty"`
	CatalogModelCount int    `json:"catalog_model_count,omitempty"`
	Error             string `json:"error,omitempty"`
}

type legacyConfig struct {
	Version           int                       `json:"version"`
	ClaudeTransport   string                    `json:"claude_transport"`
	UserFacingSummary *bool                     `json:"user_facing_summary,omitempty"`
	Models            map[string][]legacyRoute  `json:"models"`
	Providers         map[string]legacyProvider `json:"providers"`
}

// Migrate converts a legacy file without writing it. The caller must present
// warnings and reject conflicts before replacing the one runtime config.
func Migrate(data []byte) (Config, MigrationReport, error) {
	report := MigrationReport{ToVersion: Version, Warnings: []string{}, Conflicts: []string{}}
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Config{}, report, err
	}
	report.FromVersion = header.Version
	if header.Version == 0 {
		if cfg, err := Decode(data); err == nil {
			report.FromVersion = Version
			return cfg, report, nil
		}
	}
	if header.Version == Version {
		cfg, err := Decode(data)
		return cfg, report, err
	}
	if header.Version != 0 && header.Version != 1 {
		return Config{}, report, fmt.Errorf("cannot migrate config schema version %d", header.Version)
	}
	var old legacyConfig
	if err := strictDecode(data, &old); err != nil {
		return Config{}, report, fmt.Errorf("invalid legacy config: %w", err)
	}
	cfg := Default()
	if old.ClaudeTransport != "" {
		cfg.ClaudeTransport = old.ClaudeTransport
	}
	cfg.UserFacingSummary = old.UserFacingSummary
	keys := make([]string, 0, len(old.Models))
	for key := range old.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		migrateEntry(&cfg, &report, key, old.Models[key])
	}
	reportMigrationCapabilityConflicts(cfg, &report)
	if len(old.Providers) > 0 {
		report.Warnings = append(report.Warnings, "Legacy provider diagnostics have no executable paths; run providers scan to discover installed entries.")
	}
	if len(report.Conflicts) > 0 {
		return cfg, report, fmt.Errorf("migration has %d conflict(s); config was not written", len(report.Conflicts))
	}
	normalized, err := normalize(cfg)
	if err != nil {
		return cfg, report, fmt.Errorf("migrated config is invalid: %w", err)
	}
	return normalized, report, nil
}

func reportMigrationCapabilityConflicts(cfg Config, report *MigrationReport) {
	type identity struct{ provider, surface, model string }
	type declaration struct {
		key    string
		target ModelTarget
	}
	seen := map[identity]declaration{}
	keys := make([]string, 0, len(cfg.Models))
	for key := range cfg.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		for provider, surfaces := range cfg.Models[key].Targets {
			for surface, target := range surfaces {
				id := identity{provider, surface, target.Model}
				if prior, ok := seen[id]; ok && (!reflect.DeepEqual(prior.target.Efforts, target.Efforts) || !reflect.DeepEqual(prior.target.Fast, target.Fast)) {
					report.Conflicts = append(report.Conflicts, fmt.Sprintf("models.%s and models.%s declare conflicting capabilities for %s/%s model %q", prior.key, key, provider, surface, target.Model))
				}
				seen[id] = declaration{key, target}
			}
		}
	}
}

func migrateEntry(cfg *Config, report *MigrationReport, oldKey string, routes []legacyRoute) {
	key := strings.TrimSpace(oldKey)
	if key == "" || len(routes) == 0 {
		report.Conflicts = append(report.Conflicts, fmt.Sprintf("models.%s has no preservable routes", oldKey))
		return
	}
	entry := ModelEntry{Targets: map[string]map[string]ModelTarget{}}
	for index, route := range routes {
		provider, ok := ProviderName(route.Provider)
		if !ok || strings.TrimSpace(route.Model) == "" {
			report.Conflicts = append(report.Conflicts, fmt.Sprintf("models.%s[%d] needs a supported provider and explicit model ID", oldKey, index))
			continue
		}
		target := ModelTarget{Model: route.Model, Origin: "manual", Efforts: route.Efforts, Fast: route.Fast}
		if entry.Targets[provider] == nil {
			entry.Targets[provider] = map[string]ModelTarget{}
		}
		if prior, exists := entry.Targets[provider]["cli"]; exists && !reflect.DeepEqual(prior, target) {
			report.Conflicts = append(report.Conflicts, fmt.Sprintf("models.%s has multiple different %s/cli targets (%q and %q); split them into distinct models", oldKey, provider, prior.Model, target.Model))
			continue
		}
		entry.Targets[provider]["cli"] = target
	}
	if len(entry.Targets) == 0 {
		return
	}
	if len(routes) > 1 {
		report.Warnings = append(report.Warnings, fmt.Sprintf("models.%s: candidate order removed; supported targets do not imply fallback or priority.", oldKey))
	}
	if _, reserved := ProviderName(key); reserved {
		key = migrationModelKey(routes[0].Model, key+"-model")
		report.Warnings = append(report.Warnings, fmt.Sprintf("models.%s renamed to %s; provider name %s is reserved and no longer a model alias.", oldKey, key, oldKey))
	} else if len(routes) == 1 {
		canonical := migrationModelKey(routes[0].Model, key)
		if canonical != key {
			entry.Aliases = append(entry.Aliases, key)
			key = canonical
		}
	}
	for existingKey, existing := range cfg.Models {
		if reflect.DeepEqual(existing.Targets, entry.Targets) {
			if key != existingKey {
				existing.Aliases = appendAlias(existing.Aliases, key, existingKey)
			}
			for _, alias := range entry.Aliases {
				existing.Aliases = appendAlias(existing.Aliases, alias, existingKey)
			}
			sort.Strings(existing.Aliases)
			cfg.Models[existingKey] = existing
			return
		}
	}
	if _, exists := cfg.Models[key]; exists {
		report.Conflicts = append(report.Conflicts, fmt.Sprintf("models.%s maps to occupied key %s with different capabilities or targets", oldKey, key))
		return
	}
	cfg.Models[key] = entry
}

func migrationModelKey(id, fallback string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return fallback
	}
	if _, reserved := ProviderName(id); reserved {
		return fallback
	}
	return id
}

func appendAlias(aliases []string, alias, key string) []string {
	if strings.EqualFold(alias, key) {
		return aliases
	}
	for _, existing := range aliases {
		if strings.EqualFold(existing, alias) {
			return aliases
		}
	}
	return append(aliases, alias)
}
