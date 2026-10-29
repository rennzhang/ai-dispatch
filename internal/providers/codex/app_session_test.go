package codex

import (
	"context"
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

func TestTurnEndedAfterIgnoresPreviousAnswer(t *testing.T) {
	old := []byte(`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"t1","last_agent_message":"OLD"}}` + "\n")
	samePrompt := append([]byte{}, old...)
	samePrompt = append(samePrompt, []byte(`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"OLD"}]}}`+"\n")...)
	if _, ok := turnEndedAfter(old, samePrompt); ok {
		t.Fatal("previous assistant text must not finish a new follow-up")
	}
	added := append([]byte{}, old...)
	added = append(added, []byte(`{"type":"event_msg","payload":{"type":"agent_message","message":"我会检查仓库"}}`+"\n")...)
	if _, ok := turnEndedAfter(old, added); ok {
		t.Fatal("commentary must not finish the turn")
	}
	added = append(added, []byte(`{"type":"event_msg","payload":{"type":"task_complete","turn_id":"t2","last_agent_message":"NEW \"line\""}}`+"\n")...)
	end, ok := turnEndedAfter(old, added)
	if !ok || end.failed || end.text != `NEW "line"` {
		t.Fatalf("end=%+v ok=%v", end, ok)
	}
}

func TestTurnEndedAfterReportsFailure(t *testing.T) {
	before := []byte{}
	data := []byte(`{"type":"event_msg","payload":{"type":"task_failed","turn_id":"t9","message":"interrupted by user"}}` + "\n")
	end, ok := turnEndedAfter(before, data)
	if !ok || !end.failed || end.errText != "interrupted by user" {
		t.Fatalf("end=%+v ok=%v", end, ok)
	}
}

func TestTurnResultRejectsFailedStatus(t *testing.T) {
	_, err := turnResult([]byte(`{"turn":{"id":"t","status":"failed","items":[],"error":{"message":"boom"}}}`), "partial")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
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
