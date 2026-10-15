package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

const (
	surfaceCLI = "cli"
	surfaceApp = "app"
)

// appEnvelope is the stdout contract between the Desktop channel and Parse.
// It is not a Codex CLI stream event.
type appEnvelope struct {
	Surface   string `json:"surface"`
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

func (Provider) RunDirect(ctx context.Context, req providers.BuildRequest) (runtime.RunResult, bool, error) {
	if normalizeSurface(req.Surface) != surfaceApp {
		return runtime.RunResult{}, false, nil
	}
	started := time.Now()
	text, sessionID, err := runAppSurface(ctx, req)
	result := runtime.RunResult{DurationMS: time.Since(started).Milliseconds(), CleanupComplete: true}
	raw, marshalErr := json.Marshal(appEnvelope{Surface: surfaceApp, SessionID: sessionID, Text: text})
	if marshalErr == nil {
		result.Stdout = append(raw, '\n')
	}
	if err != nil {
		result.ExitCode = 1
		result.Error = err.Error()
		result.Stderr = []byte(err.Error())
		return result, true, nil
	}
	if marshalErr != nil {
		return runtime.RunResult{}, true, marshalErr
	}
	return result, true, nil
}

func runAppSurface(ctx context.Context, req providers.BuildRequest) (string, string, error) {
	prompt, err := promptText(req)
	if err != nil {
		return "", "", err
	}
	if req.SessionID == "" {
		return startDesktopThread(ctx, req, prompt)
	}
	return continueDesktopThread(ctx, req, prompt)
}

func startDesktopThread(ctx context.Context, req providers.BuildRequest, prompt string) (string, string, error) {
	client, err := startAppServer(ctx)
	if err != nil {
		return "", "", err
	}
	defer client.Close()
	if err := client.initialize(ctx); err != nil {
		return "", "", err
	}
	threadID, err := client.startThread(ctx, req)
	if err != nil {
		return "", "", err
	}
	text, err := client.startTurn(ctx, req, threadID, prompt)
	if err != nil {
		return "", threadID, err
	}
	focusDesktopThread(threadID)
	return text, threadID, nil
}

func continueDesktopThread(ctx context.Context, req providers.BuildRequest, prompt string) (string, string, error) {
	rollout, err := findRollout(req.SessionID)
	if err != nil {
		return "", req.SessionID, err
	}
	before, err := os.ReadFile(rollout)
	if err != nil && !os.IsNotExist(err) {
		return "", req.SessionID, fmt.Errorf("read codex rollout: %w", err)
	}
	if err := queueDesktopMessage(ctx, req, prompt); err != nil {
		return "", req.SessionID, err
	}
	focusDesktopThread(req.SessionID)
	text, err := waitForQueuedReply(ctx, rollout, before, prompt)
	if err != nil {
		return "", req.SessionID, err
	}
	return text, req.SessionID, nil
}

func queueDesktopMessage(ctx context.Context, req providers.BuildRequest, prompt string) error {
	args := []string{"codex", "queue", "--thread", req.SessionID, "--message", prompt}
	if cwd := strings.TrimSpace(req.CWD); cwd != "" {
		args = append(args, "-C", cwd)
	}
	if model := strings.TrimSpace(req.Target.Model); model != "" {
		args = append(args, "--model", model)
	}
	args = append(args, sharedConfigArgs(req)...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("codex queue: %s", msg)
	}
	return nil
}

func sharedConfigArgs(req providers.BuildRequest) []string {
	var args []string
	if req.Effort != "" && req.Effort != contract.EffortAuto {
		args = append(args, "-c", fmt.Sprintf(`model_reasoning_effort="%s"`, req.Effort))
	}
	if req.Fast {
		args = append(args, "-c", `service_tier="priority"`)
	}
	return args
}

type appServer struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	nextID int
}

func startAppServer(ctx context.Context) (*appServer, error) {
	cmd := exec.CommandContext(ctx, "codex", "app-server")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	return &appServer{cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), nextID: 1}, nil
}

func (s *appServer) Close() {
	_ = s.stdin.Close()
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
	}
}

func (s *appServer) initialize(ctx context.Context) error {
	_, err := s.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "Codex Desktop", "version": "ai-dispatch"},
		"capabilities": map[string]any{
			"experimentalApi": true,
		},
	})
	if err != nil {
		return err
	}
	return s.notify("initialized", map[string]any{})
}

