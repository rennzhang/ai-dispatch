package claude

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	defaultTmuxCommandTimeout = 5 * time.Second
	defaultTmuxCaptureTimeout = 2 * time.Second
	defaultTmuxCleanupTimeout = 5 * time.Second
)

type tmuxClient struct {
	path           string
	commandTimeout time.Duration
	captureTimeout time.Duration
	cleanupTimeout time.Duration
}

func newTmuxClient(path string) tmuxClient {
	return tmuxClient{
		path:           path,
		commandTimeout: defaultTmuxCommandTimeout,
		captureTimeout: defaultTmuxCaptureTimeout,
		cleanupTimeout: defaultTmuxCleanupTimeout,
	}
}

func (c tmuxClient) start(ctx context.Context, sessionName string, cwd string, command []string) error {
	args := []string{"new-session", "-d", "-s", sessionName}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, command...)
	out, err := c.run(ctx, c.commandTimeout, args...)
	return tmuxCommandError("new-session", out, err)
}

func (c tmuxClient) pasteInput(ctx context.Context, sessionName string, input string) error {
	target := sessionName + ":0.0"
	if out, err := c.run(ctx, c.commandTimeout, "send-keys", "-t", target, "C-u"); err != nil {
		return tmuxCommandError("send-keys clear", out, err)
	}
	if !strings.Contains(input, "\n") && len([]rune(input)) <= 4000 {
		return c.pasteLiteralInput(ctx, target, input)
	}
	return c.pasteBufferedInput(ctx, target, input)
}

func (c tmuxClient) pasteLiteralInput(ctx context.Context, target string, input string) error {
	if out, err := c.run(ctx, c.commandTimeout, "send-keys", "-t", target, "-l", input); err != nil {
		return tmuxCommandError("send-keys literal", out, err)
	}
	if out, err := c.run(ctx, c.commandTimeout, "send-keys", "-t", target, "Enter"); err != nil {
		return tmuxCommandError("send-keys enter", out, err)
	}
	return nil
}

func (c tmuxClient) pasteBufferedInput(ctx context.Context, target string, input string) (runErr error) {
	tmp, err := os.CreateTemp("", "ai-dispatch-claude-input-*.txt")
	if err != nil {
		return fmt.Errorf("cannot create Claude tmux input file: %w", err)
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.WriteString(input); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write Claude tmux input file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close Claude tmux input file: %w", err)
	}
	buffer := "ai-dispatch-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if out, err := c.run(ctx, c.commandTimeout, "load-buffer", "-b", buffer, path); err != nil {
		return tmuxCommandError("load-buffer", out, err)
	}
	defer func() {
		if err := c.deleteBuffer(buffer); err != nil {
			if runErr == nil {
				runErr = err
			} else {
				runErr = errors.Join(runErr, err)
			}
		}
	}()
	if out, err := c.run(ctx, c.commandTimeout, "paste-buffer", "-b", buffer, "-t", target); err != nil {
		return tmuxCommandError("paste-buffer", out, err)
	}
	if out, err := c.run(ctx, c.commandTimeout, "send-keys", "-t", target, "Enter"); err != nil {
		return tmuxCommandError("send-keys enter", out, err)
	}
	return nil
}

func (c tmuxClient) capture(ctx context.Context, sessionName string) (string, error) {
	out, err := c.run(ctx, c.captureTimeout, "capture-pane", "-p", "-t", sessionName, "-S", "-2000")
	if err != nil {
		return "", tmuxCommandError("capture-pane", out, err)
	}
	return string(out), nil
}

func (c tmuxClient) waitForReady(ctx context.Context, sessionName string, pattern string, timeout time.Duration) (bool, error) {
	if timeout <= 0 || pattern == "" {
		return true, nil
	}
	readyCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		pane, err := c.capture(readyCtx, sessionName)
		if err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			if readyCtx.Err() != nil {
				return false, nil
			}
			return false, err
		}
		if strings.Contains(pane, pattern) || paneLooksReady(pane) {
			return true, nil
		}
		if err := sleepContext(readyCtx, 200*time.Millisecond); err != nil {
			if ctx.Err() != nil {
				return false, ctx.Err()
			}
			return false, nil
		}
	}
}

