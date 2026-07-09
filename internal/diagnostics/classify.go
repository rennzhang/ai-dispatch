package diagnostics

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

type Failure struct {
	Status contract.Status
	Class  contract.FailureClass
	Stderr string
}

func Classify(provider string, stdout string, stderr string, runError string) Failure {
	message := cleanDiagnosticStderr(stderr)
	combined := strings.ToLower(strings.Join([]string{stdout, stderr, runError}, "\n"))
	class := contract.FailureRuntime
	status := contract.StatusError

	switch {
	case containsAny(combined, "quota", "rate limit", "rate_limit", "usage limit", "exceeded your current quota", "insufficient credits", "credit balance", "purchase more credits"):
		class = contract.FailureQuota
		status = contract.StatusQuota
	case strings.EqualFold(provider, "Antigravity") && containsAny(combined, "agy completed without output"):
		class = contract.FailureConfig
	case containsAny(combined, "permission requested") && containsAny(combined, "auto-rejecting"):
		class = contract.FailureConfig
	case containsAny(combined, "not logged in", "please run /login", "api key", "apikey", "unauthorized", "forbidden", "permission denied", "authentication", "auth", "terms of service", "provider terms", "prohibited due to"):
		class = contract.FailureConfig
	case strings.EqualFold(provider, "OpenCode") && containsAny(combined,
		"not available in your region",
		"unsupported region",
		"country, region, or territory",
		"no endpoints found",
		"model not found",
		"model does not exist",
		"not a valid model",
		"do not have access to this model",
		"not available for your account",
	):
		class = contract.FailureConfig
	case strings.EqualFold(provider, "Grok") && containsAny(combined,
		"unknown model",
		"invalid model",
		"model not found",
		"model does not exist",
		"not available for your account",
		"do not have access to this model",
	):
		class = contract.FailureConfig
	case containsAny(combined, "no such host", "network is unreachable", "connection refused", "connection reset", "tls handshake", "temporary failure", "timeout awaiting response"):
		class = contract.FailureNetwork
	case containsAny(combined, "executable file not found", "command not found", "no such file or directory"):
		class = contract.FailureConfig
	case containsAny(combined, "unexpected eof", "invalid character", "malformed json", "json parse"):
		class = contract.FailureRuntime
	}

	if message == "" {
		message = strings.TrimSpace(runError)
	}
	if message == "" {
		message = provider + " returned no successful result"
	}
	if strings.EqualFold(provider, "Antigravity") && strings.TrimSpace(message) == "agy completed without output" {
		message = "agy completed without output; verify agy login and Chrome authorization before retrying"
	}
	return Failure{Status: status, Class: class, Stderr: message}
}

func NoResultMessage(provider string, stdout string, stderr string, exitCode int) string {
	events, lastEvent, nonJSON := streamSummary(stdout)
	parts := []string{provider + " returned no successful result"}
	if events > 0 || nonJSON > 0 {
		parts = append(parts, fmt.Sprintf("stdout_events=%d", events))
		if lastEvent != "" {
			parts = append(parts, "last_event="+lastEvent)
		}
		if nonJSON > 0 {
			parts = append(parts, fmt.Sprintf("non_json_stdout_lines=%d", nonJSON))
		}
	}
	parts = append(parts,
		fmt.Sprintf("stdout_bytes=%d", len(stdout)),
		fmt.Sprintf("stderr_bytes=%d", len(stderr)),
		fmt.Sprintf("exit_code=%d", exitCode),
	)
	return strings.Join(parts, "; ")
}

func TimeoutMessage(provider string, fixed bool, activity bool, fixedSeconds int, activitySeconds int) string {
	switch {
	case fixed:
		return fmt.Sprintf("%s timed out after %d seconds wall-clock limit", provider, fixedSeconds)
	case activity:
		return fmt.Sprintf("%s timed out after %d seconds without provider activity", provider, activitySeconds)
	default:
		return provider + " timed out"
	}
}

func streamSummary(stdout string) (events int, lastEvent string, nonJSON int) {
	for _, raw := range strings.Split(stdout, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			nonJSON++
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			nonJSON++
			continue
		}
		events++
		lastEvent = eventName(event)
	}
	return events, lastEvent, nonJSON
}

func eventName(event map[string]any) string {
	for _, key := range []string{"type", "event", "subtype"} {
		if value, ok := event[key].(string); ok && strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "unknown"
}

func cleanDiagnosticStderr(stderr string) string {
	lines := []string{}
	for _, line := range strings.Split(stderr, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || isBenignCodexLoaderWarning(trimmed) {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func isBenignCodexLoaderWarning(line string) bool {
	lowered := strings.ToLower(line)
	return strings.Contains(lowered, "warn codex_core_skills::loader:") &&
		strings.Contains(lowered, "ignoring interface.icon_")
}

func containsAny(value string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}
