package progress

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

const (
	maxProgressLineBytes  = 64 << 10
	maxProgressChunkBytes = 64 << 10
	maxSeenEventKeys      = 1024
)

const droppedProgressLineSummary = "progress line exceeded 65536 bytes and was dropped"

type Emitter struct {
	Provider string
	Writer   io.Writer

	mu sync.Mutex
	// seenOrder is a fixed-size FIFO dedup window. Old keys may be emitted
	// again after eviction, but unique provider events cannot grow memory.
	seen       map[[sha256.Size]byte]struct{}
	seenOrder  [][sha256.Size]byte
	seenNext   int
	stdoutMu   sync.Mutex
	stdoutFeed LineFeed
	stderrMu   sync.Mutex
	stderrFeed LineFeed
}

func NewEmitter(provider string, writer io.Writer) *Emitter {
	return &Emitter{
		Provider: provider,
		Writer:   writer,
		seen:     map[[sha256.Size]byte]struct{}{},
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
	e.FeedStdout(chunk)
}

func (e *Emitter) FeedStdout(chunk []byte) {
	if e == nil || e.Writer == nil {
		return
	}
	e.feedChunks(chunk, &e.stdoutMu, &e.stdoutFeed)
}

func (e *Emitter) FeedStderr(chunk []byte) {
	if e == nil || e.Writer == nil {
		return
	}
	e.feedChunks(chunk, &e.stderrMu, &e.stderrFeed)
}

func (e *Emitter) feedChunks(chunk []byte, streamMu *sync.Mutex, lineFeed *LineFeed) {
	streamMu.Lock()
	defer streamMu.Unlock()
	for len(chunk) > 0 {
		chunkSize := len(chunk)
		if chunkSize > maxProgressChunkBytes {
			chunkSize = maxProgressChunkBytes
		}
		for _, event := range lineFeed.Feed(string(chunk[:chunkSize]), e.Provider) {
			e.emit(event)
		}
		chunk = chunk[chunkSize:]
	}
}

func (e *Emitter) Close() {
	if e == nil || e.Writer == nil {
		return
	}
	e.stdoutMu.Lock()
	stdoutEvents := e.stdoutFeed.Close(e.Provider)
	e.stdoutMu.Unlock()
	e.stderrMu.Lock()
	stderrEvents := e.stderrFeed.Close(e.Provider)
	e.stderrMu.Unlock()
	for _, event := range append(stdoutEvents, stderrEvents...) {
		e.emit(event)
	}
}

func (e *Emitter) emit(event contract.ProgressEvent) {
	key := progressEventKey(event)
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.seen[key]; ok {
		return
	}
	if len(e.seenOrder) < maxSeenEventKeys {
		e.seenOrder = append(e.seenOrder, key)
	} else {
		delete(e.seen, e.seenOrder[e.seenNext])
		e.seenOrder[e.seenNext] = key
		e.seenNext = (e.seenNext + 1) % maxSeenEventKeys
	}
	e.seen[key] = struct{}{}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = e.Writer.Write(append(data, '\n'))
}

func progressEventKey(event contract.ProgressEvent) [sha256.Size]byte {
	hash := sha256.New()
	for _, value := range []string{
		string(event.Kind), event.Name, event.Summary, event.Provider, event.SessionID, event.RunID,
	} {
		_, _ = io.WriteString(hash, value)
		_, _ = hash.Write([]byte{0})
	}
	var key [sha256.Size]byte
	copy(key[:], hash.Sum(nil))
	return key
}

type LineFeed struct {
	pending string
	// discarding means the current line exceeded maxProgressLineBytes. The
	// whole line is ignored through its newline; truncated JSON is never parsed.
	discarding bool
}

func (l *LineFeed) Feed(chunk string, provider string) []contract.ProgressEvent {
	events := []contract.ProgressEvent{}
	for len(chunk) > 0 {
		newline := strings.IndexByte(chunk, '\n')
		if l.discarding {
			if newline < 0 {
				return events
			}
			l.discarding = false
			chunk = chunk[newline+1:]
			continue
		}
		if newline < 0 {
			if len(l.pending)+len(chunk) > maxProgressLineBytes {
				l.pending = ""
				l.discarding = true
				events = append(events, droppedProgressLineEvent(provider))
			} else {
				l.pending += chunk
			}
			return events
		}
		segment := chunk[:newline]
		if len(l.pending)+len(segment) > maxProgressLineBytes {
			l.pending = ""
			events = append(events, droppedProgressLineEvent(provider))
		} else {
			line := l.pending + segment
			l.pending = ""
			events = append(events, eventsFromLine(line, provider)...)
		}
		chunk = chunk[newline+1:]
	}
	return events
}

func (l *LineFeed) Close(provider string) []contract.ProgressEvent {
	if l.discarding {
		l.pending = ""
		l.discarding = false
		return nil
	}
	if strings.TrimSpace(l.pending) == "" {
		return nil
	}
	line := l.pending
	l.pending = ""
	return eventsFromLine(line, provider)
}

func droppedProgressLineEvent(provider string) contract.ProgressEvent {
	event := contract.NewProgress(contract.ProgressWarning, "progress_line_dropped", droppedProgressLineSummary)
	event.Provider = provider
	return event
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
	// Provider output is data, not the dispatch control plane. Only dispatch may
	// emit terminal done/error events after process execution has completed.
	if kind == contract.ProgressDone || kind == contract.ProgressError {
		kind = contract.ProgressTool
	}
	event := contract.NewProgress(kind, firstString(payload, "name"), firstString(payload, "summary"))
	event.Provider = provider
	event.SessionID = firstString(payload, "session_id")
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
