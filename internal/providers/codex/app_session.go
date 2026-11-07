package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
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
	Surface   string   `json:"surface"`
	SessionID string   `json:"session_id"`
	Text      string   `json:"text"`
	State     string   `json:"state"`
	Error     string   `json:"error,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
}

func (Provider) RunDirect(ctx context.Context, req providers.BuildRequest) (runtime.RunResult, bool, error) {
	if normalizeSurface(req.Surface) != surfaceApp {
		return runtime.RunResult{}, false, nil
	}
	started := time.Now()
	text, sessionID, err := runAppSurface(ctx, req)
	result := runtime.RunResult{DurationMS: time.Since(started).Milliseconds(), CleanupComplete: true}
	state, reason := "completed", ""
	if err != nil {
		state, reason = "failed", err.Error()
	}
	var warnings []string
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		warnings = append(warnings, "Stopped waiting for Codex Desktop; its queued or running turn may still execute. Check the retained session in the same Desktop instance before retrying.")
	}
	raw, marshalErr := json.Marshal(appEnvelope{Surface: surfaceApp, SessionID: sessionID, Text: text, State: state, Error: reason, Warnings: warnings})
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
	prompt, err = uniqueDesktopPrompt(prompt)
	if err != nil {
		return "", req.SessionID, err
	}
	host, err := resolveDesktopHost(ctx)
	if err != nil {
		return "", req.SessionID, err
	}
	host, err = ensureDesktopRunning(ctx, host)
	if err != nil {
		return "", req.SessionID, err
	}
	if req.SessionID == "" {
		return startDesktopThread(ctx, host, req, prompt)
	}
	return continueDesktopThread(ctx, host, req, prompt)
}

func startDesktopThread(ctx context.Context, host desktopHost, req providers.BuildRequest, prompt string) (string, string, error) {
	threadID, err := createDesktopThread(ctx, host, req, prompt)
	if err != nil {
		return "", threadID, err
	}
	req.SessionID = threadID
	lease, err := acquireDesktopSession(ctx, host, threadID)
	if err != nil {
		return "", threadID, err
	}
	defer lease.Close()
	return deliverDesktopMessage(ctx, host, req, "", nil, prompt, lease)
}

// Naming persists the empty thread before its temporary backend releases it.
func createDesktopThread(ctx context.Context, host desktopHost, req providers.BuildRequest, prompt string) (string, error) {
	client, err := startAppServer(ctx, host)
	if err != nil {
		return "", err
	}
	defer client.Close()
	if err := client.initialize(ctx); err != nil {
		return "", err
	}
	threadID, err := client.startThread(ctx, req)
	if err != nil {
		return "", err
	}
	if _, err := client.call(ctx, "thread/name/set", map[string]any{"threadId": threadID, "name": desktopThreadTitle(prompt, threadID)}); err != nil {
		return threadID, err
	}
	return threadID, nil
}

func continueDesktopThread(ctx context.Context, host desktopHost, req providers.BuildRequest, prompt string) (string, string, error) {
	lease, err := acquireDesktopSession(ctx, host, req.SessionID)
	if err != nil {
		return "", req.SessionID, err
	}
	defer lease.Close()
	rollout, err := findRollout(req.SessionID)
	if errors.Is(err, errRolloutNotFound) {
		// A persisted empty thread may have been rejected before its first
		// queue/add. It can be resumed without inventing a prior turn.
		return deliverDesktopMessage(ctx, host, req, "", nil, prompt, lease)
	}
	if err != nil {
		return "", req.SessionID, err
	}
	before, err := os.ReadFile(rollout)
	if err != nil && !os.IsNotExist(err) {
		return "", req.SessionID, fmt.Errorf("read codex rollout: %w", err)
	}
	turnID, err := activeDesktopTurnInRollout(req.SessionID, rollout)
	if err != nil {
		return "", req.SessionID, fmt.Errorf("read codex rollout history: %w", err)
	}
	if turnID != "" {
		return "", req.SessionID, fmt.Errorf("codex desktop turn %s is still active (app_turn_active); wait or interrupt it in the same Desktop instance before resuming session %s", turnID, req.SessionID)
	}
	return deliverDesktopMessage(ctx, host, req, rollout, before, prompt, lease)
}

func deliverDesktopMessage(ctx context.Context, host desktopHost, req providers.BuildRequest, rollout string, before []byte, prompt string, lease *desktopLock) (string, string, error) {
	if err := prepareDesktopMessage(ctx, host, req, prompt, lease); err != nil {
		return "", req.SessionID, err
	}
	text, err := waitForQueuedReply(ctx, req, rollout, before, prompt)
	if err == nil {
		err = lease.setPending("")
	}
	return text, req.SessionID, err
}

func prepareDesktopMessage(ctx context.Context, host desktopHost, req providers.BuildRequest, prompt string, lease *desktopLock) error {
	lock, err := acquireDesktopLock(ctx, desktopLockKey(host, "prepare"), true)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := openDesktopThread(ctx, host, req.SessionID, false); err != nil {
		return err
	}
	if err := queueDesktopMessage(ctx, host, req, prompt, lease); err != nil {
		return err
	}
	// The wake route resumes queued work on the GUI's own app-server. It does
	// not navigate, so open the ordinary thread route as well.
	if err := openDesktopThread(ctx, host, req.SessionID, true); err != nil {
		return err
	}
	if err := openDesktopThread(ctx, host, req.SessionID, false); err != nil {
		return err
	}
	return nil
}

func queueDesktopMessage(ctx context.Context, host desktopHost, req providers.BuildRequest, prompt string, lease *desktopLock) error {
	client, err := startAppServer(ctx, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.initialize(ctx); err != nil {
		return err
	}
	raw, err := client.call(ctx, "thread/read", map[string]any{"threadId": req.SessionID, "includeTurns": false})
	if err != nil {
		return err
	}
	var info struct {
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &info); err != nil {
		return fmt.Errorf("read Desktop thread name: %w", err)
	}
	if info.Thread.Name == "" {
		return fmt.Errorf("the Desktop thread has no persisted name; no input was queued")
	}
	if err := configureDesktopThread(ctx, host, req, info.Thread.Name); err != nil {
		return err
	}
	if err := client.requireCold(ctx); err != nil {
		return err
	}
	messageID := desktopPromptID(prompt)
	if messageID == "" {
		messageID, err = newDesktopMessageID()
		if err != nil {
			return err
		}
	}
	if lease != nil {
		if err := lease.setPending(messageID); err != nil {
			return err
		}
	}
	if _, err := client.call(ctx, "thread/queue/add", map[string]any{
		"threadId": req.SessionID, "clientUserMessageId": messageID,
		"input": []map[string]any{{"type": "text", "text": prompt, "text_elements": []any{}}},
	}); err != nil {
		return err
	}
	return client.requireCold(ctx)
}

func (s *appServer) initialize(ctx context.Context) error {
	_, err := s.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "ai-dispatch", "version": "ai-dispatch"},
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
		"threadSource": "user",
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
	if req.Effort != "" && req.Effort != contract.EffortAuto {
		params["config"] = map[string]any{"model_reasoning_effort": string(req.Effort)}
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
	return s.encoder.Encode(payload)
}

func (s *appServer) read(ctx context.Context) (rpcMessage, error) {
	if err := ctx.Err(); err != nil {
		return rpcMessage{}, err
	}
	var message rpcMessage
	if err := s.decoder.Decode(&message); err != nil {
		if ctx.Err() != nil {
			return rpcMessage{}, ctx.Err()
		}
		return rpcMessage{}, fmt.Errorf("read temporary Desktop backend response: %w", err)
	}
	return message, nil
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

func openDesktopThread(ctx context.Context, host desktopHost, threadID string, wake bool) error {
	if threadID == "" {
		return fmt.Errorf("cannot open Codex Desktop without a thread id")
	}
	path := threadID
	if wake {
		path = "wake/" + threadID
	}
	return sendDesktopRoute(ctx, host, "codex://threads/"+path)
}

// appSurfacePickupLimit bounds how long a queued Desktop message may go without
// any rollout activity before we call it a lost delivery. App turns themselves
// are not wall-clock capped and, once ours landed, keep waiting unless the user
// set --activity-timeout: a turn parked on a tool approval is legitimate.
const appSurfacePickupLimit = 180 * time.Second

func waitForQueuedReply(ctx context.Context, req providers.BuildRequest, rollout string, before []byte, prompt string) (string, error) {
	idle := appSurfacePickupLimit // --activity-timeout unset: pickup guard only
	if req.ActivityTimeoutSeconds > 0 {
		idle = time.Duration(req.ActivityTimeoutSeconds) * time.Second
	} else if req.ActivityTimeoutSeconds == 0 {
		idle = 0 // explicit --activity-timeout 0 turns the guard off
	}
	started := time.Now()
	lastChange := started
	lastSize := -1
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		var data []byte
		var err error
		if rollout == "" {
			rollout, err = findRollout(req.SessionID)
			if errors.Is(err, errRolloutNotFound) {
				err = nil // metadata-only threads materialize after GUI pickup
			}
		}
		if err == nil && rollout != "" {
			data, err = readActiveRollout(req.SessionID, rollout)
		}
		if err != nil {
			return "", fmt.Errorf("read codex rollout: %w", err)
		}
		if size := len(data); size != lastSize {
			if lastSize >= 0 {
				lastChange = time.Now()
			}
			lastSize = size
		}
		scan, err := queuedReply(before, data, prompt)
		if err != nil {
			return scan.text, err
		}
		if scan.ready {
			return scan.text, validateDesktopTurnSettings(data, scan.turnID, req)
		}
		if idle > 0 && time.Since(lastChange) > idle {
			if scan.landed && req.ActivityTimeoutSeconds <= 0 {
				idle = 0 // landed turn: keep waiting, no inactivity limit
			} else {
				return "", queuedStallError(scan, time.Since(started), idle)
			}
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func queuedStallError(scan queuedScan, waited, idle time.Duration) error {
	if !scan.landed {
		hint := ""
		if scan.other != "" {
			hint = fmt.Sprintf("; the thread received a different dispatch prompt instead (%s) — app_prompt_mismatch", scan.other)
		}
		return fmt.Errorf("codex desktop never picked up the queued message: no matching [ai-dispatch] turn in %s (app_not_picked_up)%s; open the thread in Codex Desktop, then retry or use --surface cli", waited.Round(time.Second), hint)
	}
	return fmt.Errorf("codex desktop turn stalled: rollout unchanged for %s while waiting %s (app_stalled)", idle, waited.Round(time.Second))
}

// queuedScan is what one rollout read tells us about the queued prompt.
type queuedScan struct {
	text   string // assistant text once the turn finished
	turnID string // only this prompt's turn can prove execution settings
	ready  bool   // our turn finished successfully
	landed bool   // a turn carrying our own stamped prompt showed up
	other  string // a different stamped prompt landed instead (diagnostic)
}

func queuedReply(before, data []byte, prompt string) (queuedScan, error) {
	known := map[string]bool{}
	for _, msg := range userTurns(before) {
		if msg.messageID != "" {
			known[msg.messageID] = true
		}
	}
	wantBody, wantMarked := dispatchPromptBody(prompt)
	var scan queuedScan
	var matches []userTurn
	for _, msg := range userTurns(data) {
		if msg.messageID == "" || known[msg.messageID] {
			continue
		}
		if wantMarked {
			body, ok := dispatchPromptBody(msg.text)
			if !ok {
				continue // only turns we stamped can be our reply
			}
			if body != wantBody {
				if scan.other == "" {
					scan.other = promptSnippet(msg.text)
				}
				continue
			}
		} else if msg.text != prompt {
			continue
		}
		scan.landed = true
		matches = append(matches, msg)
	}
	if len(matches) == 0 {
		return scan, nil
	}
	if len(matches) > 1 {
		return scan, fmt.Errorf("codex desktop queued prompt matched %d new turns", len(matches))
	}
	match := matches[0]
	if match.turnID == "" {
		return scan, nil
	}
	scan.turnID = match.turnID
	end, ok := terminalByID(data, match.turnID)
	if !ok {
		return scan, nil
	}
	scan.text = end.text
	if end.failed {
		return scan, fmt.Errorf("codex desktop turn %s: %s", end.status, end.errText)
	}
	if strings.TrimSpace(end.text) == "" {
		return scan, fmt.Errorf("codex desktop turn completed without an assistant message")
	}
	scan.text = end.text
	scan.ready = true
	return scan, nil
}

// dispatchPromptBody splits a stamped prompt. The marker is a hard requirement:
// only dispatcher-sent turns can match, and a missing marker means "not ours".
// Only the blank line after the marker and trailing whitespace are normalized;
// the body itself stays exact.
func dispatchPromptBody(text string) (string, bool) {
	if !strings.HasPrefix(text, contract.DispatchPromptMarker) {
		return "", false
	}
	rest := strings.TrimPrefix(text, contract.DispatchPromptMarker)
	rest = strings.TrimLeft(rest, "\r\n")
	return strings.TrimRight(rest, " \t\r\n"), true
}

func promptSnippet(text string) string {
	flat := strings.Join(strings.Fields(text), " ")
	runes := []rune(flat)
	if len(runes) <= 100 {
		return flat
	}
	return string(runes[:100]) + "…"
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
