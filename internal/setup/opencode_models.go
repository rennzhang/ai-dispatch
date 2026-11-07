package setup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// listOpenCodeModels runs `opencode models` (or `--refresh`) and parses the
// one-model-per-line output. Only models from providers the user has actually
// authenticated are counted, so the summary reflects the relevant catalog
// size. When auth.json is unavailable, only the built-in opencode catalog is
// counted. This does not prove that every model is runnable for the user's
// region, account, quota, or OpenRouter endpoint.
func listOpenCodeModels(bin string, refresh bool) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"models"}
	if refresh {
		args = append(args, "--refresh")
	}
	stdout, err := catalogCommand(ctx, bin, args...)
	if err != nil {
		return nil, err
	}
	all := parseOpenCodeModels(string(stdout))
	configured := openCodeConfiguredProviders()
	if len(configured) == 0 {
		// Do not claim third-party provider catalogs unless opencode auth
		// confirms that provider is configured on this machine.
		return filterOpenCodeModels(all, map[string]bool{"opencode": true}), nil
	}
	return filterOpenCodeModels(all, configured), nil
}

func parseOpenCodeModels(output string) []string {
	models := []string{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// opencode models prints one model id per line; banner/help lines are
		// either empty, contain spaces in the middle without a slash, or are
		// the ASCII logo. Model ids always contain a '/' (provider/model) or
		// match the opencode/ prefix.
		if !strings.Contains(line, "/") && !strings.HasPrefix(line, "opencode") {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			continue
		}
		models = append(models, line)
	}
	return models
}

// providerPrefixForAuth maps an opencode auth.json provider id to the prefix
// used in `opencode models` output. codex auth enables openai/ models; the
// built-in opencode/ free models are always included.
var providerPrefixForAuth = map[string]string{
	"openai":     "openai",
	"codex":      "openai",
	"google":     "google",
	"openrouter": "openrouter",
	"anthropic":  "anthropic",
	"opencode":   "opencode",
}

// openCodeConfiguredProviders reads opencode's auth.json and returns the set of
// authenticated provider ids. Returns nil when auth.json is unavailable.
func openCodeConfiguredProviders() map[string]bool {
	data, err := os.ReadFile(openCodeAuthPath())
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := map[string]bool{}
	for key := range raw {
		out[key] = true
	}
	// opencode/ built-in free models are always usable.
	out["opencode"] = true
	return out
}

func filterOpenCodeModels(models []string, configured map[string]bool) []string {
	filtered := make([]string, 0, len(models))
	allowed := map[string]bool{}
	for name, enabled := range configured {
		if !enabled {
			continue
		}
		prefix := providerPrefixForAuth[name]
		if prefix == "" {
			prefix = name
		}
		allowed[prefix] = true
	}
	for _, model := range models {
		prefix := model
		if idx := strings.Index(model, "/"); idx > 0 {
			prefix = model[:idx]
		}
		if allowed[prefix] {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func openCodeAuthPath() string {
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "opencode", "auth.json")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}
