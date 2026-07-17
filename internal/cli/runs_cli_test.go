package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

func TestRunsList(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("stdout=%q", stdout.String())
	}
}

func TestRunsListStatusFilter(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	success := contract.SuccessResult("ok")
	success.RequestedTarget = "gpt5.5"
	success.ProviderUsed = "codex"
	if err := runstore.WriteResult(root, "run-success", success); err != nil {
		t.Fatal(err)
	}
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResult(root, "run-quota", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--status", "quota"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListReturnsValidRecordsAndSurfacesCorruptOnes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	result := contract.SuccessResult("ok")
	result.RequestedTarget = "gpt5.5"
	if err := runstore.WriteResult(root, "run-valid", result); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "run-corrupt"), 0o755); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-valid" {
		t.Fatalf("records=%v", records)
	}
	if !strings.Contains(stderr.String(), "skipped invalid run record run-corrupt") {
		t.Fatalf("stderr=%s", stderr.String())
	}
}

func TestRunsListTaskNameAndFailureClassFilters(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	success := contract.SuccessResult("ok")
	success.RequestedTarget = "gpt5.5"
	success.ProviderUsed = "codex"
	if err := runstore.WriteResultWithTask(root, "run-success", "review-r1", success); err != nil {
		t.Fatal(err)
	}
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResultWithTask(root, "run-quota", "review-r2", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--task-name", "review-r*", "--failure-class", "quota"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" || records[0]["task_name"] != "review-r2" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListAcceptsRelativeSince(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	failure.ProviderUsed = "claude"
	if err := runstore.WriteResultWithTask(root, "run-quota", "review-r2", failure); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list", "--since", "7d"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-quota" {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsListSummarizesWithoutResultBody(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	result := contract.ErrorResult(contract.StatusError, contract.FailureRuntime, "SECRET STDERR BODY", 1)
	result.Text = "SECRET ASSISTANT BODY"
	result.ProviderUsed = "codex"
	result.ModelUsed = "gpt-5.5"
	result.RequestedTarget = "gpt5.5"
	if err := runstore.WriteResultWithTask(root, "run-secret", "review-r1", result); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "list"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "SECRET") || strings.Contains(stdout.String(), "\"result\"") || strings.Contains(stdout.String(), "\"text\"") || strings.Contains(stdout.String(), "\"stderr\"") {
		t.Fatalf("run list leaked full result: %s", stdout.String())
	}
	var records []map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0]["run_id"] != "run-secret" || records[0]["stderr_bytes"] == nil {
		t.Fatalf("records=%v", records)
	}
}

func TestRunsFailuresSummarizesWithoutText(t *testing.T) {
	root := t.TempDir()
	t.Setenv("AI_DISPATCH_RUNS_DIR", root)
	failure := contract.ErrorResult(contract.StatusError, contract.FailureRuntime, "\x1b[91mError:\x1b[0m Unexpected error database is locked", 1)
	failure.RequestedTarget = "kimi"
	failure.ProviderUsed = "opencode"
	failure.ModelUsed = "openrouter/moonshotai/kimi-k2.7-code"
	failure.Text = "SECRET ASSISTANT BODY"
	if err := runstore.WriteResultWithTask(root, "run-db", "review-r1", failure); err != nil {
		t.Fatal(err)
	}
	degraded := contract.SuccessResult("SECRET FALLBACK BODY")
	degraded.RequestedTarget = "sonnet4.6"
	degraded.ProviderUsed = "codex"
	degraded.ModelUsed = "gpt-5.5"
	degraded.Degraded = true
	degraded.DegradeReason = "claude failed with error/config; switched to codex:gpt-5.5"
	if err := runstore.WriteResultWithTask(root, "run-degraded", "review-r2", degraded); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "failures", "--since", "7d", "--limit", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if strings.Contains(stdout.String(), "SECRET") {
		t.Fatalf("failure summary leaked result text: %s", stdout.String())
	}
	var payload struct {
		Total         int             `json:"total"`
		Returned      int             `json:"returned"`
		ByFingerprint map[string]int  `json:"by_fingerprint"`
		Records       []failureRecord `json:"records"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Total != 2 || payload.Returned != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.ByFingerprint["database_locked"] != 1 || payload.ByFingerprint["degraded"] != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if len(payload.Records) != 1 || payload.Records[0].Fingerprint == "" {
		t.Fatalf("payload=%+v", payload)
	}
}

func TestRunsFailuresRejectsUnsupportedFormat(t *testing.T) {
	t.Setenv("AI_DISPATCH_RUNS_DIR", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Main([]string{"runs", "failures", "--format", "text"}, &stdout, &stderr)
	if code != 2 || !strings.Contains(stderr.String(), "--format for runs failures must be json") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestParseSinceRelativeDurations(t *testing.T) {
	now := time.Date(2026, 6, 17, 12, 0, 0, 0, time.UTC)
	got, err := parseSince("7d", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 6, 10, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got=%s", got)
	}
	got, err = parseSince("24h", now)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("got=%s", got)
	}
}
