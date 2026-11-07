//go:build !windows

package codex

import (
	"os"
	"syscall"
)

func tryDesktopFileLock(file *os.File) (bool, error) {
	err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == syscall.EWOULDBLOCK || err == syscall.EAGAIN {
		return false, nil
	}
	return err == nil, err
}

func unlockDesktopFile(file *os.File) error { return syscall.Flock(int(file.Fd()), syscall.LOCK_UN) }
