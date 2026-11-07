package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/rennzhang/ai-dispatch/internal/setup"
)

// scanLocalProviders is shared by init and scan so unit tests can supply a
// catalog implementation without touching an account or a running GUI.
var scanLocalProviders = setup.Scan

func providersCommand(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch providers scan [--dry-run] [--refresh] [--format json|text]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Discovers supported local execution entries and their models.")
		fmt.Fprintln(stdout, "Updates the same config.json; --dry-run only previews changes.")
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
	dryRun := fs.Bool("dry-run", false, "preview discovery without writing config")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if err := validateFormat(*format); err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	report, err := scanLocalProviders(setup.ScanOptions{Refresh: *refresh, DryRun: *dryRun})
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 1)
	}
	if *format == "json" {
		return writeJSON(stdout, struct {
			OK bool `json:"ok"`
			setup.ScanReport
		}{true, report}, 0)
	}
	for _, name := range []string{"omp", "codex", "grok", "claude", "cursor", "opencode", "antigravity"} {
		entry, ok := report.Providers[name]
		if !ok {
			continue
		}
		fmt.Fprintf(stdout, "%s: %s\n", name, entry.Status)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintln(stdout, "warning:", warning)
	}
	if *dryRun {
		fmt.Fprintln(stdout, "preview only; config unchanged")
	}
	return 0
}
