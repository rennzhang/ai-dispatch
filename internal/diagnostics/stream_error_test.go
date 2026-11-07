package diagnostics

import (
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestStdoutErrorRetainsQuotaReasonInsteadOfExitStatus(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","stopReason":"error","errorMessage":"429 OpenCode Go usage limit exceeded; retry later","content":[{"type":"thinking","thinking":"private reasoning"}]}}`
	got := Classify("OMP", stdout, "", "exit status 1")
	if got.Class != contract.FailureQuota || !strings.Contains(got.Stderr, "usage limit exceeded") {
		t.Fatalf("failure=%+v", got)
	}
	if strings.Contains(got.Stderr, "private reasoning") {
		t.Fatal("reasoning leaked into diagnostics")
	}
}

func TestStdoutContentIsNotAnErrorMessage(t *testing.T) {
	stdout := `{"type":"message_end","message":{"role":"assistant","content":[{"type":"text","text":"private task text"}]}}`
	got := Classify("OMP", stdout, "", "exit status 1")
	if got.Stderr != "exit status 1" {
		t.Fatalf("unexpected diagnostic: %q", got.Stderr)
	}
}
