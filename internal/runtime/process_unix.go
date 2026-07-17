//go:build darwin || linux

package runtime

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

const (
	processTermGrace = 750 * time.Millisecond
	processKillGrace = 250 * time.Millisecond
)

func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// unixProcessGroup owns exactly the process group created for cmd. It never
// walks PPID relationships: a process that deliberately detaches into another
// group is an adapter-owned resource and is outside this generic boundary.
type unixProcessGroup struct {
	cmd  *exec.Cmd
	once sync.Once
	mu   sync.Mutex
	done processCleanup
	err  error
}

func newProcessGroupController(cmd *exec.Cmd) processGroupController {
	return &unixProcessGroup{cmd: cmd}
}

func (g *unixProcessGroup) Cancel() error {
	g.cleanup()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.err
}

func (g *unixProcessGroup) Cleanup() processCleanup {
	g.cleanup()
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.done
}

func (g *unixProcessGroup) cleanup() {
	g.once.Do(func() {
		result, err := terminateOwnedProcessGroup(g.processGroupID())
		g.mu.Lock()
		g.done = result
		g.err = err
		g.mu.Unlock()
	})
}

func (g *unixProcessGroup) processGroupID() int {
	if g.cmd == nil || g.cmd.Process == nil {
		return 0
	}
	// Setpgid makes the new process the leader, so its PID is the ID of the
	// process group we created. We only address that owned group and never walk
	// PPID relationships or signal individually discovered PIDs.
	return g.cmd.Process.Pid
}

func terminateOwnedProcessGroup(pgid int) (processCleanup, error) {
	result := processCleanup{Attempted: true}
	if pgid <= 0 {
		result.Error = "process group was not initialized"
		return result, errors.New(result.Error)
	}

	exists, err := processGroupExists(pgid)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}
	if !exists {
		result.Complete = true
		return result, os.ErrProcessDone
	}

	if err := signalProcessGroup(pgid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		result.Error = fmt.Sprintf("send SIGTERM to owned process group %d: %v", pgid, err)
		return result, errors.New(result.Error)
	}
	if waitForProcessGroupExit(pgid, processTermGrace) {
		result.Complete = true
		return result, nil
	}

	if err := signalProcessGroup(pgid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		result.Error = fmt.Sprintf("send SIGKILL to owned process group %d: %v", pgid, err)
		return result, errors.New(result.Error)
	}
	if waitForProcessGroupExit(pgid, processKillGrace) {
		result.Complete = true
		return result, nil
	}

	result.Error = fmt.Sprintf("owned process group %d still exists after SIGTERM and SIGKILL", pgid)
	return result, errors.New(result.Error)
}

func signalProcessGroup(pgid int, signal syscall.Signal) error {
	if pgid <= 0 {
		return syscall.ESRCH
	}
	return syscall.Kill(-pgid, signal)
}

func processGroupExists(pgid int) (bool, error) {
	err := signalProcessGroup(pgid, syscall.Signal(0))
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, nil
	case errors.Is(err, syscall.ESRCH):
		return false, nil
	default:
		return false, fmt.Errorf("inspect owned process group %d: %w", pgid, err)
	}
}

func waitForProcessGroupExit(pgid int, grace time.Duration) bool {
	deadline := time.Now().Add(grace)
	for {
		exists, err := processGroupExists(pgid)
		if err == nil && !exists {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(10 * time.Millisecond)
	}
}
