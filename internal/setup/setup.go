package setup

import (
	"fmt"
	"os"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

// FirstRunHint is the message injected into the first ProviderResult after a
// config setup, so the calling Agent knows to tell the user about preferences.
const FirstRunHint = "本次是首次使用 dispatch skill。完成后请提醒用户可以在 ~/.ai-dispatch/preferences.md 中根据自己的喜好设定模型调用偏好。"

// Summary captures the initialization details for surfacing to the calling
// Agent on the first run. It is only populated when FirstRun is true.
type Summary struct {
	InitializedAt   string                 `json:"initialized_at"`
	HomeDir         string                 `json:"home_dir"`
	ConfigPath      string                 `json:"config_path"`
	PreferencesPath string                 `json:"preferences_path"`
	ClaudeTransport string                 `json:"claude_transport"`
	Providers       config.ProvidersConfig `json:"providers,omitempty"`
}

// Result describes what happened during a config setup pass.
type Result struct {
	FirstRun bool
	Config   config.Config
	Summary  *Summary
}

// Options controls config setup behavior.
type Options struct {
	ClaudeTransport string
	Force           bool
	ScanProviders   bool
}

// Ensure creates the minimum config state needed by send/resume. It does not
// scan providers; provider probing is reserved for init or providers scan.
func Ensure() (Result, error) {
	if !config.StateMissing() {
		cfg, err := config.Load()
		return Result{FirstRun: false, Config: cfg}, err
	}
	return Run(Options{})
}

// Run unconditionally initializes the runtime. Provider probing only happens
// when ScanProviders is true, so normal send/resume stays side-effect-light.
// config.Save is atomic (temp+rename), so concurrent config setups won't corrupt
// the config — the last writer wins with a valid file.
func Run(opts Options) (Result, error) {
	for _, dir := range []string{config.HomeDir(), config.RunsDir(), config.LogsDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Result{}, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	configMissing := fileMissing(config.ConfigPath())
	preferencesCreated, err := config.EnsurePreferences()
	if err != nil {
		return Result{}, fmt.Errorf("ensure preferences: %w", err)
	}

	var cfg config.Config
	if opts.Force {
		cfg = config.Default()
	} else {
		var err error
		cfg, err = config.Load()
		if err != nil {
			return Result{}, fmt.Errorf("load existing config: %w", err)
		}
	}
	firstRun := configMissing || preferencesCreated

	if opts.ClaudeTransport != "" {
		cfg.ClaudeTransport = opts.ClaudeTransport
	}
	if cfg.Version == 0 {
		cfg.Version = config.Version
	}
	if cfg.ClaudeTransport == "" {
		cfg.ClaudeTransport = "print"
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if opts.ScanProviders {
		cfg.Providers = ProbeAll(false)
	}
	if opts.Force || configMissing || opts.ScanProviders {
		if err := config.Save(cfg); err != nil {
			return Result{FirstRun: firstRun, Config: cfg}, fmt.Errorf("save config: %w", err)
		}
	}
	var summary *Summary
	if firstRun {
		summary = &Summary{
			InitializedAt:   now,
			HomeDir:         config.HomeDir(),
			ConfigPath:      config.ConfigPath(),
			PreferencesPath: config.PreferencesPath(),
			ClaudeTransport: cfg.ClaudeTransport,
		}
		if opts.ScanProviders {
			summary.Providers = cfg.Providers
		}
	}
	return Result{FirstRun: firstRun, Config: cfg, Summary: summary}, nil
}

// Rescan re-probes providers and updates only the providers diagnostic summary.
// Used by `providers scan`. When refresh is true, opencode re-fetches its model
// cache from models.dev first.
func Rescan(refresh bool) (config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, err
	}
	cfg.Providers = ProbeAll(refresh)
	return cfg, config.Save(cfg)
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
