package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

var openCodeInProcessLock = make(chan struct{}, 1)

func withProviderExecutionLock(ctx context.Context, provider string, run func() execruntime.RunResult) execruntime.RunResult {
	if provider != "opencode" || strings.EqualFold(os.Getenv("AI_DISPATCH_OPENCODE_LOCK"), "off") {
		return run()
	}
	select {
	case openCodeInProcessLock <- struct{}{}:
		defer func() { <-openCodeInProcessLock }()
	case <-ctx.Done():
		return lockTimeoutResult()
	}
	release, err := acquireFileLock(ctx, openCodeLockPath())
	if err != nil {
		return execruntime.RunResult{
			ExitCode: 1,
			Error:    "cannot acquire opencode execution lock: " + err.Error(),
		}
	}
	defer release()
	return run()
}

func lockTimeoutResult() execruntime.RunResult {
	return execruntime.RunResult{
		ExitCode:     124,
		TimedOut:     true,
		FixedTimeout: true,
		Error:        "timed out waiting for opencode execution lock",
	}
}

func openCodeLockPath() string {
	if path := strings.TrimSpace(os.Getenv("AI_DISPATCH_OPENCODE_LOCK_PATH")); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "ai-dispatch-opencode.lock")
}
