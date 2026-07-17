package dispatch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/output"
	"github.com/rennzhang/ai-dispatch/internal/progress"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/providers/antigravity"
	"github.com/rennzhang/ai-dispatch/internal/providers/claude"
	"github.com/rennzhang/ai-dispatch/internal/providers/codex"
	"github.com/rennzhang/ai-dispatch/internal/providers/grok"
	"github.com/rennzhang/ai-dispatch/internal/providers/opencode"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
	execruntime "github.com/rennzhang/ai-dispatch/internal/runtime"
)

type Options struct {
	ProgressWriter io.Writer
	Context        context.Context
}

func Execute(req contract.DispatchRequest) contract.ProviderResult {
	return ExecuteWithOptions(req, Options{})
}

func ExecuteWithOptions(req contract.DispatchRequest, opts Options) contract.ProviderResult {
	dispatchStarted := time.Now()
	// Normalize empty effort at the shared entry so CLI and programmatic callers share one contract.
	req.Effort = contract.NormalizeEffort(req.Effort)

	baseCtx := opts.Context
	stopSignals := func() {}
	if baseCtx == nil {
		baseCtx, stopSignals = signal.NotifyContext(context.Background(), dispatchSignals()...)
	}
	defer stopSignals()
	dispatchCtx := baseCtx
	stopTimeout := func() {}
	if req.TimeoutSeconds > 0 {
		dispatchCtx, stopTimeout = context.WithTimeout(baseCtx, seconds(req.TimeoutSeconds))
	}
	defer stopTimeout()

	target, err := resolveTarget(req)
	if err != nil {
		return completeDispatchResult(req, opts, contract.ErrorResult(contract.StatusError, contract.FailureInput, err.Error(), 2))
	}
	if result, done := contextTargetResult(dispatchCtx, target, elapsedMS(dispatchStarted)); done {
		return completeDispatchResult(req, opts, result)
	}
	if os.Getenv("AI_DISPATCH_GO_PROVIDER_EXECUTION") != "on" {
		result := contract.ErrorResult(contract.StatusDisabled, contract.FailureConfig, "go provider execution is disabled; set AI_DISPATCH_GO_PROVIDER_EXECUTION=on for explicit smoke", 3)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		resolution := providers.EffortAuto(req.Effort, target.Model)
		ensureRouteMetadata(&result, target, 0)
		applyEffortResolution(&result, resolution)
		return completeDispatchResult(req, opts, result)
	}
	result := executeCandidates(dispatchCtx, req, target, opts)
	if req.OutputFile != "" && result.OK {
		if err := output.WriteFile(req.OutputFile, result); err != nil {
			failure := contract.FailureRuntime
			result.OK = false
			result.Status = contract.StatusError
			result.FailureClass = &failure
			result.NextAction = contract.NextInspectError
			result.ExitCode = 1
			result.Stderr = "failed to write output file: " + err.Error()
		} else {
			result.OutputFile = req.OutputFile
		}
	}
	return completeDispatchResult(req, opts, result)
}

func completeDispatchResult(req contract.DispatchRequest, opts Options, result contract.ProviderResult) contract.ProviderResult {
	if result.RequestedEffort == "" {
		result.RequestedEffort = contract.NormalizeEffort(req.Effort)
	}
	if result.AppliedEffort == "" {
		result.AppliedEffort = contract.EffortAuto
	}
	// Fill empty route-step effort only. Candidate-level values already stamped
	// by executeTarget must be preserved across multi-candidate merges.
	for i := range result.RouteSteps {
		if result.RouteSteps[i].AppliedEffort == "" {
			result.RouteSteps[i].AppliedEffort = result.AppliedEffort
			if result.RouteSteps[i].EffortFallbackReason == "" {
				result.RouteSteps[i].EffortFallbackReason = result.EffortFallbackReason
			}
		}
	}
	if !req.StreamProgress || opts.ProgressWriter == nil {
		return result
	}
	emitter := progress.NewEmitter(result.ProviderUsed, opts.ProgressWriter)
	if result.OK {
		emitter.Emit(contract.ProgressDone, "done", "completed")
	} else {
		emitter.Emit(contract.ProgressError, "error", result.Stderr)
	}
	return result
}

func executeCandidates(ctx context.Context, req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
	return executeCandidatesWith(ctx, req, target, opts, executeTarget)
}

type targetExecutor func(context.Context, contract.DispatchRequest, routing.DispatchTarget, Options) contract.ProviderResult

