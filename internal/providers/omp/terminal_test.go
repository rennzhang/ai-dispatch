package omp

import (
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
	"strings"
	"testing"
)

func TestExitZeroDoesNotOverrideOMPFailedMessage(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"usage limit exceeded","content":[{"type":"text","text":"partial output"}]}}`
	got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
	if got.OK || got.Status != contract.StatusQuota || !strings.Contains(got.Stderr, "usage limit exceeded") {
		t.Fatalf("result=%+v", got)
	}
}

func TestPartialOMPStreamWithoutTerminalIsNotSuccess(t *testing.T) {
	stdout := `{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"partial output"}]}}`
	got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
	if got.OK {
		t.Fatalf("partial stream accepted: %+v", got)
	}
}

func TestLaterCompletedOMPMessageCanRecoverEarlierError(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"temporary error"}}` + "\n" +
		`{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"text","text":"final output"}]}}`
	got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
	if !got.OK || got.Text != "final output" {
		t.Fatalf("result=%+v", got)
	}
}

func TestEarlierOMPMessageCannotCompleteALaterPartialMessage(t *testing.T) {
	for _, reason := range []string{"toolUse", "stop"} {
		t.Run(reason, func(t *testing.T) {
			stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"` + reason + `","content":[{"type":"text","text":"checking"}]}}` + "\n" +
				`{"type":"message_update","message":{"role":"assistant","content":[{"type":"text","text":"partial final"}]}}`
			got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
			if got.OK {
				t.Fatalf("earlier message completed a later partial: %+v", got)
			}
		})
	}
}

func TestOMPToolCallIsNotFinalAnswer(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"text","text":"checking"}]}}`
	got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
	if got.OK {
		t.Fatalf("tool call accepted as final: %+v", got)
	}
}

func TestEmptyFinalOMPMessageDoesNotReuseEarlierToolText(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"toolUse","content":[{"type":"text","text":"checking"}]}}` + "\n" +
		`{"type":"message_start","message":{"role":"assistant","content":[]}}` + "\n" +
		`{"type":"message_end","message":{"role":"assistant","stopReason":"stop","content":[{"type":"thinking","thinking":"private reasoning"}]}}`
	got := Provider{}.Parse(runtime.RunResult{Stdout: []byte(stdout)}, providers.BuildRequest{})
	if got.OK || got.Text != "" {
		t.Fatalf("earlier text reused: %+v", got)
	}
}
