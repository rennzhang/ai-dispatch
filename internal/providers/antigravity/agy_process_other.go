//go:build !darwin && !linux

package antigravity

import (
	"errors"
	"os"
	"os/exec"
)

func configureAgyProcess(_ *exec.Cmd) {}

func terminateAgyProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
