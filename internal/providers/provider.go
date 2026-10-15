package providers

import (
	"context"
	"fmt"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

type BuildRequest struct {
	Prompt                 string
	PromptFile             string
	Target                 routing.DispatchTarget
	CWD                    string
	SessionID              string
	TimeoutSeconds         int
	ActivityTimeoutSeconds int
	// Effort is the already-resolved applied effort for this candidate.
	// Providers must not re-decide fallback here; ResolveEffort owns that.
	Effort          contract.Effort
	Fast            bool
	ProviderOptions map[string]string
	// Surface is the delivery channel. Empty means the provider CLI.
	// Codex accepts "app" for the Desktop client.
	Surface string
}

// EffortRequest is the per-candidate input to ResolveEffort.
type EffortRequest struct {
	Model           string
	Requested       contract.Effort
	ProviderOptions map[string]string
	// DeclaredEfforts comes from config ModelRoute.efforts for this candidate.
	// Empty means undeclared: explicit --effort falls back to auto.
	DeclaredEfforts []string
}

// EffortResolution is the exact effort decision for one candidate before Build.
// AppliedModel is the model token/label Build should use. Empty means do not
// send a model override (Antigravity default model path).
type EffortResolution struct {
	Requested    contract.Effort
	Applied      contract.Effort
	AppliedModel string
	Fallback     bool
	Reason       string
}

// FastRequest is the per-candidate input to ResolveFast.
type FastRequest struct {
	Model     string
	Requested bool
	// DeclaredFast comes from config ModelRoute.fast. nil means undeclared:
	// --fast falls back to standard speed.
	DeclaredFast *bool
}

// FastResolution records whether a provider can honor the shared --fast intent.
// Unsupported providers continue at standard speed and must expose the reason.
type FastResolution struct {
	Requested bool
	Applied   bool
	Fallback  bool
	Reason    string
}

type Provider interface {
	Name() string
	ResolveEffort(context.Context, EffortRequest) EffortResolution
	ResolveFast(context.Context, FastRequest) FastResolution
	Build(BuildRequest) (runtime.CommandSpec, error)
	Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
}

// DirectRunner is an optional provider path for a delivery channel that is
// not one subprocess command. handled=false means the caller should use Build.
type DirectRunner interface {
	RunDirect(context.Context, BuildRequest) (runtime.RunResult, bool, error)
}

// EffortAuto keeps the original model and applies no effort override.
func EffortAuto(requested contract.Effort, model string) EffortResolution {
	return EffortResolution{
		Requested:    contract.NormalizeEffort(requested),
		Applied:      contract.EffortAuto,
		AppliedModel: model,
	}
}

// EffortExact applies the requested effort with the given model token/label.
func EffortExact(requested contract.Effort, model string) EffortResolution {
	return EffortResolution{
		Requested:    contract.NormalizeEffort(requested),
		Applied:      contract.NormalizeEffort(requested),
		AppliedModel: model,
	}
}

// EffortFallback records that an explicit level could not be applied exactly.
func EffortFallback(requested contract.Effort, model string, reason string) EffortResolution {
	return EffortResolution{
		Requested:    contract.NormalizeEffort(requested),
		Applied:      contract.EffortAuto,
		AppliedModel: model,
		Fallback:     true,
		Reason:       reason,
	}
}

func FastStandard(requested bool) FastResolution {
	return FastResolution{Requested: requested}
}

func FastExact(requested bool) FastResolution {
	return FastResolution{Requested: requested, Applied: requested}
}

func FastFallback(requested bool, provider string, model string) FastResolution {
	if !requested {
		return FastStandard(false)
	}
	label := model
	if label == "" {
		label = "default"
	}
	return FastUnavailable(true, provider+"/"+label+" does not support fast mode; applied standard speed")
}

func FastUnavailable(requested bool, reason string) FastResolution {
	if !requested {
		return FastStandard(false)
	}
	return FastResolution{Requested: true, Fallback: true, Reason: reason}
}

// ResolveEffortFromDeclared uses config ModelRoute.efforts as execution authority.
// Missing/empty declaration means explicit levels fall back to auto. Membership is
// exact; there is no nearest-neighbor clamp.
func ResolveEffortFromDeclared(req EffortRequest, provider string) EffortResolution {
	requested := contract.NormalizeEffort(req.Requested)
	if requested == contract.EffortAuto {
		return EffortAuto(requested, req.Model)
	}
	label := strings.TrimSpace(req.Model)
	if label == "" {
		label = "default"
	}
	if len(req.DeclaredEfforts) == 0 {
		return EffortFallback(requested, req.Model,
			fmt.Sprintf("effort %s is not declared in config for %s/%s; applied auto", requested, provider, label))
	}
	if containsDeclaredEffort(req.DeclaredEfforts, requested) {
		return EffortExact(requested, req.Model)
	}
	return EffortFallback(requested, req.Model,
		fmt.Sprintf("effort %s is not in declared efforts [%s] for %s/%s; applied auto",
			requested, strings.Join(req.DeclaredEfforts, ","), provider, label))
}

// ResolveFastFromDeclared uses config ModelRoute.fast as execution authority.
func ResolveFastFromDeclared(req FastRequest, provider string) FastResolution {
	if !req.Requested {
		return FastStandard(false)
	}
	label := strings.TrimSpace(req.Model)
	if label == "" {
		label = "default"
	}
	if req.DeclaredFast == nil {
		return FastUnavailable(true,
			fmt.Sprintf("%s/%s does not declare fast in config; applied standard speed", provider, label))
	}
	if *req.DeclaredFast {
		return FastExact(true)
	}
	return FastUnavailable(true,
		fmt.Sprintf("%s/%s declares fast=false; applied standard speed", provider, label))
}

func containsDeclaredEffort(declared []string, wanted contract.Effort) bool {
	want := string(contract.NormalizeEffort(wanted))
	for _, item := range declared {
		if strings.TrimSpace(item) == want {
			return true
		}
	}
	return false
}
