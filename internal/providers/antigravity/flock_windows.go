//go:build windows

package antigravity

import (
	"fmt"
	"os"
)

func flock(file *os.File) error {
	return fmt.Errorf("agy settings lock is not supported on windows")
}

func funlock(file *os.File) {}
