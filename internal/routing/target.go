package routing

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

//go:embed models.json
var modelRegistryFS embed.FS

type DispatchTarget struct {
	Requested  string           `json:"requested"`
	Provider   string           `json:"provider"`
	Model      string           `json:"model,omitempty"`
	Source     string           `json:"source"`
	ModelKey   string           `json:"model_key,omitempty"`
	ActualID   string           `json:"actual_id,omitempty"`
	Candidates []RouteCandidate `json:"candidates,omitempty"`
}

type RouteCandidate struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
	Source   string `json:"source,omitempty"`
	ModelKey string `json:"model_key,omitempty"`
	ActualID string `json:"actual_id,omitempty"`
}

func Resolve(rawTarget string, explicitModel string) (DispatchTarget, error) {
	target := strings.TrimSpace(rawTarget)
	model := strings.TrimSpace(explicitModel)
	if target == "" {
		return DispatchTarget{}, fmt.Errorf("target is required")
	}
	normalized := strings.ToLower(target)
	switch normalized {
	case "codex":
		if model == "" {
			var err error
			model, err = defaultProviderModel("codex")
			if err != nil {
				return DispatchTarget{}, err
			}
		}
		return providerTarget(target, "codex", model)
	case "opencode":
		return providerTarget(target, "opencode", model)
	case "claude":
		return providerTarget(target, "claude", model)
	case "antigravity":
		return providerTarget(target, "antigravity", model)
	case "gemini":
		if model == "" {
			var err error
			model, err = defaultProviderModel("antigravity")
			if err != nil {
				return DispatchTarget{}, err
			}
		}
		return providerTarget(target, "antigravity", model)
	}
	if model != "" {
		return DispatchTarget{}, fmt.Errorf("cannot combine explicit model with model target")
	}
	if target, ok, err := lookupConfiguredModelTarget(target); err != nil {
		return DispatchTarget{}, err
	} else if ok {
		return target, nil
	}
	if match, ok, err := lookupRegistryMatch(target); err != nil {
		return DispatchTarget{}, err
	} else if ok {
		entry := match.entry
		provider := normalizeProvider(entry.DispatchRunner)
		if provider == "" {
			provider = normalizeProvider(entry.Provider)
		}
		model := entryModelForProvider(entry, provider)
		if preserveExplicitActualModelID(provider, entry, match) {
			model = strings.TrimSpace(target)
		}
		return DispatchTarget{
			Requested: target,
			Provider:  provider,
			Model:     model,
			Source:    "registry",
			ModelKey:  entry.Key,
			ActualID:  entry.ActualModelID,
		}, nil
	}
	if geminiModel, ok := geminiAliasModel(normalized); ok {
		return DispatchTarget{Requested: target, Provider: "antigravity", Model: geminiModel, Source: "alias"}, nil
	}
	if strings.HasPrefix(normalized, "gpt-") {
		return DispatchTarget{Requested: target, Provider: "codex", Model: target, Source: "inferred"}, nil
	}
	if strings.HasPrefix(normalized, "claude-") || strings.Contains(normalized, "sonnet") || strings.Contains(normalized, "opus") {
		return DispatchTarget{Requested: target, Provider: "claude", Model: target, Source: "inferred"}, nil
	}
	if strings.HasPrefix(normalized, "google/gemini-") {
		return DispatchTarget{Requested: target, Provider: "antigravity", Model: target, Source: "inferred"}, nil
	}
	if strings.HasPrefix(normalized, "openrouter/") || strings.HasPrefix(normalized, "openai/") {
		return DispatchTarget{Requested: target, Provider: "opencode", Model: target, Source: "inferred"}, nil
	}
	return DispatchTarget{}, fmt.Errorf("unsupported target: %s", rawTarget)
}

func providerTarget(target string, provider string, model string) (DispatchTarget, error) {
	resolved := model
	if configured, ok, err := lookupConfiguredModelTarget(model); err != nil {
		return DispatchTarget{}, err
	} else if ok {
		matches := []DispatchTarget{}
		for _, candidate := range CandidateTargets(configured) {
			if candidate.Provider == provider {
				candidate.Requested = target
				candidate.Source = "provider"
				matches = append(matches, candidate)
			}
		}
		if len(matches) > 0 {
			return withCandidates(target, matches), nil
		}
		return DispatchTarget{}, fmt.Errorf("model alias %q has no %s candidate", model, provider)
	}
	if match, ok, err := lookupRegistryMatch(model); err != nil {
		return DispatchTarget{}, err
	} else if ok {
		entry := match.entry
		if preserveExplicitActualModelID(provider, entry, match) {
			resolved = strings.TrimSpace(model)
		} else {
			resolved = entryModelForProvider(entry, provider)
		}
		return DispatchTarget{
			Requested: target,
			Provider:  provider,
			Model:     resolved,
			Source:    "provider",
			ModelKey:  entry.Key,
			ActualID:  entry.ActualModelID,
		}, nil
	}
	return DispatchTarget{
		Requested: target,
		Provider:  provider,
		Model:     resolved,
		Source:    "provider",
	}, nil
}

