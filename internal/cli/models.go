package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/routing"
)

func models(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) > 0 && argv[0] == "resolve" {
		return modelsResolve(argv[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
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
	payload := map[string]any{
		"ok":      true,
		"targets": routing.SupportedTargets(),
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintln(stdout, strings.Join(payload["targets"].([]string), "\n"))
	return 0
}

func validateFormat(format string) error {
	if format == "text" || format == "json" {
		return nil
	}
	return fmt.Errorf("--format must be text or json")
}

func modelsResolve(argv []string, stdout io.Writer, stderr io.Writer) int {
	var err error
	argv, err = reorderInterspersedFlags(argv)
	if err != nil {
		fmt.Fprintln(stderr, "ai-dispatch models resolve:", err)
		return 2
	}
	fs := flag.NewFlagSet("models resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "json")
	model := fs.String("model", "", "explicit model")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	args := fs.Args()
	if len(args) != 1 {
		fmt.Fprintln(stderr, "ai-dispatch models resolve: expected exactly one target")
		return 2
	}
	target, err := routing.Resolve(args[0], *model)
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	return writeJSON(stdout, target, 0)
}