func (c tmuxClient) cleanupSession(sessionName string) error {
	ctx, cancel := context.WithTimeout(context.Background(), effectiveTimeout(c.cleanupTimeout, defaultTmuxCleanupTimeout))
	defer cancel()
	killErr := c.killSession(ctx, sessionName)
	stillExists, verifyErr := c.hasSession(ctx, sessionName)
	if verifyErr != nil {
		return errors.Join(killErr, fmt.Errorf("tmux has-session after cleanup failed: %w", verifyErr))
	}
	if !stillExists {
		return nil
	}
	if killErr != nil {
		return killErr
	}
	return fmt.Errorf("tmux session %q still exists after kill-session", sessionName)
}

func (c tmuxClient) cleanupStaleSessions(ctx context.Context) error {
	ttl := claudePTYSessionTTL()
	if ttl <= 0 {
		return nil
	}
	out, err := c.run(ctx, c.commandTimeout, "list-sessions", "-F", "#{session_name}\t#{session_created}")
	if err != nil {
		if tmuxHasNoSessions(out, err) {
			return nil
		}
		return tmuxCommandError("list-sessions", out, err)
	}
	cutoff := time.Now().Add(-ttl).Unix()
	var cleanupErr error
	for _, line := range strings.Split(string(out), "\n") {
		name, createdRaw, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !isClaudePTYSessionName(name) {
			continue
		}
		created, err := strconv.ParseInt(strings.TrimSpace(createdRaw), 10, 64)
		if err != nil || created > cutoff {
			continue
		}
		if err := c.killSession(ctx, name); err != nil {
			cleanupErr = errors.Join(cleanupErr, err)
		}
	}
	return cleanupErr
}

func (c tmuxClient) hasSession(ctx context.Context, sessionName string) (bool, error) {
	out, err := c.run(ctx, c.commandTimeout, "has-session", "-t", sessionName)
	if err == nil {
		return true, nil
	}
	if tmuxHasNoSessions(out, err) {
		return false, nil
	}
	return false, tmuxCommandError("has-session", out, err)
}

func (c tmuxClient) killSession(ctx context.Context, sessionName string) error {
	out, err := c.run(ctx, c.commandTimeout, "kill-session", "-t", sessionName)
	return tmuxCommandError("kill-session", out, err)
}

func (c tmuxClient) deleteBuffer(buffer string) error {
	ctx, cancel := context.WithTimeout(context.Background(), effectiveTimeout(c.cleanupTimeout, defaultTmuxCleanupTimeout))
	defer cancel()
	out, err := c.run(ctx, c.commandTimeout, "delete-buffer", "-b", buffer)
	return tmuxCommandError("delete-buffer", out, err)
}

func (c tmuxClient) run(parent context.Context, timeout time.Duration, args ...string) ([]byte, error) {
	if parent == nil {
		parent = context.Background()
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	commandCtx, cancel := context.WithTimeout(parent, effectiveTimeout(timeout, defaultTmuxCommandTimeout))
	defer cancel()
	out, err := exec.CommandContext(commandCtx, c.path, args...).CombinedOutput()
	if commandCtx.Err() != nil {
		return out, commandCtx.Err()
	}
	return out, err
}

func tmuxCommandError(operation string, output []byte, err error) error {
	if err == nil {
		return nil
	}
	detail := strings.TrimSpace(string(output))
	if detail == "" {
		return fmt.Errorf("tmux %s failed: %w", operation, err)
	}
	return fmt.Errorf("tmux %s failed: %w: %s", operation, err, detail)
}

func tmuxHasNoSessions(output []byte, err error) bool {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 1 {
		return false
	}
	detail := strings.ToLower(strings.TrimSpace(string(output)))
	return detail == "" ||
		strings.Contains(detail, "no server running") ||
		strings.Contains(detail, "failed to connect") ||
		strings.Contains(detail, "can't find session") ||
		strings.Contains(detail, "no sessions")
}

func effectiveTimeout(value time.Duration, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}

func claudePTYSessionTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS"))
	if raw == "" {
		return 6 * time.Hour
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 6 * time.Hour
	}
	return time.Duration(seconds) * time.Second
}

func isClaudePTYSessionName(name string) bool {
	return strings.HasPrefix(name, "ai-dispatch-claude-") || strings.HasPrefix(name, "claude-pty-")
}
