//go:build windows

package codex

import "os/exec"

func prepareDesktopBackendProcess(_ *exec.Cmd) {}
func stopDesktopBackendProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
