//go:build !darwin && !linux

package runtime

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
)

func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

type rootProcessController struct {
	cmd  *exec.Cmd
	once sync.Once
	done processCleanup
	err  error
}

func newProcessGroupController(cmd *exec.Cmd) processGroupController {
	return &rootProcessController{cmd: cmd}
}

func (c *rootProcessController) Cancel() error {
	c.cleanup()
	return c.err
}

func (c *rootProcessController) Cleanup() processCleanup {
	c.cleanup()
	return c.done
}

func (c *rootProcessController) cleanup() {
	c.once.Do(func() {
		c.done.Attempted = true
		if c.cmd == nil || c.cmd.Process == nil {
			c.done.Error = "process was not initialized"
			c.err = errors.New(c.done.Error)
			return
		}
		if err := c.cmd.Process.Kill(); err != nil {
			if errors.Is(err, os.ErrProcessDone) {
				c.done.Complete = true
				c.err = os.ErrProcessDone
				return
			}
			c.done.Error = err.Error()
			c.err = err
			return
		}
		c.done.Complete = true
	})
}
