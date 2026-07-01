package antigravity

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/diagnostics"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

type Provider struct{}

func (Provider) Name() string { return "antigravity" }

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	driver, err := agyDriverCommand()
	if err != nil {
		return runtime.CommandSpec{}, err
	}
	args := append([]string{}, driver...)
	if req.Target.Model != "" {
		args = append(args, "--model", req.Target.Model)
	}
	if req.SessionID != "" {
		args = append(args, "--session-id", req.SessionID)
	}
	if req.CWD != "" {
		args = append(args, "--project", req.CWD)
	}
	if req.TimeoutSeconds > 0 {
		args = append(args, "--print-timeout", durationArg(req.TimeoutSeconds))
	}
	if bin := providerOpt(req, "bin"); bin != "" {
		args = append(args, "--agy-bin", bin)
	}
	if root := providerOpt(req, "root"); root != "" {
		args = append(args, "--agy-root", root)
	}
	if req.PromptFile != "" {
		args = append(args, "--prompt-file", req.PromptFile)
	} else if req.Prompt != "" {
		args = append(args, "--prompt", req.Prompt)
	}
	return runtime.CommandSpec{Args: args}, nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	text, sessionID, model := parseAgyDriverStream(string(run.Stdout))
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
			stderr = diagnostics.TimeoutMessage("Antigravity agy", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classified := diagnostics.Classify("Antigravity", string(run.Stdout), stderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "antigravity")
		stderr = classified.Stderr
		if stderr == "Antigravity returned no successful result" {
			stderr = diagnostics.NoResultMessage("Antigravity", string(run.Stdout), string(run.Stderr), run.ExitCode)
		}
	}
	if model == "" {
		model = req.Target.Model
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "antigravity",
		ModelUsed:       model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("antigravity", model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "antigravity",
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

func parseAgyDriverStream(stdout string) (text string, sessionID string, model string) {
	texts := []string{}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			texts = append(texts, line)
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if sid, ok := event["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
		if m, ok := event["model"].(string); ok && m != "" {
			model = m
		}
		switch event["event"] {
		case "assistant_text":
			if t, ok := event["text"].(string); ok && strings.TrimSpace(t) != "" {
				texts = append(texts, t)
			}
		case "done":
			if t, ok := event["text"].(string); ok && strings.TrimSpace(t) != "" {
				texts = []string{t}
			}
		}
	}
	return strings.Join(texts, ""), sessionID, model
}

func agyDriverCommand() ([]string, error) {
	if value := os.Getenv("AI_DISPATCH_AGY_GO_DRIVER"); value != "" {
		return []string{value}, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return []string{exe, "__agy-driver"}, nil
}

func durationArg(seconds int) string {
	if seconds <= 0 {
		return ""
	}
	return fmt.Sprintf("%ds", seconds)
}

func providerOpt(req providers.BuildRequest, key string) string {
	if req.ProviderOptions == nil {
		return ""
	}
	return req.ProviderOptions[key]
}

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}
