package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const Version = 1

type Config struct {
	Version         int          `json:"version"`
	ClaudeTransport string       `json:"claude_transport"`
	Models          ModelsConfig `json:"models"`
	Hooks           HooksConfig  `json:"hooks"`
}

type ModelsConfig struct {
	RegistryPath string `json:"registry_path,omitempty"`
}

type HooksConfig struct {
	NotifyCommand string `json:"notify_command,omitempty"`
}

type InitOptions struct {
	ClaudeTransport string
	Force           bool
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

func ConfigPath() string {
	if value := os.Getenv("AI_DISPATCH_CONFIG"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "config.json")
}

func RunsDir() string {
	if value := os.Getenv("AI_DISPATCH_RUNS_DIR"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "runs")
}

func CacheDir() string {
	if value := os.Getenv("AI_DISPATCH_CACHE_DIR"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "cache")
}

func LogsDir() string {
	if value := os.Getenv("AI_DISPATCH_LOGS_DIR"); value != "" {
		return value
	}
	return filepath.Join(HomeDir(), "logs")
}

func HooksDir() string {
	return filepath.Join(HomeDir(), "hooks")
}

func Default() Config {
	return Config{
		Version:         Version,
		ClaudeTransport: "print",
		Models:          ModelsConfig{},
		Hooks:           HooksConfig{},
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
	cfg := Default()
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("read %s: %w", path, err)
	}
	if cfg.Version == 0 {
		cfg.Version = Version
	}
	if cfg.ClaudeTransport == "" {
		cfg.ClaudeTransport = "print"
	}
	return cfg, nil
}

func Init(opts InitOptions) (Config, error) {
	cfg := Default()
	if opts.ClaudeTransport != "" {
		cfg.ClaudeTransport = opts.ClaudeTransport
	}
	for _, dir := range []string{HomeDir(), RunsDir(), CacheDir(), LogsDir(), HooksDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Config{}, err
		}
	}
	path := ConfigPath()
	if !opts.Force {
		if _, err := os.Stat(path); err == nil {
			return Load()
		}
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return Config{}, err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func ValidClaudeTransport(value string) bool {
	switch value {
	case "print", "pty", "auto", "disabled":
		return true
	default:
		return false
	}
}
