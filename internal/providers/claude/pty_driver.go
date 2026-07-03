package claude

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
}

func RunPTYDriverCLI(argv []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := parsePTYDriverArgs(argv, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := runGoPTYDriver(cfg, stdout, stderr); err != nil {
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
	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux is required for Claude PTY transport: %w", err)
	}
	cleanupStaleClaudePTYSession(tmuxPath)
	sessionName := fmt.Sprintf("ai-dispatch-claude-%d", time.Now().UnixNano())
	started := time.Now()
	startLine := 0
	if cfg.SessionID != "" {
		if path := findClaudeSessionFile(cfg.ClaudeBaseDir, cfg.CWD, cfg.SessionID, started.Add(-2*time.Minute)); path != "" {
			startLine = countLines(path)
		}
	}
	if err := tmuxStart(tmuxPath, sessionName, cfg.CWD, cfg.Command); err != nil {
		return err
	}
	defer func() { _ = exec.Command(tmuxPath, "kill-session", "-t", sessionName).Run() }()
	if !waitForReady(tmuxPath, sessionName, cfg.StartupReadyPattern, cfg.StartupWait) {
		emitJSON(stdout, map[string]any{"event": "warning", "message": "Claude tmux startup ready pattern not observed before input"})
	}
	time.Sleep(600 * time.Millisecond)
	sessionPath := ""
	if cfg.SessionID != "" {
		sessionPath = findClaudeSessionFile(cfg.ClaudeBaseDir, cfg.CWD, cfg.SessionID, started.Add(-5*time.Second))
		if sessionPath != "" {
			startLine = countLines(sessionPath)
		}
	}
	if err := tmuxPasteInput(tmuxPath, sessionName, cfg.Input); err != nil {
		return err
	}
	inputSubmittedAt := time.Now()
	time.Sleep(800 * time.Millisecond)
	if pane := tmuxCapture(tmuxPath, sessionName); !paneContainsInput(pane, cfg.Input) && extractPaneAssistantText(pane, cfg.Input) == "" {
		_ = tmuxPasteInput(tmuxPath, sessionName, cfg.Input)
	}
	deadline := time.Now().Add(cfg.Timeout)
	var last observedSession
	sessionEmitted := false
	assistantEmitted := ""
	stableSince := time.Time{}
	lastSignature := ""
	for time.Now().Before(deadline) {
		if sessionPath == "" {
			sessionPath = findClaudeSessionFile(cfg.ClaudeBaseDir, cfg.CWD, cfg.SessionID, inputSubmittedAt.Add(-500*time.Millisecond))
			if sessionPath != "" {
				if cfg.SessionID != "" {
					startLine = min(startLine, countLines(sessionPath))
				} else {
					startLine = 0
				}
			}
		}
		if sessionPath != "" {
			obs := parseObservedSession(sessionPath, startLine)
			if obs.sessionID != "" && !sessionEmitted {
				emitJSON(stdout, map[string]any{"event": "session_start", "session_file": sessionPath, "session_id": obs.sessionID})
				sessionEmitted = true
			}
			if obs.assistantText != "" && obs.assistantText != assistantEmitted {
				emitJSON(stdout, map[string]any{"event": "assistant_text", "text": obs.assistantText})
				assistantEmitted = obs.assistantText
			}
			last = obs
		}
		pane := tmuxCapture(tmuxPath, sessionName)
		if last.assistantText == "" {
			if paneText := extractPaneAssistantText(pane, cfg.Input); paneText != "" {
				last.assistantText = paneText
			}
		}
		if strings.Contains(pane, "Interrupted") || strings.Contains(pane, "interrupt") && strings.Contains(pane, "continue") {
			emitDone(stdout, "interrupted_prompt", started, last)
			return nil
		}
		signature := last.assistantText + "|" + last.lastStopReason + "|" + fmt.Sprint(last.assistantMessageCount, last.toolCallCount)
		if last.assistantText != "" && paneLooksReady(pane) {
			if signature != lastSignature {
				lastSignature = signature
				stableSince = time.Now()
			} else if time.Since(stableSince) >= 1200*time.Millisecond {
				emitDone(stdout, "done", started, last)
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	emitDoneWithTail(stdout, "hard_timeout", started, last, tmuxCapture(tmuxPath, sessionName))
	fmt.Fprintln(stderr, "Claude PTY timed out")
	return nil
}

func cleanupStaleClaudePTYSession(tmuxPath string) {
	ttl := claudePTYSessionTTL()
	if ttl <= 0 {
		return
	}
	out, err := exec.Command(tmuxPath, "list-sessions", "-F", "#{session_name}\t#{session_created}").Output()
	if err != nil {
		return
	}
	cutoff := time.Now().Add(-ttl).Unix()
	for _, line := range strings.Split(string(out), "\n") {
		name, createdRaw, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || !isClaudePTYSessionName(name) {
			continue
		}
		created, err := strconv.ParseInt(strings.TrimSpace(createdRaw), 10, 64)
		if err != nil || created > cutoff {
			continue
		}
		_ = exec.Command(tmuxPath, "kill-session", "-t", name).Run()
	}
}

func claudePTYSessionTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("AI_DISPATCH_CLAUDE_PTY_SESSION_TTL_SECONDS"))
	if raw == "" {
		return 6 * time.Hour
	}
	seconds, err := strconv.Atoi(raw)
	if err != nil || seconds < 0 {
		return 6 * time.Hour
	}
	return time.Duration(seconds) * time.Second
}

func isClaudePTYSessionName(name string) bool {
	return strings.HasPrefix(name, "ai-dispatch-claude-") || strings.HasPrefix(name, "claude-pty-")
}

func tmuxStart(tmuxPath string, sessionName string, cwd string, command []string) error {
	args := []string{"new-session", "-d", "-s", sessionName}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	args = append(args, command...)
	if out, err := exec.Command(tmuxPath, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux new-session failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func tmuxPasteInput(tmuxPath string, sessionName string, input string) error {
	target := sessionName + ":0.0"
	_ = exec.Command(tmuxPath, "send-keys", "-t", target, "C-u").Run()
	if !strings.Contains(input, "\n") && len([]rune(input)) <= 4000 {
		if out, err := exec.Command(tmuxPath, "send-keys", "-t", target, "-l", input).CombinedOutput(); err != nil {
			return fmt.Errorf("tmux send-keys literal failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		if out, err := exec.Command(tmuxPath, "send-keys", "-t", target, "Enter").CombinedOutput(); err != nil {
			return fmt.Errorf("tmux send-keys enter failed: %w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	tmp, err := os.CreateTemp("", "ai-dispatch-claude-input-*.txt")
	if err != nil {
		return err
	}
	path := tmp.Name()
	defer os.Remove(path)
	if _, err := tmp.WriteString(input); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	buffer := "ai-dispatch-" + fmt.Sprint(time.Now().UnixNano())
	if out, err := exec.Command(tmuxPath, "load-buffer", "-b", buffer, path).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux load-buffer failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.Command(tmuxPath, "paste-buffer", "-b", buffer, "-t", target).CombinedOutput(); err != nil {
		return fmt.Errorf("tmux paste-buffer failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command(tmuxPath, "delete-buffer", "-b", buffer).Run()
	if out, err := exec.Command(tmuxPath, "send-keys", "-t", target, "Enter").CombinedOutput(); err != nil {
		return fmt.Errorf("tmux send-keys failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func waitForReady(tmuxPath string, sessionName string, pattern string, timeout time.Duration) bool {
	if timeout <= 0 {
		return true
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pane := tmuxCapture(tmuxPath, sessionName)
		if pattern == "" || strings.Contains(pane, pattern) || paneLooksReady(pane) {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func tmuxCapture(tmuxPath string, sessionName string) string {
	out, err := exec.Command(tmuxPath, "capture-pane", "-p", "-t", sessionName, "-S", "-2000").Output()
	if err != nil {
		return ""
	}
	return string(out)
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

func parseObservedSession(path string, startLine int) observedSession {
	file, err := os.Open(path)
	if err != nil {
		return observedSession{}
	}
	defer file.Close()
	obs := observedSession{path: path, sessionID: strings.TrimSuffix(filepath.Base(path), ".jsonl")}
	scanner := bufio.NewScanner(file)
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
	return obs
}

func countLines(path string) int {
	file, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	count := 0
	for scanner.Scan() {
		count++
	}
	return count
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
