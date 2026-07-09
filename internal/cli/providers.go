package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/rennzhang/ai-dispatch/internal/setup"
)

func providersCommand(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch providers scan [--refresh] [--format json|text]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Re-probes local provider binaries and writes a shareable")
		fmt.Fprintln(stdout, "availability summary to config.json's providers field.")
		return 0
	}
	switch argv[0] {
	case "scan":
		return providersScan(argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ai-dispatch providers: unknown subcommand %q\n", argv[0])
		return 2
	}
}

func providersScan(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("providers scan", flag.ContinueOnError)
	fs.SetOutput(stderr)
	refresh := fs.Bool("refresh", false, "refresh opencode model cache from models.dev before listing")
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
	cfg, err := setup.Rescan(*refresh)
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 1)
	}
	payload := map[string]any{
		"ok":        true,
		"providers": cfg.Providers,
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	for _, name := range []string{"claude", "codex", "opencode", "antigravity", "grok"} {
		ps := cfg.Providers[name]
		status := "unavailable"
		if ps.Available {
			status = "available"
		}
		fmt.Fprintf(stdout, "%s: %s", name, status)
		if ps.Version != "" {
			fmt.Fprintf(stdout, " (%s)", ps.Version)
		}
		fmt.Fprintln(stdout)
		if name == "opencode" && ps.CatalogModelCount > 0 {
			fmt.Fprintf(stdout, "  catalog models: %d\n", ps.CatalogModelCount)
		}
	}
	return 0
}