func preserveExplicitActualModelID(provider string, entry registryEntry, match registryMatch) bool {
	return provider == "claude" &&
		match.kind == registryMatchActualID &&
		strings.TrimSpace(entry.ActualModelID) != "" &&
		strings.TrimSpace(entry.ActualModelID) != strings.TrimSpace(entryModelForProvider(entry, provider))
}

func lookupConfiguredModelTarget(value string) (DispatchTarget, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return DispatchTarget{}, false, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return DispatchTarget{}, false, err
	}
	for key, routes := range cfg.Models {
		if strings.ToLower(strings.TrimSpace(key)) != normalized {
			continue
		}
		candidates := make([]DispatchTarget, 0, len(routes))
		for _, route := range routes {
			provider := normalizeProvider(route.Provider)
			model := strings.TrimSpace(route.Model)
			if provider == "" {
				return DispatchTarget{}, false, fmt.Errorf("config.models.%s has empty provider", key)
			}
			if !implementedProvider(provider) {
				return DispatchTarget{}, false, fmt.Errorf("config.models.%s uses unsupported provider %q", key, route.Provider)
			}
			candidates = append(candidates, DispatchTarget{
				Requested: strings.TrimSpace(value),
				Provider:  provider,
				Model:     model,
				Source:    "config",
				ModelKey:  strings.TrimSpace(key),
				ActualID:  model,
			})
		}
		if len(candidates) == 0 {
			return DispatchTarget{}, false, fmt.Errorf("config.models.%s has no candidates", key)
		}
		return withCandidates(strings.TrimSpace(value), candidates), true, nil
	}
	return DispatchTarget{}, false, nil
}

func withCandidates(requested string, candidates []DispatchTarget) DispatchTarget {
	first := candidates[0]
	first.Requested = requested
	if len(candidates) > 1 {
		first.Candidates = make([]RouteCandidate, 0, len(candidates))
		for _, candidate := range candidates {
			first.Candidates = append(first.Candidates, RouteCandidate{
				Provider: candidate.Provider,
				Model:    candidate.Model,
				Source:   candidate.Source,
				ModelKey: candidate.ModelKey,
				ActualID: candidate.ActualID,
			})
		}
	}
	return first
}

func CandidateTargets(target DispatchTarget) []DispatchTarget {
	if len(target.Candidates) == 0 {
		return []DispatchTarget{target}
	}
	out := make([]DispatchTarget, 0, len(target.Candidates))
	for _, candidate := range target.Candidates {
		out = append(out, DispatchTarget{
			Requested: target.Requested,
			Provider:  candidate.Provider,
			Model:     candidate.Model,
			Source:    candidate.Source,
			ModelKey:  candidate.ModelKey,
			ActualID:  candidate.ActualID,
		})
	}
	return out
}

func normalizeProvider(provider string) string {
	provider = strings.ToLower(strings.TrimSpace(provider))
	if provider == "gemini" {
		return "antigravity"
	}
	return provider
}

func geminiAliasModel(normalized string) (string, bool) {
	switch normalized {
	case "gemini-flash", "geminiflash":
		return "flash", true
	case "gemini-pro", "geminipro":
		return "pro", true
	case "gemini-pro-low", "geminiprolow":
		return "pro-low", true
	default:
		return "", false
	}
}

type registryPayload struct {
	Models []registryEntry `json:"models"`
}

type registryEntry struct {
	Key            string   `json:"key"`
	Provider       string   `json:"provider"`
	DispatchRunner string   `json:"dispatchRunner"`
	DispatchModel  string   `json:"dispatchModel"`
	ActualModelID  string   `json:"actualModelId"`
	Aliases        []string `json:"aliases"`
}

type RegistryModel struct {
	Key            string
	Provider       string
	DispatchRunner string
	DispatchModel  string
	ActualModelID  string
	Aliases        []string
}

