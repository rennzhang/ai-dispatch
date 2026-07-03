package runstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestWriteListRead(t *testing.T) {
	root := t.TempDir()
	result := contract.SuccessResult("hello")
	result.RequestedTarget = "gpt5.5"
	result.ProviderUsed = "codex"
	if err := WriteResult(root, "run-1", result); err != nil {
		t.Fatal(err)
	}
	records, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RunID != "run-1" {
		t.Fatalf("records=%+v", records)
	}
	record, err := Read(root, "run-1")
	if err != nil {
		t.Fatal(err)
	}
	if record.Result == nil || record.Result.Text != "hello" {
		t.Fatalf("record=%+v", record)
	}
}

func TestListFiltered(t *testing.T) {
	root := t.TempDir()
	success := contract.SuccessResult("hello")
	success.RequestedTarget = "gpt5.5"
	if err := WriteResult(root, "run-success", success); err != nil {
		t.Fatal(err)
	}
	failure := contract.ErrorResult(contract.StatusQuota, contract.FailureQuota, "quota", 3)
	failure.RequestedTarget = "claude"
	if err := WriteResult(root, "run-quota", failure); err != nil {
		t.Fatal(err)
	}
	records, err := ListFiltered(root, ListFilter{Status: contract.StatusQuota})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RunID != "run-quota" {
		t.Fatalf("records=%+v", records)
	}
	records, err = ListFiltered(root, ListFilter{Target: "gpt5.5", Limit: 1, Since: time.Now().Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RunID != "run-success" {
		t.Fatalf("records=%+v", records)
	}
	records, err = ListFiltered(root, ListFilter{TaskNameGlob: "audit-*", FailureClass: contract.FailureQuota})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records=%+v", records)
	}
}

func TestListFilteredReportsCorruptRunDirectories(t *testing.T) {
	root := t.TempDir()
	runID := "run-corrupt"
	if err := os.Mkdir(filepath.Join(root, runID), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := ListFiltered(root, ListFilter{})
	if err == nil || !strings.Contains(err.Error(), runID) {
		t.Fatalf("err=%v", err)
	}
}

func TestWriteResultWithTask(t *testing.T) {
	root := t.TempDir()
	result := contract.SuccessResult("hello")
	result.RequestedTarget = "gpt5.5"
	if err := WriteResultWithTask(root, "run-task", "audit-r1", result); err != nil {
		t.Fatal(err)
	}
	records, err := ListFiltered(root, ListFilter{TaskNameGlob: "audit-*"})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].TaskName != "audit-r1" {
		t.Fatalf("records=%+v", records)
	}
}

func TestFindBySessionID(t *testing.T) {
	root := t.TempDir()
	result := contract.SuccessResult("hello")
	result.RequestedTarget = "gpt5.5"
	result.ProviderUsed = "codex"
	result.ModelUsed = "gpt-5.5"
	result.SessionID = "s1"
	if err := WriteResult(root, "run-1", result); err != nil {
		t.Fatal(err)
	}
	record, ok, err := FindBySessionID(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.Result == nil || record.Result.ProviderUsed != "codex" {
		t.Fatalf("record=%+v ok=%v", record, ok)
	}
}

func TestReadRejectsPathTraversal(t *testing.T) {
	if _, err := Read(t.TempDir(), "../outside"); err == nil {
		t.Fatal("expected invalid run id error")
	}
}

func TestGeneratedRunIDsIncludeEntropy(t *testing.T) {
	now := time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC)
	first := NewRunID(now)
	second := NewRunID(now)
	if first == second {
		t.Fatalf("run ids should differ: %q", first)
	}
}
