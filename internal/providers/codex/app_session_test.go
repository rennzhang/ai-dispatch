package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if text, ready, err := queuedReply([]byte(old), []byte(pending), "PROMPT"); err != nil || ready || text != "" {
		t.Fatalf("text=%q ready=%v err=%v", text, ready, err)
	}
	done := pending + terminalLine("task_complete", "turn-new", "ANSWER", "")
	text, ready, err := queuedReply([]byte(old), []byte(done), "PROMPT")
	if err != nil || !ready || text != "ANSWER" {
		t.Fatalf("text=%q ready=%v err=%v", text, ready, err)
	}
}

func TestQueuedReplyRejectsAmbiguousAndFailedTurns(t *testing.T) {
	ambiguous := userLine("msg-a", "turn-a", "PROMPT") + userLine("msg-b", "turn-b", "PROMPT")
	if _, _, err := queuedReply(nil, []byte(ambiguous), "PROMPT"); err == nil {
		t.Fatal("two new copies of the same prompt must not be guessed")
	}
	failed := userLine("msg-new", "turn-new", "PROMPT") + terminalLine("task_failed", "turn-new", "", "boom")
	if _, _, err := queuedReply(nil, []byte(failed), "PROMPT"); err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("err=%v", err)
	}
}

func TestWaitForQueuedReplyReturnsReadError(t *testing.T) {
	_, err := waitForQueuedReply(context.Background(), filepath.Join(t.TempDir(), "missing.jsonl"), nil, "PROMPT")
	if err == nil {
		t.Fatal("a rollout read error must stop the wait")
	}
}

func userLine(messageID, turnID, text string) string {
	return fmt.Sprintf(`{"type":"response_item","payload":{"type":"message","id":%q,"role":"user","content":[{"type":"input_text","text":%q}],"internal_chat_message_metadata_passthrough":{"turn_id":%q}}}`+"\n", messageID, text, turnID)
}

func terminalLine(kind, turnID, text, message string) string {
	return fmt.Sprintf(`{"type":"event_msg","payload":{"type":%q,"turn_id":%q,"last_agent_message":%q,"message":%q}}`+"\n", kind, turnID, text, message)
}
