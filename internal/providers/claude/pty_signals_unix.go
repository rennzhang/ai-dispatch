//go:build !windows

package claude

import (
	"os"
	"syscall"
)

func ptyDriverSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
