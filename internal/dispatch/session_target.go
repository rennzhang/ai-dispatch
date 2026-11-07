package dispatch

import (
	"fmt"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

// Resume is bound to the previous actual entry. Global defaults only apply to
// new requests, never to an existing session.
func resolveTarget(req contract.DispatchRequest) (routing.DispatchTarget, error) {
	if req.Command != "resume" {
		return routing.ResolveSurface(req.Target, req.Model, req.Surface)
	}
	record, found, err := runstore.FindBySessionID("", req.SessionID)
	if err != nil {
		return routing.DispatchTarget{}, err
	}
	storedProvider, storedModel, storedSurface, requested := "", "", "", ""
	if found && record.Result != nil {
		storedProvider = record.Result.ProviderUsed
		storedModel = record.Result.ModelUsed
		storedSurface = record.Result.Surface
		requested = firstNonEmpty(record.Result.RequestedTarget, storedProvider)
	}
	if req.Surface != "" && storedSurface != "" && req.Surface != storedSurface {
		return routing.DispatchTarget{}, fmt.Errorf("session_id %q belongs to surface %q, not %q", req.SessionID, storedSurface, req.Surface)
	}
	rawTarget := firstNonEmpty(req.Target, req.SessionProvider, storedProvider)
	if rawTarget == "" {
		return routing.DispatchTarget{}, fmt.Errorf("cannot infer provider for session_id %q; pass --target or --session-provider", req.SessionID)
	}
	surface := firstNonEmpty(req.Surface, storedSurface, "cli")
	target, err := routing.ResolveSurface(rawTarget, firstNonEmpty(req.Model, storedModel), surface)
	if err != nil {
		return routing.DispatchTarget{}, err
	}
	if storedProvider != "" && target.Provider != storedProvider {
		return routing.DispatchTarget{}, fmt.Errorf("session_id %q belongs to provider %q, not %q", req.SessionID, storedProvider, target.Provider)
	}
	target.Source = "session"
	target.Requested = firstNonEmpty(req.Target, requested, rawTarget)
	return target, nil
}
