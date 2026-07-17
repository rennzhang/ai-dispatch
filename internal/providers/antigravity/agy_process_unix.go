//go:build darwin || linux

package antigravity

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
	"time"
)

const (
	agyProcessTermGrace = 750 * time.Millisecond
	agyProcessKillGrace = 250 * time.Millisecond
)

func configureAgyProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateAgyProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid := cmd.Process.Pid
	exists, err := agyProcessGroupExists(pgid)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := syscall.Kill(-pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("send SIGTERM to agy process group %d: %w", pgid, err)
	}
	if waitForAgyProcessGroupExit(pgid, agyProcessTermGrace) {
		return nil
	}
	if err := syscall.Kill(-pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("send SIGKILL to agy process group %d: %w", pgid, err)
	}
	if waitForAgyProcessGroupExit(pgid, agyProcessKillGrace) {
		return nil
	}
	return fmt.Errorf("agy process group %d still exists after SIGTERM and SIGKILL", pgid)
}

func agyProcessGroupExists(pgid int) (bool, error) {
	err := syscall.Kill(-pgid, syscall.Signal(0))
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	default:
		return false, fmt.Errorf("inspect agy process group %d: %w", pgid, err)
	}
}

func waitForAgyProcessGroupExit(pgid int, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for {
		exists, err := agyProcessGroupExists(pgid)
		if err == nil && !exists {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
