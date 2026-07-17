package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestFromDebugInfoReturnsMachineReadableIdentity(t *testing.T) {
	got := fromDebugInfo(&debug.BuildInfo{
		GoVersion: "go1.24.3",
		Main:      debug.Module{Path: "github.com/rennzhang/ai-dispatch", Version: "v0.3.0+dirty"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "0123456789abcdef"},
			{Key: "vcs.modified", Value: "true"},
			{Key: "vcs.time", Value: "2026-07-10T03:06:32Z"},
		},
	}, true)
	if got.Version != "v0.3.0+dirty" || got.Revision != "0123456789abcdef" || !got.Modified {
		t.Fatalf("identity=%+v", got)
	}
	if got.GoVersion != "go1.24.3" || got.Module != "github.com/rennzhang/ai-dispatch" {
		t.Fatalf("identity=%+v", got)
	}
}

func TestFromDebugInfoFallsBackWithoutBuildMetadata(t *testing.T) {
	got := fromDebugInfo(nil, false)
	if got.Version != "dev" || got.Module == "" || got.GoVersion == "" || got.Modified {
		t.Fatalf("identity=%+v", got)
	}
}

func TestVersionOverrideKeepsDirtyStateObservable(t *testing.T) {
	got := applyVersionOverride(Info{Version: "dev", Modified: true}, "v0.3.0", false)
	if got.Version != "v0.3.0+dirty" {
		t.Fatalf("identity=%+v", got)
	}
}

func TestVersionOverrideMarksCleanUntaggedBuild(t *testing.T) {
	got := applyVersionOverride(Info{Version: "dev", Revision: "0123456789abcdef"}, "v0.3.0", false)
	if got.Version != "v0.3.0+dev.0123456789ab" {
		t.Fatalf("identity=%+v", got)
	}
}

func TestVersionOverrideKeepsOfficialReleaseExact(t *testing.T) {
	got := applyVersionOverride(Info{
		Version:  "dev",
		Revision: "0123456789abcdef",
	}, "v0.3.0", true)
	if got.Version != "v0.3.0" || got.Modified {
		t.Fatalf("identity=%+v", got)
	}
}
