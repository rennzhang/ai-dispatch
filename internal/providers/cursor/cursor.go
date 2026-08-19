package cursor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/diagnostics"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

type Provider struct{}

func (Provider) Name() string { return "cursor" }

func (Provider) ResolveEffort(_ context.Context, req providers.EffortRequest) providers.EffortResolution {
	requested := contract.NormalizeEffort(req.Requested)
	if requested == contract.EffortAuto {
		return providers.EffortAuto(requested, req.Model)
	}
	// Cursor Agent model ids carry effort suffixes (e.g. claude-fable-5-thinking-high).
	// The CLI has no --effort flag; the resolved model already encodes the effort tier,
	// so explicit effort cannot be applied exactly and stays auto.
	return providers.EffortFallback(requested, req.Model,
		fmt.Sprintf("effort %s is not supported by the Cursor Agent CLI; the resolved model %s already encodes its effort tier", requested, effortModelLabel(req.Model)))
}

func (Provider) ResolveFast(_ context.Context, req providers.FastRequest) providers.FastResolution {
	return providers.FastFallback(req.Requested, "cursor", req.Model)
}

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	bin, err := cursorBinary()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := []string{bin, "--print", "--output-format", "stream-json", "--trust"}
	if approval := strings.TrimSpace(req.ProviderOptions["approval"]); approval != "" {
		switch approval {
		case "always":
			args = append(args, "--force")
		case "default":
		default:
			return runtime.CommandSpec{}, fmt.Errorf("cursor.approval must be always or default")
		}
	}
	if req.Target.Model != "" {
		args = append(args, "--model", req.Target.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if req.CWD != "" {
		args = append(args, "--workspace", req.CWD)
	}
	var stdin []byte
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			// Do not wrap the raw OS error: it embeds the absolute prompt path.
			return runtime.CommandSpec{}, fmt.Errorf("cannot read prompt file for cursor: %s", sanitizePromptFileError(err))
		}
		stdin = data
	} else if req.Prompt != "" {
		stdin = []byte(req.Prompt)
	}
	return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil), Stdin: stdin}, nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	stdout := string(run.Stdout)
	stderr := redactWorkspace(string(run.Stderr), req.CWD)
	text, sessionID, model, usage, isError := parseCursorResult(stdout)
	text = redactWorkspace(text, req.CWD)
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	ok := run.ExitCode == 0 && !isError && strings.TrimSpace(text) != ""
	if model == "" {
		model = req.Target.Model
	}
	resultStderr := stderr
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		if strings.TrimSpace(resultStderr) == "" {
			resultStderr = diagnostics.TimeoutMessage("Cursor", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		diagnosticStderr := resultStderr
		if isError && strings.TrimSpace(text) != "" && strings.TrimSpace(diagnosticStderr) == "" {
			// A JSON result with is_error=true carries the real error body in
			// the result field; surface it instead of a generic no-result line.
			diagnosticStderr = text
		}
		classified := diagnostics.Classify("Cursor", stdout, diagnosticStderr, redactWorkspace(run.Error, req.CWD))
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "cursor")
		resultStderr = classified.Stderr
		if resultStderr == "Cursor returned no successful result" {
			resultStderr = diagnostics.NoResultMessage("Cursor", stdout, string(run.Stderr), run.ExitCode)
		}
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "cursor",
		ModelUsed:       model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("cursor", model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "cursor",
			Model:      model,
			Status:     status,
			DurationMS: run.DurationMS,
		}},
		Usage:        usage,
		ExitCode:     run.ExitCode,
		DurationMS:   run.DurationMS,
		Stderr:       resultStderr,
		Warnings:     []string{},
		NextAction:   next,
		FailureClass: failure,
	}
}

func parseCursorResult(stdout string) (text string, sessionID string, model string, usage *contract.UsageInfo, isError bool) {
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["type"] != "result" {
			continue
		}
		if v, ok := event["is_error"].(bool); ok {
			isError = v
		}
		if v, ok := event["session_id"].(string); ok {
			sessionID = v
		}
		if v, ok := event["result"].(string); ok {
			text = v
		}
		if v, ok := event["model"].(string); ok {
			model = v
		}
		if raw, ok := event["usage"].(map[string]any); ok {
			usage = parseCursorUsage(raw)
		}
		break
	}
	return text, sessionID, model, usage, isError
}

func parseCursorUsage(raw map[string]any) *contract.UsageInfo {
	usage := &contract.UsageInfo{}
	if v, ok := raw["inputTokens"].(float64); ok {
		usage.InputTokens = int(v)
	}
	if v, ok := raw["outputTokens"].(float64); ok {
		usage.OutputTokens = int(v)
	}
	if v, ok := raw["cacheReadTokens"].(float64); ok {
		usage.CacheReadTokens = int(v)
	}
	if v, ok := raw["cacheWriteTokens"].(float64); ok {
		usage.CacheCreationTokens = int(v)
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheCreationTokens == 0 {
		return nil
	}
	return usage
}

func cursorBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("AI_DISPATCH_CURSOR_BIN")); explicit != "" {
		if path, err := executablePath(explicit, "AI_DISPATCH_CURSOR_BIN override"); err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
	if path, err := exec.LookPath("cursor-agent"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, candidate := range []string{
			filepath.Join(home, ".local", "bin", "cursor-agent"),
			filepath.Join(home, ".cursor", "bin", "cursor-agent"),
		} {
			if path, err := executablePath(candidate, "cursor-agent fallback"); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("cursor binary not found; install Cursor Agent or set AI_DISPATCH_CURSOR_BIN")
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

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}

func sanitizePromptFileError(err error) string {
	if err == nil {
		return "unreadable"
	}
	return "unreadable prompt file"
}

func redactWorkspace(stderr string, cwd string) string {
	if stderr == "" || cwd == "" {
		return stderr
	}
	return strings.ReplaceAll(stderr, cwd, "<workspace>")
}

func effortModelLabel(model string) string {
	if model == "" {
		return "(default)"
	}
	return model
}
