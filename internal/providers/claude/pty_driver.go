package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

type ptyDriverConfig struct {
	CWD                 string
	Timeout             time.Duration
	Input               string
	SessionID           string
	ClaudeBaseDir       string
	StartupWait         time.Duration
	StartupReadyPattern string
	Command             []string
}

type claudeSessionSummary struct {
	SessionID              string `json:"session_id"`
	FilePath               string `json:"file_path"`
	AssistantText          string `json:"assistant_text,omitempty"`
	LastStopReason         string `json:"last_stop_reason,omitempty"`
	SawPlaceholderResponse bool   `json:"saw_placeholder_response"`
}

type observedSession struct {
	path                  string
	sessionID             string
	assistantText         string
	lastStopReason        string
	sawPlaceholder        bool
	assistantMessageCount int
	toolCallCount         int
	scanError             string
}

type claudePTYExecution struct {
	tmux           tmuxClient
	cfg            ptyDriverConfig
	sessionName    string
	sessionPath    string
	startLine      int
	started        time.Time
	sessionEmitted bool
	locator        *claudeSessionLocator
	stdout         io.Writer
	stderr         io.Writer
}

type claudeSessionLocator struct {
	baseDir          string
	cwd              string
	sessionID        string
	fallbackInterval time.Duration
	lastFallback     time.Time
}

const (
	claudeSessionJSONLTokenLimit  = 4 << 20
	claudeSessionFallbackInterval = 3 * time.Second
)

func RunPTYDriverCLI(argv []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := parsePTYDriverArgs(argv, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), ptyDriverSignals()...)
	defer stop()
	if err := runGoPTYDriverContext(ctx, cfg, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parsePTYDriverArgs(argv []string, stderr io.Writer) (ptyDriverConfig, error) {
	fs := flag.NewFlagSet("__claude-pty-driver", flag.ContinueOnError)
	fs.SetOutput(stderr)
	cwd := fs.String("cwd", "", "working directory")
	transport := fs.String("transport", "tmux", "transport")
	startupWait := fs.Float64("startup-wait", 30, "startup wait seconds")
	startupReadyPattern := fs.String("startup-ready-pattern", "\u276f", "startup ready pattern")
	timeout := fs.Float64("timeout", 300, "timeout seconds")
	input := fs.String("input", "", "input prompt")
	inputFile := fs.String("input-file", "", "input prompt file")
	sessionID := fs.String("session-id", "", "session id")
	claudeBaseDir := fs.String("claude-base-dir", filepath.Join(os.Getenv("HOME"), ".claude"), "Claude base dir")
	if err := fs.Parse(argv); err != nil {
		return ptyDriverConfig{}, err
	}
	if *transport != "tmux" {
		return ptyDriverConfig{}, fmt.Errorf("Go Claude PTY driver only supports tmux transport")
	}
	command := fs.Args()
	if len(command) > 0 && command[0] == "--" {
		command = command[1:]
	}
	if len(command) == 0 {
		return ptyDriverConfig{}, fmt.Errorf("command is required after --")
	}
	prompt := *input
	if *inputFile != "" {
		data, err := os.ReadFile(*inputFile)
		if err != nil {
			return ptyDriverConfig{}, fmt.Errorf("cannot read --input-file: %w", err)
		}
		prompt = string(data)
	}
	if strings.TrimSpace(prompt) == "" {
		return ptyDriverConfig{}, fmt.Errorf("--input or --input-file is required")
	}
	workingDir := *cwd
	if workingDir == "" {
		workingDir, _ = os.Getwd()
	}
	return ptyDriverConfig{
		CWD:                 workingDir,
		Timeout:             durationSeconds(*timeout),
		Input:               prompt,
		SessionID:           *sessionID,
		ClaudeBaseDir:       *claudeBaseDir,
		StartupWait:         durationSeconds(*startupWait),
		StartupReadyPattern: *startupReadyPattern,
		Command:             command,
	}, nil
}

func runGoPTYDriver(cfg ptyDriverConfig, stdout io.Writer, stderr io.Writer) error {
	return runGoPTYDriverContext(context.Background(), cfg, stdout, stderr)
}

func runGoPTYDriverContext(ctx context.Context, cfg ptyDriverConfig, stdout io.Writer, stderr io.Writer) (runErr error) {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("Claude PTY canceled before start: %w", err)
	}
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux is required for Claude PTY transport: %w", err)
	}
	tmux := newTmuxClient(tmuxPath)
	if err := tmux.cleanupStaleSessions(ctx); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Claude PTY canceled while cleaning stale tmux sessions: %w", ctx.Err())
		}
		emitJSON(stdout, map[string]any{"event": "warning", "message": err.Error()})
	}
	sessionName := fmt.Sprintf("ai-dispatch-claude-%d", time.Now().UnixNano())
	started := time.Now()
	startLine := 0
	sessionPath := ""
	locator := newClaudeSessionLocator(cfg.ClaudeBaseDir, cfg.CWD, cfg.SessionID, started)
	if cfg.SessionID != "" {
		sessionPath = findClaudeSessionFile(cfg.ClaudeBaseDir, cfg.CWD, cfg.SessionID, started.Add(-2*time.Minute))
		if sessionPath != "" {
			startLine = sessionLineCount(sessionPath, stdout)
		}
	}
	sessionEmitted := false
	if cfg.SessionID != "" {
		emitJSON(stdout, map[string]any{"event": "session_start", "session_id": cfg.SessionID})
		sessionEmitted = true
	}
	defer func() {
		if err := tmux.cleanupSession(sessionName); err != nil {
			cleanupErr := fmt.Errorf("Claude tmux session cleanup failed: %w", err)
			emitJSON(stdout, map[string]any{"event": "warning", "message": cleanupErr.Error()})
			if runErr == nil {
				runErr = cleanupErr
			} else {
				runErr = errors.Join(runErr, cleanupErr)
			}
		}
	}()
	if err := tmux.start(ctx, sessionName, cfg.CWD, cfg.Command); err != nil {
		return err
	}
	run := claudePTYExecution{
		tmux:           tmux,
		cfg:            cfg,
		sessionName:    sessionName,
		sessionPath:    sessionPath,
		startLine:      startLine,
		started:        started,
		sessionEmitted: sessionEmitted,
		locator:        locator,
		stdout:         stdout,
		stderr:         stderr,
	}
	return run.execute(ctx)
}

