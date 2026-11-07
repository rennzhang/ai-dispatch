// Package providerinfo owns the supported local CLI names and executable lookup.
// Both discovery and execution use these rules; it does not install providers.
package providerinfo

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Spec struct {
	Binary      string
	EnvOverride string
	Fallbacks   []string
	Surfaces    []string
}

var specs = map[string]Spec{
	"claude":      {Binary: "claude", EnvOverride: "AI_DISPATCH_CLAUDE_BIN", Surfaces: []string{"cli"}},
	"codex":       {Binary: "codex", EnvOverride: "AI_DISPATCH_CODEX_BIN", Surfaces: []string{"cli", "app"}},
	"opencode":    {Binary: "opencode", EnvOverride: "AI_DISPATCH_OPENCODE_BIN", Fallbacks: []string{"~/.opencode/bin/opencode"}, Surfaces: []string{"cli"}},
	"antigravity": {Binary: "agy", EnvOverride: "AI_DISPATCH_AGY_BIN", Fallbacks: []string{"~/.local/bin/agy"}, Surfaces: []string{"cli"}},
	"grok":        {Binary: "grok", EnvOverride: "AI_DISPATCH_GROK_BIN", Fallbacks: []string{"~/.grok/bin/grok", "~/.local/bin/grok"}, Surfaces: []string{"cli"}},
	"cursor":      {Binary: "cursor-agent", EnvOverride: "AI_DISPATCH_CURSOR_BIN", Fallbacks: []string{"~/.local/bin/cursor-agent", "~/.cursor/bin/cursor-agent"}, Surfaces: []string{"cli"}},
	"omp":         {Binary: "omp", EnvOverride: "AI_DISPATCH_OMP_BIN", Fallbacks: []string{"~/.bun/bin/omp", "~/.local/bin/omp"}, Surfaces: []string{"cli"}},
}

func Names() []string {
	return []string{"antigravity", "claude", "codex", "cursor", "grok", "omp", "opencode"}
}

func Lookup(name string) (Spec, bool) {
	spec, ok := specs[name]
	spec.Fallbacks = append([]string(nil), spec.Fallbacks...)
	spec.Surfaces = append([]string(nil), spec.Surfaces...)
	return spec, ok
}

func Surfaces(name string) []string {
	spec, _ := Lookup(name)
	return spec.Surfaces
}

// ResolveBinary gives an explicit environment override precedence over the
// scanned config path, then searches PATH and the documented install locations.
// An explicit invalid path never falls through to a different installation.
func ResolveBinary(name string, configured ...string) (string, error) {
	spec, ok := Lookup(name)
	if !ok {
		return "", fmt.Errorf("unsupported provider %q", name)
	}
	if value := strings.TrimSpace(os.Getenv(spec.EnvOverride)); value != "" {
		path, err := Executable(value)
		if err != nil {
			return "", fmt.Errorf("%s override: binary not found or not executable", spec.EnvOverride)
		}
		return path, nil
	}
	if len(configured) > 0 && strings.TrimSpace(configured[0]) != "" {
		path, err := Executable(configured[0])
		if err != nil {
			return "", fmt.Errorf("configured %s binary not found or not executable; run providers scan", name)
		}
		return path, nil
	}
	if path, err := exec.LookPath(spec.Binary); err == nil {
		return Executable(path)
	}
	for _, fallback := range spec.Fallbacks {
		if path, err := Executable(fallback); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%s binary not found", name)
}

func Executable(path string) (string, error) {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("file is not executable")
	}
	return path, nil
}
