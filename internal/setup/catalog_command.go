package setup

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Catalog subprocesses have their own process group. Some CLIs spawn children
// which inherit stdout; deadline + WaitDelay bounds both the command and pipe
// drain, and group cleanup prevents a canceled scan leaving those children.
func catalogCommand(ctx context.Context, bin string, args ...string) ([]byte, error) {
	return runCatalogCommand(ctx, bin, nil, args...)
}

// catalogCommandEnv runs a catalog command with an explicit environment. It
// exists for CLIs whose catalog depends on a config tree that the caller may
// have redirected; the catalog must come from the same tree execution reads.
func catalogCommandEnv(ctx context.Context, bin string, env []string, args ...string) ([]byte, error) {
	return runCatalogCommand(ctx, bin, env, args...)
}

func runCatalogCommand(ctx context.Context, bin string, env []string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	var output limitedOutput
	cmd.Stdout = &output
	cmd.Stderr = &limitedOutput{}
	if env != nil {
		cmd.Env = env
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("catalog command failed")
	}
	defer syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("catalog command failed")
	}
	if output.exceeded {
		return nil, fmt.Errorf("catalog output exceeded limit")
	}
	return output.data, nil
}

type limitedOutput struct {
	data     []byte
	exceeded bool
}

func (w *limitedOutput) Write(p []byte) (int, error) {
	const limit = 8 << 20
	count := len(p)
	if len(w.data)+len(p) > limit {
		w.exceeded = true
		p = p[:limit-len(w.data)]
	}
	w.data = append(w.data, p...)
	return count, nil
}

func runVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := catalogCommand(ctx, bin, "--version")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]), nil
}
