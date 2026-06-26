package routing

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

//go:embed models.json
var modelRegistryFS embed.FS

type DispatchTarget struct {
	Requested string
	Provider  string
	Model     string
	Source    string
	ModelKey  string
	ActualID  string
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
		return providerTarget(target, "codex", firstNonEmpty(model, "gpt-5.5")), nil
	case "opencode":
		return providerTarget(target, "opencode", model), nil
	case "claude":
		return providerTarget(target, "claude", model), nil
	case "antigravity":
		return providerTarget(target, "antigravity", model), nil
	case "gemini":
		return providerTarget(target, "antigravity", firstNonEmpty(model, "flash")), nil
	}
	if model != "" {
		return DispatchTarget{}, fmt.Errorf("cannot combine explicit model with model target")
	}
	if match, ok := lookupRegistryMatch(target); ok {
		entry := match.entry
		provider := strings.ToLower(strings.TrimSpace(entry.DispatchRunner))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(entry.Provider))
		}
		if provider == "gemini" {
			provider = "antigravity"
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
	if normalized == "gpt5.5" {
		return DispatchTarget{Requested: target, Provider: "codex", Model: "gpt-5.5", Source: "alias"}, nil
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

func providerTarget(target string, provider string, model string) DispatchTarget {
	resolved := model
	if match, ok := lookupRegistryMatch(model); ok {
		entry := match.entry
		if preserveExplicitActualModelID(provider, entry, match) {
			resolved = strings.TrimSpace(model)
		} else {
			resolved = entryModelForProvider(entry, provider)
		}
		return DispatchTarget{
			Requested: providerRequestedTarget(provider, target, resolved),
			Provider:  provider,
			Model:     resolved,
			Source:    "provider",
			ModelKey:  entry.Key,
			ActualID:  entry.ActualModelID,
		}
	}
	return DispatchTarget{
		Requested: providerRequestedTarget(provider, target, resolved),
		Provider:  provider,
		Model:     resolved,
		Source:    "provider",
	}
}

func providerRequestedTarget(provider string, target string, model string) string {
	return target
}

func preserveExplicitActualModelID(provider string, entry registryEntry, match registryMatch) bool {
	return provider == "claude" &&
		match.kind == registryMatchActualID &&
		strings.TrimSpace(entry.ActualModelID) != "" &&
		strings.TrimSpace(entry.ActualModelID) != strings.TrimSpace(entryModelForProvider(entry, provider))
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

func lookupRegistry(value string) (registryEntry, bool) {
	match, ok := lookupRegistryMatch(value)
	return match.entry, ok
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

func lookupRegistryMatch(value string) (registryMatch, bool) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return registryMatch{}, false
	}
	payload, err := loadRegistry()
	if err != nil {
		return registryMatch{}, false
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
				return registryMatch{entry: entry, kind: candidate.kind}, true
			}
		}
	}
	return registryMatch{}, false
}

func RegistryTargets() []string {
	payload, err := loadRegistry()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	targets := []string{}
	for _, entry := range payload.Models {
		provider := strings.ToLower(strings.TrimSpace(entry.DispatchRunner))
		if provider == "" {
			provider = strings.ToLower(strings.TrimSpace(entry.Provider))
		}
		if provider == "gemini" {
			provider = "antigravity"
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
	path := os.Getenv("AI_DISPATCH_MODEL_REGISTRY")
	if path == "" {
		cfg, err := config.Load()
		if err == nil {
			path = strings.TrimSpace(cfg.Models.RegistryPath)
		}
	}
	var data []byte
	var err error
	if path == "" {
		data, err = modelRegistryFS.ReadFile("models.json")
	} else {
		data, err = os.ReadFile(path)
	}
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
