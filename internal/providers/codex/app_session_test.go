package codex

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestCLISurfaceStaysOnExec(t *testing.T) {
	fakeCodexBinary(t)
	target := routing.DispatchTarget{Requested: "codex", Provider: "codex", Model: "gpt-5.5"}
	for _, surface := range []string{"", "cli"} {
		run, handled, err := Provider{}.RunDirect(context.Background(), providers.BuildRequest{
			Prompt:  "hello",
			Target:  target,
			Surface: surface,
		})
		if err != nil || handled || len(run.Stdout) != 0 {
			t.Fatalf("surface %q handled=%v err=%v run=%+v", surface, handled, err, run)
		}
		spec, err := Provider{}.Build(providers.BuildRequest{Prompt: "hello", Target: target, Surface: surface})
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(spec.Args, " "), "exec") {
			t.Fatalf("cli surface must stay on exec: %#v", spec.Args)
		}
	}
}

func TestParseAppEnvelopeKeepsSessionOnFailure(t *testing.T) {
	raw := `{"surface":"app","session_id":"thread-1","text":""}` + "\n"
	result := Provider{}.Parse(runtime.RunResult{ExitCode: 1, Stderr: []byte("turn failed"), Stdout: []byte(raw)}, providers.BuildRequest{
		Target: routing.DispatchTarget{Requested: "codex", Provider: "codex", Model: "gpt-5.5"},
	})
	if result.OK || result.SessionID != "thread-1" || result.Surface != "app" {
		t.Fatalf("result=%+v", result)
	}
}

func TestQueuedReplyRejectsFailedTerminalKinds(t *testing.T) {
	for _, kind := range []string{"turn_aborted", "task_interrupted", "turn_interrupted", "cancelled", "task_failed"} {
		t.Run(kind, func(t *testing.T) {
			data := userLine("msg-current", "current", stamped("PROMPT")) + terminalLine(kind, "current", "partial output", "interrupted")
			if scan, err := queuedReply(nil, []byte(data), stamped("PROMPT")); err == nil || scan.ready {
				t.Fatalf("failed terminal accepted: scan=%+v err=%v", scan, err)
			}
		})
	}
}

func TestQueuedReplyAbortCannotBeOverriddenByReplayedComplete(t *testing.T) {
	prompt := stamped("PROMPT")
	data := userLine("new", "current", prompt) +
		`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"current","reason":"interrupted"}}` + "\n" +
		terminalLine("task_complete", "old", "OLD", "") + terminalLine("task_complete", "current", "REPLAYED", "")
	scan, err := queuedReply(nil, []byte(data), prompt)
	if err == nil || scan.ready || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
}

func TestParseRejectsZeroExitWithAbortedOrUnconfirmedTurn(t *testing.T) {
	for _, raw := range []string{
		`{"surface":"app","session_id":"thread-1","text":"partial","state":"failed","error":"interrupted"}`,
		`{"surface":"app","session_id":"thread-1","text":"partial","state":"queued"}`,
		`{"surface":"app","session_id":"thread-1","text":"partial"}`,
		`{"thread_id":"thread-1","item":{"type":"agent_message","text":"partial"}}` + "\n" + `{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"current","reason":"interrupted"}}`,
	} {
		result := Provider{}.Parse(runtime.RunResult{ExitCode: 0, Stdout: []byte(raw)}, providers.BuildRequest{})
		if result.OK || result.Status == contract.StatusSuccess || result.SessionID != "thread-1" || result.Stderr == "" {
			t.Fatalf("raw=%s result=%+v", raw, result)
		}
	}
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(`{"surface":"app","session_id":"thread-1","text":"answer","state":"completed"}`)}, providers.BuildRequest{})
	if !result.OK {
		t.Fatalf("confirmed completion rejected: %+v", result)
	}
}

