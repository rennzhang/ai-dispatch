//go:build !darwin && !linux

package dispatch

import "os"

func dispatchSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}
