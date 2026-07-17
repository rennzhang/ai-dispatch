//go:build windows

package claude

import "os"

func ptyDriverSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
