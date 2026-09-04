package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

const (
	AgentHint = "You MUST paste the dispatch result user_facing_summary into the final reply to the user verbatim! Keep only the latest wrap-up per session ID; replace any earlier card for the same session, do not stack them."
)

func AttachUserFacing(result contract.ProviderResult) contract.ProviderResult {
	return ApplyWrapUp(result, true)
}

func ApplyWrapUp(result contract.ProviderResult, enabled bool) contract.ProviderResult {
	result.DurationText = FormatDurationText(result.DurationMS)
	if !enabled {
		result.AgentHint = ""
		result.UserFacingSummary = ""
		return result
	}
	result.UserFacingSummary = RenderUserFacingSummary(result)
	if result.UserFacingSummary != "" {
		result.AgentHint = AgentHint
	}
	return result
}

func FormatDurationText(durationMS int64) string {
	if durationMS < 0 {
		durationMS = 0
	}
	totalSeconds := (durationMS + 500) / 1000
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)
}

func RenderUserFacingSummary(result contract.ProviderResult) string {
	header := "[ai-dispatch result]"
	if !result.OK {
		header = "[ai-dispatch failed]"
	} else if result.Degraded {
		header = "[ai-dispatch degraded]"
	}
	lines := []string{header}
	if route := routeLabel(result); route != "" {
		lines = append(lines, "Target: "+route)
	}
	if !result.OK {
		lines = append(lines, "Failed: "+failureLabel(result))
	}
	if result.Degraded {
		lines = append(lines, "Degraded: "+degradeLine(result))
	}
	if includeDuration(result) {
		lines = append(lines, "Duration: "+FormatDurationText(result.DurationMS))
	}
	if sessionID := strings.TrimSpace(result.SessionID); sessionID != "" {
		lines = append(lines, "Session ID: "+sessionID)
	}
	return strings.Join(lines, "\n")
}

func WriteUserFacingNotice(w io.Writer, result contract.ProviderResult) {
	if w == nil {
		return
	}
	summary := strings.TrimSpace(result.UserFacingSummary)
	if summary == "" {
		return
	}
	fmt.Fprintln(w, summary)
	if hint := strings.TrimSpace(result.AgentHint); hint != "" {
		fmt.Fprintln(w, hint)
	}
}

func routeLabel(result contract.ProviderResult) string {
	provider := strings.TrimSpace(result.ProviderUsed)
	model := strings.TrimSpace(result.ModelUsed)
	switch {
	case provider != "" && model != "":
		return provider + " / " + model
	case provider != "":
		return provider
	case model != "":
		return model
	default:
		return ""
	}
}

func includeDuration(result contract.ProviderResult) bool {
	return result.DurationMS > 0 || result.ProviderUsed != "" || result.OK
}

func degradeLine(result contract.ProviderResult) string {
	fromProvider, fromModel := degradeSource(result)
	line := ""
	switch {
	case fromProvider != "" && fromModel != "":
		line += fromProvider + " / " + fromModel + " failed"
	case fromProvider != "":
		line += fromProvider + " failed"
	default:
		line += "switched candidate"
	}
	to := strings.TrimSpace(result.ProviderUsed)
	toModel := strings.TrimSpace(result.ModelUsed)
	if to != "" && (to != fromProvider || toModel != fromModel) {
		switched := to
		if to == fromProvider && toModel != "" {
			switched = to + " / " + toModel
		}
		line += " → " + switched
	}
	return line
}

func degradeSource(result contract.ProviderResult) (string, string) {
	for _, step := range result.RouteSteps {
		if step.Status != "" && step.Status != contract.StatusSuccess {
			return strings.TrimSpace(step.Provider), strings.TrimSpace(step.Model)
		}
	}
	reason := result.DegradeReason
	if reason == "" && len(result.Warnings) > 0 {
		reason = result.Warnings[0]
	}
	source, _, ok := strings.Cut(reason, " failed with ")
	if !ok {
		if len(result.RouteSteps) > 0 {
			return strings.TrimSpace(result.RouteSteps[0].Provider), strings.TrimSpace(result.RouteSteps[0].Model)
		}
		return "", ""
	}
	provider, model, found := strings.Cut(strings.TrimSpace(source), ":")
	if !found {
		return provider, ""
	}
	return provider, model
}

func failureLabel(result contract.ProviderResult) string {
	if result.FailureClass != nil {
		switch *result.FailureClass {
		case contract.FailureConfig:
			return "config error"
		case contract.FailureRuntime:
			return "runtime error"
		case contract.FailureNetwork:
			return "network error"
		case contract.FailureQuota:
			return "quota exceeded"
		case contract.FailureTimeout:
			return "timeout"
		case contract.FailureInput:
			return "input error"
		case contract.FailureUnknown:
			return "failed"
		}
		if label := strings.TrimSpace(string(*result.FailureClass)); label != "" {
			return label
		}
	}
	if result.Status != "" && result.Status != contract.StatusSuccess {
		switch result.Status {
		case contract.StatusQuota:
			return "quota exceeded"
		case contract.StatusTimeout:
			return "timeout"
		case contract.StatusNotFound:
			return "not found"
		case contract.StatusDisabled:
			return "unavailable"
		}
	}
	return "failed"
}
