package claude

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/diagnostics"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

type Provider struct{}

func (Provider) Name() string { return "claude" }

func (Provider) ResolveEffort(_ context.Context, req providers.EffortRequest) providers.EffortResolution {
	requested := contract.NormalizeEffort(req.Requested)
	if requested == contract.EffortAuto {
		return providers.EffortAuto(requested, req.Model)
	}
	// Current Claude Code CLI (print and PTY) rejects --effort as unknown.
	// Anthropic "effort" docs describe API/model semantics, not this CLI flag.
	// Keep the shared ResolveEffort contract so a future verified CLI can opt in.
	return providers.EffortFallback(requested, req.Model,
		fmt.Sprintf("effort %s is not supported by the Claude Code CLI; applied auto", requested))
}

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	transport := effectiveTransport(req)
	if transport == "disabled" {
		return runtime.CommandSpec{}, fmt.Errorf("claude provider is disabled in ai-dispatch config")
	}
	if transport == "pty" {
		return buildPTY(req)
	}
	return buildAPI(req)
}

func buildAPI(req providers.BuildRequest) (runtime.CommandSpec, error) {
	args := []string{
		"claude",
		"-p",
		"--setting-sources",
		"user,project",
		"--dangerously-skip-permissions",
		"--output-format",
		"stream-json",
		"--verbose",
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if shouldPassClaudeModel(req) {
		args = append(args, "--model", req.Target.Model)
	}
	// Never pass --effort: ResolveEffort falls back to auto until CLI support is verified.
	var stdin []byte
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return runtime.CommandSpec{}, fmt.Errorf("cannot read prompt file for claude: %w", err)
		}
		stdin = data
	} else if req.Prompt != "" {
		args = append(args, req.Prompt)
	}
	return runtime.CommandSpec{Args: args, Env: claudeEnv(""), Stdin: stdin}, nil
}

func buildPTY(req providers.BuildRequest) (runtime.CommandSpec, error) {
	driver, err := ptyDriverCommand()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	cwd := req.CWD
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	timeout := req.TimeoutSeconds
	driverTimeout := timeout
	if timeout > 10 {
		driverTimeout = timeout - 5
	}
	driverSessionID := req.SessionID
	if driverSessionID == "" {
		driverSessionID, err = newClaudeSessionID()
		if err != nil {
			return runtime.CommandSpec{}, fmt.Errorf("cannot allocate Claude session id: %w", err)
		}
	}
	claudeArgs := []string{"claude", "--dangerously-skip-permissions"}
	if shouldPassClaudeModel(req) {
		claudeArgs = append(claudeArgs, "--model", req.Target.Model)
	}
	// Never pass --effort: ResolveEffort falls back to auto until CLI support is verified.
	if req.SessionID != "" {
		claudeArgs = append(claudeArgs, "--resume", req.SessionID)
	} else {
		claudeArgs = append(claudeArgs, "--session-id", driverSessionID)
	}
	command := append([]string{
		"env",
		"CLAUDE_SESSION_ID=" + driverSessionID,
		"AI_DISPATCH_CLAUDE_SESSION_ID=" + driverSessionID,
	}, claudeArgs...)
	env := claudeEnv(driverSessionID)
	args := append([]string{}, driver...)
	args = append(args,
		"--transport", "tmux",
		"--cwd", cwd,
		"--startup-wait", "30",
		"--startup-ready-pattern", "\u276f",
		"--timeout", fmt.Sprintf("%d", driverTimeout),
		"--session-id", driverSessionID,
		"--claude-base-dir", filepath.Join(os.Getenv("HOME"), ".claude"),
	)
	if req.PromptFile != "" {
		args = append(args, "--input-file", req.PromptFile)
	} else {
		args = append(args, "--input", req.Prompt)
	}
	args = append(args, "--")
	args = append(args, command...)
	return runtime.CommandSpec{Args: args, Env: env}, nil
}

func newClaudeSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	encoded := make([]byte, 36)
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded), nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	if effectiveTransport(req) == "pty" {
		return parsePTY(run, req)
	}
	text, sessionID, model, isError := parseClaudeStream(string(run.Stdout))
	stderr := string(run.Stderr)
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	ok := run.ExitCode == 0 && !isError && strings.TrimSpace(text) != ""
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		if strings.TrimSpace(stderr) == "" {
			stderr = diagnostics.TimeoutMessage("Claude", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		diagnosticStderr := stderr
		if strings.TrimSpace(diagnosticStderr) == "" && strings.TrimSpace(text) != "" && (isError || isClaudeErrorText(text)) {
			diagnosticStderr = text
		}
		classified := diagnostics.Classify("Claude", string(run.Stdout), diagnosticStderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "claude")
		if isError && f == contract.FailureRuntime {
			next = contract.NextSwitchStrategy
		}
		stderr = classified.Stderr
		if stderr == "Claude returned no successful result" {
			stderr = diagnostics.NoResultMessage("Claude", string(run.Stdout), string(run.Stderr), run.ExitCode)
		}
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "claude",
		ModelUsed:       model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("claude", model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "claude",
			Model:      model,
			Status:     status,
			DurationMS: run.DurationMS,
		}},
		ExitCode:     run.ExitCode,
		DurationMS:   run.DurationMS,
		Stderr:       stderr,
		Warnings:     []string{},
		NextAction:   next,
		FailureClass: failure,
	}
}

