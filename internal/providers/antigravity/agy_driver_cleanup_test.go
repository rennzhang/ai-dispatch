package antigravity

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestAgyProcessCleanupRunsOnceAcrossCancelAndPostRun(t *testing.T) {
	injected := errors.New("injected cleanup failure")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	cleanup := &agyProcessCleanup{run: func() error {
		calls.Add(1)
		close(started)
		<-release
		return injected
	}}

	cancelResult := make(chan error, 1)
	postRunResult := make(chan error, 1)
	go func() { cancelResult <- cleanup.cleanup() }()
	<-started
	go func() { postRunResult <- cleanup.cleanup() }()
	close(release)

	if err := <-cancelResult; !errors.Is(err, injected) {
		t.Fatalf("cancel cleanup error=%v", err)
	}
	if err := <-postRunResult; !errors.Is(err, injected) {
		t.Fatalf("post-run cleanup error=%v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("cleanup calls=%d want=1", got)
	}
}
