package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
)

var (
	versionOverride string
	releaseIdentity string
)

// Info is the machine-readable identity of the running binary. It deliberately
// excludes build paths and environment values so it is safe to expose from
// doctor/version output.
type Info struct {
	Version   string `json:"version"`
	Revision  string `json:"revision,omitempty"`
	Modified  bool   `json:"modified"`
	VCSTime   string `json:"vcs_time,omitempty"`
	GoVersion string `json:"go_version"`
	Module    string `json:"module"`
}

// Current returns identity embedded by the Go toolchain. Tagged builds report
// their module version; local dirty builds additionally report modified=true.
func Current() Info {
	return applyVersionOverride(
		fromDebugInfo(debug.ReadBuildInfo()),
		versionOverride,
		releaseIdentity == "true",
	)
}

func applyVersionOverride(info Info, override string, release bool) Info {
	override = strings.TrimSpace(override)
	if override == "" {
		return info
	}
	developmentBuild := info.Version == "dev"
	info.Version = override
	if release {
		return info
	}
	if info.Modified && !strings.Contains(info.Version, "+") {
		info.Version += "+dirty"
	} else if developmentBuild && info.Revision != "" && !strings.Contains(info.Version, "+") {
		revision := info.Revision
		if len(revision) > 12 {
			revision = revision[:12]
		}
		info.Version += "+dev." + revision
	}
	return info
}

func fromDebugInfo(info *debug.BuildInfo, ok bool) Info {
	result := Info{
		Version:   "dev",
		GoVersion: runtime.Version(),
		Module:    "github.com/rennzhang/ai-dispatch",
	}
	if !ok || info == nil {
		return result
	}
	if value := strings.TrimSpace(info.Main.Path); value != "" {
		result.Module = value
	}
	if value := strings.TrimSpace(info.Main.Version); value != "" && value != "(devel)" {
		result.Version = value
	}
	if value := strings.TrimSpace(info.GoVersion); value != "" {
		result.GoVersion = value
	}
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			result.Revision = setting.Value
		case "vcs.modified":
			result.Modified, _ = strconv.ParseBool(setting.Value)
		case "vcs.time":
			result.VCSTime = setting.Value
		}
	}
	return result
}
