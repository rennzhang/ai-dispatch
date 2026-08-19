package grok

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/diagnostics"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

type Provider struct{}

func (Provider) Name() string { return "grok" }

func (Provider) ResolveEffort(_ context.Context, req providers.EffortRequest) providers.EffortResolution {
	requested := contract.NormalizeEffort(req.Requested)
	if requested == contract.EffortAuto {
		return providers.EffortAuto(requested, req.Model)
	}
	if grokSupportsEffort(req.Model, requested) {
		return providers.EffortExact(requested, req.Model)
	}
	return providers.EffortFallback(requested, req.Model,
		fmt.Sprintf("effort %s is not supported by grok/%s; applied auto", requested, effortModelLabel(req.Model)))
}

func (Provider) ResolveFast(_ context.Context, req providers.FastRequest) providers.FastResolution {
	return providers.FastFallback(req.Requested, "grok", req.Model)
}

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	bin, err := grokBinary()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := []string{
		bin,
		"--output-format", "streaming-json",
	}
	if err := appendGrokOptions(&args, req.ProviderOptions); err != nil {
		return runtime.CommandSpec{}, err
	}
	if req.Effort != "" && req.Effort != contract.EffortAuto {
		args = append(args, "--reasoning-effort", string(req.Effort))
	}
	if req.CWD != "" {
		args = append(args, "--cwd", req.CWD)
	}
	if req.Target.Model != "" {
		if strings.HasPrefix(req.Target.Model, "openrouter/") {
			return runtime.CommandSpec{}, fmt.Errorf("grok provider cannot run OpenRouter model %q; use an OpenCode target instead", req.Target.Model)
		}
		args = append(args, "--model", req.Target.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.PromptFile != "" {
		args = append(args, "--prompt-file", req.PromptFile)
	} else if req.Prompt != "" {
		args = append(args, "--single", req.Prompt)
	}
	return runtime.CommandSpec{Args: args, Env: grokEnv()}, nil
}

func appendGrokOptions(args *[]string, opts map[string]string) error {
	approval := strings.TrimSpace(opts["approval"])
	if approval == "" {
		approval = "always"
	}
	switch approval {
	case "always":
		*args = append(*args, "--always-approve")
	case "default":
	default:
		return fmt.Errorf("unsupported grok.approval: %s", approval)
	}
	if maxTurns := strings.TrimSpace(opts["max-turns"]); maxTurns != "" {
		value, err := strconv.Atoi(maxTurns)
		if err != nil || value <= 0 {
			return fmt.Errorf("grok.max-turns must be a positive integer")
		}
		*args = append(*args, "--max-turns", maxTurns)
	}
	if webSearch := strings.TrimSpace(opts["web-search"]); webSearch != "" {
		switch webSearch {
		case "off":
			*args = append(*args, "--disable-web-search")
		case "on":
		default:
			return fmt.Errorf("grok.web-search must be on or off")
		}
	}
	subagents := strings.TrimSpace(opts["subagents"])
	if subagents == "" {
		subagents = "off"
	}
	switch subagents {
	case "off":
		*args = append(*args, "--no-subagents")
	case "on":
	default:
		return fmt.Errorf("grok.subagents must be on or off")
	}
	return nil
}

func grokBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("AI_DISPATCH_GROK_BIN")); explicit != "" {
		if path, err := executablePath(explicit, "AI_DISPATCH_GROK_BIN override"); err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
	if path, err := exec.LookPath("grok"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, candidate := range []string{
			filepath.Join(home, ".grok", "bin", "grok"),
			filepath.Join(home, ".local", "bin", "grok"),
		} {
			if path, err := executablePath(candidate, "grok fallback"); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("grok binary not found; install Grok Build or set AI_DISPATCH_GROK_BIN")
}

func executablePath(candidate string, label string) (string, error) {
	if path, err := exec.LookPath(candidate); err == nil {
		return path, nil
	}
	if !strings.Contains(candidate, string(os.PathSeparator)) {
		return "", fmt.Errorf("%s binary not found", label)
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("%s binary not found", label)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s binary not found", label)
	}
	return candidate, nil
}

func grokEnv() []string {
	overrides := map[string]string{}
	for _, key := range []string{
		"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "all_proxy", "no_proxy",
		"GROK_CLI_PROXY",
	} {
		if value := os.Getenv(key); value != "" {
			overrides[key] = value
		}
	}
	return runtime.SanitizedEnv(overrides)
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	stdout := string(run.Stdout)
	stderr := string(run.Stderr)
	text, sessionID, parseErr := parseGrokStreamingJSON(stdout)
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	ok := run.ExitCode == 0 && parseErr == nil && strings.TrimSpace(text) != ""
	resultStderr := ""
	warnings := []string{}
	if ok {
		warnings = grokWarnings(stderr)
	}
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		resultStderr = redactGrokDiagnostics(stderr)
		if strings.TrimSpace(resultStderr) == "" {
			resultStderr = diagnostics.TimeoutMessage("Grok", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classifiedStdout := stdout
		classifiedStderr := redactGrokDiagnostics(stderr)
		if run.ExitCode == 0 && parseErr != nil {
			classifiedStdout = "invalid streaming json: " + parseErr.Error()
		}
		classified := diagnostics.Classify("Grok", classifiedStdout, classifiedStderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "grok")
		resultStderr = classified.Stderr
		if run.ExitCode == 0 && parseErr != nil {
			resultStderr = "Grok returned invalid streaming JSON despite --output-format streaming-json: " + parseErr.Error()
		} else if resultStderr == "Grok returned no successful result" {
			resultStderr = diagnostics.NoResultMessage("Grok", stdout, redactGrokDiagnostics(stderr), run.ExitCode)
		}
	}
	if !ok {
		// Streaming output may contain an incomplete answer before a real failure
		// or inactivity timeout. Keep failure diagnostics authoritative.
		text = ""
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "grok",
		ModelUsed:       req.Target.Model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("grok", req.Target.Model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "grok",
			Model:      req.Target.Model,
			Status:     status,
			DurationMS: run.DurationMS,
		}},
		ExitCode:     run.ExitCode,
		DurationMS:   run.DurationMS,
		Stderr:       resultStderr,
		Warnings:     warnings,
		NextAction:   next,
		FailureClass: failure,
	}
}

func parseGrokStreamingJSON(stdout string) (string, string, error) {
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return "", "", errors.New("empty stdout")
	}

	var text strings.Builder
	var sessionID string
	sawStreamingEvent := false
	sawEnd := false
	for index, rawLine := range strings.Split(trimmed, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		var event struct {
			Type       string `json:"type"`
			Data       string `json:"data"`
			Message    string `json:"message"`
			SessionID  string `json:"sessionId"`
			SessionKey string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			return "", "", fmt.Errorf("line %d: %w", index+1, err)
		}

		eventSessionID := firstNonEmptyString(event.SessionID, event.SessionKey)
		if event.Type == "" {
			return "", "", fmt.Errorf("line %d: missing streaming event type", index+1)
		}
		sawStreamingEvent = true
		if eventSessionID != "" {
			sessionID = eventSessionID
		}
		switch event.Type {
		case "text":
			text.WriteString(event.Data)
		case "end":
			sawEnd = true
		case "error":
			message := firstNonEmptyString(event.Message, event.Data)
			if message == "" {
				message = "provider emitted an error event"
			}
			return text.String(), sessionID, errors.New(message)
		}
	}

	if !sawStreamingEvent {
		return "", "", errors.New("no JSON events")
	}
	if !sawEnd {
		return text.String(), sessionID, errors.New("stream ended without terminal end event")
	}
	return text.String(), sessionID, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func grokWarnings(stderr string) []string {
	if strings.TrimSpace(stderr) == "" {
		return []string{}
	}
	return []string{"grok emitted non-fatal stderr; suppressed from ProviderResult stderr"}
}

func redactGrokDiagnostics(value string) string {
	redacted := stripANSI(value)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		redacted = strings.ReplaceAll(redacted, home, "~")
	}
	redacted = redactQueryParam(redacted, "sc_token")
	return redacted
}

func stripANSI(value string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range value {
		if inEscape {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
			}
			continue
		}
		if r == '\x1b' {
			inEscape = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func redactQueryParam(value string, key string) string {
	needle := key + "="
	var b strings.Builder
	offset := 0
	for {
		idx := strings.Index(value[offset:], needle)
		if idx < 0 {
			b.WriteString(value[offset:])
			return b.String()
		}
		idx += offset
		start := idx + len(needle)
		end := start
		for end < len(value) {
			switch value[end] {
			case '&', '"', '\'', ' ', '\n', '\r', '\t', ')':
				goto done
			default:
				end++
			}
		}
	done:
		b.WriteString(value[offset:start])
		b.WriteString("<redacted>")
		offset = end
	}
}

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}

func grokSupportsEffort(model string, effort contract.Effort) bool {
	// Only verified single-model reasoning-depth IDs. Multi-agent models use
	// agent-count semantics and must not receive --reasoning-effort here.
	switch effort {
	case contract.EffortLow, contract.EffortMedium, contract.EffortHigh:
	default:
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(model))
	if normalized == "" {
		return false
	}
	if strings.Contains(normalized, "multi") || strings.Contains(normalized, "agent") {
		return false
	}
	switch normalized {
	case "grok-4.5", "grok-4", "grok4.5", "grok4":
		return true
	default:
		return false
	}
}

func effortModelLabel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "default"
	}
	return model
}
