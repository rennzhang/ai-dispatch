package grok

import (
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

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	bin, err := grokBinary()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := []string{
		bin,
		"--output-format", "json",
	}
	if err := appendGrokOptions(&args, req.ProviderOptions); err != nil {
		return runtime.CommandSpec{}, err
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
	if effort := strings.TrimSpace(opts["effort"]); effort != "" {
		*args = append(*args, "--reasoning-effort", effort)
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
	if subagents := strings.TrimSpace(opts["subagents"]); subagents != "" {
		switch subagents {
		case "off":
			*args = append(*args, "--no-subagents")
		case "on":
		default:
			return fmt.Errorf("grok.subagents must be on or off")
		}
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
	text, sessionID, parseErr := parseGrokJSON(stdout)
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	ok := run.ExitCode == 0 && parseErr == nil && strings.TrimSpace(text) != ""
	resultStderr := ""
	warnings := grokWarnings(stderr)
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		resultStderr = stderr
		if strings.TrimSpace(resultStderr) == "" {
			resultStderr = diagnostics.TimeoutMessage("Grok", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classifiedStdout := stdout
		classifiedStderr := redactGrokDiagnostics(stderr)
		if run.ExitCode == 0 && parseErr != nil {
			classifiedStdout = "malformed json: " + parseErr.Error()
		}
		classified := diagnostics.Classify("Grok", classifiedStdout, classifiedStderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "grok")
		resultStderr = classified.Stderr
		if run.ExitCode == 0 && parseErr != nil {
			resultStderr = "Grok returned malformed JSON despite --output-format json: " + parseErr.Error()
		} else if resultStderr == "Grok returned no successful result" {
			resultStderr = diagnostics.NoResultMessage("Grok", stdout, redactGrokDiagnostics(stderr), run.ExitCode)
		}
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

func parseGrokJSON(stdout string) (string, string, error) {
	var event map[string]any
	trimmed := strings.TrimSpace(stdout)
	if trimmed == "" {
		return "", "", errors.New("empty stdout")
	}
	if err := json.Unmarshal([]byte(trimmed), &event); err != nil {
		return "", "", err
	}
	text, _ := event["text"].(string)
	sessionID, _ := event["sessionId"].(string)
	if sessionID == "" {
		sessionID, _ = event["session_id"].(string)
	}
	return text, sessionID, nil
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
