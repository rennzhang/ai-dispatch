package diagnostics

import (
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestClassifyAuthFailure(t *testing.T) {
	got := Classify("Claude", "", "Not logged in · Please run /login", "")
	if got.Status != contract.StatusError || got.Class != contract.FailureConfig {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyQuotaFailure(t *testing.T) {
	got := Classify("Codex", "", "rate limit: insufficient credits", "")
	if got.Status != contract.StatusQuota || got.Class != contract.FailureQuota {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyCodexUsageLimitFromStdoutDespiteLoaderWarnings(t *testing.T) {
	got := Classify(
		"Codex",
		`{"msg":"You've hit your usage limit. Visit https://chatgpt.com/codex/settings/usage to purchase more credits or try again at 7:30 PM."}`,
		"2026-06-16T10:12:01Z WARN codex_core_skills::loader: ignoring interface.icon_small",
		"",
	)
	if got.Status != contract.StatusQuota || got.Class != contract.FailureQuota {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyDropsBenignCodexLoaderWarningsFromMessage(t *testing.T) {
	got := Classify(
		"Codex",
		"",
		"2026-06-16T10:12:01Z WARN codex_core_skills::loader: ignoring interface.icon_small: icon path with '..' must resolve under plugin assets/\n"+
			"2026-06-16T10:12:01Z WARN codex_core_skills::loader: ignoring interface.icon_large: icon path with '..' must resolve under plugin assets/",
		"",
	)
	if got.Stderr != "Codex returned no successful result" {
		t.Fatalf("stderr=%q", got.Stderr)
	}
}

func TestClassifyNetworkFailure(t *testing.T) {
	got := Classify("OpenCode", "", "dial tcp: no such host", "")
	if got.Class != contract.FailureNetwork {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyMissingBinary(t *testing.T) {
	got := Classify("Codex", "", "", `exec: "codex": executable file not found in $PATH`)
	if got.Class != contract.FailureConfig {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyOpenCodePermissionAutorejectAsConfig(t *testing.T) {
	got := Classify("OpenCode", "", "! permission requested: external_directory (/tmp/*); auto-rejecting", "")
	if got.Status != contract.StatusError || got.Class != contract.FailureConfig {
		t.Fatalf("got=%+v", got)
	}
}

func TestClassifyOpenCodeModelAvailabilityAsConfig(t *testing.T) {
	cases := []string{
		"OpenRouter: This model is not available in your region",
		"No endpoints found for openrouter/example/model",
		"model not found or you do not have access to this model",
	}
	for _, message := range cases {
		got := Classify("OpenCode", "", message, "")
		if got.Status != contract.StatusError || got.Class != contract.FailureConfig {
			t.Fatalf("message=%q got=%+v", message, got)
		}
	}
}

func TestClassifyAntigravityEmptyOutputAsActionableConfig(t *testing.T) {
	got := Classify("Antigravity", "", "", "agy completed without output")
	if got.Status != contract.StatusError || got.Class != contract.FailureConfig {
		t.Fatalf("got=%+v", got)
	}
	if got.Stderr != "agy completed without output; verify agy login and Chrome authorization before retrying" {
		t.Fatalf("stderr=%q", got.Stderr)
	}
}

func TestNoResultMessageSummarizesStreamShape(t *testing.T) {
	got := NoResultMessage("OpenCode", "{\"type\":\"session.updated\"}\n{\"event\":\"done\"}\nplain\n", "", 1)
	for _, want := range []string{"stdout_events=2", "last_event=done", "non_json_stdout_lines=1", "exit_code=1"} {
		if !strings.Contains(got, want) {
			t.Fatalf("message=%q missing %q", got, want)
		}
	}
}

func TestTimeoutMessageSeparatesFixedAndActivityTimeouts(t *testing.T) {
	fixed := TimeoutMessage("OpenCode", true, false, 1, 120)
	if !strings.Contains(fixed, "1 seconds wall-clock") || strings.Contains(fixed, "without provider activity") {
		t.Fatalf("fixed=%q", fixed)
	}
	activity := TimeoutMessage("OpenCode", false, true, 180, 120)
	if !strings.Contains(activity, "120 seconds without provider activity") {
		t.Fatalf("activity=%q", activity)
	}
}
