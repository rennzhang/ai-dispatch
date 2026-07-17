package runstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

func TestWritePublishesOneCanonicalResultFile(t *testing.T) {
	root := t.TempDir()
	result := contract.SuccessResult("hello")
	result.RequestedTarget = "gpt5.5"
	if err := WriteResult(root, "run-canonical", result); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "run-canonical"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "run.json" {
		t.Fatalf("run directory must contain one canonical record: %v", entries)
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
	valid := contract.SuccessResult("ok")
	valid.RequestedTarget = "gpt5.5"
	if err := WriteResult(root, "run-valid", valid); err != nil {
		t.Fatal(err)
	}
	runID := "run-corrupt"
	if err := os.Mkdir(filepath.Join(root, runID), 0o755); err != nil {
		t.Fatal(err)
	}

	records, err := ListFiltered(root, ListFilter{})
	if err == nil || !strings.Contains(err.Error(), runID) {
		t.Fatalf("err=%v", err)
	}
	if len(records) != 1 || records[0].RunID != "run-valid" {
		t.Fatalf("valid records must remain available: %+v", records)
	}
	var invalid *InvalidRecordsError
	if !errors.As(err, &invalid) || len(invalid.Records) != 1 || invalid.Records[0].RunID != runID || invalid.Records[0].Error == "" {
		t.Fatalf("invalid records=%+v err=%v", invalid, err)
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

func TestFindBySessionIDIgnoresUnrelatedCorruptRecord(t *testing.T) {
	root := t.TempDir()
	result := contract.SuccessResult("hello")
	result.RequestedTarget = "gpt5.5"
	result.ProviderUsed = "codex"
	result.SessionID = "s1"
	if err := WriteResult(root, "run-valid", result); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "run-corrupt"), 0o755); err != nil {
		t.Fatal(err)
	}
	record, ok, err := FindBySessionID(root, "s1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok || record.RunID != "run-valid" {
		t.Fatalf("record=%+v ok=%v", record, ok)
	}
}

func TestConcurrentWritesPublishOnlyCompleteRecords(t *testing.T) {
	root := t.TempDir()
	const count = 24
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result := contract.SuccessResult(fmt.Sprintf("result-%d", i))
			result.RequestedTarget = "gpt5.5"
			errs <- WriteResult(root, fmt.Sprintf("run-%02d", i), result)
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	records, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != count {
		t.Fatalf("records=%d want=%d", len(records), count)
	}
}

func TestConcurrentSameRunIDHasOneImmutableWinner(t *testing.T) {
	root := t.TempDir()
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, text := range []string{"first", "second"} {
		wg.Add(1)
		go func(text string) {
			defer wg.Done()
			result := contract.SuccessResult(text)
			result.RequestedTarget = "gpt5.5"
			errs <- WriteResult(root, "run-same", result)
		}(text)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("successful writes=%d want=1", successes)
	}
	if _, err := Read(root, "run-same"); err != nil {
		t.Fatal(err)
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