func (run *claudePTYExecution) execute(ctx context.Context) error {
	ready, err := run.tmux.waitForReady(ctx, run.sessionName, run.cfg.StartupReadyPattern, run.cfg.StartupWait)
	if err != nil {
		return fmt.Errorf("Claude tmux startup readiness failed: %w", err)
	}
	if !ready {
		emitJSON(run.stdout, map[string]any{"event": "warning", "message": "Claude tmux startup ready pattern not observed before input"})
	}
	if err := sleepContext(ctx, 600*time.Millisecond); err != nil {
		return fmt.Errorf("Claude PTY canceled before input: %w", err)
	}
	if run.sessionPath == "" && run.locator != nil {
		run.sessionPath = run.locator.find()
	}
	if run.sessionPath != "" {
		run.startLine = sessionLineCount(run.sessionPath, run.stdout)
	}
	if err := run.tmux.pasteInput(ctx, run.sessionName, run.cfg.Input); err != nil {
		return err
	}
	inputSubmittedAt := time.Now()
	if err := sleepContext(ctx, 800*time.Millisecond); err != nil {
		return fmt.Errorf("Claude PTY canceled after input: %w", err)
	}
	pane, err := run.tmux.capture(ctx, run.sessionName)
	if err != nil {
		return fmt.Errorf("Claude tmux input verification failed: %w", err)
	}
	if !paneContainsInput(pane, run.cfg.Input) && extractPaneAssistantText(pane, run.cfg.Input) == "" {
		if err := run.tmux.pasteInput(ctx, run.sessionName, run.cfg.Input); err != nil {
			return fmt.Errorf("Claude tmux input retry failed: %w", err)
		}
	}
	return run.poll(ctx, inputSubmittedAt)
}

