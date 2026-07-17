package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/rennzhang/ai-dispatch/internal/buildinfo"
)

func versionCommand(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("version", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() != 0 {
		return emitCLIError(stdout, stderr, *format == "json", "version does not accept positional arguments", 2)
	}
	if err := validateFormat(*format); err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	info := buildinfo.Current()
	if *format == "json" {
		return writeJSON(stdout, info, 0)
	}
	dirty := ""
	if info.Modified {
		dirty = " dirty"
	}
	revision := info.Revision
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if revision == "" {
		revision = "unknown"
	}
	fmt.Fprintf(stdout, "ai-dispatch %s (%s%s) %s\n", info.Version, revision, dirty, info.GoVersion)
	return 0
}
