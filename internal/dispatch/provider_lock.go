package dispatch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

var openCodeInProcessLock = make(chan struct{}, 1)

func withProviderExecutionLock(ctx context.Context, provider string, run func() execruntime.RunResult) execruntime.RunResult {
	if provider != "opencode" || strings.EqualFold(os.Getenv("AI_DISPATCH_OPENCODE_LOCK"), "off") {
		return run()
	}
	started := time.Now()
	select {
	case openCodeInProcessLock <- struct{}{}:
		defer func() { <-openCodeInProcessLock }()
	case <-ctx.Done():
		if ctx.Err() == context.Canceled {
			return execruntime.RunResult{ExitCode: 130, DurationMS: elapsedMS(started), Canceled: true, Error: context.Canceled.Error()}
		}
		return lockTimeoutResult(elapsedMS(started))
	}
	release, err := acquireFileLock(ctx, openCodeLockPath())
	if err != nil {
		if ctx.Err() == context.Canceled {
			return execruntime.RunResult{ExitCode: 130, DurationMS: elapsedMS(started), Canceled: true, Error: context.Canceled.Error()}
		}
		if ctx.Err() == context.DeadlineExceeded {
			return lockTimeoutResult(elapsedMS(started))
		}
		return execruntime.RunResult{
			ExitCode:   1,
			DurationMS: elapsedMS(started),
			Error:      "cannot acquire opencode execution lock: " + err.Error(),
		}
	}
	defer release()
	return run()
}

func lockTimeoutResult(durationMS int64) execruntime.RunResult {
	return execruntime.RunResult{
		ExitCode:     124,
		DurationMS:   durationMS,
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
