package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Version identifies the config schema, not a provider CLI release.
const Version = 2

type Config struct {
	Version           int             `json:"version"`
	Defaults          DefaultsConfig  `json:"defaults"`
	ClaudeTransport   string          `json:"claude_transport,omitempty"`
	UserFacingSummary *bool           `json:"user_facing_summary,omitempty"`
	Models            ModelsConfig    `json:"models"`
	Providers         ProvidersConfig `json:"providers"`
}

type DefaultsConfig struct {
	Provider string `json:"provider"`
	Surface  string `json:"surface"`
	Model    string `json:"model,omitempty"`
}

type ModelsConfig map[string]ModelEntry

type ModelEntry struct {
	Aliases []string                          `json:"aliases,omitempty"`
	Targets map[string]map[string]ModelTarget `json:"targets"`
}

type ModelTarget struct {
	Model   string   `json:"model"`
	Origin  string   `json:"origin"`
	Efforts []string `json:"efforts,omitempty"`
	Fast    *bool    `json:"fast,omitempty"`
}

type ProviderStatus struct {
	Executable string   `json:"executable"`
	Surfaces   []string `json:"surfaces"`
	Error      string   `json:"error,omitempty"`
}

type ProvidersConfig map[string]ProviderStatus

func (c Config) UserFacingSummaryEnabled() bool {
	return c.UserFacingSummary != nil && *c.UserFacingSummary
}

func HomeDir() string {
	if value := os.Getenv("AI_DISPATCH_HOME"); value != "" {
		return value
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ".ai-dispatch"
	}
	return filepath.Join(home, ".ai-dispatch")
}

func ConfigPath() string      { return pathOverride("AI_DISPATCH_CONFIG", "config.json") }
func PreferencesPath() string { return pathOverride("AI_DISPATCH_PREFERENCES", "preferences.md") }
func RunsDir() string         { return pathOverride("AI_DISPATCH_RUNS_DIR", "runs") }
func LogsDir() string         { return pathOverride("AI_DISPATCH_LOGS_DIR", "logs") }

func pathOverride(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), fallback)
}

func Default() Config {
	return Config{
		Version:         Version,
		Defaults:        DefaultsConfig{Provider: "omp", Surface: "cli", Model: "gpt-6.1-sol"},
		ClaudeTransport: "print", Models: ModelsConfig{}, Providers: ProvidersConfig{},
	}
}

func Load() (Config, error) {
	data, err := os.ReadFile(ConfigPath())
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := Decode(data)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", ConfigPath(), err)
	}
	return cfg, nil
}

// Decode reads only the current schema; migration is an explicit operation.
func Decode(data []byte) (Config, error) {
	var header struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		return Config{}, err
	}
	if header.Version != 0 && header.Version != Version {
		return Config{}, fmt.Errorf("config schema version %d is unsupported; run config migrate before using this runtime", header.Version)
	}
	cfg := Default()
	if err := strictDecode(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config: %w", err)
	}
	return normalize(cfg)
}

func decodeConfig(data []byte) (Config, error) { return Decode(data) }

func strictDecode(data []byte, value any) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return fmt.Errorf("expected an object, got null")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON value")
	}
	return nil
}

func Validate(cfg Config) error { _, err := normalize(cfg); return err }

// Save atomically replaces a validated config. Update callers must hold their
// lock across reload, merge and Save to avoid lost updates.
func Save(cfg Config) error {
	cfg, err := normalize(cfg)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	_, writeErr := tmp.Write(append(data, '\n'))
	closeErr := tmp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(tmp.Name(), path)
}

func EnsurePreferences() (bool, error) {
	path := PreferencesPath()
	if _, err := os.Stat(path); err == nil {
		return false, nil
	} else if !os.IsNotExist(err) {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	return true, os.WriteFile(path, []byte(DefaultPreferencesMarkdown), 0o600)
}

func ValidClaudeTransport(value string) bool {
	switch value {
	case "print", "pty", "auto", "disabled":
		return true
	}
	return false
}

func StateMissing() bool           { return fileMissing(ConfigPath()) || fileMissing(PreferencesPath()) }
func fileMissing(path string) bool { _, err := os.Stat(path); return os.IsNotExist(err) }
