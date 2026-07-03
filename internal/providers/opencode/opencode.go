package opencode

import (
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

func (Provider) Name() string { return "opencode" }

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	format := strings.TrimSpace(req.ProviderOptions["format"])
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "default" {
		return runtime.CommandSpec{}, fmt.Errorf("unsupported opencode.format: %s", format)
	}

	bin, err := openCodeBinary()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := []string{bin, "run", "--format", format, "--title", "ai-dispatch", "--pure", "--auto"}
	if req.Target.Model != "" {
		args = append(args, "--model", req.Target.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--session", req.SessionID)
	}
	if req.PromptFile != "" {
		args = append(args, "--file", req.PromptFile)
		args = append(args, "Read the attached prompt file and follow it exactly.")
		return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil)}, nil
	}
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}
	return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil)}, nil
}

func openCodeBinary() (string, error) {
	if explicit := strings.TrimSpace(os.Getenv("AI_DISPATCH_OPENCODE_BIN")); explicit != "" {
		if path, err := executablePath(explicit, "AI_DISPATCH_OPENCODE_BIN override"); err == nil {
			return path, nil
		} else {
			return "", err
		}
	}
	if path, err := exec.LookPath("opencode"); err == nil {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err == nil {
		if path, err := executablePath(filepath.Join(home, ".opencode", "bin", "opencode"), "opencode fallback"); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("opencode binary not found; install OpenCode or set AI_DISPATCH_OPENCODE_BIN")
}

func executablePath(candidate string, label string) (string, error) {
	if path, err := exec.LookPath(candidate); err == nil {
		return path, nil
	}
	if !strings.Contains(candidate, string(os.PathSeparator)) {
		return "", fmt.Errorf("%s is not executable or not found", label)
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() {
		return "", fmt.Errorf("%s is not executable or not found", label)
	}
	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable or not found", label)
	}
	return candidate, nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	text, sessionID := parseOpenCodeStream(string(run.Stdout))
	stderr := string(run.Stderr)
	status := contract.StatusSuccess
	var failure *contract.FailureClass
	next := contract.NextDone
	ok := run.ExitCode == 0 && strings.TrimSpace(text) != ""
	if run.TimedOut {
		status = contract.StatusTimeout
		f := contract.FailureTimeout
		failure = &f
		next = contract.NextRetry
		ok = false
		if strings.TrimSpace(stderr) == "" {
			stderr = diagnostics.TimeoutMessage("OpenCode", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classified := diagnostics.Classify("OpenCode", string(run.Stdout), stderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "opencode")
		stderr = classified.Stderr
		if stderr == "OpenCode returned no successful result" {
			stderr = diagnostics.NoResultMessage("OpenCode", string(run.Stdout), string(run.Stderr), run.ExitCode)
		}
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "opencode",
		ModelUsed:       req.Target.Model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("opencode", req.Target.Model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "opencode",
			Model:      req.Target.Model,
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

func parseOpenCodeStream(stdout string) (string, string) {
	texts := []string{}
	sessionID := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			if line != "" {
				texts = append(texts, line)
			}
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if sid, ok := event["sessionID"].(string); ok && sid != "" {
			sessionID = sid
		}
		if sid, ok := event["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
		if text := openCodeText(event); text != "" {
			texts = append(texts, text)
		}
	}
	return strings.Join(texts, ""), sessionID
}

func openCodeText(event map[string]any) string {
	if message, ok := event["message"].(map[string]any); ok {
		if role, _ := message["role"].(string); role != "" && role != "assistant" {
			return ""
		}
	}
	if text, ok := event["text"].(string); ok && text != "" {
		return text
	}
	if result, ok := event["result"].(string); ok && result != "" {
		return result
	}
	if part, ok := event["part"].(map[string]any); ok {
		if text := textFromPart(part); text != "" {
			return text
		}
	}
	if message, ok := event["message"].(map[string]any); ok {
		if text := textFromContent(message["content"]); text != "" {
			return text
		}
		if parts, ok := message["parts"].([]any); ok {
			for _, item := range parts {
				if text := textFromPartMap(item); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func textFromPart(part map[string]any) string {
	if text, ok := part["text"].(string); ok && text != "" {
		return text
	}
	if text, ok := part["content"].(string); ok && text != "" {
		return text
	}
	if text, ok := part["delta"].(string); ok && text != "" {
		return text
	}
	if delta, ok := part["delta"].(map[string]any); ok {
		if text, ok := delta["text"].(string); ok && text != "" {
			return text
		}
	}
	return textFromContent(part["content"])
}

func textFromPartMap(value any) string {
	part, ok := value.(map[string]any)
	if !ok {
		return ""
	}
	return textFromPart(part)
}

func textFromContent(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []any:
		for _, item := range typed {
			if text := textFromPartMap(item); text != "" {
				return text
			}
		}
	}
	return ""
}

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}
