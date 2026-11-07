//go:build windows

package codex

import (
	"fmt"
	"os"
)

func tryDesktopFileLock(_ *os.File) (bool, error) {
	return false, fmt.Errorf("Codex Desktop delivery requires macOS")
}
func unlockDesktopFile(_ *os.File) error { return nil }
