package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/diagnostics"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

const (
	agyModelsQueryTimeout = 5 * time.Second
	agyModelsCacheTTL     = 5 * time.Minute
)

var agyEffortLabelRE = regexp.MustCompile(`^(.+) \((Low|Medium|High)\)$`)

type Provider struct{}

func (Provider) Name() string { return "antigravity" }

func (Provider) ResolveEffort(ctx context.Context, req providers.EffortRequest) providers.EffortResolution {
	requested := contract.NormalizeEffort(req.Requested)
	appliedModel, baseOK := resolveAgyAppliedModel(req.Model)

	// Custom antigravity.bin is not the default capability catalog source.
	// Until the resolver queries that specific binary, explicit effort stays auto.
	if customBin := strings.TrimSpace(req.ProviderOptions["bin"]); customBin != "" {
		if requested == contract.EffortAuto {
			return providers.EffortAuto(requested, appliedModel)
		}
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s cannot be confirmed with custom antigravity.bin; applied auto", requested))
	}

	if requested == contract.EffortAuto {
		return providers.EffortAuto(requested, appliedModel)
	}
	wantLevel, ok := antigravityEffortLevel(requested)
	if !ok {
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s is not supported by antigravity/%s; applied auto", requested, effortModelLabel(req.Model)))
	}
	if strings.TrimSpace(req.Model) == "" {
		return providers.EffortFallback(requested, "",
			fmt.Sprintf("effort %s cannot be applied without a selected antigravity model; applied auto", requested))
	}
	if !baseOK || appliedModel == "" {
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s could not be confirmed for antigravity/%s; applied auto", requested, effortModelLabel(req.Model)))
	}
	family, currentLevel, ok := parseAgyEffortLabel(appliedModel)
	if !ok {
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s is not supported by antigravity/%s; applied auto", requested, effortModelLabel(req.Model)))
	}
	targetLabel := family + " (" + wantLevel + ")"
	if currentLevel == wantLevel {
		return providers.EffortExact(requested, targetLabel)
	}
	labels, err := loadAgyModelLabels(ctx)
	if err != nil {
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s could not be confirmed for antigravity/%s: %s; applied auto", requested, effortModelLabel(req.Model), compactAgyQueryError(err)))
	}
	if !containsExactString(labels, targetLabel) {
		return providers.EffortFallback(requested, appliedModel,
			fmt.Sprintf("effort %s is not supported by antigravity/%s; applied auto", requested, effortModelLabel(req.Model)))
	}
	return providers.EffortExact(requested, targetLabel)
}

// resolveAgyAppliedModel maps known aliases to final agy labels for the dispatch
// path. Empty model stays empty (no --model override). Unknown values keep the
// original token so the driver can fail closed at the direct entry boundary.
func resolveAgyAppliedModel(model string) (string, bool) {
	if strings.TrimSpace(model) == "" {
		return "", true
	}
	label, err := resolveModelLabel(model)
	if err != nil {
		return model, false
	}
	return label, true
}

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
	return runtime.CommandSpec{Args: args, Env: runtime.SanitizedEnv(nil)}, nil
}

func (Provider) Parse(run runtime.RunResult, req providers.BuildRequest) contract.ProviderResult {
	text, sessionID, model, warnings := parseAgyDriverStream(string(run.Stdout))
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
		Warnings:     warnings,
		NextAction:   next,
		FailureClass: failure,
	}
}

func parseAgyDriverStream(stdout string) (text string, sessionID string, model string, warnings []string) {
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
		case "warning":
			if message, ok := event["message"].(string); ok && strings.TrimSpace(message) != "" {
				warnings = appendUnique(warnings, strings.TrimSpace(message))
			}
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
	return strings.Join(texts, ""), sessionID, model, warnings
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
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

func antigravityEffortLevel(effort contract.Effort) (string, bool) {
	switch effort {
	case contract.EffortLow:
		return "Low", true
	case contract.EffortMedium:
		return "Medium", true
	case contract.EffortHigh:
		return "High", true
	default:
		return "", false
	}
}

func parseAgyEffortLabel(label string) (family string, level string, ok bool) {
	matches := agyEffortLabelRE.FindStringSubmatch(strings.TrimSpace(label))
	if len(matches) != 3 {
		return "", "", false
	}
	return matches[1], matches[2], true
}

type agyModelsCacheEntry struct {
	fetchedAt time.Time
	labels    []string
}

var (
	agyModelsCacheMu sync.Mutex
	agyModelsCache   agyModelsCacheEntry
	queryAgyModels   = defaultQueryAgyModels
)

func loadAgyModelLabels(ctx context.Context) ([]string, error) {
	agyModelsCacheMu.Lock()
	if len(agyModelsCache.labels) > 0 && time.Since(agyModelsCache.fetchedAt) < agyModelsCacheTTL {
		labels := append([]string{}, agyModelsCache.labels...)
		agyModelsCacheMu.Unlock()
		return labels, nil
	}
	agyModelsCacheMu.Unlock()

	queryCtx, cancel := context.WithTimeout(ctx, agyModelsQueryTimeout)
	defer cancel()
	raw, err := queryAgyModels(queryCtx)
	if err != nil {
		return nil, err
	}
	labels := parseAgyModelsOutput(raw)
	if len(labels) == 0 {
		return nil, fmt.Errorf("empty models output")
	}
	agyModelsCacheMu.Lock()
	agyModelsCache = agyModelsCacheEntry{fetchedAt: time.Now(), labels: append([]string{}, labels...)}
	agyModelsCacheMu.Unlock()
	return labels, nil
}

func defaultQueryAgyModels(ctx context.Context) ([]byte, error) {
	bin, err := resolveAgyBinary("")
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, bin, "models")
	cmd.Env = runtime.SanitizedEnv(nil)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return stdout.Bytes(), nil
}

func parseAgyModelsOutput(raw []byte) []string {
	labels := []string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		labels = append(labels, line)
	}
	return labels
}

func containsExactString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func compactAgyQueryError(err error) string {
	msg := strings.Join(strings.Fields(err.Error()), " ")
	runes := []rune(msg)
	if len(runes) > 160 {
		runes = runes[:160]
	}
	return string(runes)
}

func effortModelLabel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "default"
	}
	return model
}

func resetAgyModelsCacheForTest() {
	agyModelsCacheMu.Lock()
	defer agyModelsCacheMu.Unlock()
	agyModelsCache = agyModelsCacheEntry{}
}
