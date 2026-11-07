package config

import (
	"fmt"
	"github.com/rennzhang/ai-dispatch/internal/providerinfo"
	"path/filepath"
	"strings"
)

// ProviderName is shared by validation and routing; aliases cannot shadow it.
func ProviderName(name string) (string, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "gemini" {
		name = "antigravity"
	}
	if _, ok := providerinfo.Lookup(name); ok {
		return name, true
	}
	return "", false
}

func normalize(cfg Config) (Config, error) {
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	if cfg.Version != Version {
		return Config{}, fmt.Errorf("config schema version must be %d", Version)
	}
	if cfg.ClaudeTransport == "" {
		cfg.ClaudeTransport = "print"
	}
	if !ValidClaudeTransport(cfg.ClaudeTransport) {
		return Config{}, fmt.Errorf("claude_transport must be print, pty, auto, or disabled")
	}
	if cfg.Defaults.Provider == "" {
		cfg.Defaults.Provider = "omp"
	}
	if cfg.Defaults.Surface == "" {
		cfg.Defaults.Surface = "cli"
	}
	if provider, ok := ProviderName(cfg.Defaults.Provider); !ok || provider != cfg.Defaults.Provider {
		return Config{}, fmt.Errorf("defaults.provider is unsupported: %q", cfg.Defaults.Provider)
	}
	if err := validateSurface(cfg.Defaults.Provider, cfg.Defaults.Surface); err != nil {
		return Config{}, fmt.Errorf("defaults: %w", err)
	}
	if cfg.Models == nil {
		cfg.Models = ModelsConfig{}
	}
	if cfg.Providers == nil {
		cfg.Providers = ProvidersConfig{}
	}
	if err := validateModels(cfg.Models); err != nil {
		return Config{}, err
	}
	for name, status := range cfg.Providers {
		provider, ok := ProviderName(name)
		if !ok || provider != name {
			return Config{}, fmt.Errorf("providers.%s is not a canonical supported provider", name)
		}
		if !filepath.IsAbs(status.Executable) {
			return Config{}, fmt.Errorf("providers.%s.executable must be an absolute path", name)
		}
		if len(status.Surfaces) == 0 {
			return Config{}, fmt.Errorf("providers.%s.surfaces must not be empty", name)
		}
		seen := map[string]bool{}
		for _, surface := range status.Surfaces {
			if err := validateSurface(name, surface); err != nil {
				return Config{}, fmt.Errorf("providers.%s: %w", name, err)
			}
			if seen[surface] {
				return Config{}, fmt.Errorf("providers.%s has duplicate surface %q", name, surface)
			}
			seen[surface] = true
		}
	}
	return cfg, nil
}

func validateModels(models ModelsConfig) error {
	names := map[string]string{}
	for key := range models {
		if err := registerModelName(names, key, key); err != nil {
			return err
		}
	}
	for key, entry := range models {
		for _, alias := range entry.Aliases {
			if err := registerModelName(names, alias, key); err != nil {
				return err
			}
		}
		if len(entry.Targets) == 0 && len(entry.Aliases) == 0 {
			return fmt.Errorf("models.%s.targets must not be empty unless preserving model aliases", key)
		}
		for provider, surfaces := range entry.Targets {
			canonical, ok := ProviderName(provider)
			if !ok || canonical != provider || len(surfaces) == 0 {
				return fmt.Errorf("models.%s.targets.%s must name a supported provider and contain surfaces", key, provider)
			}
			for surface, target := range surfaces {
				at := "models." + key + ".targets." + provider + "." + surface
				if err := validateSurface(provider, surface); err != nil {
					return fmt.Errorf("%s: %w", at, err)
				}
				if strings.TrimSpace(target.Model) == "" {
					return fmt.Errorf("%s.model is required", at)
				}
				if target.Origin == "" {
					target.Origin = "manual"
				}
				if target.Origin != "scan" && target.Origin != "manual" {
					return fmt.Errorf("%s.origin must be scan or manual", at)
				}
				if err := validateEfforts(at, target.Efforts); err != nil {
					return err
				}
				surfaces[surface] = target
			}
		}
	}
	return nil
}

func registerModelName(names map[string]string, name, key string) error {
	if name == "" || strings.TrimSpace(name) != name {
		return fmt.Errorf("model names and aliases must be nonempty and have no surrounding whitespace")
	}
	if _, ok := ProviderName(name); ok {
		return fmt.Errorf("model name or alias %q is reserved for a provider", name)
	}
	normalized := strings.ToLower(name)
	if prior, ok := names[normalized]; ok {
		return fmt.Errorf("model name or alias %q conflicts with models.%s", name, prior)
	}
	names[normalized] = key
	return nil
}

func validateSurface(provider, surface string) error {
	if surface != "cli" && surface != "app" {
		return fmt.Errorf("surface must be cli or app")
	}
	if surface == "app" && provider != "codex" {
		return fmt.Errorf("surface app is only supported by codex")
	}
	return nil
}

func validateEfforts(at string, efforts []string) error {
	seen := map[string]bool{}
	for _, effort := range efforts {
		switch effort {
		case "none", "minimal", "low", "medium", "high", "xhigh", "max":
		default:
			return fmt.Errorf("%s.efforts contains unsupported effort %q; omit efforts for auto", at, effort)
		}
		if seen[effort] {
			return fmt.Errorf("%s.efforts contains duplicate %q", at, effort)
		}
		seen[effort] = true
	}
	return nil
}