func (s *appServer) startThread(ctx context.Context, req providers.BuildRequest) (string, error) {
	params := map[string]any{
		"threadSource": "vscode",
		"historyMode":  "legacy",
	}
	if cwd := strings.TrimSpace(req.CWD); cwd != "" {
		params["cwd"] = cwd
	}
	if model := strings.TrimSpace(req.Target.Model); model != "" {
		params["model"] = model
	}
	if req.Fast {
		params["serviceTier"] = "priority"
	}
	raw, err := s.call(ctx, "thread/start", params)
	if err != nil {
		return "", err
	}
	var parsed struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("parse thread/start: %w", err)
	}
	if parsed.Thread.ID == "" {
		return "", fmt.Errorf("thread/start returned no thread id")
	}
	return parsed.Thread.ID, nil
}

func (s *appServer) startTurn(ctx context.Context, req providers.BuildRequest, threadID string, prompt string) (string, error) {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]string{{
			"type": "text",
			"text": prompt,
		}},
	}
	if cwd := strings.TrimSpace(req.CWD); cwd != "" {
		params["cwd"] = cwd
	}
	if model := strings.TrimSpace(req.Target.Model); model != "" {
		params["model"] = model
	}
	if req.Effort != "" && req.Effort != contract.EffortAuto {
		params["effort"] = string(req.Effort)
	}
	if req.Fast {
		params["serviceTier"] = "priority"
	}
	if _, err := s.call(ctx, "turn/start", params); err != nil {
		return "", err
	}
	return s.waitTurn(ctx)
}

func (s *appServer) waitTurn(ctx context.Context) (string, error) {
	var text strings.Builder
	for {
		msg, err := s.read(ctx)
		if err != nil {
			return text.String(), err
		}
		if len(msg.ID) > 0 && msg.Method != "" {
			return text.String(), fmt.Errorf("codex app surface is waiting on %s; approve it in Codex Desktop or retry", msg.Method)
		}
		switch msg.Method {
		case "item/agentMessage/delta":
			var params struct {
				Delta string `json:"delta"`
			}
			_ = json.Unmarshal(msg.Params, &params)
			text.WriteString(params.Delta)
		case "turn/completed":
			return turnResult(msg.Params, text.String())
		}
	}
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (s *appServer) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := s.nextID
	s.nextID++
	if err := s.write(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		msg, err := s.read(ctx)
		if err != nil {
			return nil, err
		}
		if !idMatches(msg.ID, id) {
			continue
		}
		if msg.Error != nil {
			return nil, fmt.Errorf("%s: %s", method, msg.Error.Message)
		}
		return msg.Result, nil
	}
}

func (s *appServer) notify(method string, params any) error {
	return s.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (s *appServer) write(payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = s.stdin.Write(raw)
	return err
}

func (s *appServer) read(ctx context.Context) (rpcMessage, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := s.stdout.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case <-ctx.Done():
		return rpcMessage{}, ctx.Err()
	case got := <-ch:
		if got.err != nil {
			return rpcMessage{}, got.err
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(got.line), &msg); err != nil {
			return rpcMessage{}, fmt.Errorf("parse app-server message: %w", err)
		}
		return msg, nil
	}
}

func idMatches(raw json.RawMessage, want int) bool {
	if len(raw) == 0 {
		return false
	}
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		return n == want
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s == fmt.Sprint(want)
	}
	return false
}

func promptText(req providers.BuildRequest) (string, error) {
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return "", fmt.Errorf("cannot read prompt file for codex: %w", err)
		}
		return string(data), nil
	}
	return req.Prompt, nil
}

func focusDesktopThread(threadID string) {
	if threadID == "" {
		return
	}
	cmd := exec.Command("open", "-a", "ChatGPT", "codex://threads/"+threadID)
	_ = cmd.Run()
}

func findRollout(threadID string) (string, error) {
	root, err := codexSessionsDir()
	if err != nil {
		return "", err
	}
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		if strings.Contains(entry.Name(), threadID) && strings.HasSuffix(entry.Name(), ".jsonl") {
			found = path
			return fmt.Errorf("stop")
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("codex desktop thread %s has no rollout yet; create it with send --surface app first", threadID)
	}
	return found, nil
}