func parseClaudeStream(stdout string) (text string, sessionID string, model string, isError bool) {
	assistantText := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		switch event["type"] {
		case "assistant":
			if sid, ok := event["session_id"].(string); ok {
				sessionID = sid
			}
			if text := assistantEventText(event); text != "" {
				assistantText = text
			}
		case "result":
			if sid, ok := event["session_id"].(string); ok {
				sessionID = sid
			}
			if m, ok := event["model"].(string); ok {
				model = m
			}
			if v, ok := event["is_error"].(bool); ok {
				isError = v
			}
			if result, ok := event["result"].(string); ok {
				text = result
			}
		default:
			continue
		}
	}
	if strings.TrimSpace(text) == "" {
		text = assistantText
	}
	return text, sessionID, model, isError
}

func assistantEventText(event map[string]any) string {
	message, ok := event["message"].(map[string]any)
	if !ok {
		return ""
	}
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	parts := []string{}
	for _, raw := range content {
		block, ok := raw.(map[string]any)
		if !ok || block["type"] != "text" {
			continue
		}
		if text, ok := block["text"].(string); ok && strings.TrimSpace(text) != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n"))
}

func parsePTY(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	events := parsePTYEvents(string(run.Stdout))
	textParts := []string{}
	toolCalls := 0
	sessionID := ""
	done := map[string]any(nil)
	sawPlaceholder := false
	warnings := []string{}
	for _, event := range events {
		switch firstString(event, "event") {
		case "warning":
			if warning := strings.TrimSpace(firstString(event, "message")); warning != "" {
				warnings = appendUniqueString(warnings, warning)
			}
		case "session_start":
			sessionID = firstNonEmpty(sessionID, firstString(event, "session_id"))
		case "assistant_text":
			text := strings.TrimSpace(firstString(event, "text"))
			if text == "" {
				continue
			}
			if isPlaceholderText(text) {
				sawPlaceholder = true
				continue
			}
			textParts = append(textParts, text)
		case "tool_use":
			toolCalls++
		case "done":
			done = event
			sessionID = firstNonEmpty(sessionID, firstString(event, "session_id"))
			if summary := valueMap(event["claude_session"]); summary != nil {
				if saw, ok := summary["saw_placeholder_response"].(bool); ok && saw {
					sawPlaceholder = true
				}
			}
		}
	}

	duration := run.DurationMS
	if done != nil {
		if value, ok := int64FromAny(done["duration_ms"]); ok {
			duration = value
		}
	}
	if run.TimedOut || (done != nil && firstString(done, "termination_reason") == "hard_timeout") {
		message := "claude timed out via PTY"
		if done != nil {
			if tail := firstString(done, "pane_tail"); tail != "" {
				message += "\n" + tail
			}
		}
		return errorResult(req, contract.StatusTimeout, contract.FailureTimeout, message, 124, duration, warnings)
	}
	if done != nil && firstString(done, "termination_reason") == "interrupted_prompt" {
		return errorResult(req, contract.StatusError, contract.FailureRuntime, "Claude entered the interactive Interrupted prompt before answering. Use --provider-opt claude.transport=print for unattended dispatch from inside tmux.", 1, duration, warnings)
	}
	if run.ExitCode != 0 {
		stderr := strings.TrimSpace(string(run.Stderr))
		if stderr == "" {
			stderr = fmt.Sprintf("claude_pty_driver subprocess exited with code %d but produced no stderr output. Check tmux availability (tmux -V), claude CLI in PATH, and that --input path is readable.", run.ExitCode)
		}
		classified := diagnostics.Classify("Claude PTY", string(run.Stdout), stderr, run.Error)
		return errorResult(req, classified.Status, classified.Class, classified.Stderr, run.ExitCode, duration, warnings)
	}

	text := finalPTYText(done)
	if text == "" && len(textParts) > 0 {
		text = strings.Join(textParts, "\n")
	}
	if isClaudeErrorText(text) {
		classified := diagnostics.Classify("Claude PTY", text, text, run.Error)
		return errorResult(req, classified.Status, classified.Class, classified.Stderr, run.ExitCode, duration, warnings)
	}
	if text == "" && toolCalls > 0 {
		text = "(no text summary - work completed via tool calls)"
	}
	ok := strings.TrimSpace(text) != "" || sawPlaceholder
	if !ok {
		stderr := ptyEmptyError(done)
		classified := diagnostics.Classify("Claude PTY", string(run.Stdout), stderr, run.Error)
		return errorResult(req, classified.Status, classified.Class, classified.Stderr, run.ExitCode, duration, warnings)
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              true,
		Status:          contract.StatusSuccess,
		Text:            text,
		ProviderUsed:    "claude",
		ModelUsed:       req.Target.Model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("claude", req.Target.Model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "claude",
			Model:      req.Target.Model,
			Status:     contract.StatusSuccess,
			DurationMS: duration,
		}},
		ExitCode:     run.ExitCode,
		DurationMS:   duration,
		Stderr:       "",
		Warnings:     warnings,
		NextAction:   contract.NextDone,
		FailureClass: nil,
	}
}

