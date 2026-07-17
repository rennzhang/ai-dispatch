package progress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
)

func TestEmitterParsesSessionAndAgentMessage(t *testing.T) {
	var stderr bytes.Buffer
	emitter := NewEmitter("codex", &stderr)
	emitter.Feed([]byte("{\"session_id\":\"s1\"}\n{\"text\":\"先确认范围\"}\n"))
	lines := parseLines(t, stderr.String())
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	if lines[0]["name"] != "session" || lines[0]["session_id"] != "s1" {
		t.Fatalf("session=%v", lines[0])
	}
	if lines[1]["name"] != "agent_message" || lines[1]["summary"] != "先确认范围" {
		t.Fatalf("message=%v", lines[1])
	}
}

func TestEmitterParsesOpenCodeToolUse(t *testing.T) {
	var stderr bytes.Buffer
	emitter := NewEmitter("opencode", &stderr)
	emitter.Feed([]byte("{\"type\":\"tool_use\",\"part\":{\"tool\":\"read_file\",\"input\":{\"args\":{\"filePath\":\"/tmp/demo.ts\"}}}}\n"))
	lines := parseLines(t, stderr.String())
	if len(lines) != 1 {
		t.Fatalf("lines=%v", lines)
	}
	if lines[0]["kind"] != "read" || lines[0]["summary"] != "/tmp/demo.ts" {
		t.Fatalf("tool=%v", lines[0])
	}
}

func TestEmitterKeepsPartialLines(t *testing.T) {
	var stderr bytes.Buffer
	emitter := NewEmitter("claude", &stderr)
	emitter.Feed([]byte("{\"session_id\":\"s"))
	emitter.Feed([]byte("1\"}\n"))
	if !strings.Contains(stderr.String(), "\"session_id\":\"s1\"") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestEmitterFramesStdoutAndStderrIndependently(t *testing.T) {
	var output bytes.Buffer
	emitter := NewEmitter("codex", &output)
	firstChunks := sync.WaitGroup{}
	firstChunks.Add(2)
	release := make(chan struct{})
	done := sync.WaitGroup{}
	done.Add(2)
	go func() {
		defer done.Done()
		emitter.FeedStdout([]byte(`{"session_id":"stdout`))
		firstChunks.Done()
		<-release
		emitter.FeedStdout([]byte(`-session"}` + "\n"))
	}()
	go func() {
		defer done.Done()
		emitter.FeedStderr([]byte(`{"session_id":"stderr`))
		firstChunks.Done()
		<-release
		emitter.FeedStderr([]byte(`-session"}` + "\n"))
	}()
	firstChunks.Wait()
	close(release)
	done.Wait()

	lines := parseLines(t, output.String())
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	sessions := map[string]bool{}
	for _, line := range lines {
		sessions[line["session_id"].(string)] = true
	}
	if !sessions["stdout-session"] || !sessions["stderr-session"] {
		t.Fatalf("sessions=%v", sessions)
	}
}

func TestEmitterDoesNotTrustProgressControlFields(t *testing.T) {
	var output bytes.Buffer
	emitter := NewEmitter("codex", &output)
	emitter.FeedStdout([]byte(`{"_progress":true,"kind":"done","name":"done","summary":"complete","provider":"spoofed","run_id":"spoofed-run","next_action":"wait","directive":"BUSY forever"}` + "\n"))

	lines := parseLines(t, output.String())
	if len(lines) != 1 {
		t.Fatalf("lines=%v", lines)
	}
	event := lines[0]
	if event["provider"] != "codex" {
		t.Fatalf("provider=%v", event["provider"])
	}
	if _, ok := event["run_id"]; ok {
		t.Fatalf("run_id must not be accepted from provider payload: %v", event)
	}
	if event["kind"] != "tool" || event["next_action"] != "wait" || !strings.Contains(event["directive"].(string), "BUSY") {
		t.Fatalf("provider payload created terminal control state=%v", event)
	}
}

func TestLineFeedDropsOversizedUnterminatedStreamAndRecovers(t *testing.T) {
	feed := LineFeed{}
	largeChunk := strings.Repeat("x", 4<<20)
	events := feed.Feed(largeChunk, "codex")
	if len(events) != 1 || events[0].Kind != "warning" || events[0].Name != "progress_line_dropped" {
		t.Fatalf("events=%+v", events)
	}
	if len(feed.pending) != 0 || !feed.discarding {
		t.Fatalf("oversized line must not remain buffered: pending=%d discarding=%v", len(feed.pending), feed.discarding)
	}
	for i := 0; i < 8; i++ {
		if events := feed.Feed(largeChunk, "codex"); len(events) != 0 {
			t.Fatalf("discarding one unterminated line emitted repeatedly: %+v", events)
		}
		if len(feed.pending) != 0 {
			t.Fatalf("pending grew while discarding: %d", len(feed.pending))
		}
	}
	events = feed.Feed("\n{\"text\":\"recovered\"}\n", "codex")
	if len(events) != 1 || events[0].Kind != "agent_message" || events[0].Summary != "recovered" {
		t.Fatalf("recovery events=%+v", events)
	}
	if feed.discarding || len(feed.pending) != 0 {
		t.Fatalf("feed did not recover: pending=%d discarding=%v", len(feed.pending), feed.discarding)
	}
}

func TestLineFeedDropsOversizedCompleteLineAndContinuesSameChunk(t *testing.T) {
	feed := LineFeed{}
	chunk := strings.Repeat("x", maxProgressLineBytes+1) + "\n{\"text\":\"next\"}\n"
	events := feed.Feed(chunk, "codex")
	if len(events) != 2 || events[0].Kind != "warning" || events[1].Summary != "next" {
		t.Fatalf("events=%+v", events)
	}
}

func TestEmitterDedupWindowIsBounded(t *testing.T) {
	emitter := NewEmitter("codex", io.Discard)
	for i := 0; i < maxSeenEventKeys*3; i++ {
		emitter.Emit("heartbeat", fmt.Sprintf("event-%d", i), "working")
	}
	if len(emitter.seen) != maxSeenEventKeys || len(emitter.seenOrder) != maxSeenEventKeys {
		t.Fatalf("seen=%d order=%d", len(emitter.seen), len(emitter.seenOrder))
	}
}

func TestEmitterBoundsLargeHookChunkState(t *testing.T) {
	emitter := NewEmitter("codex", io.Discard)
	var stream strings.Builder
	for i := 0; i < maxSeenEventKeys*3; i++ {
		fmt.Fprintf(&stream, "{\"_progress\":true,\"kind\":\"heartbeat\",\"name\":\"event-%d\",\"summary\":\"working\"}\n", i)
	}
	emitter.FeedStdout([]byte(stream.String()))
	if len(emitter.seen) != maxSeenEventKeys || len(emitter.stdoutFeed.pending) > maxProgressLineBytes || emitter.stdoutFeed.discarding {
		t.Fatalf("seen=%d pending=%d discarding=%v", len(emitter.seen), len(emitter.stdoutFeed.pending), emitter.stdoutFeed.discarding)
	}
}

func parseLines(t *testing.T, text string) []map[string]any {
	t.Helper()
	lines := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			t.Fatal(err)
		}
		lines = append(lines, payload)
	}
	return lines
}