func codexSessionsDir() (string, error) {
	if home := strings.TrimSpace(os.Getenv("CODEX_HOME")); home != "" {
		return filepath.Join(home, "sessions"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "sessions"), nil
}

func waitForQueuedReply(ctx context.Context, rollout string, before []byte, prompt string) (string, error) {
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		data, err := os.ReadFile(rollout)
		if err != nil {
			return "", fmt.Errorf("read codex rollout: %w", err)
		}
		text, ready, err := queuedReply(before, data, prompt)
		if err != nil || ready {
			return text, err
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func queuedReply(before, data []byte, prompt string) (string, bool, error) {
	known := map[string]bool{}
	for _, msg := range userTurns(before) {
		if msg.messageID != "" {
			known[msg.messageID] = true
		}
	}
	var matches []userTurn
	for _, msg := range userTurns(data) {
		if msg.messageID == "" || known[msg.messageID] || msg.text != prompt {
			continue
		}
		matches = append(matches, msg)
	}
	if len(matches) == 0 {
		return "", false, nil
	}
	if len(matches) > 1 {
		return "", false, fmt.Errorf("codex desktop queued prompt matched %d new turns", len(matches))
	}
	match := matches[0]
	if match.turnID == "" {
		return "", false, nil
	}
	end, ok := terminalByID(data, match.turnID)
	if !ok {
		return "", false, nil
	}
	if end.failed {
		return "", false, fmt.Errorf("codex desktop turn %s: %s", end.status, end.errText)
	}
	if strings.TrimSpace(end.text) == "" {
		return "", false, fmt.Errorf("codex desktop turn completed without an assistant message")
	}
	return end.text, true, nil
}

type userTurn struct {
	messageID string
	turnID    string
	text      string
}

func userTurns(data []byte) []userTurn {
	var turns []userTurn
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Payload struct {
				Type    string          `json:"type"`
				ID      string          `json:"id"`
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
				Meta    struct {
					TurnID string `json:"turn_id"`
				} `json:"internal_chat_message_metadata_passthrough"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Payload.Type != "message" || event.Payload.Role != "user" {
			continue
		}
		turns = append(turns, userTurn{
			messageID: event.Payload.ID,
			turnID:    event.Payload.Meta.TurnID,
			text:      messageText(event.Payload.Content),
		})
	}
	return turns
}

func messageText(content json.RawMessage) string {
	var parts []struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(content, &parts); err == nil && len(parts) > 0 {
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(part.Text)
		}
		return b.String()
	}
	var text string
	if err := json.Unmarshal(content, &text); err == nil {
		return text
	}
	return ""
}

func terminalByID(data []byte, turnID string) (turnEnd, bool) {
	var found turnEnd
	ok := false
	for _, end := range terminalTurns(data) {
		if end.id == turnID {
			found = end
			ok = true
		}
	}
	return found, ok
}

type turnEnd struct {
	id      string
	status  string
	text    string
	errText string
	failed  bool
}

func turnResult(params json.RawMessage, streamed string) (string, error) {
	var note struct {
		Turn struct {
			Status string `json:"status"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		} `json:"turn"`
	}
	_ = json.Unmarshal(params, &note)
	switch note.Turn.Status {
	case "failed", "interrupted":
		msg := note.Turn.Status
		if note.Turn.Error != nil && note.Turn.Error.Message != "" {
			msg = note.Turn.Error.Message
		}
		return "", fmt.Errorf("codex app turn %s: %s", note.Turn.Status, msg)
	case "completed", "":
		if strings.TrimSpace(streamed) == "" {
			return "", fmt.Errorf("codex app turn completed without an assistant message")
		}
		return streamed, nil
	default:
		return "", fmt.Errorf("codex app turn ended with status %s", note.Turn.Status)
	}
}

func turnEndedAfter(before, data []byte) (turnEnd, bool) {
	seen := map[string]bool{}
	for _, end := range terminalTurns(before) {
		if end.id != "" {
			seen[end.id] = true
		}
	}
	var fresh []turnEnd
	for _, end := range terminalTurns(data) {
		if end.id == "" || seen[end.id] {
			continue
		}
		fresh = append(fresh, end)
	}
	if len(fresh) == 0 {
		return turnEnd{}, false
	}
	return fresh[len(fresh)-1], true
}

func terminalTurns(data []byte) []turnEnd {
	var ends []turnEnd
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Payload struct {
				Type             string `json:"type"`
				Status           string `json:"status"`
				TurnID           string `json:"turn_id"`
				LastAgentMessage string `json:"last_agent_message"`
				Message          string `json:"message"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		kind := event.Payload.Type
		if kind == "" {
			kind = event.Payload.Status
		}
		switch kind {
		case "task_complete":
			ends = append(ends, turnEnd{id: event.Payload.TurnID, status: "completed", text: event.Payload.LastAgentMessage})
		case "task_failed", "turn_failed", "error":
			ends = append(ends, turnEnd{id: event.Payload.TurnID, status: "failed", failed: true, errText: firstNonEmpty(event.Payload.Message, "failed")})
		case "task_interrupted", "turn_interrupted":
			ends = append(ends, turnEnd{id: event.Payload.TurnID, status: "interrupted", failed: true, errText: firstNonEmpty(event.Payload.Message, "interrupted")})
		}
	}
	return ends
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func normalizeSurface(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", surfaceCLI:
		return surfaceCLI
	case surfaceApp:
		return surfaceApp
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}
