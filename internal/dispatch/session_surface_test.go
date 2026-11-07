package dispatch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

func TestResumeKeepsAppSurfaceAndModelDespiteOMPDefault(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	previous := contract.SuccessResult("ok")
	previous.ProviderUsed = "codex"
	previous.Surface = "app"
	previous.ModelUsed = "gpt-6.1-sol"
	previous.SessionID = "app-surface-test"
	previous.RequestedTarget = "codex"
	if err := runstore.WriteResult(root, runstore.NewRunID(time.Now()), previous); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTarget(contract.DispatchRequest{Command: "resume", SessionID: previous.SessionID})
	if err != nil {
		t.Fatal(err)
	}
	if got.Provider != "codex" || got.Surface != "app" || got.Model != "gpt-6.1-sol" {
		t.Fatalf("target=%+v", got)
	}
	_, err = resolveTarget(contract.DispatchRequest{Command: "resume", SessionID: previous.SessionID, Surface: "cli"})
	if err == nil {
		t.Fatal("cross-surface resume must fail before executing")
	}
}

func TestProtocolFailureCannotReturnExitZero(t *testing.T) {
	failed := contract.ErrorResult(contract.StatusError, contract.FailureRuntime, "incomplete provider stream", 0)
	got := completeDispatchResult(contract.DispatchRequest{DisableUserFacing: true}, Options{}, failed)
	if got.OK || got.ExitCode == 0 {
		t.Fatalf("result=%+v", got)
	}
}

func TestLegacySessionWithoutSurfaceAcceptsExplicitApp(t *testing.T) {
	t.Setenv("AI_DISPATCH_CONFIG", filepath.Join(t.TempDir(), "config.json"))
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	previous := contract.SuccessResult("legacy")
	previous.ProviderUsed = "codex"
	previous.ModelUsed = "gpt-6.1-sol"
	previous.SessionID = "legacy-surface"
	if err := runstore.WriteResult(root, runstore.NewRunID(time.Now()), previous); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTarget(contract.DispatchRequest{Command: "resume", SessionID: previous.SessionID, Surface: "app"})
	if err != nil || got.Surface != "app" {
		t.Fatalf("target=%+v error=%v", got, err)
	}
}
