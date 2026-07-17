//go:build darwin || linux

package dispatch

import (
	"os"
	"syscall"
)

func dispatchSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
}