func executeCandidatesWith(ctx context.Context, req contract.DispatchRequest, target routing.DispatchTarget, opts Options, execute targetExecutor) contract.ProviderResult {
	candidates := routing.CandidateTargets(target)
	var routeTrace []string
	var routeSteps []contract.RouteStep
	var degradeReasons []string
	var routeWarnings []string
	var lastResult contract.ProviderResult
	var lastCandidate routing.DispatchTarget
	lastRouteStepStart := 0
	haveLastResult := false
	pendingDegradeReason := ""
	for i, candidate := range candidates {
		if ctx.Err() != nil && haveLastResult {
			switch {
			case errors.Is(ctx.Err(), context.Canceled):
				applyCanceledContract(&lastResult, lastCandidate, lastResult.DurationMS)
			case errors.Is(ctx.Err(), context.DeadlineExceeded):
				applyTimeoutContract(&lastResult, lastCandidate, lastResult.DurationMS)
			}
			routeSteps = append(routeSteps[:lastRouteStepStart], lastResult.RouteSteps...)
			return finalizeCandidateResult(lastResult, target.Requested, routeTrace, routeSteps, degradeReasons, routeWarnings)
		}
		if result, done := contextTargetResult(ctx, candidate, 0); done {
			routeTrace = append(routeTrace, result.RouteTrace...)
			routeSteps = append(routeSteps, result.RouteSteps...)
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons, routeWarnings)
		}
		if pendingDegradeReason != "" {
			degradeReasons = append(degradeReasons, pendingDegradeReason)
			pendingDegradeReason = ""
		}
		attemptReq := req
		if i > 0 {
			attemptReq.SessionID = ""
			attemptReq.SessionProvider = ""
		}
		attemptStarted := time.Now()
		result := execute(ctx, attemptReq, candidate, opts)
		attemptDuration := maxDurationMS(result.DurationMS, elapsedMS(attemptStarted))
		switch {
		case errors.Is(ctx.Err(), context.Canceled):
			applyCanceledContract(&result, candidate, attemptDuration)
		case errors.Is(ctx.Err(), context.DeadlineExceeded):
			applyTimeoutContract(&result, candidate, attemptDuration)
		}
		routeTrace = append(routeTrace, result.RouteTrace...)
		lastRouteStepStart = len(routeSteps)
		routeSteps = append(routeSteps, result.RouteSteps...)
		for _, warning := range result.Warnings {
			routeWarnings = appendUnique(routeWarnings, warning)
		}
		if ctx.Err() != nil {
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons, routeWarnings)
		}
		if result.OK {
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons, routeWarnings)
		}
		if i == len(candidates)-1 || !shouldTryNextCandidate(req, result) {
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons, routeWarnings)
		}
		lastResult = result
		lastCandidate = candidate
		haveLastResult = true
		pendingDegradeReason = degradeReason(result, candidates[i+1])
	}
	return contract.ErrorResult(contract.StatusError, contract.FailureConfig, "no route candidates available", 2)
}

func finalizeCandidateResult(result contract.ProviderResult, requested string, routeTrace []string, routeSteps []contract.RouteStep, degradeReasons []string, routeWarnings []string) contract.ProviderResult {
	if requested != "" {
		result.RequestedTarget = requested
	}
	if len(routeTrace) > 0 {
		result.RouteTrace = routeTrace
	}
	if len(routeSteps) > 0 {
		result.RouteSteps = routeSteps
	}
	result.Warnings = append([]string{}, routeWarnings...)
	if len(degradeReasons) > 0 {
		reason := strings.Join(degradeReasons, "; ")
		result.Degraded = true
		result.DegradeReason = reason
		if !containsExact(result.Warnings, reason) {
			result.Warnings = append([]string{reason}, result.Warnings...)
		}
	}
	return result
}

