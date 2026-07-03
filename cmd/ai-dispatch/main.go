package main

import (
	"io"
	"os"

	"github.com/rennzhang/ai-dispatch/internal/cli"
)

func main() {
	// Default to real provider execution. The old bash wrapper used to set
	// this; now the Go binary owns the default. Set
	// AI_DISPATCH_GO_PROVIDER_EXECUTION=off to disable for dev/testing.
	if os.Getenv("AI_DISPATCH_GO_PROVIDER_EXECUTION") == "" {
		os.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on")
	}
	os.Exit(cli.MainWithInput(os.Args[1:], os.Stdout, os.Stderr, stdinReader(os.Stdin)))
}

func stdinReader(file *os.File) io.Reader {
	info, err := file.Stat()
	if err != nil {
		return nil
	}
	if info.Mode()&os.ModeCharDevice != 0 {
		return nil
	}
	return file
}
