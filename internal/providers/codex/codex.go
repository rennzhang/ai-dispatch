package codex

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

func (Provider) Name() string { return "codex" }

func (Provider) Build(req providers.BuildRequest) (runtime.CommandSpec, error) {
	args := []string{
		"codex",
		"exec",
		"--dangerously-bypass-approvals-and-sandbox",
		"--json",
		"-c",
		`model_reasoning_effort="high"`,
	}
	if req.Target.Model != "" {
		if strings.HasPrefix(req.Target.Model, "openrouter/") {
			return runtime.CommandSpec{}, fmt.Errorf("codex provider cannot run OpenRouter model %q; use an OpenCode target instead", req.Target.Model)
		}
		args = append(args, "--model", req.Target.Model)
	}
	var stdin []byte
	if req.SessionID != "" {
		args = append(args, "resume", req.SessionID)
	}
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return runtime.CommandSpec{}, fmt.Errorf("cannot read prompt file for codex: %w", err)
		}
		stdin = data
		args = append(args, "-")
	} else if req.Prompt != "" {
		args = append(args, req.Prompt)
	}
	return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil), Stdin: stdin}, nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	stdout := string(run.Stdout)
	text, sessionID := parseCodexStream(stdout)
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
			stderr = diagnostics.TimeoutMessage("Codex", run.FixedTimeout, run.ActivityTimeout, req.TimeoutSeconds, req.ActivityTimeoutSeconds)
		}
	} else if !ok {
		classified := diagnostics.Classify("Codex", stdout, stderr, run.Error)
		status = classified.Status
		f := classified.Class
		failure = &f
		next = contract.NextActionForFailure(f, "codex")
		stderr = classified.Stderr
		if stderr == "Codex returned no successful result" {
			stderr = diagnostics.NoResultMessage("Codex", stdout, string(run.Stderr), run.ExitCode)
		}
	}
	return contract.ProviderResult{
		SchemaVersion:   "2.0",
		OK:              ok,
		Status:          status,
		Text:            text,
		ProviderUsed:    "codex",
		ModelUsed:       req.Target.Model,
		SessionID:       sessionID,
		RequestedTarget: req.Target.Requested,
		RouteTrace:      []string{routeLabel("codex", req.Target.Model)},
		RouteSteps: []contract.RouteStep{{
			Provider:   "codex",
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

func parseCodexStream(stdout string) (string, string) {
	fallbackTexts := []string{}
	finalAgentMessage := ""
	sawAgentMessage := false
	sessionID := ""
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			if line != "" {
				fallbackTexts = append(fallbackTexts, line)
			}
			continue
		}
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		if sid, ok := event["session_id"].(string); ok && sid != "" {
			sessionID = sid
		}
		if sid, ok := event["sessionId"].(string); ok && sid != "" {
			sessionID = sid
		}
		if sid, ok := event["thread_id"].(string); ok && sid != "" {
			sessionID = sid
		}
		if t, ok := event["text"].(string); ok && t != "" {
			fallbackTexts = append(fallbackTexts, t)
		}
		if msg, ok := event["message"].(string); ok && msg != "" {
			fallbackTexts = append(fallbackTexts, msg)
		}
		if item, ok := event["item"].(map[string]any); ok {
			if sid, ok := item["thread_id"].(string); ok && sid != "" {
				sessionID = sid
			}
			if item["type"] == "agent_message" {
				if t, ok := item["text"].(string); ok && t != "" {
					finalAgentMessage = t
					sawAgentMessage = true
				}
			}
		}
	}
	if sawAgentMessage {
		return finalAgentMessage, sessionID
	}
	return strings.Join(fallbackTexts, ""), sessionID
}

func routeLabel(provider string, model string) string {
	if model == "" {
		return provider
	}
	return provider + ":" + model
}
