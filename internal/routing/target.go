package routing

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

// DispatchTarget is one fully resolved execution, never an ordered fallback
// pool. Executable and model capabilities come from the same config snapshot.
type DispatchTarget struct {
	Requested   string   `json:"requested"`
	Provider    string   `json:"provider"`
	Surface     string   `json:"surface"`
	Executable  string   `json:"executable,omitempty"`
	Model       string   `json:"model,omitempty"`
	Source      string   `json:"source"`
	ModelKey    string   `json:"model_key,omitempty"`
	ActualID    string   `json:"actual_id,omitempty"`
	ModelListed bool     `json:"model_listed"`
	Efforts     []string `json:"efforts,omitempty"`
	Fast        *bool    `json:"fast,omitempty"`
}

func Resolve(rawTarget, explicitModel string) (DispatchTarget, error) {
	return ResolveSurface(rawTarget, explicitModel, "")
}

func ResolveSurface(rawTarget, explicitModel, surface string) (DispatchTarget, error) {
	cfg, err := config.Load()
	if err != nil {
		return DispatchTarget{}, err
	}
	return resolve(cfg, rawTarget, explicitModel, surface)
}

func resolve(cfg config.Config, rawTarget, explicitModel, surface string) (DispatchTarget, error) {
	requested := strings.TrimSpace(rawTarget)
	model := strings.TrimSpace(explicitModel)
	implicitDefault := requested == "" && model == ""
	surface = strings.ToLower(strings.TrimSpace(surface))
	if strings.Contains(requested, ":") {
		return DispatchTarget{}, fmt.Errorf("colon route syntax is not supported; use a provider target with --model")
	}
	provider, explicitProvider := config.ProviderName(requested)
	source := "provider"
	if !explicitProvider {
		provider = cfg.Defaults.Provider
		source = "default"
		if surface == "" {
			surface = cfg.Defaults.Surface
		}
		if requested != "" {
			if model != "" {
				return DispatchTarget{}, fmt.Errorf("cannot combine explicit model with model target")
			}
			model = requested
			source = "config"
		} else if model == "" {
			model = cfg.Defaults.Model
		}
	}
	if surface == "" {
		surface = "cli"
	}
	if surface != "cli" && surface != "app" {
		return DispatchTarget{}, fmt.Errorf("surface must be cli or app")
	}
	if surface == "app" && provider != "codex" {
		return DispatchTarget{}, fmt.Errorf("surface app is only supported by codex; specify codex --surface app")
	}
	target := DispatchTarget{Requested: requested, Provider: provider, Surface: surface, Source: source, Executable: cfg.Providers[provider].Executable}
	if model == "" {
		return target, nil
	}
	key, entry, known := lookupModel(cfg, model)
	if known {
		if resolved, ok := entry.Targets[provider][surface]; ok {
			return modelTarget(target, key, resolved), nil
		}
		// An exact native ID remains valid input to an explicitly selected entry,
		// even when the same literal is catalogued under a different provider.
		if explicitProvider && !isAlias(entry, model) && isNativeID(cfg, model) {
			return nativeTarget(target, model), nil
		}
		if implicitDefault {
			return DispatchTarget{}, defaultModelMissing(model, provider, surface)
		}
		return DispatchTarget{}, fmt.Errorf("model %q has no %s/%s target; specify a supported entry or an explicit native model ID", model, provider, surface)
	}
	if exactKey, exact, ok, err := lookupExact(cfg, provider, surface, model); err != nil {
		return DispatchTarget{}, err
	} else if ok {
		return modelTarget(target, exactKey, exact), nil
	}
	if implicitDefault {
		return DispatchTarget{}, defaultModelMissing(model, provider, surface)
	}
	if requested != "" && !explicitProvider {
		return DispatchTarget{}, fmt.Errorf("unsupported target: %s; specify a provider with --model for an unlisted native ID", requested)
	}
	return nativeTarget(target, model), nil
}

func defaultModelMissing(model, provider, surface string) error {
	return fmt.Errorf("default model %q has no discovered or configured %s/%s target; run init or providers scan before default dispatch", model, provider, surface)
}

func modelTarget(target DispatchTarget, key string, model config.ModelTarget) DispatchTarget {
	target.Model, target.ActualID, target.ModelKey = model.Model, model.Model, key
	target.ModelListed = true
	target.Efforts = append([]string(nil), model.Efforts...)
	target.Fast = copyBoolPtr(model.Fast)
	return target
}

func nativeTarget(target DispatchTarget, model string) DispatchTarget {
	target.Model, target.ActualID = model, model
	return target
}

func lookupModel(cfg config.Config, value string) (string, config.ModelEntry, bool) {
	for key, entry := range cfg.Models {
		if strings.EqualFold(key, value) || isAlias(entry, value) {
			return key, entry, true
		}
	}
	return "", config.ModelEntry{}, false
}

func isAlias(entry config.ModelEntry, value string) bool {
	for _, alias := range entry.Aliases {
		if strings.EqualFold(alias, value) {
			return true
		}
	}
	return false
}

func isNativeID(cfg config.Config, value string) bool {
	for _, entry := range cfg.Models {
		for _, surfaces := range entry.Targets {
			for _, target := range surfaces {
				if target.Model == value {
					return true
				}
			}
		}
	}
	return false
}

func lookupExact(cfg config.Config, provider, surface, value string) (string, config.ModelTarget, bool, error) {
	keys := sortedModelKeys(cfg)
	var selected config.ModelTarget
	selectedKey := ""
	for _, key := range keys {
		target, ok := cfg.Models[key].Targets[provider][surface]
		if ok && target.Model == value {
			if selectedKey != "" && (!reflect.DeepEqual(selected.Efforts, target.Efforts) || !reflect.DeepEqual(selected.Fast, target.Fast)) {
				return "", config.ModelTarget{}, false, fmt.Errorf("native model ID %q has ambiguous capabilities for %s/%s in models.%s and models.%s; use a logical model name or correct the config", value, provider, surface, selectedKey, key)
			}
			selectedKey, selected = key, target
		}
	}
	return selectedKey, selected, selectedKey != "", nil
}

func ConfigModelTargets() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	targets := []string{}
	for key, entry := range cfg.Models {
		targets = append(targets, key)
		targets = append(targets, entry.Aliases...)
	}
	sort.Strings(targets)
	return targets
}

func sortedModelKeys(cfg config.Config) []string {
	keys := make([]string, 0, len(cfg.Models))
	for key := range cfg.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func copyBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
