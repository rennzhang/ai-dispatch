package output

import (
	"fmt"
	"io"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

const (
	userFacingTitle = "**ai-dispatch Result**"
	agentHint       = "You MUST paste the dispatch result user_facing_summary into the final reply to the user verbatim!"
)

func AttachUserFacing(result contract.ProviderResult) contract.ProviderResult {
	result.DurationText = FormatDurationText(result.DurationMS)
	result.UserFacingSummary = RenderUserFacingSummary(result)
	if result.UserFacingSummary != "" {
		result.AgentHint = agentHint
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
	lines := []string{userFacingTitle}
	if line := actualCallLine(result); line != "" {
		lines = append(lines, line)
	}
	if result.Degraded {
		lines = append(lines, degradeLine(result))
	}
	if !result.OK {
		lines = append(lines, "失败："+failureLabel(result))
	}
	if includeDuration(result) {
		lines = append(lines, "Duration："+FormatDurationText(result.DurationMS))
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
		summary = RenderUserFacingSummary(result)
	}
	if summary == "" {
		return
	}
	fmt.Fprintln(w, summary)
	fmt.Fprintln(w, agentHint)
}

func actualCallLine(result contract.ProviderResult) string {
	provider := strings.TrimSpace(result.ProviderUsed)
	model := strings.TrimSpace(result.ModelUsed)
	switch {
	case provider != "" && model != "":
		return "Target：" + provider + " / " + model
	case provider != "":
		return "Target：" + provider
	case model != "":
		return "Target：" + model
	default:
		return ""
	}
}

func includeDuration(result contract.ProviderResult) bool {
	return result.DurationMS > 0 || result.ProviderUsed != "" || result.OK
}

func degradeLine(result contract.ProviderResult) string {
	fromProvider, fromModel := degradeSource(result)
	line := "降级："
	switch {
	case fromProvider != "" && fromModel != "":
		line += fromProvider + " / " + fromModel + " 失败"
	case fromProvider != "":
		line += fromProvider + " 失败"
	default:
		line += "已切换候选"
	}
	to := strings.TrimSpace(result.ProviderUsed)
	toModel := strings.TrimSpace(result.ModelUsed)
	if to != "" && (to != fromProvider || toModel != fromModel) {
		switched := to
		if to == fromProvider && toModel != "" {
			switched = to + " / " + toModel
		}
		line += "，已切换到 " + switched
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
			return "配置错误"
		case contract.FailureRuntime:
			return "运行时错误"
		case contract.FailureNetwork:
			return "网络错误"
		case contract.FailureQuota:
			return "额度不足"
		case contract.FailureTimeout:
			return "超时"
		case contract.FailureInput:
			return "输入错误"
		case contract.FailureUnknown:
			return "调用失败"
		}
		if label := strings.TrimSpace(string(*result.FailureClass)); label != "" {
			return label
		}
	}
	if result.Status != "" && result.Status != contract.StatusSuccess {
		switch result.Status {
		case contract.StatusQuota:
			return "额度不足"
		case contract.StatusTimeout:
			return "超时"
		case contract.StatusNotFound:
			return "未找到"
		case contract.StatusDisabled:
			return "不可用"
		}
	}
	return "调用失败"
}
