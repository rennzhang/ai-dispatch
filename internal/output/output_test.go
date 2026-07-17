package output

import (
	"os"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestWriteFileFrontmatter(t *testing.T) {
	result := contract.SuccessResult("hello")
	result.ProviderUsed = "codex"
	result.ModelUsed = "gpt-5.5"
	result.RequestedTarget = "gpt5.5"
	result.SessionID = "s1"
	result.RequestedEffort = contract.EffortXHigh
	result.AppliedEffort = contract.EffortAuto
	result.EffortFallbackReason = "effort xhigh is not supported; applied auto"
	result.RouteTrace = []string{"codex:gpt-5.5"}
	result.DurationMS = 12
	path := t.TempDir() + "/result.md"
	if err := WriteFile(path, result); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		`ai_dispatch:`,
		`  provider: "codex"`,
		`  model: "gpt-5.5"`,
		`  requested_target: "gpt5.5"`,
		`  requested_effort: "xhigh"`,
		`  applied_effort: "auto"`,
		`  effort_fallback_reason: "effort xhigh is not supported; applied auto"`,
		`  session_id: "s1"`,
		`route_trace:`,
		`    - "codex:gpt-5.5"`,
		"hello\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}
