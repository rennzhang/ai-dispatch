package codex

import (
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
)

func TestDesktopSettingsAreProvedByTheBoundTurn(t *testing.T) {
	req := providers.BuildRequest{CWD: "/project", Effort: contract.EffortHigh, Target: routing.DispatchTarget{Model: "gpt-6.1-sol"}}
	const matching = `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6.1-sol","effort":"high","cwd":"/project"}}` + "\n"
	const foreign = `{"type":"turn_context","payload":{"turn_id":"foreign","model":"gpt-6.1-sol","effort":"high","cwd":"/project"}}` + "\n"
	cases := []struct {
		name      string
		data      string
		wantError bool
	}{
		{"matching", matching, false},
		{"foreign-cannot-prove-settings", foreign, true},
		{"explicit-high-must-not-disappear", `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6.1-sol","cwd":"/project"}}` + "\n" + foreign, true},
		{"foreign-high-must-not-mask-medium", `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6.1-sol","effort":"medium","cwd":"/project"}}` + "\n" + foreign, true},
		{"wrong-model", `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6-astra","effort":"high","cwd":"/project"}}` + "\n", true},
		{"wrong-workspace", `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6.1-sol","effort":"high","cwd":"/other"}}` + "\n", true},
		{"unrequested-priority", `{"type":"turn_context","payload":{"turn_id":"bound","model":"gpt-6.1-sol","effort":"high","cwd":"/project","service_tier":"priority"}}` + "\n", true},
		{"foreign-change-cannot-invalidate-bound-turn", matching + `{"type":"turn_context","payload":{"turn_id":"foreign","model":"gpt-6-astra","effort":"medium","cwd":"/other"}}` + "\n", false},
	}
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			err := validateDesktopTurnSettings([]byte(item.data), "bound", req)
			if (err != nil) != item.wantError {
				t.Fatalf("execution settings accepted=%t, want accepted=%t: %v", err == nil, !item.wantError, err)
			}
		})
	}
}
