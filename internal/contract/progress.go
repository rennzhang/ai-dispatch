package contract

type ProgressKind string

const (
	ProgressRead         ProgressKind = "read"
	ProgressEdit         ProgressKind = "edit"
	ProgressBash         ProgressKind = "bash"
	ProgressSearch       ProgressKind = "search"
	ProgressBrowse       ProgressKind = "browse"
	ProgressFetch        ProgressKind = "fetch"
	ProgressTool         ProgressKind = "tool"
	ProgressReasoning    ProgressKind = "reasoning"
	ProgressAgentMessage ProgressKind = "agent_message"
	ProgressWarning      ProgressKind = "warning"
	ProgressRetry        ProgressKind = "retry"
	ProgressHeartbeat    ProgressKind = "heartbeat"
	ProgressSession      ProgressKind = "session"
	ProgressDone         ProgressKind = "done"
	ProgressError        ProgressKind = "error"
)

const (
	DefaultProgressDirective         = "BUSY -- do not interrupt; wait for output_file or final result."
	DefaultTerminalProgressDirective = "TERMINAL -- dispatch finished; use the final result."
)

type ProgressEvent struct {
	Progress      bool         `json:"_progress"`
	SchemaVersion string       `json:"schema_version"`
	Kind          ProgressKind `json:"kind"`
	Name          string       `json:"name"`
	Summary       string       `json:"summary"`
	Provider      string       `json:"provider,omitempty"`
	SessionID     string       `json:"session_id,omitempty"`
	RunID         string       `json:"run_id,omitempty"`
	NextAction    string       `json:"next_action"`
	Directive     string       `json:"directive"`
}

func NewProgress(kind ProgressKind, name string, summary string) ProgressEvent {
	event := ProgressEvent{
		Progress:      true,
		SchemaVersion: "2.0",
		Kind:          kind,
		Name:          name,
		Summary:       summary,
		NextAction:    "wait",
		Directive:     DefaultProgressDirective,
	}
	if kind == ProgressDone || kind == ProgressError {
		event.NextAction = "done"
		event.Directive = DefaultTerminalProgressDirective
	}
	return event
}
