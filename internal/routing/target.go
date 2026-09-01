package routing

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

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

type ConfiguredModel struct {
	Key        string
	Candidates []RouteCandidate
}

func Resolve(rawTarget string, explicitModel string) (DispatchTarget, error) {
	target := strings.TrimSpace(rawTarget)
	model := strings.TrimSpace(explicitModel)
	if target == "" {
		return DispatchTarget{}, fmt.Errorf("target is required")
	}
	if strings.Contains(target, ":") || strings.Contains(model, ":") {
		return DispatchTarget{}, fmt.Errorf("colon route syntax is not supported; use a provider target with --model")
	}
	normalized := strings.ToLower(target)
	if model == "" {
		if configured, ok, err := lookupConfiguredModelTarget(target); err != nil {
			return DispatchTarget{}, err
		} else if ok {
			return configured, nil
		}
		if exact, ok, err := lookupConfiguredExactID(target); err != nil {
			return DispatchTarget{}, err
		} else if ok {
			return exact, nil
		}
		if provider, ok := providerName(normalized); ok {
			return providerTarget(target, provider, "")
		}
		return DispatchTarget{}, fmt.Errorf("unsupported target: %s", rawTarget)
	}
	provider, ok := providerName(normalized)
	if !ok {
		return DispatchTarget{}, fmt.Errorf("cannot combine explicit model with model target")
	}
	return providerTarget(target, provider, model)
}

func providerName(normalized string) (string, bool) {
	switch normalized {
	case "codex", "opencode", "claude", "antigravity", "grok", "cursor":
		return normalized, true
	case "gemini":
		return "antigravity", true
	default:
		return "", false
	}
}

func providerTarget(target string, provider string, model string) (DispatchTarget, error) {
	if strings.TrimSpace(model) == "" {
		return DispatchTarget{
			Requested: target,
			Provider:  provider,
			Source:    "provider",
		}, nil
	}
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
	hits, err := lookupConfiguredExactHits(model)
	if err != nil {
		return DispatchTarget{}, err
	}
	matches := []DispatchTarget{}
	for _, hit := range hits {
		if hit.Provider == provider {
			hit.Requested = target
			hit.Source = "provider"
			matches = append(matches, hit)
		}
	}
	if len(matches) > 0 {
		first := matches[0]
		first.Candidates = nil
		return first, nil
	}
	if len(hits) > 0 {
		return DispatchTarget{}, fmt.Errorf("model id %q belongs to provider %q, not %q", model, hits[0].Provider, provider)
	}
	return DispatchTarget{
		Requested: target,
		Provider:  provider,
		Model:     model,
		Source:    "provider",
		ActualID:  model,
	}, nil
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
		candidates, err := configuredCandidates(strings.TrimSpace(value), key, routes)
		if err != nil {
			return DispatchTarget{}, false, err
		}
		return withCandidates(strings.TrimSpace(value), candidates), true, nil
	}
	return DispatchTarget{}, false, nil
}

func lookupConfiguredExactHits(value string) ([]DispatchTarget, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return nil, nil
	}
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(cfg.Models))
	for key := range cfg.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var hits []DispatchTarget
	for _, key := range keys {
		for _, route := range cfg.Models[key] {
			provider := normalizeProvider(route.Provider)
			model := strings.TrimSpace(route.Model)
			if strings.ToLower(model) != normalized {
				continue
			}
			if provider == "" || !implementedProvider(provider) {
				continue
			}
			hits = append(hits, DispatchTarget{
				Requested: strings.TrimSpace(value),
				Provider:  provider,
				Model:     model,
				Source:    "config",
				ModelKey:  strings.TrimSpace(key),
				ActualID:  model,
			})
		}
	}
	return hits, nil
}

func lookupConfiguredExactID(value string) (DispatchTarget, bool, error) {
	hits, err := lookupConfiguredExactHits(value)
	if err != nil {
		return DispatchTarget{}, false, err
	}
	if len(hits) == 0 {
		return DispatchTarget{}, false, nil
	}
	first := hits[0]
	for _, hit := range hits[1:] {
		if hit.Provider != first.Provider || hit.Model != first.Model {
			return DispatchTarget{}, false, fmt.Errorf("exact model id %q is ambiguous in config.models", value)
		}
	}
	first.Requested = strings.TrimSpace(value)
	first.Candidates = nil
	return first, true, nil
}

func configuredCandidates(requested string, key string, routes []config.ModelRoute) ([]DispatchTarget, error) {
	candidates := make([]DispatchTarget, 0, len(routes))
	for _, route := range routes {
		provider := normalizeProvider(route.Provider)
		model := strings.TrimSpace(route.Model)
		if provider == "" {
			return nil, fmt.Errorf("config.models.%s has empty provider", key)
		}
		if !implementedProvider(provider) {
			return nil, fmt.Errorf("config.models.%s uses unsupported provider %q", key, route.Provider)
		}
		candidates = append(candidates, DispatchTarget{
			Requested: requested,
			Provider:  provider,
			Model:     model,
			Source:    "config",
			ModelKey:  strings.TrimSpace(key),
			ActualID:  model,
		})
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("config.models.%s has no candidates", key)
	}
	return candidates, nil
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
	} else {
		first.Candidates = nil
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

func ConfiguredModels() ([]ConfiguredModel, error) {
	keys := ConfigModelTargets()
	models := make([]ConfiguredModel, 0, len(keys))
	for _, key := range keys {
		target, ok, err := lookupConfiguredModelTarget(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		models = append(models, ConfiguredModel{
			Key:        key,
			Candidates: routeCandidates(target),
		})
	}
	return models, nil
}

func routeCandidates(target DispatchTarget) []RouteCandidate {
	if len(target.Candidates) > 0 {
		return target.Candidates
	}
	return []RouteCandidate{{
		Provider: target.Provider,
		Model:    target.Model,
		Source:   target.Source,
		ModelKey: target.ModelKey,
		ActualID: target.ActualID,
	}}
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
	for _, target := range append([]string{"codex", "opencode", "claude", "antigravity", "gemini", "grok", "cursor"}, ConfigModelTargets()...) {
		if target == "" || seen[target] {
			continue
		}
		seen[target] = true
		targets = append(targets, target)
	}
	return targets
}

func implementedProvider(provider string) bool {
	switch provider {
	case "codex", "opencode", "claude", "antigravity", "grok", "cursor":
		return true
	default:
		return false
	}
}
