//go:build !windows

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDesktopCatalogDescendant(t *testing.T) {
	if os.Getenv("TEST_DESKTOP_MODEL_DESCENDANT") != "1" {
		return
	}
	state := os.Getenv("TEST_DESKTOP_STATE")
	if err := os.WriteFile(filepath.Join(state, "descendant-ready"), nil, 0o600); err != nil {
		os.Exit(96)
	}
	time.Sleep(350 * time.Millisecond)
	_ = os.WriteFile(filepath.Join(state, "descendant-survived"), nil, 0o600)
	os.Exit(0)
}

func TestDesktopCatalogCancellationClosesPipesAndKillsDescendants(t *testing.T) {
	host, _, _ := desktopBackendFixture(t)
	t.Setenv("TEST_DESKTOP_MODEL_DESCENDANT", "1")
	state := os.Getenv("TEST_DESKTOP_STATE")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := DiscoverCLIModels(ctx, host.binary); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(filepath.Join(state, "descendant-ready")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture descendant did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation=%v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("catalog wait remained blocked on a descendant output pipe")
	}
	time.Sleep(450 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(state, "descendant-survived")); !os.IsNotExist(err) {
		t.Fatalf("catalog descendant survived cancellation: %v", err)
	}
}
