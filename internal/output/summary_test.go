package output

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestFormatDurationText(t *testing.T) {
	cases := []struct {
		ms   int64
		want string
	}{
		{0, "00:00:00"},
		{-3, "00:00:00"},
		{499, "00:00:00"},
		{500, "00:00:01"},
		{12000, "00:00:12"},
		{289000, "00:04:49"},
		{108541, "00:01:49"},
		{3723000, "01:02:03"},
	}
	for _, tc := range cases {
		if got := FormatDurationText(tc.ms); got != tc.want {
			t.Fatalf("FormatDurationText(%d)=%q want %q", tc.ms, got, tc.want)
		}
	}
}

func TestRenderUserFacingSummarySuccess(t *testing.T) {
	result := contract.ProviderResult{
		OK:           true,
		Status:       contract.StatusSuccess,
		ProviderUsed: "cursor",
		ModelUsed:    "kimi-k3-high",
		SessionID:    "d42b2733-9e2f-44a9-9dc8-47bcd4f044f7",
		DurationMS:   289000,
	}
	got := RenderUserFacingSummary(result)
	want := strings.Join([]string{
		"**ai-dispatch 调用说明**",
		"实际调用：cursor / kimi-k3-high",
		"耗时：00:04:49",
		"Session ID: d42b2733-9e2f-44a9-9dc8-47bcd4f044f7",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "降级") || strings.Contains(got, "失败") || strings.Contains(got, "请求") {
		t.Fatalf("success summary leaked extra fields:\n%s", got)
	}
}

func TestRenderUserFacingSummaryDegraded(t *testing.T) {
	result := contract.ProviderResult{
		OK:            true,
		Status:        contract.StatusSuccess,
		ProviderUsed:  "cursor",
		ModelUsed:     "claude-fable-5-thinking-high",
		SessionID:     "4acd848e-518d-4595-8bee-c7a7f9352f08",
		DurationMS:    584340,
		Degraded:      true,
		DegradeReason: "claude:claude-fable-5 failed with error/config; switched to cursor:claude-fable-5-thinking-high",
		RouteSteps: []contract.RouteStep{
			{Provider: "claude", Model: "claude-fable-5", Status: contract.StatusError},
			{Provider: "cursor", Model: "claude-fable-5-thinking-high", Status: contract.StatusSuccess},
		},
	}
	got := RenderUserFacingSummary(result)
	want := strings.Join([]string{
		"**ai-dispatch 调用说明**",
		"实际调用：cursor / claude-fable-5-thinking-high",
		"降级：claude / claude-fable-5 失败，已切换到 cursor",
		"耗时：00:09:44",
		"Session ID: 4acd848e-518d-4595-8bee-c7a7f9352f08",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderUserFacingSummaryFailure(t *testing.T) {
	failure := contract.FailureConfig
	result := contract.ProviderResult{
		OK:           false,
		Status:       contract.StatusError,
		ProviderUsed: "cursor",
		ModelUsed:    "kimi-k3-high",
		DurationMS:   12000,
		FailureClass: &failure,
	}
	got := RenderUserFacingSummary(result)
	want := strings.Join([]string{
		"**ai-dispatch 调用说明**",
		"实际调用：cursor / kimi-k3-high",
		"失败：配置错误",
		"耗时：00:00:12",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(got, "Session ID") {
		t.Fatalf("missing session should omit the line:\n%s", got)
	}
}

func TestRenderUserFacingSummarySameProviderDegrade(t *testing.T) {
	result := contract.ProviderResult{
		OK:           true,
		Status:       contract.StatusSuccess,
		ProviderUsed: "opencode",
		ModelUsed:    "model-b",
		Degraded:     true,
		DurationMS:   1500,
		RouteSteps: []contract.RouteStep{
			{Provider: "opencode", Model: "model-a", Status: contract.StatusError},
			{Provider: "opencode", Model: "model-b", Status: contract.StatusSuccess},
		},
	}
	got := RenderUserFacingSummary(result)
	if !strings.Contains(got, "降级：opencode / model-a 失败，已切换到 opencode / model-b") {
		t.Fatalf("got:\n%s", got)
	}
}

func TestRenderUserFacingSummaryInputErrorOmitsEmptyFields(t *testing.T) {
	failure := contract.FailureInput
	result := contract.ErrorResult(contract.StatusError, failure, "unsupported target: mimo-pro", 2)
	got := RenderUserFacingSummary(result)
	want := strings.Join([]string{
		"**ai-dispatch 调用说明**",
		"失败：输入错误",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestAttachUserFacingJSONKeyOrder(t *testing.T) {
	result := contract.ProviderResult{
		SchemaVersion: "2.0",
		OK:            true,
		Status:        contract.StatusSuccess,
		Text:          "hello from the model",
		ProviderUsed:  "cursor",
		ModelUsed:     "kimi-k3-high",
		SessionID:     "sess",
		DurationMS:    12000,
	}
	result = AttachUserFacing(result)
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(raw)
	summaryAt := strings.Index(encoded, `"user_facing_summary"`)
	providerAt := strings.Index(encoded, `"provider_used"`)
	modelAt := strings.Index(encoded, `"model_used"`)
	sessionAt := strings.Index(encoded, `"session_id"`)
	degradedAt := strings.Index(encoded, `"degraded"`)
	durationTextAt := strings.Index(encoded, `"duration_text"`)
	textAt := strings.Index(encoded, `"text"`)
	if summaryAt < 0 || providerAt < 0 || modelAt < 0 || sessionAt < 0 || degradedAt < 0 || durationTextAt < 0 || textAt < 0 {
		t.Fatalf("missing keys in %s", encoded)
	}
	if !(summaryAt < providerAt && providerAt < modelAt && modelAt < sessionAt && sessionAt < degradedAt && degradedAt < durationTextAt && durationTextAt < textAt) {
		t.Fatalf("key order is wrong: %s", encoded)
	}
}

func TestWriteUserFacingNotice(t *testing.T) {
	var buf strings.Builder
	result := AttachUserFacing(contract.ProviderResult{
		OK:           true,
		Status:       contract.StatusSuccess,
		ProviderUsed: "cursor",
		ModelUsed:    "kimi-k3-high",
		DurationMS:   12000,
	})
	WriteUserFacingNotice(&buf, result)
	got := buf.String()
	if !strings.Contains(got, result.UserFacingSummary) {
		t.Fatalf("missing summary:\n%s", got)
	}
	if !strings.Contains(got, "把以上调用说明原样写进给用户的最终回复，不要改写。") {
		t.Fatalf("missing hint:\n%s", got)
	}
}
