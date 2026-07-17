//go:build windows

package antigravity

import (
	"context"
	"fmt"
	"os"
)

func flockContext(_ context.Context, file *os.File) error {
	return fmt.Errorf("legacy agy settings recovery lock is not supported on windows")
}

func funlock(file *os.File) {}
