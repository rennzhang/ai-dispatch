package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/setup"
)

func migrateConfig(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("config migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	apply := fs.Bool("apply", false, "write the migrated config after preserving the original")
	dryRun := fs.Bool("dry-run", false, "preview only (the default)")
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
	if *apply && *dryRun {
		return emitCLIError(stdout, stderr, *format == "json", "--apply and --dry-run are mutually exclusive", 2)
	}
	var cfg config.Config
	var report config.MigrationReport
	backup := ""
	err := setup.WithConfigLock(func() error {
		data, err := os.ReadFile(config.ConfigPath())
		if err != nil {
			return err
		}
		cfg, report, err = config.Migrate(data)
		if err != nil || !*apply {
			return err
		}
		backup = config.ConfigPath() + ".before-v2-" + time.Now().UTC().Format("20060102T150405.000000000Z") + ".bak"
		file, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err = file.Write(data); err != nil {
			_ = file.Close()
			return err
		}
		if err = file.Close(); err != nil {
			return err
		}
		return config.Save(cfg)
	})
	payload := map[string]any{"ok": err == nil, "applied": err == nil && *apply, "report": report}
	if err == nil {
		payload["config"] = cfg
	}
	if backup != "" {
		payload["backup"] = backup
	}
	if err != nil {
		payload["error"] = err.Error()
	}
	code := 0
	if err != nil {
		code = 1
	}
	if *format == "json" {
		return writeJSON(stdout, payload, code)
	}
	if err != nil {
		fmt.Fprintln(stderr, "ai-dispatch config migrate:", err)
		return code
	}
	for _, warning := range report.Warnings {
		fmt.Fprintln(stdout, "warning:", warning)
	}
	if *apply {
		fmt.Fprintln(stdout, "config migrated; original preserved at", backup)
	} else {
		fmt.Fprintln(stdout, "migration preview only; use --apply to write")
	}
	return 0
}
