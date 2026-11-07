package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/setup"
	"io"
)

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "ai-dispatch go")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  ai-dispatch send [target] [prompt] [flags]")
	fmt.Fprintln(w, "  ai-dispatch resume --session-id <id> [--target <target>] [prompt] [flags]")
	fmt.Fprintln(w, "  ai-dispatch config path|show|migrate")
	fmt.Fprintln(w, "  ai-dispatch guide models")
	fmt.Fprintln(w, "  ai-dispatch runs list|show|tail|failures")
	fmt.Fprintln(w, "  ai-dispatch init [--claude-transport print|pty|auto|disabled] [--force]")
	fmt.Fprintln(w, "  ai-dispatch providers scan [--dry-run] [--refresh] [--format json]")
	fmt.Fprintln(w, "  ai-dispatch doctor --format json")
	fmt.Fprintln(w, "  ai-dispatch models --format json")
	fmt.Fprintln(w, "  ai-dispatch preferences path|show|open")
	fmt.Fprintln(w, "  ai-dispatch version [--format json]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Common flags:")
	fmt.Fprintln(w, "  --json-result                  write ProviderResult JSON to stdout")
	fmt.Fprintln(w, "  --stream-progress              write progress NDJSON to stderr")
	fmt.Fprintln(w, "  --wrap-up                      enable the user-facing wrap-up card (default: off)")
	fmt.Fprintln(w, "  --no-wrap-up                   do not emit the user-facing wrap-up card")
	fmt.Fprintln(w, "  --effort level                 reasoning effort: auto|none|minimal|low|medium|high|xhigh|max")
	fmt.Fprintln(w, "  --fast                         request provider fast mode; unsupported providers use standard speed with fast_fallback_reason")
	fmt.Fprintln(w, "  --surface cli|app              Codex delivery channel; default cli. app sends into Codex Desktop")
	fmt.Fprintln(w, "  --provider-opt key=value        provider option, e.g. claude.transport=print|pty|auto")
	fmt.Fprintln(w, "  --activity-timeout seconds      fail after no provider activity; 0 disables")
}

var initializeLocalProviders = setup.Run

func initConfig(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	claudeTransport := fs.String("claude-transport", "print", "print, pty, auto, or disabled")
	force := fs.Bool("force", false, "overwrite existing config")
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !config.ValidClaudeTransport(*claudeTransport) {
		return emitCLIError(stdout, stderr, *format == "json", "--claude-transport must be print, pty, auto, or disabled", 2)
	}
	if err := validateFormat(*format); err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	result, err := initializeLocalProviders(setup.Options{
		ClaudeTransport: *claudeTransport,
		Force:           *force,
		ScanProviders:   true,
	})
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 1)
	}
	payload := map[string]any{
		"ok":               true,
		"home":             config.HomeDir(),
		"config":           config.ConfigPath(),
		"runs":             config.RunsDir(),
		"logs":             config.LogsDir(),
		"claude_transport": result.Config.ClaudeTransport,
		"first_run":        result.FirstRun,
	}
	if result.ScanReport != nil {
		payload["scan"] = result.ScanReport.Providers
		payload["warnings"] = result.ScanReport.Warnings
	}
	if len(result.Config.Providers) > 0 {
		payload["providers"] = result.Config.Providers
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintf(stdout, "ai-dispatch initialized at %s\n", config.HomeDir())
	fmt.Fprintf(stdout, "config: %s\n", config.ConfigPath())
	for name, ps := range result.Config.Providers {
		status := "unavailable"
		if ps.Executable != "" {
			status = "available"
		}
		fmt.Fprintf(stdout, "provider %s: %s\n", name, status)
	}
	return 0
}

func configCommand(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch config path")
		fmt.Fprintln(stdout, "  ai-dispatch config show")
		fmt.Fprintln(stdout, "  ai-dispatch config migrate [--dry-run|--apply] [--format json]")
		return 0
	}
	switch argv[0] {
	case "migrate":
		return migrateConfig(argv[1:], stdout, stderr)
	case "path":
		fmt.Fprintln(stdout, config.ConfigPath())
		return 0
	case "show":
		cfg, err := config.Load()
		if err != nil {
			return emitCLIError(stdout, stderr, false, err.Error(), 1)
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return emitCLIError(stdout, stderr, false, err.Error(), 1)
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	default:
		fmt.Fprintf(stderr, "ai-dispatch config: unknown subcommand %q\n", argv[0])
		return 2
	}
}
