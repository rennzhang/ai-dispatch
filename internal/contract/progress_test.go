package contract

import (
	"strings"
	"testing"
)

func TestTerminalProgressEventsDoNotTellCallersToWait(t *testing.T) {
	for _, kind := range []ProgressKind{ProgressDone, ProgressError} {
		t.Run(string(kind), func(t *testing.T) {
			event := NewProgress(kind, string(kind), "finished")
			if event.NextAction != "done" {
				t.Fatalf("next_action=%q", event.NextAction)
			}
			if strings.Contains(event.Directive, "BUSY") || strings.Contains(event.Directive, "wait") {
				t.Fatalf("terminal directive=%q", event.Directive)
			}
		})
	}
}
