package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

func doctor(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := validateFormat(*format); err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	cfg, cfgErr := config.Load()
	payload := map[string]any{
		"ok":                 true,
		"runtime":            "go",
		"provider_execution": providerExecutionStatus(),
		"contract":           "2.0",
		"claude_env":         claudeEnvStatus(),
	}
	if cfgErr != nil {
		payload["ok"] = false
		payload["config_error"] = sanitizeDiagnosticError(cfgErr)
	} else {
		payload["config"] = doctorConfigSummary(cfg)
	}
	if *format == "json" {
		exitCode := 0
		if cfgErr != nil {
			exitCode = 1
		}
		return writeJSON(stdout, payload, exitCode)
	}
	if cfgErr != nil {
		fmt.Fprintln(stdout, "ai-dispatch go: config error")
		fmt.Fprintln(stderr, "ai-dispatch doctor:", sanitizeDiagnosticError(cfgErr))
		return 1
	}
	fmt.Fprintln(stdout, "ai-dispatch go: ok")
	return 0
}

func doctorConfigSummary(cfg config.Config) map[string]any {
	out := map[string]any{
		"version":               cfg.Version,
		"claude_transport":      cfg.ClaudeTransport,
		"model_alias_count":     len(cfg.Models),
		"provider_status_count": len(cfg.Providers),
	}
	if len(cfg.Providers) > 0 {
		out["providers"] = doctorProviderSummary(cfg.Providers)
	}
	return out
}

func doctorProviderSummary(providers config.ProvidersConfig) map[string]map[string]any {
	out := map[string]map[string]any{}
	for name, ps := range providers {
		item := map[string]any{
			"available": ps.Available,
			"has_error": ps.Error != "",
		}
		if ps.CatalogModelCount > 0 {
			item["catalog_model_count"] = ps.CatalogModelCount
		}
		out[name] = item
	}
	return out
}

func providerExecutionStatus() string {
	if os.Getenv("AI_DISPATCH_GO_PROVIDER_EXECUTION") == "on" {
		return "enabled"
	}
	return "disabled_by_default"
}

func claudeEnvStatus() map[string]any {
	status := map[string]any{
		"api_env_present": false,
	}
	status["model_env_present"] = strings.TrimSpace(os.Getenv("ANTHROPIC_MODEL")) != ""
	status["base_url_env_present"] = strings.TrimSpace(os.Getenv("ANTHROPIC_BASE_URL")) != ""
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			status["api_env_present"] = true
			break
		}
	}
	return status
}

func sanitizeDiagnosticError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	replacements := []string{
		config.HomeDir(),
		config.ConfigPath(),
		config.PreferencesPath(),
		config.RunsDir(),
		config.LogsDir(),
	}
	for _, path := range replacements {
		if path != "" {
			msg = strings.ReplaceAll(msg, path, "<path>")
		}
	}
	msg = strings.TrimPrefix(msg, "read <path>: ")
	if msg == err.Error() {
		return msg
	}
	return "invalid config: " + msg
}
