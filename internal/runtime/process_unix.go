//go:build darwin || linux

package runtime

import (
	"os/exec"
	"syscall"
	"time"
)

func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

func terminateProcessGroup(pid int) {
	if pid <= 0 {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); err != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

var _ = exec.ErrDot
