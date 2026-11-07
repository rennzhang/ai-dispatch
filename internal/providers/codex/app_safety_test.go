package codex

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestDesktopIdenticalPromptsHaveDistinctReceipts(t *testing.T) {
	first, err := uniqueDesktopPrompt(stamped("same task"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := uniqueDesktopPrompt(stamped("same task"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || desktopPromptID(first) == "" || desktopPromptID(second) == "" {
		t.Fatal("dispatch IDs are not unique")
	}
	if !strings.HasPrefix(first, "[ai-dispatch]\n\n") || desktopPromptBody(first) != "same task" {
		t.Fatalf("prompt=%q", first)
	}
	data := userLine("message-first", "first", first) + terminalLine("task_complete", "first", "FIRST", "") +
		userLine("message-second", "second", second) + terminalLine("task_complete", "second", "SECOND", "")
	for _, item := range []struct{ prompt, want string }{{first, "FIRST"}, {second, "SECOND"}} {
		scan, err := queuedReply(nil, []byte(data), item.prompt)
		if err != nil || !scan.ready || scan.text != item.want {
			t.Fatalf("scan=%+v err=%v", scan, err)
		}
	}
	if title := desktopThreadTitle(first, "thread-42"); title != "same task · thread-4" {
		t.Fatalf("title=%q", title)
	}
}

func TestDesktopLockChildProcess(t *testing.T) {
	if os.Getenv("TEST_DESKTOP_LOCK_CHILD") != "1" {
		return
	}
	lock, err := acquireDesktopLock(context.Background(), os.Getenv("TEST_DESKTOP_LOCK_KEY"), false)
	if err != nil && strings.Contains(err.Error(), "app_session_busy") {
		os.Exit(19)
	}
	if err != nil {
		os.Exit(20)
	}
	lock.Close()
	os.Exit(0)
}

func TestDesktopLockSerializesAcrossProcessesAndRespectsCancellation(t *testing.T) {
	t.Setenv("AI_DISPATCH_HOME", t.TempDir())
	lock, err := acquireDesktopLock(context.Background(), "same-instance", true)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	child := exec.Command(os.Args[0], "-test.run", "^TestDesktopLockChildProcess$")
	child.Env = append(os.Environ(), "TEST_DESKTOP_LOCK_CHILD=1", "TEST_DESKTOP_LOCK_KEY=same-instance")
	err = child.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 19 {
		t.Fatalf("a second process acquired the lock: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if next, err := acquireDesktopLock(ctx, "same-instance", true); !errors.Is(err, context.DeadlineExceeded) {
		if next != nil {
			next.Close()
		}
		t.Fatalf("waiting lock ignored cancellation: %v", err)
	}
	other, err := acquireDesktopLock(context.Background(), "other-instance", false)
	if err != nil {
		t.Fatal(err)
	}
	other.Close()
}

func TestDesktopSessionRetainsUnconfirmedDeliveryAcrossRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("AI_DISPATCH_HOME", t.TempDir())
	const session = "thread-pending"
	lease, err := acquireDesktopSession(context.Background(), desktopHost{pid: 1}, session)
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := uniqueDesktopPrompt(stamped("task"))
	if err != nil {
		t.Fatal(err)
	}
	id := desktopPromptID(prompt)
	if err := lease.setPending(id); err != nil {
		t.Fatal(err)
	}
	if concurrent, err := acquireDesktopSession(context.Background(), desktopHost{pid: 1}, session); err == nil {
		concurrent.Close()
		t.Fatal("same session acquired twice")
	}
	lease.Close()
	// Changing the App PID cannot discard an unconfirmed persisted submission.
	if retry, err := acquireDesktopSession(context.Background(), desktopHost{pid: 2}, session); err == nil || !strings.Contains(err.Error(), "app_delivery_pending") {
		if retry != nil {
			retry.Close()
		}
		t.Fatalf("unconfirmed submission was forgotten: %v", err)
	}
	writeRollout(t, home, "rollout-2026-11-07T10-00-00-"+session+".jsonl", sessionMetaLine("")+userLine("message", "turn", prompt)+terminalLine("task_complete", "turn", "DONE", ""))
	retry, err := acquireDesktopSession(context.Background(), desktopHost{pid: 2}, session)
	if err != nil {
		t.Fatal(err)
	}
	defer retry.Close()
	if pending, err := retry.pending(); err != nil || pending != "" {
		t.Fatalf("pending=%q err=%v", pending, err)
	}
}

func TestDesktopPreparationReleasesInstanceWhileKeepingSessionOwned(t *testing.T) {
	host, _, queued := desktopBackendFixture(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	lease, err := acquireDesktopSession(context.Background(), host, "thread-42")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	prompt, err := uniqueDesktopPrompt(stamped("TASK"))
	if err != nil {
		t.Fatal(err)
	}
	if err := prepareDesktopMessage(context.Background(), host, providers.BuildRequest{SessionID: "thread-42"}, prompt, lease); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(queued); err != nil {
		t.Fatal(err)
	}
	instance, err := acquireDesktopLock(context.Background(), desktopLockKey(host, "prepare"), false)
	if err != nil {
		t.Fatalf("instance lock is still held during model waiting: %v", err)
	}
	instance.Close()
	other, err := acquireDesktopSession(context.Background(), host, "another-thread")
	if err != nil {
		t.Fatalf("other threads cannot prepare while one awaits reply: %v", err)
	}
	other.Close()
}

func TestDesktopFailureWarningsSurviveParsing(t *testing.T) {
	raw, err := json.Marshal(appEnvelope{Surface: "app", SessionID: "retained", State: "failed", Error: "context canceled", Warnings: []string{"Stopped waiting; the Desktop turn may still execute"}})
	if err != nil {
		t.Fatal(err)
	}
	result := (Provider{}).Parse(runtime.RunResult{Stdout: raw, ExitCode: 1}, providers.BuildRequest{})
	if result.OK || result.SessionID != "retained" || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "may still execute") {
		t.Fatalf("result=%+v", result)
	}
}

func TestDesktopSettingsReceiptMustMatchBoundInstance(t *testing.T) {
	host, _, _ := desktopBackendFixture(t)
	host.pid = 42
	if err := configureDesktopThread(context.Background(), host, providers.BuildRequest{}, "Bound thread"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_DESKTOP_UI_WRONG_INSTANCE", "1")
	if err := configureDesktopThread(context.Background(), host, providers.BuildRequest{}, "Bound thread"); err == nil {
		t.Fatal("a different App PID proved the requested settings")
	}
}

func TestDesktopEmptyPersistedThreadCanReachDeliveryOnResume(t *testing.T) {
	host, _, _ := desktopBackendFixture(t)
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("TEST_DESKTOP_UI_WRONG_INSTANCE", "1")
	// Fail the route intentionally: reaching this error proves that absent
	// rollout metadata no longer makes the persisted thread impossible to retry.
	_, session, err := continueDesktopThread(context.Background(), host, providers.BuildRequest{SessionID: "empty-thread"}, stamped("retry"))
	if err == nil || !strings.Contains(err.Error(), "open codex desktop thread") || session != "empty-thread" {
		t.Fatalf("session=%q err=%v", session, err)
	}
}