func executeTarget(baseCtx context.Context, req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
	targetStarted := time.Now()
	p, ok := providerFor(target.Provider)
	if !ok {
		result := contract.ErrorResult(contract.StatusDisabled, contract.FailureConfig, "provider is not implemented in go runtime: "+target.Provider, 3)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		ensureRouteMetadata(&result, target, 0)
		applyEffortResolution(&result, providers.EffortAuto(req.Effort, target.Model))
		return result
	}
	if result, done := contextTargetResult(baseCtx, target, elapsedMS(targetStarted)); done {
		applyEffortResolution(&result, providers.EffortAuto(req.Effort, target.Model))
		return result
	}

	// Capability queries are bounded so effort resolution cannot stall execution.
	effortCtx, cancelEffort := context.WithTimeout(baseCtx, 5*time.Second)
	resolution := p.ResolveEffort(effortCtx, providers.EffortRequest{
		Model:           target.Model,
		Requested:       req.Effort,
		ProviderOptions: req.ProviderOpts[target.Provider],
	})
	cancelEffort()

	buildTarget := target
	buildTarget.Model = resolution.AppliedModel
	buildReq := providers.BuildRequest{
		Prompt:                 req.Prompt,
		PromptFile:             req.PromptFile,
		Target:                 buildTarget,
		CWD:                    req.CWD,
		SessionID:              req.SessionID,
		TimeoutSeconds:         req.TimeoutSeconds,
		ActivityTimeoutSeconds: req.ActivityTimeoutSeconds,
		Effort:                 resolution.Applied,
		ProviderOptions:        req.ProviderOpts[target.Provider],
	}
	spec, err := p.Build(buildReq)
	if err != nil {
		failure, exitCode := buildFailure(err)
		result := contract.ErrorResult(contract.StatusError, failure, err.Error(), exitCode)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		ensureRouteMetadata(&result, buildTarget, 0)
		applyEffortResolution(&result, resolution)
		return result
	}
	if result, done := contextTargetResult(baseCtx, buildTarget, elapsedMS(targetStarted)); done {
		applyEffortResolution(&result, resolution)
		return result
	}
	spec.CWD = req.CWD
	var emitter *progress.Emitter
	hooks := execruntime.StreamHooks{}
	if req.StreamProgress && opts.ProgressWriter != nil {
		emitter = progress.NewEmitter(target.Provider, opts.ProgressWriter)
		emitter.Emit(contract.ProgressSession, "session", target.Provider+" session started")
		hooks.Stdout = emitter.FeedStdout
		hooks.Stderr = emitter.FeedStderr
	}
	run := withProviderExecutionLock(baseCtx, target.Provider, func() execruntime.RunResult {
		return execruntime.RunProcess(baseCtx, spec, execruntime.RunOptions{
			FixedTimeout:    0,
			ActivityTimeout: seconds(req.ActivityTimeoutSeconds),
		}, hooks)
	})
	if emitter != nil {
		emitter.Close()
	}
	return providerResultFromRun(p, run, buildReq, buildTarget, resolution)
}

func providerResultFromRun(p providers.Provider, run execruntime.RunResult, buildReq providers.BuildRequest, target routing.DispatchTarget, resolution providers.EffortResolution) contract.ProviderResult {
	result := p.Parse(run, buildReq)
	ensureRouteMetadata(&result, target, run.DurationMS)
	applyEffortResolution(&result, resolution)
	if run.Canceled {
		applyCanceledContract(&result, target, run.DurationMS)
		// Preserve effort fields after cancel rewrite.
		applyEffortResolution(&result, resolution)
	}
	applyRuntimeWarnings(&result, run)
	return result
}

func applyCanceledContract(result *contract.ProviderResult, target routing.DispatchTarget, durationMS int64) {
	if result.SchemaVersion == "" {
		result.SchemaVersion = "2.0"
	}
	result.OK = false
	result.Status = contract.StatusError
	result.ExitCode = 130
	result.DurationMS = durationMS
	result.Stderr = "dispatch canceled"
	result.NextAction = contract.NextDone
	result.FailureClass = nil
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	ensureRouteMetadata(result, target, durationMS)
	for i := range result.RouteSteps {
		result.RouteSteps[i].Status = contract.StatusError
		result.RouteSteps[i].DurationMS = durationMS
	}
}

// applyEffortResolution stamps top-level and route-step effort fields owned by dispatch.
// Effort fallback never sets degraded; that remains routing-only.
func applyEffortResolution(result *contract.ProviderResult, resolution providers.EffortResolution) {
	result.RequestedEffort = contract.NormalizeEffort(resolution.Requested)
	result.AppliedEffort = contract.NormalizeEffort(resolution.Applied)
	if resolution.Fallback {
		result.EffortFallbackReason = resolution.Reason
	} else {
		result.EffortFallbackReason = ""
	}
	for i := range result.RouteSteps {
		result.RouteSteps[i].AppliedEffort = result.AppliedEffort
		if resolution.Fallback {
			result.RouteSteps[i].EffortFallbackReason = resolution.Reason
		} else {
			result.RouteSteps[i].EffortFallbackReason = ""
		}
	}
}