func (run *claudePTYExecution) poll(ctx context.Context, inputSubmittedAt time.Time) error {
	deadline := time.Time{}
	if run.cfg.Timeout > 0 {
		deadline = time.Now().Add(run.cfg.Timeout)
	}
	last := observedSession{sessionID: run.cfg.SessionID}
	assistantEmitted := ""
	lastScanError := ""
	stableSince := time.Time{}
	lastSignature := ""
	for deadline.IsZero() || time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("Claude PTY canceled: %w", err)
		}
		if run.sessionPath == "" {
			if run.locator != nil {
				run.sessionPath = run.locator.find()
			} else {
				run.sessionPath = findClaudeSessionFile(run.cfg.ClaudeBaseDir, run.cfg.CWD, "", inputSubmittedAt.Add(-500*time.Millisecond))
			}
			if run.sessionPath != "" {
				if run.cfg.SessionID != "" {
					run.startLine = min(run.startLine, sessionLineCount(run.sessionPath, run.stdout))
				} else {
					run.startLine = 0
				}
			}
		}
		if run.sessionPath != "" {
			obs := parseObservedSession(run.sessionPath, run.startLine)
			if obs.scanError != "" && obs.scanError != lastScanError {
				emitJSON(run.stdout, map[string]any{"event": "warning", "message": obs.scanError})
				lastScanError = obs.scanError
			}
			if obs.sessionID != "" && !run.sessionEmitted {
				emitJSON(run.stdout, map[string]any{"event": "session_start", "session_file": run.sessionPath, "session_id": obs.sessionID})
				run.sessionEmitted = true
			}
			if obs.assistantText != "" && obs.assistantText != assistantEmitted {
				emitJSON(run.stdout, map[string]any{"event": "assistant_text", "text": obs.assistantText})
				assistantEmitted = obs.assistantText
			}
			last = obs
		}
		pane, err := run.tmux.capture(ctx, run.sessionName)
		if err != nil {
			return fmt.Errorf("Claude tmux capture failed: %w", err)
		}
		if last.assistantText == "" {
			if paneText := extractPaneAssistantText(pane, run.cfg.Input); paneText != "" {
				last.assistantText = paneText
			}
		}
		if strings.Contains(pane, "Interrupted") || strings.Contains(pane, "interrupt") && strings.Contains(pane, "continue") {
			emitDone(run.stdout, "interrupted_prompt", run.started, last)
			return nil
		}
		signature := last.assistantText + "|" + last.lastStopReason + "|" + fmt.Sprint(last.assistantMessageCount, last.toolCallCount)
		if last.assistantText != "" && paneLooksReady(pane) {
			if signature != lastSignature {
				lastSignature = signature
				stableSince = time.Now()
			} else if time.Since(stableSince) >= 1200*time.Millisecond {
				emitDone(run.stdout, "done", run.started, last)
				return nil
			}
		}
		if err := sleepContext(ctx, 200*time.Millisecond); err != nil {
			return fmt.Errorf("Claude PTY canceled: %w", err)
		}
	}
	pane, captureErr := run.tmux.capture(ctx, run.sessionName)
	if captureErr != nil {
		emitJSON(run.stdout, map[string]any{"event": "warning", "message": fmt.Sprintf("Claude final tmux capture failed: %v", captureErr)})
	}
	emitDoneWithTail(run.stdout, "hard_timeout", run.started, last, pane)
	fmt.Fprintln(run.stderr, "Claude PTY timed out")
	if captureErr != nil {
		return fmt.Errorf("Claude final tmux capture failed: %w", captureErr)
	}
	return nil
}

func sleepContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func paneLooksReady(pane string) bool {
	trimmed := strings.TrimSpace(pane)
	return strings.Contains(trimmed, "\u276f") || strings.Contains(trimmed, "? for shortcuts")
}

