package progress

import (
	"bytes"
	"encoding/json"
	"strings"
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
