package dispatch

import (
	"context"
	"fmt"
	"io"
	"os"
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
}

func Execute(req contract.DispatchRequest) contract.ProviderResult {
	return ExecuteWithOptions(req, Options{})
}

func ExecuteWithOptions(req contract.DispatchRequest, opts Options) contract.ProviderResult {
	target, err := resolveTarget(req)
	if err != nil {
		return contract.ErrorResult(contract.StatusError, contract.FailureInput, err.Error(), 2)
	}
	if os.Getenv("AI_DISPATCH_GO_PROVIDER_EXECUTION") != "on" {
		result := contract.ErrorResult(contract.StatusDisabled, contract.FailureConfig, "go provider execution is disabled; set AI_DISPATCH_GO_PROVIDER_EXECUTION=on for explicit smoke", 3)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		ensureRouteMetadata(&result, target, 0)
		return result
	}
	result := executeCandidates(req, target, opts)
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
	if err := runstore.WriteResultWithTask("", "", req.TaskName, result); err != nil {
		result.Warnings = append(result.Warnings, "runstore write failed: "+err.Error())
	}
	return result
}

func executeCandidates(req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
	candidates := routing.CandidateTargets(target)
	var routeTrace []string
	var routeSteps []contract.RouteStep
	var degradeReasons []string
	for i, candidate := range candidates {
		attemptReq := req
		if i > 0 {
			attemptReq.SessionID = ""
			attemptReq.SessionProvider = ""
		}
		result := executeTarget(attemptReq, candidate, opts)
		routeTrace = append(routeTrace, result.RouteTrace...)
		routeSteps = append(routeSteps, result.RouteSteps...)
		if result.OK {
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons)
		}
		if i == len(candidates)-1 || !shouldTryNextCandidate(req, result) {
			return finalizeCandidateResult(result, target.Requested, routeTrace, routeSteps, degradeReasons)
		}
		degradeReasons = append(degradeReasons, degradeReason(result, candidates[i+1]))
	}
	return contract.ErrorResult(contract.StatusError, contract.FailureConfig, "no route candidates available", 2)
}

func finalizeCandidateResult(result contract.ProviderResult, requested string, routeTrace []string, routeSteps []contract.RouteStep, degradeReasons []string) contract.ProviderResult {
	if requested != "" {
		result.RequestedTarget = requested
	}
	if len(routeTrace) > 0 {
		result.RouteTrace = routeTrace
	}
	if len(routeSteps) > 0 {
		result.RouteSteps = routeSteps
	}
	if len(degradeReasons) > 0 {
		reason := strings.Join(degradeReasons, "; ")
		result.Degraded = true
		result.DegradeReason = reason
		result.Warnings = append([]string{reason}, result.Warnings...)
	}
	return result
}

func executeTarget(req contract.DispatchRequest, target routing.DispatchTarget, opts Options) contract.ProviderResult {
	p, ok := providerFor(target.Provider)
	if !ok {
		result := contract.ErrorResult(contract.StatusDisabled, contract.FailureConfig, "provider is not implemented in go runtime: "+target.Provider, 3)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		ensureRouteMetadata(&result, target, 0)
		return result
	}
	buildReq := providers.BuildRequest{
		Prompt:                 req.Prompt,
		PromptFile:             req.PromptFile,
		Target:                 target,
		CWD:                    req.CWD,
		SessionID:              req.SessionID,
		TimeoutSeconds:         req.TimeoutSeconds,
		ActivityTimeoutSeconds: req.ActivityTimeoutSeconds,
		ProviderOptions:        req.ProviderOpts[target.Provider],
	}
	spec, err := p.Build(buildReq)
	if err != nil {
		failure, exitCode := buildFailure(err)
		result := contract.ErrorResult(contract.StatusError, failure, err.Error(), exitCode)
		result.RequestedTarget = target.Requested
		result.ProviderUsed = target.Provider
		ensureRouteMetadata(&result, target, 0)
		return result
	}
	spec.CWD = req.CWD
	var emitter *progress.Emitter
	hooks := execruntime.StreamHooks{}
	if req.StreamProgress && opts.ProgressWriter != nil {
		emitter = progress.NewEmitter(target.Provider, opts.ProgressWriter)
		emitter.Emit(contract.ProgressSession, "session", target.Provider+" session started")
		hooks.Stdout = emitter.Feed
		hooks.Stderr = emitter.Feed
	}
	ctx := context.Background()
	var cancel context.CancelFunc
	if req.TimeoutSeconds > 0 {
		ctx, cancel = context.WithTimeout(ctx, seconds(req.TimeoutSeconds))
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	defer cancel()
	run := withProviderExecutionLock(ctx, target.Provider, func() execruntime.RunResult {
		return execruntime.RunProcess(ctx, spec, execruntime.RunOptions{
			FixedTimeout:    seconds(req.TimeoutSeconds),
			ActivityTimeout: seconds(req.ActivityTimeoutSeconds),
		}, hooks)
	})
	if emitter != nil {
		emitter.Close()
	}
	result := p.Parse(run, buildReq)
	ensureRouteMetadata(&result, target, run.DurationMS)
	if emitter != nil {
		if result.OK {
			emitter.Emit(contract.ProgressDone, "done", "completed")
		} else {
			emitter.Emit(contract.ProgressError, "error", result.Stderr)
		}
	}
	return result
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