func extractPaneAssistantText(pane string, input string) string {
	if input != "" {
		needle := inputNeedle(input)
		if needle == "" {
			return ""
		}
		normalizedPane := strings.Join(strings.Fields(pane), " ")
		idx := strings.LastIndex(normalizedPane, needle)
		if idx < 0 {
			return ""
		}
		after := normalizedPane[idx+len(needle):]
		marker := strings.LastIndex(after, "⏺")
		if marker < 0 {
			return ""
		}
		text := strings.TrimSpace(after[marker+len("⏺"):])
		for _, sep := range []string{" ✻ ", " Thought for ", " Crunched for ", " ─", " ⏵", " ❯ "} {
			if cut := strings.Index(text, sep); cut >= 0 {
				text = strings.TrimSpace(text[:cut])
			}
		}
		if text != "" && !isPlaceholderText(text) && !isPaneProgressText(text) {
			return text
		}
		return ""
	}
	lines := strings.Split(pane, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.HasPrefix(line, "⏺") {
			text := strings.TrimSpace(strings.TrimPrefix(line, "⏺"))
			if text != "" && !isPlaceholderText(text) && !isPaneProgressText(text) {
				return text
			}
		}
	}
	return ""
}

func isPaneProgressText(text string) bool {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if strings.Contains(normalized, "ctrl+o") || strings.Contains(normalized, "reading ") {
		return true
	}
	return strings.HasSuffix(normalized, "…") || strings.HasSuffix(normalized, "...")
}

func paneContainsInput(pane string, input string) bool {
	needle := inputNeedle(input)
	if needle == "" {
		return false
	}
	return strings.Contains(strings.Join(strings.Fields(pane), " "), needle)
}

func inputNeedle(input string) string {
	input = strings.Join(strings.Fields(input), " ")
	if input == "" {
		return ""
	}
	runes := []rune(input)
	if len(runes) > 80 {
		return string(runes[:80])
	}
	return input
}

func findClaudeSessionFile(baseDir string, cwd string, sessionID string, threshold time.Time) string {
	projectsDir := filepath.Join(baseDir, "projects")
	if sessionID != "" {
		if direct := findClaudeSessionFileInCWD(baseDir, cwd, sessionID); direct != "" {
			return direct
		}
		var direct string
		_ = filepath.WalkDir(projectsDir, func(path string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() && filepath.Base(path) == sessionID+".jsonl" {
				direct = path
				return filepath.SkipAll
			}
			return nil
		})
		if direct != "" {
			return direct
		}
		return ""
	}
	searchRoot := projectsDir
	if cwd != "" {
		projectDir := filepath.Join(projectsDir, strings.ReplaceAll(cwd, "/", "-"))
		if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
			searchRoot = projectDir
		} else {
			return ""
		}
	}
	var best string
	var bestMod time.Time
	_ = filepath.WalkDir(searchRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil || info.ModTime().Before(threshold) {
			return nil
		}
		if info.ModTime().After(bestMod) {
			best = path
			bestMod = info.ModTime()
		}
		return nil
	})
	return best
}

func findClaudeSessionFileInCWD(baseDir string, cwd string, sessionID string) string {
	if cwd == "" || sessionID == "" {
		return ""
	}
	path := filepath.Join(baseDir, "projects", strings.ReplaceAll(cwd, "/", "-"), sessionID+".jsonl")
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		return path
	}
	return ""
}

func newClaudeSessionLocator(baseDir string, cwd string, sessionID string, lastFallback time.Time) *claudeSessionLocator {
	if sessionID == "" {
		return nil
	}
	return &claudeSessionLocator{
		baseDir:          baseDir,
		cwd:              cwd,
		sessionID:        sessionID,
		fallbackInterval: claudeSessionFallbackInterval,
		lastFallback:     lastFallback,
	}
}

