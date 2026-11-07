package setup

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

// FirstRunHint is the message injected into the first ProviderResult after a
// config setup, so the calling Agent knows to tell the user about preferences.
const FirstRunHint = "本次是首次使用 dispatch skill。默认入口为 OMP，默认模型为 Sol。请运行 providers scan 发现本机入口和模型；config.json 保存默认选择、入口与模型对应关系，preferences.md 只保存场景偏好。未声明的 effort/fast 保持 auto/标准速度。"

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
	FirstRun   bool
	Config     config.Config
	Summary    *Summary
	ScanReport *ScanReport
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
// Discovery runs before one locked commit, so concurrent default/model edits
// are not overwritten by an intermediate initialization save.
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

	cfg, snapshot, err := initializationSnapshot(opts.Force)
	if err != nil {
		return Result{}, fmt.Errorf("load config: %w", err)
	}
	var discovered map[string]discoveredProvider
	if opts.ScanProviders {
		discovered = collectDiscovery(cfg, false)
	}
	cfg, scan, err := initializeConfig(opts, snapshot, discovered)
	if err != nil {
		return Result{}, fmt.Errorf("initialize config: %w", err)
	}
	firstRun := configMissing || preferencesCreated
	now := time.Now().UTC().Format(time.RFC3339)
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
	return Result{FirstRun: firstRun, Config: cfg, Summary: summary, ScanReport: scan}, nil
}

func initializationSnapshot(force bool) (config.Config, []byte, error) {
	raw, err := os.ReadFile(config.ConfigPath())
	if os.IsNotExist(err) {
		return config.Default(), nil, nil
	}
	if err != nil {
		return config.Config{}, nil, err
	}
	if force {
		return config.Default(), raw, nil
	}
	cfg, err := config.Decode(raw)
	return cfg, raw, err
}

func initializeConfig(opts Options, snapshot []byte, discovered map[string]discoveredProvider) (config.Config, *ScanReport, error) {
	var cfg config.Config
	var report *ScanReport
	err := WithConfigLock(func() error {
		var err error
		if opts.Force {
			current, readErr := os.ReadFile(config.ConfigPath())
			if readErr != nil && !os.IsNotExist(readErr) {
				return readErr
			}
			if !bytes.Equal(current, snapshot) {
				return fmt.Errorf("config changed during init --force; no changes written")
			}
			cfg = config.Default()
		} else {
			cfg, err = config.Load()
			if err != nil {
				return err
			}
		}
		if opts.ClaudeTransport != "" {
			cfg.ClaudeTransport = opts.ClaudeTransport
		}
		if opts.ScanProviders {
			value := discoveryReport(discovered, false)
			report = &value
			mergeDiscovery(&cfg, discovered, report)
			report.Config = cfg
		}
		if opts.Force || opts.ScanProviders || fileMissing(config.ConfigPath()) || opts.ClaudeTransport != "" {
			return config.Save(cfg)
		}
		return nil
	})
	return cfg, report, err
}

func fileMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}
