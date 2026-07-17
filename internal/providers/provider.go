package providers

import (
	"context"

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
	ProviderOptions map[string]string
}

// EffortRequest is the per-candidate input to ResolveEffort.
type EffortRequest struct {
	Model           string
	Requested       contract.Effort
	ProviderOptions map[string]string
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

type Provider interface {
	Name() string
	ResolveEffort(context.Context, EffortRequest) EffortResolution
	Build(BuildRequest) (runtime.CommandSpec, error)
	Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
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
