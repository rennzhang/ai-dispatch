package setup

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

// consolidateSharedModels joins only the identities explicitly declared by an
// adapter. Native IDs, aliases and capabilities from manual entries survive.
func consolidateSharedModels(models config.ModelsConfig, report *ScanReport, confirmed map[string]bool) {
	groups := map[string][]string{}
	for key, entry := range models {
		if identity := entrySharedIdentity(entry); identity != "" && confirmed[identity] {
			groups[identity] = append(groups[identity], key)
		}
	}
	identities := make([]string, 0, len(groups))
	for identity := range groups {
		identities = append(identities, identity)
	}
	sort.Strings(identities)
	for _, identity := range identities {
		keys := groups[identity]
		sort.Strings(keys)
		if len(keys) == 1 && keys[0] == identity {
			continue
		}
		if err := consolidateModelGroup(models, identity, keys); err != nil {
			report.Warnings = append(report.Warnings, "model "+identity+": "+err.Error()+"; separate entries retained")
		}
	}
	if confirmed["gpt-6.1-sol"] && entrySharedIdentity(models["gpt-6.1-sol"]) == "gpt-6.1-sol" {
		addUnclaimedAlias(models, "gpt-6.1-sol", "sol")
	}
}

func addUnclaimedAlias(models config.ModelsConfig, key, alias string) {
	for name, entry := range models {
		if strings.EqualFold(name, alias) {
			return
		}
		for _, existing := range entry.Aliases {
			if strings.EqualFold(existing, alias) {
				return
			}
		}
	}
	entry := models[key]
	entry.Aliases = append(entry.Aliases, alias)
	sort.Strings(entry.Aliases)
	models[key] = entry
}

func entrySharedIdentity(entry config.ModelEntry) string {
	identity := ""
	for provider, surfaces := range entry.Targets {
		for _, target := range surfaces {
			current := sharedIdentity(provider, target.Model)
			// A hand-maintained logical entry may group unrelated models. Do
			// not reinterpret that entry as one shared model by guessing.
			if current == "" || (identity != "" && current != identity) {
				return ""
			}
			identity = current
		}
	}
	return identity
}

func sharedIdentity(provider, model string) string {
	if identity := sharedIdentities[provider+":"+model]; identity != "" {
		return identity
	}
	if provider == "codex" || provider == "grok" {
		for _, confirmed := range sharedIdentities {
			if model == confirmed {
				return confirmed
			}
		}
	}
	return ""
}

func consolidateModelGroup(models config.ModelsConfig, identity string, keys []string) error {
	selected := map[string]bool{}
	for _, key := range keys {
		selected[key] = true
	}
	for key, entry := range models {
		if selected[key] {
			continue
		}
		if strings.EqualFold(key, identity) {
			return fmt.Errorf("canonical key is occupied")
		}
		for _, alias := range entry.Aliases {
			if strings.EqualFold(alias, identity) {
				return fmt.Errorf("canonical alias is occupied")
			}
		}
	}
	merged := config.ModelEntry{Targets: map[string]map[string]config.ModelTarget{}}
	aliases := map[string]string{}
	for _, key := range keys {
		entry := models[key]
		for _, alias := range append(append([]string(nil), entry.Aliases...), key) {
			if !strings.EqualFold(alias, identity) {
				aliases[strings.ToLower(alias)] = alias
			}
		}
		for provider, surfaces := range entry.Targets {
			if merged.Targets[provider] == nil {
				merged.Targets[provider] = map[string]config.ModelTarget{}
			}
			for surface, target := range surfaces {
				prior, exists := merged.Targets[provider][surface]
				if exists {
					if prior.Model != target.Model {
						return fmt.Errorf("conflicting native targets")
					}
					if prior.Origin != "scan" && target.Origin != "scan" && (!reflect.DeepEqual(prior.Efforts, target.Efforts) || !reflect.DeepEqual(prior.Fast, target.Fast)) {
						return fmt.Errorf("conflicting manual capabilities")
					}
					if prior.Origin != "scan" {
						continue
					}
				}
				merged.Targets[provider][surface] = target
			}
		}
	}
	for _, alias := range aliases {
		merged.Aliases = append(merged.Aliases, alias)
	}
	sort.Strings(merged.Aliases)
	for _, key := range keys {
		delete(models, key)
	}
	models[identity] = merged
	return nil
}
