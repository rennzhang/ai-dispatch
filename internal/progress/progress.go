package progress

import (
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

type Emitter struct {
	Provider string
	Writer   io.Writer

	mu       sync.Mutex
	seen     map[string]bool
	lineFeed LineFeed
}

func NewEmitter(provider string, writer io.Writer) *Emitter {
	return &Emitter{
		Provider: provider,
		Writer:   writer,
		seen:     map[string]bool{},
	}
}

func (e *Emitter) Emit(kind contract.ProgressKind, name string, summary string) {
	if e == nil || e.Writer == nil {
		return
	}
	event := contract.NewProgress(kind, name, trimSummary(summary))
	event.Provider = e.Provider
	e.emit(event)
}

func (e *Emitter) Feed(chunk []byte) {
	if e == nil || e.Writer == nil {
		return
	}
	events := e.lineFeed.Feed(string(chunk), e.Provider)
	for _, event := range events {
		e.emit(event)
	}
}

func (e *Emitter) Close() {
	if e == nil || e.Writer == nil {
		return
	}
	for _, event := range e.lineFeed.Close(e.Provider) {
		e.emit(event)
	}
}

func (e *Emitter) emit(event contract.ProgressEvent) {
	key := string(event.Kind) + "\x00" + event.Name + "\x00" + event.Summary
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.seen[key] {
		return
	}
	e.seen[key] = true
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = e.Writer.Write(append(data, '\n'))
}

type LineFeed struct {
	pending string
}

func (l *LineFeed) Feed(chunk string, provider string) []contract.ProgressEvent {
	text := l.pending + chunk
	lines := strings.Split(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		l.pending = lines[len(lines)-1]
		lines = lines[:len(lines)-1]
	} else {
		l.pending = ""
	}
	events := []contract.ProgressEvent{}
	for _, line := range lines {
		events = append(events, eventsFromLine(line, provider)...)
	}
	return events
}

func (l *LineFeed) Close(provider string) []contract.ProgressEvent {
	if strings.TrimSpace(l.pending) == "" {
		return nil
	}
	line := l.pending
	l.pending = ""
	return eventsFromLine(line, provider)
}

func eventsFromLine(line string, provider string) []contract.ProgressEvent {
	line = strings.TrimSpace(line)
	if line == "" || !strings.HasPrefix(line, "{") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(line), &payload); err != nil {
		return nil
	}
	if progress, _ := payload["_progress"].(bool); progress {
		return []contract.ProgressEvent{eventFromProgressPayload(payload, provider)}
	}
	events := []contract.ProgressEvent{}
	if sessionID := firstString(payload, "session_id", "sessionId", "sessionID"); sessionID != "" {
		event := contract.NewProgress(contract.ProgressSession, "session", provider+" session "+sessionID)
		event.Provider = provider
		event.SessionID = sessionID
		events = append(events, event)
	}
	if kind, name, summary := toolProgress(payload); name != "" || summary != "" {
		event := contract.NewProgress(kind, name, summary)
		event.Provider = provider
		events = append(events, event)
	}
	if summary := textProgress(payload); summary != "" {
		event := contract.NewProgress(contract.ProgressAgentMessage, "agent_message", summary)
		event.Provider = provider
		events = append(events, event)
	}
	return events
}

func eventFromProgressPayload(payload map[string]any, provider string) contract.ProgressEvent {
	kind := contract.ProgressKind(firstString(payload, "kind"))
	if kind == "" {
		kind = progressKind(firstString(payload, "name"))
	}
	event := contract.NewProgress(kind, firstString(payload, "name"), firstString(payload, "summary"))
	event.Provider = firstNonEmpty(firstString(payload, "provider"), provider)
	event.SessionID = firstString(payload, "session_id")
	event.RunID = firstString(payload, "run_id")
	if nextAction := firstString(payload, "next_action"); nextAction != "" {
		event.NextAction = nextAction
	}
	if directive := firstString(payload, "directive"); directive != "" {
		event.Directive = directive
	}
	return event
}

func toolProgress(payload map[string]any) (contract.ProgressKind, string, string) {
	name := firstString(payload, "name", "tool")
	input := valueMap(payload["input"])
	if part := valueMap(payload["part"]); len(part) > 0 {
		name = firstNonEmpty(firstString(part, "tool", "name"), name)
		input = firstNonEmptyMap(valueMap(part["input"]), input)
	}
	if name == "" {
		return "", "", ""
	}
	return progressKind(name), name, toolSummary(name, input)
}

func textProgress(payload map[string]any) string {
	if text := firstString(payload, "text", "message", "result"); text != "" {
		return trimSummary(text)
	}
	if part := valueMap(payload["part"]); len(part) > 0 {
		return trimSummary(firstString(part, "text"))
	}
	if message := valueMap(payload["message"]); len(message) > 0 {
		if content, ok := message["content"].([]any); ok {
			for _, item := range content {
				block := valueMap(item)
				if text := firstString(block, "text"); text != "" {
					return trimSummary(text)
				}
			}
		}
	}
	return ""
}

func toolSummary(name string, input map[string]any) string {
	args := valueMap(input["args"])
	for _, key := range []string{"filePath", "file_path", "path", "cmd", "command", "query", "url"} {
		if value := firstString(input, key); value != "" {
			return trimSummary(value)
		}
		if value := firstString(args, key); value != "" {
			return trimSummary(value)
		}
	}
	return name
}

func progressKind(name string) contract.ProgressKind {
	normalized := strings.ToLower(name)
	switch {
	case strings.Contains(normalized, "read"):
		return contract.ProgressRead
	case strings.Contains(normalized, "edit"), strings.Contains(normalized, "write"):
		return contract.ProgressEdit
	case strings.Contains(normalized, "bash"), strings.Contains(normalized, "shell"), strings.Contains(normalized, "command"):
		return contract.ProgressBash
	case strings.Contains(normalized, "search"), strings.Contains(normalized, "grep"):
		return contract.ProgressSearch
	case strings.Contains(normalized, "web"), strings.Contains(normalized, "browser"):
		return contract.ProgressBrowse
	case strings.Contains(normalized, "fetch"):
		return contract.ProgressFetch
	default:
		return contract.ProgressTool
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func valueMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmptyMap(values ...map[string]any) map[string]any {
	for _, value := range values {
		if len(value) > 0 {
			return value
		}
	}
	return nil
}

func trimSummary(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len([]rune(value)) <= 160 {
		return value
	}
	runes := []rune(value)
	return string(runes[:160])
}