func errorResult(req providers.BuildRequest, status contract.Status, failure contract.FailureClass, stderr string, exitCode int, duration int64, warnings []string) contract.ProviderResult {
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              false,
		Status:          status,
		ProviderUsed:    "claude",
		ModelUsed:       req.Target.Model,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("claude", req.Target.Model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "claude",
			Model:      req.Target.Model,
			Status:     status,
			DurationMS: duration,
		}},
		ExitCode:     exitCode,
		DurationMS:   duration,
		Stderr:       stderr,
		Warnings:     warnings,
		NextAction:   contract.NextActionForFailure(failure, "claude"),
		FailureClass: &failure,
	}
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func parsePTYEvents(stdout string) []map[string]any {
	events := []map[string]any{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err == nil {
			events = append(events, event)
		}
	}
	return events
}

func finalPTYText(done map[string]any) string {
	if done == nil {
		return ""
	}
	if text := strings.TrimSpace(firstString(done, "response_text")); text != "" && !isPlaceholderText(text) {
		return text
	}
	if summary := valueMap(done["claude_session"]); summary != nil {
		if text := strings.TrimSpace(firstString(summary, "assistant_text")); text != "" && !isPlaceholderText(text) {
			return text
		}
	}
	return ""
}

func ptyEmptyError(done map[string]any) string {
	if done == nil {
		return "Claude PTY produced no output"
	}
	switch reason := firstString(done, "termination_reason"); reason {
	case "process_exit":
		return "Claude PTY session exited before completion"
	case "ready_signal":
		return "Claude PTY produced no response before returning to prompt"
	case "interrupted_prompt":
		return "Claude entered the interactive Interrupted prompt before answering. Use --provider-opt claude.transport=print for unattended dispatch from inside tmux."
	case "":
		return "Claude PTY produced no output"
	default:
		return "Claude PTY failed with termination_reason=" + reason
	}
}

func isPlaceholderText(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed == "No response requested." || trimmed == "Continue from where you left off."
}

func isClaudeErrorText(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, needle := range []string{
		"api error:",
		"please run /login",
		"violation of provider terms of service",
		"unauthorized",
		"forbidden",
	} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return false
}

func shouldPassClaudeModel(req providers.BuildRequest) bool {
	if strings.TrimSpace(req.Target.Model) == "" {
		return false
	}
	if hasClaudeAPIBackend(processClaudeEnv()) {
		switch req.Target.Source {
		case "registry", "alias", "inferred":
			if strings.TrimSpace(req.Target.ActualID) != "" && strings.TrimSpace(req.Target.ActualID) != strings.TrimSpace(req.Target.Model) {
				return false
			}
		}
	}
	return true
}

func effectiveTransport(req providers.BuildRequest) string {
	if req.ProviderOptions != nil {
		if value := req.ProviderOptions["transport"]; value != "" {
			if value == "api" {
				return "print"
			}
			if value != "auto" {
				return value
			}
		}
	}
	cfg, err := config.Load()
	if err == nil {
		if cfg.ClaudeTransport == "auto" {
			return defaultTransportForEnv(processClaudeEnv())
		}
		return cfg.ClaudeTransport
	}
	return "print"
}

func defaultTransportForEnv(env map[string]string) string {
	if hasClaudeAPIBackend(env) {
		return "print"
	}
	return "pty"
}

func hasClaudeAPIBackend(env map[string]string) bool {
	for _, key := range []string{
		"ANTHROPIC_BASE_URL",
		"ANTHROPIC_MODEL",
		"ANTHROPIC_API_KEY",
		"ANTHROPIC_AUTH_TOKEN",
	} {
		if strings.TrimSpace(env[key]) != "" {
			return true
		}
	}
	return false
}

func ptyDriverCommand() ([]string, error) {
	if value := os.Getenv("AI_DISPATCH_CLAUDE_PTY_GO_DRIVER"); value != "" {
		return []string{value}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return []string{exe, "__claude-pty-driver"}, nil
}

func claudeEnv(sessionID string) []string {
	env := map[string]string{}
	if sessionID != "" {
		env["CLAUDE_SESSION_ID"] = sessionID
		env["AI_DISPATCH_CLAUDE_SESSION_ID"] = sessionID
	}
	return runtime.SanitizedEnv(env)
}

func processClaudeEnv() map[string]string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		if !strings.HasPrefix(key, "ANTHROPIC_") {
			continue
		}
		if value != "" {
			env[key] = value
		}
	}
	return env
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && value != "" {
			return value
		}
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
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

func int64FromAny(value any) (int64, bool) {
	switch typed := value.(type) {
	case float64:
		return int64(typed), true
	case int64:
		return typed, true
	case int:
		return int64(typed), true
	default:
		return 0, false
	}
}

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}