func (locator *claudeSessionLocator) find() string {
	if direct := findClaudeSessionFileInCWD(locator.baseDir, locator.cwd, locator.sessionID); direct != "" {
		return direct
	}
	now := time.Now()
	if now.Sub(locator.lastFallback) < locator.fallbackInterval {
		return ""
	}
	locator.lastFallback = now
	return findClaudeSessionFile(locator.baseDir, "", locator.sessionID, time.Time{})
}

func parseObservedSession(path string, startLine int) observedSession {
	file, err := os.Open(path)
	if err != nil {
		return observedSession{}
	}
	defer file.Close()
	obs := observedSession{path: path, sessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), claudeSessionJSONLTokenLimit)
	lineNo := 0
	for scanner.Scan() {
		if lineNo < startLine {
			lineNo++
			continue
		}
		lineNo++
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if sid, ok := entry["sessionId"].(string); ok && sid != "" {
			obs.sessionID = sid
		}
		message, ok := entry["message"].(map[string]any)
		if !ok || entry["type"] != "assistant" {
			continue
		}
		if stop, ok := message["stop_reason"].(string); ok {
			obs.lastStopReason = stop
		}
		content, ok := message["content"].([]any)
		if !ok {
			continue
		}
		textParts := []string{}
		for _, raw := range content {
			block, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch block["type"] {
			case "text":
				if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
					if isPlaceholderText(text) {
						obs.sawPlaceholder = true
					} else {
						textParts = append(textParts, text)
					}
				}
			case "tool_use":
				obs.toolCallCount++
			}
		}
		if len(textParts) > 0 {
			obs.assistantText = strings.TrimSpace(strings.Join(textParts, "\n"))
			obs.assistantMessageCount++
		}
	}
	if err := scanner.Err(); err != nil {
		obs.scanError = fmt.Sprintf("Claude session JSONL scan failed within the %d-byte token limit", claudeSessionJSONLTokenLimit)
	}
	return obs
}

func sessionLineCount(path string, stdout io.Writer) int {
	count, err := countLines(path)
	if err == nil {
		return count
	}
	emitJSON(stdout, map[string]any{"event": "warning", "message": "Claude session JSONL line count failed"})
	return int(^uint(0) >> 1)
}

func countLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	buffer := make([]byte, 64<<10)
	count := 0
	var total int64
	last := byte('\n')
	for {
		n, readErr := file.Read(buffer)
		if n > 0 {
			count += bytes.Count(buffer[:n], []byte{'\n'})
			total += int64(n)
			last = buffer[n-1]
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return 0, readErr
		}
	}
	if total > 0 && last != '\n' {
		count++
	}
	return count, nil
}

func emitDone(stdout io.Writer, reason string, started time.Time, obs observedSession) {
	emitDoneWithTail(stdout, reason, started, obs, "")
}

func emitDoneWithTail(stdout io.Writer, reason string, started time.Time, obs observedSession, paneTail string) {
	summary := claudeSessionSummary{
		SessionID:              obs.sessionID,
		FilePath:               obs.path,
		AssistantText:          obs.assistantText,
		LastStopReason:         obs.lastStopReason,
		SawPlaceholderResponse: obs.sawPlaceholder,
	}
	emitJSON(stdout, map[string]any{
		"event":              "done",
		"session_id":         obs.sessionID,
		"duration_ms":        time.Since(started).Milliseconds(),
		"termination_reason": reason,
		"response_text":      obs.assistantText,
		"claude_session":     summary,
		"pane_tail":          trimPaneTail(paneTail),
	})
}

func emitJSON(stdout io.Writer, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = stdout.Write(append(data, '\n'))
}

func durationSeconds(value float64) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value * float64(time.Second))
}

func trimPaneTail(value string) string {
	value = strings.TrimSpace(value)
	if len([]rune(value)) <= 1200 {
		return value
	}
	runes := []rune(value)
	return string(runes[len(runes)-1200:])
}

func min(a int, b int) int {
	if a < b {
		return a
	}
	return b
}
