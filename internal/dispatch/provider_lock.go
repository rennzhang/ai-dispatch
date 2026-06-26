package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

var openCodeInProcessLock sync.Mutex

func withProviderExecutionLock(provider string, run func() execruntime.RunResult) execruntime.RunResult {
	if provider != "opencode" || strings.EqualFold(os.Getenv("AI_DISPATCH_OPENCODE_LOCK"), "off") {
		return run()
	}
	openCodeInProcessLock.Lock()
	defer openCodeInProcessLock.Unlock()
	release, err := acquireFileLock(openCodeLockPath())
	if err != nil {
		return execruntime.RunResult{
			ExitCode: 1,
			Error:    "cannot acquire opencode execution lock: " + err.Error(),
		}
	}
	defer release()
	return run()
}

func openCodeLockPath() string {
	if path := strings.TrimSpace(os.Getenv("AI_DISPATCH_OPENCODE_LOCK_PATH")); path != "" {
		return path
	}
	return filepath.Join(os.TempDir(), "ai-dispatch-opencode.lock")
}