func applyRuntimeWarnings(result *contract.ProviderResult, run execruntime.RunResult) {
	if run.CleanupError != "" || (run.CleanupAttempted && !run.CleanupComplete) {
		warning := "runtime_cleanup=incomplete"
		if detail := compactWarningDetail(run.CleanupError); detail != "" {
			warning += " error=" + detail
		}
		appendWarning(result, warning)
	}
	if run.StdoutTruncated || run.StdoutDroppedBytes > 0 {
		appendWarning(result, fmt.Sprintf("runtime_stdout_truncated=true dropped_bytes=%d", run.StdoutDroppedBytes))
	}
	if run.StderrTruncated || run.StderrDroppedBytes > 0 {
		appendWarning(result, fmt.Sprintf("runtime_stderr_truncated=true dropped_bytes=%d", run.StderrDroppedBytes))
	}
}

func appendWarning(result *contract.ProviderResult, warning string) {
	result.Warnings = appendUnique(result.Warnings, warning)
}

func appendUnique(values []string, value string) []string {
	if containsExact(values, value) {
		return values
	}
	return append(values, value)
}

func containsExact(values []string, value string) bool {
	for _, existing := range values {
		if existing == value {
			return true
		}
	}
	return false
}

func compactWarningDetail(value string) string {
	runes := []rune(strings.Join(strings.Fields(value), " "))
	if len(runes) > 240 {
		runes = runes[:240]
	}
	return string(runes)
}

func canceledTargetResult(target routing.DispatchTarget, durationMS int64) contract.ProviderResult {
	result := contract.ProviderResult{
		SchemaVersion: "2.0",
		OK:            false,
		Status:        contract.StatusError,
		ExitCode:      130,
		DurationMS:    durationMS,
		Stderr:        "dispatch canceled",
		Warnings:      []string{},
		NextAction:    contract.NextDone,
	}
	ensureRouteMetadata(&result, target, durationMS)
	return result
}

func timeoutTargetResult(target routing.DispatchTarget, durationMS int64) contract.ProviderResult {
	result := contract.ProviderResult{}
	applyTimeoutContract(&result, target, durationMS)
	return result
}

func applyTimeoutContract(result *contract.ProviderResult, target routing.DispatchTarget, durationMS int64) {
	if result.SchemaVersion == "" {
		result.SchemaVersion = "2.0"
	}
	failure := contract.FailureTimeout
	result.OK = false
	result.Status = contract.StatusTimeout
	result.ExitCode = 124
	result.DurationMS = durationMS
	if strings.TrimSpace(result.Stderr) == "" {
		result.Stderr = "dispatch timeout exceeded"
	}
	result.NextAction = contract.NextRetry
	result.FailureClass = &failure
	if result.Warnings == nil {
		result.Warnings = []string{}
	}
	ensureRouteMetadata(result, target, durationMS)
	for i := range result.RouteSteps {
		result.RouteSteps[i].Status = contract.StatusTimeout
		result.RouteSteps[i].DurationMS = durationMS
	}
}

func contextTargetResult(ctx context.Context, target routing.DispatchTarget, durationMS int64) (contract.ProviderResult, bool) {
	if ctx == nil {
		return contract.ProviderResult{}, false
	}
	switch {
	case errors.Is(ctx.Err(), context.Canceled):
		return canceledTargetResult(target, durationMS), true
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return timeoutTargetResult(target, durationMS), true
	default:
		return contract.ProviderResult{}, false
	}
}

func shouldTryNextCandidate(req contract.DispatchRequest, result contract.ProviderResult) bool {
	if req.Command != "send" || result.OK {
		return false
	}
	if isPermissionRejection(result.Stderr) {
		return false
	}
	if result.FailureClass == nil {
		return result.Status == contract.StatusQuota || result.Status == contract.StatusTimeout || result.Status == contract.StatusDisabled || result.Status == contract.StatusNotFound
	}
	switch *result.FailureClass {
	case contract.FailureConfig, contract.FailureQuota, contract.FailureTimeout, contract.FailureNetwork:
		return true
	default:
		return false
	}
}

func isPermissionRejection(message string) bool {
	lower := strings.ToLower(message)
	return strings.Contains(lower, "permission requested") ||
		strings.Contains(lower, "auto-rejecting") ||
		strings.Contains(lower, "permission denied")
}

func degradeReason(previous contract.ProviderResult, fallback routing.DispatchTarget) string {
	failure := ""
	if previous.FailureClass != nil {
		failure = "/" + string(*previous.FailureClass)
	}
	previousLabel := previous.ProviderUsed
	if previous.ModelUsed != "" {
		previousLabel += ":" + previous.ModelUsed
	}
	label := fallback.Provider
	if fallback.Model != "" {
		label += ":" + fallback.Model
	}
	return fmt.Sprintf("%s failed with %s%s; switched to %s", previousLabel, previous.Status, failure, label)
}

