package codex

import (
	"bytes"
	"encoding/json"
	"os"
)

func terminalByID(data []byte, turnID string) (turnEnd, bool) {
	var found turnEnd
	ok := false
	for _, end := range terminalTurns(data) {
		if end.id == turnID && (!ok || !found.failed) {
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

func activeDesktopTurnInRollout(threadID, anchor string) (string, error) {
	paths, err := rolloutHistoryPaths(threadID, anchor)
	if err != nil {
		return "", err
	}
	users, ended := make(map[string]bool), make(map[string]bool)
	active := ""
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		active = observeDesktopTurnActivity(data, active, users, ended)
	}
	if ended[active] {
		return "", nil
	}
	return active, nil
}

// Keep only activity state between segments, not a concatenated history buffer.
func observeDesktopTurnActivity(data []byte, active string, users, ended map[string]bool) string {
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string `json:"type"`
				Status string `json:"status"`
				ID     string `json:"id"`
				TurnID string `json:"turn_id"`
				Role   string `json:"role"`
				Meta   struct {
					TurnID string `json:"turn_id"`
				} `json:"internal_chat_message_metadata_passthrough"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Type == "response_item" && event.Payload.Type == "message" && event.Payload.Role == "user" && event.Payload.Meta.TurnID != "" {
			if event.Payload.ID != "" {
				if users[event.Payload.ID] {
					continue
				}
				users[event.Payload.ID] = true
			}
			if !ended[event.Payload.Meta.TurnID] {
				active = event.Payload.Meta.TurnID
			}
		} else if event.Type == "event_msg" {
			kind := firstNonEmpty(event.Payload.Type, event.Payload.Status)
			switch kind {
			case "task_started":
				if !ended[event.Payload.TurnID] {
					active = event.Payload.TurnID
				}
			case "task_complete", "task_failed", "turn_failed", "error", "task_interrupted", "turn_interrupted", "turn_aborted", "task_aborted", "interrupted", "aborted", "cancelled", "canceled":
				ended[event.Payload.TurnID] = true
			}
		}
	}
	return active
}

func terminalTurns(data []byte) []turnEnd {
	var ends []turnEnd
	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				Type             string `json:"type"`
				Status           string `json:"status"`
				TurnID           string `json:"turn_id"`
				LastAgentMessage string `json:"last_agent_message"`
				Message          string `json:"message"`
				Reason           string `json:"reason"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if event.Type != "event_msg" {
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
		case "task_interrupted", "turn_interrupted", "turn_aborted", "task_aborted", "interrupted", "aborted", "cancelled", "canceled":
			ends = append(ends, turnEnd{id: event.Payload.TurnID, status: "interrupted", failed: true, errText: firstNonEmpty(event.Payload.Message, event.Payload.Reason, "interrupted")})
		}
	}
	return ends
}
