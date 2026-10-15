package omp

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

// Provider adapts the oh-my-pi / omp CLI as a first-class ai-dispatch provider.
type Provider struct{}

func (Provider) Name() string { return "omp" }

func (Provider) ResolveEffort(_ context.Context, req providers.EffortRequest) providers.EffortResolution {
	return providers.ResolveEffortFromDeclared(req, "omp")
}

func (Provider) ResolveFast(_ context.Context, req providers.FastRequest) providers.FastResolution {
	return providers.ResolveFastFromDeclared(req, "omp")
}

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	bin, err := ompBinary()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := []string{bin, "--print", "--mode", "json"}
	if err := appendOMPOptions(&args, req.ProviderOptions, req.SessionID); err != nil {
		return runtime.CommandSpec{}, err
	}
	if req.Target.Model != "" {
		args = append(args, "--model", req.Target.Model)
	}
	if thinking, ok := thinkingFlag(req.Effort); ok {
		args = append(args, "--thinking", thinking)
	}
	if req.CWD != "" {
		args = append(args, "--cwd", req.CWD)
	}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}

	var stdin []byte
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return runtime.CommandSpec{}, fmt.Errorf("cannot read prompt file for omp: %s", sanitizePromptFileError(err))
		}
		stdin = data
	} else if req.Prompt != "" {
		stdin = []byte(req.Prompt)
	}
	// omp treats an inherited pipe as "read prompt from stdin" and will hang
	// waiting for EOF. runtime only creates a stdin pipe when len(Stdin) > 0,
	// so always provide prompt bytes here (CLI requires a prompt for send/resume).
	if len(stdin) == 0 {
		return runtime.CommandSpec{}, fmt.Errorf("omp requires a prompt or --prompt-file")
	}
	return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil), Stdin: stdin}, nil
}

func appendOMPOptions(args *[]string, opts map[string]string, sessionID string) error {
	approval := strings.TrimSpace(opts["approval"])
	if approval == "" {
		approval = "always"
	}
	switch approval {
	case "always":
		*args = append(*args, "--auto-approve")
	case "default":
	default:
		return fmt.Errorf("omp.approval must be always or default")
	}
	session := strings.TrimSpace(opts["session"])
	if session == "" {
		session = "persist"
	}
	switch session {
	case "persist":
	case "none":
		if strings.TrimSpace(sessionID) != "" {
			return fmt.Errorf("omp.session=none cannot resume a session")
		}
		*args = append(*args, "--no-session")
	default:
		return fmt.Errorf("omp.session must be persist or none")
	}
	return nil
}

// thinkingFlag maps ai-dispatch effort to omp --thinking. auto omits the flag.
// OMP uses "off" where ai-dispatch uses "none".
func thinkingFlag(effort contract.Effort) (string, bool) {
	switch contract.NormalizeEffort(effort) {
	case contract.EffortAuto:
		return "", false
	case contract.EffortNone:
		return "off", true
	default:
		return string(contract.NormalizeEffort(effort)), true
	}
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	stdout := string(run.Stdout)
	text, sessionID, model, usage := parseOMPStream(stdout)
	if model == "" {
		model = req.Target.Model
	}
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	stderr := string(run.Stderr)
	ok := run.ExitCode == 0 && strings.TrimSpace(text) != ""
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		if strings.TrimSpace(stderr) == "" {
			stderr = diagnostics.TimeoutMessage("OMP", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classified := diagnostics.Classify("OMP", stdout, stderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "omp")
		stderr = classified.Stderr
		if stderr == "OMP returned no successful result" {
			stderr = diagnostics.NoResultMessage("OMP", stdout, string(run.Stderr), run.ExitCode)
		}
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "omp",
		ModelUsed:       model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("omp", model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "omp",
			Model:      model,
			Status:     status,
			DurationMS: run.DurationMS,
		}},
		Usage:        usage,
		ExitCode:     run.ExitCode,
		DurationMS:   run.DurationMS,
		Stderr:       stderr,
		Warnings:     []string{},
		NextAction:   next,
		FailureClass: failure,
	}
}

func parseOMPStream(stdout string) (text string, sessionID string, model string, usage *contract.UsageInfo) {
	texts := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if event["type"] == "session" {
			if id, ok := event["id"].(string); ok && id != "" {
				sessionID = id
			}
		}
		if message, ok := event["message"].(map[string]any); ok {
			if role, _ := message["role"].(string); role == "assistant" {
				if t := assistantText(message); t != "" {
					texts = append(texts, t)
				}
				if m, ok := message["model"].(string); ok && m != "" {
					if provider, ok := message["provider"].(string); ok && provider != "" {
						model = provider + "/" + m
					} else {
						model = m
					}
				}
				if raw, ok := message["usage"].(map[string]any); ok {
					if parsed := parseOMPUsage(raw); parsed != nil {
						usage = parsed
					}
				}
			}
		}
		if event["type"] == "turn_end" || event["type"] == "agent_end" {
			if message, ok := event["message"].(map[string]any); ok {
				if t := assistantText(message); t != "" {
					texts = append(texts, t)
				}
			}
		}
	}
	if len(texts) == 0 {
		return "", sessionID, model, usage
	}
	// Prefer the last assistant text chunk (final turn).
	return texts[len(texts)-1], sessionID, model, usage
}

func assistantText(message map[string]any) string {
	content, ok := message["content"].([]any)
	if !ok {
		return ""
	}
	parts := []string{}
	for _, item := range content {
		part, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if part["type"] != "text" {
			continue
		}
		if t, ok := part["text"].(string); ok && t != "" {
			parts = append(parts, t)
		}
	}
	return strings.Join(parts, "")
}

func parseOMPUsage(raw map[string]any) *contract.UsageInfo {
	usage := &contract.UsageInfo{}
	if v, ok := raw["input"].(float64); ok {
		usage.InputTokens = int(v)
	}
	if v, ok := raw["output"].(float64); ok {
		usage.OutputTokens = int(v)
	}
	if v, ok := raw["cacheRead"].(float64); ok {
		usage.CacheReadTokens = int(v)
	}
	if v, ok := raw["cacheWrite"].(float64); ok {
		usage.CacheCreationTokens = int(v)
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.CacheReadTokens == 0 && usage.CacheCreationTokens == 0 {
		return nil
	}
	return usage
}

func ompBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("AI_DISPATCH_OMP_BIN")); explicit != "" {
		if path, err := executablePath(explicit, "AI_DISPATCH_OMP_BIN override"); err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
	if path, err := exec.LookPath("omp"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		for _, candidate := range []string{
			filepath.Join(home, ".bun", "bin", "omp"),
			filepath.Join(home, ".local", "bin", "omp"),
		} {
			if path, err := executablePath(candidate, "omp fallback"); err == nil {
				return path, nil
			}
		}
	}
	return "", fmt.Errorf("omp binary not found; install oh-my-pi (omp) or set AI_DISPATCH_OMP_BIN")
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

// SuggestEffortLevels returns optional init/detect hints for known OMP models.
// Execution never consults this list; config ModelRoute.efforts is authority.
func SuggestEffortLevels(model string) []string {
	id := strings.ToLower(strings.TrimSpace(model))
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	switch id {
	case "deepseek-v4.1-flash", "deepseek-v4-flash", "deepseek-v4-pro", "deepseek-v4-flash-vision-exp", "deepseek-flash":
		return []string{"low", "high", "max"}
	default:
		return nil
	}
}
