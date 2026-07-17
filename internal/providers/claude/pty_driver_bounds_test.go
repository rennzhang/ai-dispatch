package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestParseObservedSessionAcceptsJSONLAboveDefaultScannerLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	want := strings.Repeat("x", 128<<10)
	line, err := json.Marshal(map[string]any{
		"type":      "assistant",
		"sessionId": "session-large-line",
		"message": map[string]any{
			"stop_reason": "end_turn",
			"content":     []any{map[string]any{"type": "text", "text": want}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	obs := parseObservedSession(path, 0)
	if obs.scanError != "" || obs.assistantText != want || obs.sessionID != "session-large-line" {
		t.Fatalf("text_len=%d session=%q scan_error=%q", len(obs.assistantText), obs.sessionID, obs.scanError)
	}
}

func TestCountLinesHandlesJSONLAboveDefaultScannerLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := strings.Repeat("x", 128<<10) + "\nsecond line without newline"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	count, err := countLines(path)
	if err != nil || count != 2 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}

func TestParseObservedSessionReportsBoundedScannerError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"type":"assistant","message":{"content":[{"type":"text","text":"` + strings.Repeat("x", claudeSessionJSONLTokenLimit) + `"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	obs := parseObservedSession(path, 0)
	if obs.scanError == "" || !strings.Contains(obs.scanError, "token limit") {
		t.Fatalf("scanner failure was silent: %+v", obs)
	}
}

func TestParseClaudePTYPreservesDriverWarnings(t *testing.T) {
	stdout := strings.Join([]string{
		`{"event":"warning","message":"Claude session JSONL scan failed"}`,
		`{"event":"done","session_id":"session-1","response_text":"done","termination_reason":"done"}`,
		"",
	}, "\n")
	result := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout), ExitCode: 0}, providers.BuildRequest{
		Target:          routing.DispatchTarget{Requested: "claude", Provider: "claude", Model: "sonnet"},
		ProviderOptions: map[string]string{"transport": "pty"},
	})
	if !result.OK || len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], "JSONL scan failed") {
		t.Fatalf("result=%+v", result)
	}
}

func TestClaudePTYCancellationBeforeStartDoesNotInvokeTmux(t *testing.T) {
	root := t.TempDir()
	logPath := filepath.Join(root, "tmux.log")
	writeFakeTmux(t, root)
	t.Setenv("PATH", root+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_TMUX_LOG", logPath)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	err := runGoPTYDriverContext(ctx, ptyDriverConfig{
		CWD:           root,
		Timeout:       0,
		Input:         "hello",
		SessionID:     "early-cancel-session",
		ClaudeBaseDir: filepath.Join(root, ".claude"),
		Command:       []string{"fake-claude"},
	}, os.Stdout, os.Stderr)
	if err == nil || !strings.Contains(err.Error(), "canceled before start") {
		t.Fatalf("err=%v", err)
	}
	if time.Since(started) > time.Second {
		t.Fatalf("early cancellation was not prompt: %s", time.Since(started))
	}
	if data, _ := os.ReadFile(logPath); len(data) != 0 {
		t.Fatalf("tmux was invoked after early cancellation: %s", data)
	}
}

func TestClaudeSessionLocatorUsesCWDDirectPathWithoutWaitingForFallback(t *testing.T) {
	baseDir := t.TempDir()
	cwd := "/tmp/direct-project"
	sessionID := "direct-session"
	path := filepath.Join(baseDir, "projects", strings.ReplaceAll(cwd, "/", "-"), sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	locator := newClaudeSessionLocator(baseDir, cwd, sessionID, time.Now())
	locator.fallbackInterval = time.Hour
	if got := locator.find(); got != path {
		t.Fatalf("direct path=%q want=%q", got, path)
	}
}

func TestClaudeSessionLocatorThrottlesExactIDTreeFallback(t *testing.T) {
	baseDir := t.TempDir()
	sessionID := "fallback-session"
	path := filepath.Join(baseDir, "projects", "normalized-project", sessionID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	locator := newClaudeSessionLocator(baseDir, "/symlinked/project", sessionID, time.Now())
	locator.fallbackInterval = time.Hour
	if got := locator.find(); got != "" {
		t.Fatalf("fallback must remain throttled, got=%q", got)
	}
	locator.lastFallback = time.Now().Add(-2 * locator.fallbackInterval)
	if got := locator.find(); got != path {
		t.Fatalf("fallback path=%q want=%q", got, path)
	}
	if elapsed := time.Since(locator.lastFallback); elapsed > time.Second {
		t.Fatalf("fallback timestamp was not refreshed: %s", elapsed)
	}
}
