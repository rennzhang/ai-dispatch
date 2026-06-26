//go:build !darwin && !linux

package runtime

import "syscall"

func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

func terminateProcessGroup(pid int) {}
