package main

import (
	"io"
	"os"

	"github.com/rennzhang/ai-dispatch/internal/cli"
)

func main() {
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