func RegistryModels() ([]RegistryModel, error) {
	payload, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	models := []RegistryModel{}
	for _, entry := range payload.Models {
		provider := normalizeProvider(entry.DispatchRunner)
		if provider == "" {
			provider = normalizeProvider(entry.Provider)
		}
		if !implementedProvider(provider) || strings.TrimSpace(entry.Key) == "" {
			continue
		}
		models = append(models, RegistryModel{
			Key:            strings.TrimSpace(entry.Key),
			Provider:       strings.TrimSpace(entry.Provider),
			DispatchRunner: provider,
			DispatchModel:  strings.TrimSpace(entry.DispatchModel),
			ActualModelID:  strings.TrimSpace(entry.ActualModelID),
			Aliases:        cleanStrings(entry.Aliases),
		})
	}
	sort.Slice(models, func(i, j int) bool {
		return models[i].Key < models[j].Key
	})
	return models, nil
}

type registryMatchKind string

const (
	registryMatchKey           registryMatchKind = "key"
	registryMatchActualID      registryMatchKind = "actualModelId"
	registryMatchDispatchModel registryMatchKind = "dispatchModel"
	registryMatchAlias         registryMatchKind = "alias"
)

type registryMatch struct {
	entry registryEntry
	kind  registryMatchKind
}

func defaultProviderModel(provider string) (string, error) {
	key := map[string]string{
		"codex":       "gpt5.5",
		"antigravity": "gemini-flash",
	}[provider]
	if key == "" {
		return "", nil
	}
	match, ok, err := lookupRegistryMatch(key)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("default model target %q is missing from registry", key)
	}
	return entryModelForProvider(match.entry, provider), nil
}

func lookupRegistryMatch(value string) (registryMatch, bool, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return registryMatch{}, false, nil
	}
	payload, err := loadRegistry()
	if err != nil {
		return registryMatch{}, false, err
	}
	for _, entry := range payload.Models {
		candidates := []struct {
			value string
			kind  registryMatchKind
		}{
			{entry.Key, registryMatchKey},
			{entry.ActualModelID, registryMatchActualID},
			{entry.DispatchModel, registryMatchDispatchModel},
		}
		for _, alias := range entry.Aliases {
			candidates = append(candidates, struct {
				value string
				kind  registryMatchKind
			}{alias, registryMatchAlias})
		}
		for _, candidate := range candidates {
			if strings.ToLower(strings.TrimSpace(candidate.value)) == normalized {
				return registryMatch{entry: entry, kind: candidate.kind}, true, nil
			}
		}
	}
	return registryMatch{}, false, nil
}

func RegistryTargets() []string {
	payload, err := loadRegistry()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	targets := []string{}
	for _, target := range ConfigModelTargets() {
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	for _, entry := range payload.Models {
		provider := normalizeProvider(entry.DispatchRunner)
		if provider == "" {
			provider = normalizeProvider(entry.Provider)
		}
		if !implementedProvider(provider) || strings.TrimSpace(entry.Key) == "" {
			continue
		}
		for _, target := range registryTargetNames(entry) {
			if target == "" || seen[target] {
				continue
			}
			seen[target] = true
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	return targets
}

func ConfigModelTargets() []string {
	cfg, err := config.Load()
	if err != nil {
		return nil
	}
	targets := make([]string, 0, len(cfg.Models))
	for target, routes := range cfg.Models {
		if strings.TrimSpace(target) == "" || len(routes) == 0 {
			continue
		}
		targets = append(targets, strings.TrimSpace(target))
	}
	sort.Strings(targets)
	return targets
}

func SupportedTargets() []string {
	seen := map[string]bool{}
	targets := []string{}
	for _, target := range append([]string{"codex", "opencode", "claude", "antigravity", "gemini", "gemini-flash", "gemini-pro"}, RegistryTargets()...) {
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets
}

func registryTargetNames(entry registryEntry) []string {
	names := []string{strings.TrimSpace(entry.Key)}
	for _, alias := range entry.Aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		names = append(names, alias)
	}
	return names
}

func loadRegistry() (registryPayload, error) {
	data, err := modelRegistryFS.ReadFile("models.json")
	if err != nil {
		return registryPayload{}, err
	}
	var payload registryPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return registryPayload{}, err
	}
	return payload, nil
}

func entryModelForProvider(entry registryEntry, provider string) string {
	if entry.DispatchModel != "" {
		return entry.DispatchModel
	}
	return entry.ActualModelID
}

func implementedProvider(provider string) bool {
	switch provider {
	case "codex", "opencode", "claude", "antigravity":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func cleanStrings(values []string) []string {
	cleaned := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	sort.Strings(cleaned)
	return cleaned
}
