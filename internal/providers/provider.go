package providers

import (
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
	ProviderOptions        map[string]string
}

type Provider interface {
	Name() string
	Build(BuildRequest) (runtime.CommandSpec, error)
	Parse(runtime.RunResult, BuildRequest) contract.ProviderResult
}