func resolveTarget(req contract.DispatchRequest) (routing.DispatchTarget, error) {
	if req.Command == "resume" {
		target := routing.DispatchTarget{Source: "session"}
		record, ok, err := runstore.FindBySessionID("", req.SessionID)
		if err != nil {
			return routing.DispatchTarget{}, err
		}
		if ok && record.Result != nil {
			target.Requested = firstNonEmpty(record.Result.RequestedTarget, record.Result.ProviderUsed)
			target.Provider = record.Result.ProviderUsed
			target.Model = firstNonEmpty(req.Model, record.Result.ModelUsed)
		}
		if req.Target != "" {
			resolved, err := routing.Resolve(req.Target, req.Model)
			if err != nil {
				return routing.DispatchTarget{}, err
			}
			if target.Provider != "" && resolved.Provider != target.Provider {
				return routing.DispatchTarget{}, fmt.Errorf("session_id %q belongs to provider %q, not %q", req.SessionID, target.Provider, resolved.Provider)
			}
			target = resolved
			target.Source = "session"
		} else if req.SessionProvider != "" {
			resolved, err := routing.Resolve(req.SessionProvider, req.Model)
			if err != nil {
				return routing.DispatchTarget{}, err
			}
			if target.Provider != "" && resolved.Provider != target.Provider {
				return routing.DispatchTarget{}, fmt.Errorf("session_id %q belongs to provider %q, not %q", req.SessionID, target.Provider, resolved.Provider)
			}
			target.Provider = resolved.Provider
			target.Model = firstNonEmpty(resolved.Model, target.Model)
			target.Requested = firstNonEmpty(target.Requested, req.SessionProvider)
		} else if req.Model != "" && target.Provider != "" {
			resolved, err := routing.Resolve(target.Provider, req.Model)
			if err != nil {
				return routing.DispatchTarget{}, err
			}
			target.Model = resolved.Model
		}
		if target.Provider == "" {
			return routing.DispatchTarget{}, fmt.Errorf("cannot infer provider for session_id %q; pass --target or --session-provider", req.SessionID)
		}
		target.Requested = firstNonEmpty(target.Requested, target.Provider)
		return target, nil
	}
	return routing.Resolve(req.Target, req.Model)
}

func providerFor(name string) (providers.Provider, bool) {
	switch name {
	case "codex":
		return codex.Provider{}, true
	case "opencode":
		return opencode.Provider{}, true
	case "claude":
		return claude.Provider{}, true
	case "antigravity":
		return antigravity.Provider{}, true
	case "grok":
		return grok.Provider{}, true
	default:
		return nil, false
	}
}

func buildFailure(err error) (contract.FailureClass, int) {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "binary not found") {
		return contract.FailureConfig, 3
	}
	return contract.FailureInput, 2
}

func ensureRouteMetadata(result *contract.ProviderResult, target routing.DispatchTarget, durationMS int64) {
	if result.RequestedTarget == "" {
		result.RequestedTarget = target.Requested
	}
	if result.ProviderUsed == "" {
		result.ProviderUsed = target.Provider
	}
	if result.ModelUsed == "" {
		result.ModelUsed = target.Model
	}
	label := target.Provider
	if target.Model != "" {
		label += ":" + target.Model
	}
	if len(result.RouteTrace) == 0 && label != "" {
		result.RouteTrace = []string{label}
	}
	if len(result.RouteSteps) == 0 && target.Provider != "" {
		result.RouteSteps = []contract.RouteStep{{
			Provider:   target.Provider,
			Model:      target.Model,
			Status:     result.Status,
			DurationMS: durationMS,
		}}
	}
	// Keep route-step effort aligned with top-level when steps are created late.
	if result.AppliedEffort != "" {
		for i := range result.RouteSteps {
			if result.RouteSteps[i].AppliedEffort == "" {
				result.RouteSteps[i].AppliedEffort = result.AppliedEffort
				result.RouteSteps[i].EffortFallbackReason = result.EffortFallbackReason
			}
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func seconds(value int) time.Duration {
	if value <= 0 {
		return 0
	}
	return time.Duration(value) * time.Second
}

func elapsedMS(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}

func maxDurationMS(values ...int64) int64 {
	var maximum int64
	for _, value := range values {
		if value > maximum {
			maximum = value
		}
	}
	return maximum
}