func TestRolloutSelectionKeepsExactThreadAndAnchor(t *testing.T) {
	if isThreadRollout("rollout-time-otherthread-42.jsonl", "thread-42") {
		t.Fatal("thread ID substring accepted")
	}
	if !isThreadRollout("rollout-time-thread-42_segment.jsonl", "thread-42") {
		t.Fatal("thread continuation rejected")
	}
	paths := rolloutSpan("missing-anchor", []rolloutSeg{{path: "unrelated", id: "other", name: "newest"}})
	if len(paths) != 1 || paths[0] != "missing-anchor" {
		t.Fatalf("switched rollout branch: %v", paths)
	}
}

func TestCodexFailureRemainsBoundToCurrentTurn(t *testing.T) {
	stream := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"current"}}` + "\n" +
		`{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"old","reason":"interrupted"}}` + "\n"
	if reason := codexTurnFailure(stream); reason != "" {
		t.Fatalf("old abort contaminated new turn: %s", reason)
	}
	stream += `{"type":"event_msg","payload":{"type":"turn_aborted","turn_id":"current","reason":"interrupted"}}` + "\n" + terminalLine("task_complete", "old", "OLD", "")
	if reason := codexTurnFailure(stream); !strings.Contains(reason, "interrupted") {
		t.Fatalf("abort lost to old complete: %s", reason)
	}
}

func TestFindRolloutUsesCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions", "2026", "10", "15")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-thread-42.jsonl")
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := findRollout("thread-42")
	if err != nil || got != path {
		t.Fatalf("got=%s err=%v", got, err)
	}
}

func TestQueuedReplyBindsOnlyTheMatchingTurn(t *testing.T) {
	old := userLine("msg-old", "turn-old", "OLD") + terminalLine("task_complete", "turn-old", "OLD", "")
	other := userLine("msg-other", "turn-other", "OTHER") + terminalLine("task_complete", "turn-other", "OTHER", "")
	pending := old + other + userLine("msg-new", "turn-new", "PROMPT")
	if scan, err := queuedReply([]byte(old), []byte(pending), "PROMPT"); err != nil || scan.ready || scan.text != "" {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
	done := pending + terminalLine("task_complete", "turn-new", "ANSWER", "")
	scan, err := queuedReply([]byte(old), []byte(done), "PROMPT")
	if err != nil || !scan.ready || scan.text != "ANSWER" {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
}

func TestQueuedReplyRejectsAmbiguousAndFailedTurns(t *testing.T) {
	ambiguous := userLine("msg-a", "turn-a", "PROMPT") + userLine("msg-b", "turn-b", "PROMPT")
	if _, err := queuedReply(nil, []byte(ambiguous), "PROMPT"); err == nil {
		t.Fatal("two new copies of the same prompt must not be guessed")
	}
	failed := userLine("msg-new", "turn-new", "PROMPT") + terminalLine("task_failed", "turn-new", "", "boom")
	if _, err := queuedReply(nil, []byte(failed), "PROMPT"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestQueuedReplyRequiresTheDispatchMarker(t *testing.T) {
	prompt := stamped("PROMPT")
	hand := userLine("msg-hand", "turn-hand", "PROMPT") + terminalLine("task_complete", "turn-hand", "HAND", "")
	if scan, err := queuedReply(nil, []byte(hand), prompt); err != nil || scan.ready || scan.landed || scan.text != "" {
		t.Fatalf("an unmarked turn must not bind: scan=%+v err=%v", scan, err)
	}
	// The marker survives while the blank line after it is collapsed.
	sent := userLine("msg-sent", "turn-sent", "[ai-dispatch]\nPROMPT\n") + terminalLine("task_complete", "turn-sent", "ANSWER", "")
	scan, err := queuedReply(nil, []byte(sent), prompt)
	if err != nil || !scan.ready || scan.text != "ANSWER" {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
}

func TestQueuedReplyReportsForeignDispatchPrompt(t *testing.T) {
	foreign := userLine("msg-other", "turn-other", stamped("SOMETHING ELSE"))
	scan, err := queuedReply(nil, []byte(foreign), stamped("PROMPT"))
	if err != nil || scan.ready || scan.landed || scan.other == "" {
		t.Fatalf("scan=%+v err=%v", scan, err)
	}
}

func TestWaitForQueuedReplyFailsWhenThreadNeverGetsThePrompt(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	stale := sessionMetaLine("") + userLine("msg-old", "turn-old", stamped("OLD")) + terminalLine("task_complete", "turn-old", "OLD", "")
	if err := os.WriteFile(rollout, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	req := providers.BuildRequest{ActivityTimeoutSeconds: 1}
	_, err := waitForQueuedReply(context.Background(), req, rollout, []byte(stale), stamped("PROMPT"))
	if err == nil || !strings.Contains(err.Error(), "app_not_picked_up") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitForQueuedReplyFailsWhenLandedTurnStalls(t *testing.T) {
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	landed := sessionMetaLine("") + userLine("msg-new", "turn-new", stamped("PROMPT"))
	if err := os.WriteFile(rollout, []byte(landed), 0o644); err != nil {
		t.Fatal(err)
	}
	req := providers.BuildRequest{ActivityTimeoutSeconds: 1}
	_, err := waitForQueuedReply(context.Background(), req, rollout, nil, stamped("PROMPT"))
	if err == nil || !strings.Contains(err.Error(), "app_stalled") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitForQueuedReplyKeepsWaitingOnLandedTurnByDefault(t *testing.T) {
	// A landed turn parked on a tool approval must not be killed unless the
	// caller asked for an inactivity limit.
	rollout := filepath.Join(t.TempDir(), "rollout.jsonl")
	landed := sessionMetaLine("") + userLine("msg-new", "turn-new", stamped("PROMPT"))
	if err := os.WriteFile(rollout, []byte(landed), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err := waitForQueuedReply(ctx, providers.BuildRequest{ActivityTimeoutSeconds: -1}, rollout, nil, stamped("PROMPT"))
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("a landed turn must keep waiting: err=%v", err)
	}
}

func TestWaitForQueuedReplyReturnsReadError(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	req := providers.BuildRequest{SessionID: "missing"}
	_, err := waitForQueuedReply(context.Background(), req, filepath.Join(t.TempDir(), "missing.jsonl"), nil, "PROMPT")
	if err == nil {
		t.Fatal("a rollout read error must stop the wait")
	}
}

func TestLiveSegmentIsTheRolloutLeaf(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	const thread = "01a13974-ade6-7ef1-8038-f9bae6596624"
	const mid = "01a14000-0000-7000-8000-000000000001"
	const leaf = "01a142d9-9b8b-70c3-a6f8-d69b38972cf8"
	writeRollout(t, home, "2026", "10", "14",
		"rollout-2026-10-14T16-08-30-"+thread+".jsonl",
		sessionMetaLine("")+userLine("msg-old", "turn-old", "PROMPT")+terminalLine("task_complete", "turn-old", "STALE", ""))
	writeRollout(t, home, "2026", "10", "15",
		"rollout-2026-10-15T10-00-00-"+thread+"_"+mid+".jsonl",
		sessionMetaLine(thread)+userLine("msg-mid", "turn-mid", "PROMPT")+terminalLine("task_complete", "turn-mid", "MID", ""))
	live := writeRollout(t, home, "2026", "10", "16",
		"rollout-2026-10-16T11-55-19-"+thread+"_"+leaf+".jsonl",
		sessionMetaLine(mid))

	got, err := findRollout(thread)
	if err != nil || got != live {
		t.Fatalf("got=%s err=%v", got, err)
	}
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(live, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(userLine("msg-new", "turn-new", "PROMPT") + terminalLine("task_complete", "turn-new", "ANSWER", "")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text, err := waitForQueuedReply(ctx, providers.BuildRequest{SessionID: thread}, live, before, "PROMPT")
	if err != nil || text != "ANSWER" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestQueuedReplyFollowsSegmentCreatedDuringWait(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	const thread = "01a13974-ade6-7ef1-8038-f9bae6596624"
	const liveID = "01a142d9-9b8b-70c3-a6f8-d69b38972cf8"
	const nextID = "01a14300-0000-7000-8000-000000000002"
	writeRollout(t, home, "2026", "10", "14",
		"rollout-2026-10-14T16-08-30-"+thread+".jsonl",
		sessionMetaLine(""))
	live := writeRollout(t, home, "2026", "10", "16",
		"rollout-2026-10-16T11-55-19-"+thread+"_"+liveID+".jsonl",
		sessionMetaLine(thread)+userLine("msg-old", "turn-old", "PROMPT")+terminalLine("task_complete", "turn-old", "STALE", ""))
	got, err := findRollout(thread)
	if err != nil || got != live {
		t.Fatalf("got=%s err=%v", got, err)
	}
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type reply struct {
		text string
		err  error
	}
	done := make(chan reply, 1)
	go func() {
		text, err := waitForQueuedReply(ctx, providers.BuildRequest{SessionID: thread}, live, before, "PROMPT")
		done <- reply{text, err}
	}()
	time.Sleep(200 * time.Millisecond)
	queued := userLine("msg-new", "turn-new", "PROMPT")
	f, err := os.OpenFile(live, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(queued); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, home, "2026", "10", "16",
		"rollout-2026-10-16T14-20-04-"+thread+"_"+nextID+".jsonl",
		sessionMetaLine(liveID)+userLine("msg-old", "turn-old", "PROMPT")+queued+terminalLine("task_complete", "turn-new", "ANSWER", ""))

	select {
	case got := <-done:
		if got.err != nil || got.text != "ANSWER" {
			t.Fatalf("text=%q err=%v", got.text, got.err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

func TestTerminalOnNextSegmentStillMatchesTheQueuedTurn(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	const thread = "01a13974-ade6-7ef1-8038-f9bae6596624"
	const liveID = "01a142d9-9b8b-70c3-a6f8-d69b38972cf8"
	const nextID = "01a14300-0000-7000-8000-000000000002"
	writeRollout(t, home, "2026", "10", "14",
		"rollout-2026-10-14T16-08-30-"+thread+".jsonl",
		sessionMetaLine("")+userLine("msg-old", "turn-old", "PROMPT")+terminalLine("task_complete", "turn-old", "STALE", ""))
	live := writeRollout(t, home, "2026", "10", "16",
		"rollout-2026-10-16T11-55-19-"+thread+"_"+liveID+".jsonl",
		sessionMetaLine(thread))
	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(live, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(userLine("msg-new", "turn-new", "PROMPT")); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	writeRollout(t, home, "2026", "10", "16",
		"rollout-2026-10-16T14-20-04-"+thread+"_"+nextID+".jsonl",
		sessionMetaLine(liveID)+terminalLine("task_complete", "turn-new", "ANSWER", ""))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text, err := waitForQueuedReply(ctx, providers.BuildRequest{SessionID: thread}, live, before, "PROMPT")
	if err != nil || text != "ANSWER" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestDesktopTurnStateFollowsOnlyAnchorAncestors(t *testing.T) {
	const thread = "thread-42"
	started := `{"type":"event_msg","payload":{"type":"task_started","turn_id":"turn-current"}}` + "\n"
	user := userLine("msg-current", "turn-current", stamped("PROMPT"))
	for _, tc := range []struct {
		name       string
		parent     string
		child      string
		sibling    string
		wantActive string
	}{
		{
			name:    "completed parent with replayed child user",
			parent:  started + user + terminalLine("task_complete", "turn-current", "DONE", ""),
			child:   user,
			sibling: started + user,
		},
		{
			name:       "active parent with metadata-only child",
			parent:     started + user,
			sibling:    terminalLine("task_complete", "turn-current", "OTHER BRANCH", ""),
			wantActive: "turn-current",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			writeRollout(t, home, "rollout-2026-11-06T10-00-00-"+thread+".jsonl", sessionMetaLine("")+tc.parent)
			writeRollout(t, home, "rollout-2026-11-06T11-00-00-"+thread+"_middle.jsonl", sessionMetaLine(thread))
			anchor := writeRollout(t, home, "rollout-2026-11-06T12-00-00-"+thread+"_leaf.jsonl", sessionMetaLine("middle")+tc.child)
			writeRollout(t, home, "rollout-2026-11-06T13-00-00-"+thread+"_sibling.jsonl", sessionMetaLine(thread)+tc.sibling)
			active, err := activeDesktopTurnInRollout(thread, anchor)
			if err != nil {
				t.Fatal(err)
			}
			if active != tc.wantActive {
				t.Fatalf("active=%q want=%q", active, tc.wantActive)
			}
			// Ancestral user messages must not expand the queued before snapshot.
			before, err := os.ReadFile(anchor)
			if err != nil {
				t.Fatal(err)
			}
			data := append([]byte(nil), before...)
			data = append(data, []byte(terminalLine("task_complete", "turn-current", "OLD", "")+userLine("msg-next", "turn-next", stamped("NEXT")))...)
			if scan, err := queuedReply(before, data, stamped("NEXT")); err != nil || !scan.landed || scan.ready {
				t.Fatalf("old completion finished queued follow-up: scan=%+v err=%v", scan, err)
			}
		})
	}
}

func TestDesktopResumeAllowsCompletedAncestorWithReplayedUser(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	bin := t.TempDir()
	t.Setenv("PATH", bin)
	if err := os.WriteFile(filepath.Join(bin, "open"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	const thread = "thread-42"
	user := userLine("msg-done", "done-parent", stamped("OLD"))
	writeRollout(t, home, "rollout-2026-11-06T10-00-00-"+thread+".jsonl", sessionMetaLine("")+user+terminalLine("task_complete", "done-parent", "DONE", ""))
	writeRollout(t, home, "rollout-2026-11-06T11-00-00-"+thread+"_leaf.jsonl", sessionMetaLine(thread)+user)
	_, sessionID, err := continueDesktopThread(context.Background(), desktopHost{binary: "codex"}, providers.BuildRequest{SessionID: thread}, stamped("NEXT"))
	// This synthetic host has no bound PID. Delivery must reach that route
	// boundary rather than rejecting a completed, replayed ancestor turn.
	if err == nil || !strings.Contains(err.Error(), "open codex desktop thread") || sessionID != thread {
		t.Fatalf("completed ancestor blocked resume: session=%q err=%v", sessionID, err)
	}
}

func TestDesktopResumeRejectsActiveAncestorBeforeQueue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("PATH", t.TempDir())
	const thread = "thread-42"
	writeRollout(t, home, "rollout-2026-11-06T10-00-00-"+thread+".jsonl", sessionMetaLine("")+`{"type":"event_msg","payload":{"type":"task_started","turn_id":"active-parent"}}`+"\n")
	writeRollout(t, home, "rollout-2026-11-06T11-00-00-"+thread+"_leaf.jsonl", sessionMetaLine(thread))
	_, sessionID, err := continueDesktopThread(context.Background(), desktopHost{binary: "codex"}, providers.BuildRequest{SessionID: thread}, stamped("NEXT"))
	if err == nil || !strings.Contains(err.Error(), "app_turn_active") || sessionID != thread {
		t.Fatalf("session=%q err=%v", sessionID, err)
	}
}

func TestRolloutHistoryRejectsBrokenAncestorChain(t *testing.T) {
	for _, parent := range []string{"missing", "leaf"} {
		t.Run(parent, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("CODEX_HOME", home)
			anchor := writeRollout(t, home, "rollout-2026-11-06T10-00-00-thread-42_leaf.jsonl", sessionMetaLine(parent))
			if _, err := activeDesktopTurnInRollout("thread-42", anchor); err == nil {
				t.Fatal("incomplete or cyclic ancestry must not permit queueing")
			}
		})
	}
}

func TestCreateDesktopThreadReleasesMetadataOwnerWithoutExecutingTurn(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	host, closed, _ := desktopBackendFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	threadID, err := createDesktopThread(ctx, host, providers.BuildRequest{}, "Metadata-only handoff")
	if err != nil || threadID != "thread-42" {
		t.Fatalf("thread=%q err=%v", threadID, err)
	}
	if _, err := os.Stat(closed); err != nil {
		t.Fatalf("metadata owner was not released before returning: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(closed), "unexpected-turn")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("metadata-only creation unexpectedly executed a turn: %v", err)
	}
}

func TestWaitForQueuedReplyPicksUpFirstRolloutAfterGUIStarts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	dir := filepath.Join(home, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := stamped("NEW TASK")
	body := sessionMetaLine("") + terminalLine("task_complete", "old", "STALE", "") + userLine("msg-other", "other", stamped("OTHER TASK")) + terminalLine("task_complete", "other", "FOREIGN", "") + userLine("msg-new", "current", prompt) + terminalLine("task_complete", "current", "GUI ANSWER", "")
	written := make(chan error, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		written <- os.WriteFile(filepath.Join(dir, "rollout-time-thread-42.jsonl"), []byte(body), 0o644)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	text, err := waitForQueuedReply(ctx, providers.BuildRequest{SessionID: "thread-42", ActivityTimeoutSeconds: 1}, "", nil, prompt)
	if writeErr := <-written; writeErr != nil {
		t.Fatal(writeErr)
	}
	if err != nil || text != "GUI ANSWER" {
		t.Fatalf("text=%q err=%v", text, err)
	}
}

func TestWaitForFirstRolloutKeepsPickupLimit(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	_, err := waitForQueuedReply(context.Background(), providers.BuildRequest{SessionID: "thread-42", ActivityTimeoutSeconds: 1}, "", nil, stamped("PROMPT"))
	if err == nil || !strings.Contains(err.Error(), "app_not_picked_up") {
		t.Fatalf("missing first rollout should exhaust pickup, not succeed or fail a read: %v", err)
	}
}

func writeRollout(t *testing.T, home string, parts ...string) string {
	t.Helper()
	if len(parts) < 2 {
		t.Fatal("writeRollout needs a filename and a body")
	}
	body := parts[len(parts)-1]
	name := parts[len(parts)-2]
	dir := filepath.Join(append([]string{home, "sessions"}, parts[:len(parts)-2]...)...)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func sessionMetaLine(parent string) string {
	if parent == "" {
		return "{\"type\":\"session_meta\",\"payload\":{\"id\":\"thread\",\"session_id\":\"thread\"}}\n"
	}
	return fmt.Sprintf("{\"type\":\"session_meta\",\"payload\":{\"id\":\"thread\",\"session_id\":\"thread\",\"history_base\":{\"thread_id\":%q}}}\n", parent)
}

func userLine(messageID, turnID, text string) string {
	return fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","id":%q,"role":"user","content":[{"type":"input_text","text":%q}],"internal_chat_message_metadata_passthrough":{"turn_id":%q}}}`+"\n", messageID, text, turnID)
}

func terminalLine(kind, turnID, text, message string) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":%q,"turn_id":%q,"last_agent_message":%q,"message":%q}}`+"\n", kind, turnID, text, message)
}

// stamped renders a prompt the way stampPrompt does before delivery.
func stamped(body string) string {
	return contract.DispatchPromptMarker + "\n\n" + body
}
