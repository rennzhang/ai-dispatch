package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const Version = 1

type Config struct {
	Version         int             `json:"version"`
	ClaudeTransport string          `json:"claude_transport"`
	Models          ModelsConfig    `json:"models"`
	Providers       ProvidersConfig `json:"providers"`
}

type ModelsConfig map[string][]ModelRoute

type ModelRoute struct {
	Provider string `json:"provider"`
	Model    string `json:"model,omitempty"`
}

// ProviderStatus records runtime availability of a single provider binary.
// It intentionally keeps only a shareable summary: no absolute binary paths
// and no full model lists.
type ProviderStatus struct {
	Available         bool   `json:"available"`
	Version           string `json:"version,omitempty"`
	CatalogModelCount int    `json:"catalog_model_count,omitempty"`
	Error             string `json:"error,omitempty"`
}

type ProvidersConfig map[string]ProviderStatus

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

func ConfigPath() string {
	if value := os.Getenv("AI_DISPATCH_CONFIG"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "config.json")
}

func PreferencesPath() string {
	if value := os.Getenv("AI_DISPATCH_PREFERENCES"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "preferences.md")
}

func RunsDir() string {
	if value := os.Getenv("AI_DISPATCH_RUNS_DIR"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "runs")
}

func LogsDir() string {
	if value := os.Getenv("AI_DISPATCH_LOGS_DIR"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "logs")
}

func Default() Config {
	return Config{
		Version:         Version,
		ClaudeTransport: "print",
		Models:          ModelsConfig{},
		Providers:       ProvidersConfig{},
	}
}

func Load() (Config, error) {
	path := ConfigPath()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	cfg, err := decodeConfig(data)
	if err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	return normalize(cfg)
}

func decodeConfig(data []byte) (Config, error) {
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return Config{}, err
	}
	cfg := Default()
	if value, ok := raw["version"]; ok {
		if err := json.Unmarshal(value, &cfg.Version); err != nil {
			return Config{}, fmt.Errorf("version must be an integer")
		}
	}
	if value, ok := raw["claude_transport"]; ok {
		if err := json.Unmarshal(value, &cfg.ClaudeTransport); err != nil {
			return Config{}, fmt.Errorf("claude_transport must be a string")
		}
	}
	if value, ok := raw["models"]; ok {
		models, err := decodeModels(value)
		if err != nil {
			return Config{}, err
		}
		cfg.Models = models
	}
	if value, ok := raw["providers"]; ok {
		if err := json.Unmarshal(value, &cfg.Providers); err != nil {
			return Config{}, fmt.Errorf("providers must be an object")
		}
	}
	return cfg, nil
}

func decodeModels(data json.RawMessage) (ModelsConfig, error) {
	if len(data) == 0 || string(data) == "null" {
		return ModelsConfig{}, nil
	}
	raw := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("models must be an object")
	}
	models := ModelsConfig{}
	for key, value := range raw {
		normalizedKey := strings.TrimSpace(key)
		var routes []ModelRoute
		if err := json.Unmarshal(value, &routes); err != nil {
			return nil, fmt.Errorf("models.%s must be an array of provider/model candidates", key)
		}
		models[normalizedKey] = routes
	}
	return models, nil
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
	default:
		return false
	}
}

// Save writes the config atomically: temp file + rename. The temp file uses
// the PID so two processes don't collide on the same temp path.
func Save(cfg Config) error {
	path := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	if cfg.ClaudeTransport == "" {
		cfg.ClaudeTransport = "print"
	}
	if cfg.Models == nil {
		cfg.Models = ModelsConfig{}
	}
	if cfg.Providers == nil {
		cfg.Providers = ProvidersConfig{}
	}
	if _, err := normalize(cfg); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func normalize(cfg Config) (Config, error) {
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	if cfg.ClaudeTransport == "" {
		cfg.ClaudeTransport = "print"
	}
	if !ValidClaudeTransport(cfg.ClaudeTransport) {
		return Config{}, fmt.Errorf("claude_transport must be print, pty, auto, or disabled")
	}
	if cfg.Models == nil {
		cfg.Models = ModelsConfig{}
	}
	if cfg.Providers == nil {
		cfg.Providers = ProvidersConfig{}
	}
	for key, routes := range cfg.Models {
		if strings.TrimSpace(key) == "" {
			return Config{}, fmt.Errorf("models contains an empty key")
		}
		for i, route := range routes {
			if strings.TrimSpace(route.Provider) == "" {
				return Config{}, fmt.Errorf("models.%s[%d].provider is required", key, i)
			}
		}
	}
	return cfg, nil
}

func StateMissing() bool {
	return fileMissing(ConfigPath()) || fileMissing(PreferencesPath())
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
