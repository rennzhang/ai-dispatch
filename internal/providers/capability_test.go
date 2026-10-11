package providers_test

import (
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
)

func TestResolveEffortFromDeclaredVsUndeclared(t *testing.T) {
	declared := []string{"high", "xhigh"}

	auto := providers.ResolveEffortFromDeclared(providers.EffortRequest{
		Model: "gpt-5.6-sol", Requested: contract.EffortAuto, DeclaredEfforts: declared,
	}, "codex")
	if auto.Applied != contract.EffortAuto || auto.Fallback {
		t.Fatalf("auto=%+v", auto)
	}

	exact := providers.ResolveEffortFromDeclared(providers.EffortRequest{
		Model: "gpt-5.6-sol", Requested: contract.EffortHigh, DeclaredEfforts: declared,
	}, "codex")
	if exact.Applied != contract.EffortHigh || exact.Fallback || exact.Reason != "" {
		t.Fatalf("exact=%+v", exact)
	}

	outside := providers.ResolveEffortFromDeclared(providers.EffortRequest{
		Model: "gpt-5.6-sol", Requested: contract.EffortMax, DeclaredEfforts: declared,
	}, "codex")
	if outside.Applied != contract.EffortAuto || !outside.Fallback || !strings.Contains(outside.Reason, "not in declared efforts") {
		t.Fatalf("outside=%+v", outside)
	}

	undeclared := providers.ResolveEffortFromDeclared(providers.EffortRequest{
		Model: "gpt-5.6-sol", Requested: contract.EffortHigh,
	}, "codex")
	if undeclared.Applied != contract.EffortAuto || !undeclared.Fallback || !strings.Contains(undeclared.Reason, "not declared in config") {
		t.Fatalf("undeclared=%+v", undeclared)
	}
}

func TestResolveFastFromDeclaredVsUndeclared(t *testing.T) {
	yes := true
	no := false

	off := providers.ResolveFastFromDeclared(providers.FastRequest{Model: "gpt-5.6-sol", Requested: false, DeclaredFast: &yes}, "codex")
	if off.Applied || off.Fallback {
		t.Fatalf("off=%+v", off)
	}

	exact := providers.ResolveFastFromDeclared(providers.FastRequest{Model: "gpt-5.6-sol", Requested: true, DeclaredFast: &yes}, "codex")
	if !exact.Applied || exact.Fallback {
		t.Fatalf("exact=%+v", exact)
	}

	denied := providers.ResolveFastFromDeclared(providers.FastRequest{Model: "gpt-5.6-sol", Requested: true, DeclaredFast: &no}, "codex")
	if denied.Applied || !denied.Fallback || !strings.Contains(denied.Reason, "fast=false") {
		t.Fatalf("denied=%+v", denied)
	}

	undeclared := providers.ResolveFastFromDeclared(providers.FastRequest{Model: "gpt-5.6-sol", Requested: true}, "codex")
	if undeclared.Applied || !undeclared.Fallback || !strings.Contains(undeclared.Reason, "does not declare fast") {
		t.Fatalf("undeclared=%+v", undeclared)
	}
}
