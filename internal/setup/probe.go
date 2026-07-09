package setup

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

// providerSpec describes how to detect a single provider binary.
type providerSpec struct {
	binary      string
	envOverride string
	fallbacks   []string
	listsModels bool
}

// providerSpecs is the authoritative list of providers that provider scan probes.
// Add a new provider here when you register a new adapter in dispatch.providerFor.
var providerSpecs = map[string]providerSpec{
	"claude":      {binary: "claude"},
	"codex":       {binary: "codex"},
	"opencode":    {binary: "opencode", envOverride: "AI_DISPATCH_OPENCODE_BIN", fallbacks: []string{"~/.opencode/bin/opencode"}, listsModels: true},
	"antigravity": {binary: "agy", envOverride: "AI_DISPATCH_AGY_BIN", fallbacks: []string{"~/.local/bin/agy"}},
	"grok":        {binary: "grok", envOverride: "AI_DISPATCH_GROK_BIN", fallbacks: []string{"~/.grok/bin/grok", "~/.local/bin/grok"}},
}

// ProbeAll probes every provider in providerSpecs and returns a ProvidersConfig.
// A provider that cannot be found is recorded as available=false with an error
// message; it never aborts the whole probe. When refresh is true, opencode
// re-fetches its model cache from models.dev before listing.
func ProbeAll(refresh bool) config.ProvidersConfig {
	out := config.ProvidersConfig{}
	for name, spec := range providerSpecs {
		out[name] = probeProvider(spec, refresh)
	}
	return out
}

// ProbeOne probes a single provider by name. Returns ok=false if the name is
// not a known provider.
func ProbeOne(name string, refresh bool) (config.ProviderStatus, bool) {
	spec, ok := providerSpecs[name]
	if !ok {
		return config.ProviderStatus{}, false
	}
	return probeProvider(spec, refresh), true
}

func probeProvider(spec providerSpec, refresh bool) config.ProviderStatus {
	bin, err := resolveBinary(spec)
	if err != nil {
		return config.ProviderStatus{Available: false, Error: sanitizeProbeError(err)}
	}
	status := config.ProviderStatus{
		Available: true,
	}
	if version, verr := runVersion(bin); verr == nil {
		status.Version = version
	} else {
		// Version check failed — the binary exists but may not be runnable
		// (permission denied, wrong architecture, corrupted). Mark as
		// unavailable so the caller doesn't try to execute a broken binary.
		status.Available = false
		status.Error = "version_check_failed"
	}
	if spec.listsModels {
		if models, merr := listOpenCodeModels(bin, refresh); merr == nil {
			status.CatalogModelCount = len(models)
		} else if status.Error == "" {
			status.Error = "catalog_scan_failed"
		}
	}
	return status
}

func resolveBinary(spec providerSpec) (string, error) {
	if spec.envOverride != "" {
		if explicit := os.Getenv(spec.envOverride); explicit != "" {
			if path, err := expandPath(explicit); err == nil {
				return path, nil
			} else {
				return "", &notFoundError{name: spec.envOverride + " override"}
			}
		}
	}
	if path, err := exec.LookPath(spec.binary); err == nil {
		return path, nil
	}
	for _, fb := range spec.fallbacks {
		if path, err := expandPath(fb); err == nil {
			return path, nil
		}
	}
	return "", &notFoundError{name: spec.binary}
}

type notFoundError struct{ name string }

func (e *notFoundError) Error() string { return e.name + " binary not found in PATH" }

func sanitizeProbeError(err error) string {
	if err == nil {
		return ""
	}
	if _, ok := err.(*notFoundError); ok {
		return err.Error()
	}
	return "provider_binary_unavailable"
}

func expandPath(path string) (string, error) {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return "", &notFoundError{name: path}
		}
		path = filepath.Join(home, strings.TrimPrefix(path, "~"))
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", &notFoundError{name: path}
	}
	// Verify the file is executable by the current user.
	if info.Mode()&0o111 == 0 {
		return "", &notFoundError{name: path + " (not executable)"}
	}
	return path, nil
}

func runVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--version")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
	return line, nil
}

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
	cmd := exec.CommandContext(ctx, bin, args...)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	all := parseOpenCodeModels(stdout.String())
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
	for _, model := range models {
		prefix := model
		if idx := strings.Index(model, "/"); idx > 0 {
			prefix = model[:idx]
		}
		for auth, pref := range providerPrefixForAuth {
			if pref == prefix && configured[auth] {
				filtered = append(filtered, model)
				break
			}
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
