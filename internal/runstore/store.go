package runstore

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/contract"
)

type RunRecord struct {
	RunID     string                   `json:"run_id"`
	CreatedAt string                   `json:"created_at"`
	TaskName  string                   `json:"task_name,omitempty"`
	Target    string                   `json:"target,omitempty"`
	Status    contract.Status          `json:"status,omitempty"`
	Result    *contract.ProviderResult `json:"result,omitempty"`
	Path      string                   `json:"path"`
}

type ListFilter struct {
	Status       contract.Status
	Target       string
	TaskNameGlob string
	FailureClass contract.FailureClass
	Since        time.Time
	Limit        int
}

type InvalidRecord struct {
	RunID string `json:"run_id"`
	Error string `json:"error"`
}

type InvalidRecordsError struct {
	Records []InvalidRecord
}

func (e *InvalidRecordsError) Error() string {
	if e == nil || len(e.Records) == 0 {
		return "invalid run record"
	}
	runIDs := make([]string, 0, len(e.Records))
	for _, record := range e.Records {
		runIDs = append(runIDs, record.RunID)
	}
	return "invalid run record(s): " + strings.Join(runIDs, ", ")
}

func DefaultRoot() string {
	return config.RunsDir()
}

func NewRunID(now time.Time) string {
	return now.Format("20060102-150405.000000000") + "-" + randomSuffix()
}

func randomSuffix() string {
	var data [3]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06x", data)
}

func WriteResult(root string, runID string, result contract.ProviderResult) error {
	return WriteResultWithTask(root, runID, "", result)
}

func WriteResultWithTask(root string, runID string, taskName string, result contract.ProviderResult) error {
	if root == "" {
		root = DefaultRoot()
	}
	if runID == "" {
		runID = NewRunID(time.Now())
	}
	if !validRunID(runID) {
		return fmt.Errorf("invalid run id")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	dir := filepath.Join(root, runID)
	if _, err := os.Lstat(dir); err == nil {
		return fmt.Errorf("run %q already exists", runID)
	} else if !os.IsNotExist(err) {
		return err
	}
	stagingDir, err := os.MkdirTemp(root, "."+runID+".tmp-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stagingDir)
	record := RunRecord{
		RunID:     runID,
		CreatedAt: time.Now().Format(time.RFC3339),
		TaskName:  taskName,
		Target:    result.RequestedTarget,
		Status:    result.Status,
		Result:    &result,
		Path:      dir,
	}
	meta, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(stagingDir, "run.json"), append(meta, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(stagingDir, dir); err != nil {
		return fmt.Errorf("publish run %q: %w", runID, err)
	}
	return nil
}

func List(root string) ([]RunRecord, error) {
	return ListFiltered(root, ListFilter{})
}

func ListFiltered(root string, filter ListFilter) ([]RunRecord, error) {
	if root == "" {
		root = DefaultRoot()
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []RunRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := []RunRecord{}
	invalid := []InvalidRecord{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !listableRunDirName(entry.Name()) {
			continue
		}
		record, err := Read(root, entry.Name())
		if err != nil {
			invalid = append(invalid, InvalidRecord{RunID: entry.Name(), Error: err.Error()})
			continue
		}
		if !matchesFilter(record, filter) {
			continue
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].RunID > records[j].RunID
	})
	if filter.Limit > 0 && len(records) > filter.Limit {
		records = records[:filter.Limit]
	}
	if len(invalid) > 0 {
		sort.Slice(invalid, func(i, j int) bool {
			return invalid[i].RunID < invalid[j].RunID
		})
		return records, &InvalidRecordsError{Records: invalid}
	}
	return records, nil
}

func matchesFilter(record RunRecord, filter ListFilter) bool {
	if filter.Status != "" && record.Status != filter.Status {
		return false
	}
	if filter.Target != "" && record.Target != filter.Target {
		return false
	}
	if filter.TaskNameGlob != "" && !globMatch(filter.TaskNameGlob, record.TaskName) {
		return false
	}
	if filter.FailureClass != "" {
		if record.Result == nil || record.Result.FailureClass == nil || *record.Result.FailureClass != filter.FailureClass {
			return false
		}
	}
	if !filter.Since.IsZero() {
		createdAt, err := time.Parse(time.RFC3339, record.CreatedAt)
		if err != nil || createdAt.Before(filter.Since) {
			return false
		}
	}
	return true
}

func globMatch(pattern string, value string) bool {
	if pattern == value {
		return true
	}
	ok, err := filepath.Match(pattern, value)
	if err == nil {
		return ok
	}
	return strings.Contains(value, pattern)
}

func Read(root string, query string) (RunRecord, error) {
	if root == "" {
		root = DefaultRoot()
	}
	if query == "" {
		return RunRecord{}, fmt.Errorf("run id is required")
	}
	if !validRunID(query) {
		return RunRecord{}, fmt.Errorf("invalid run id")
	}
	dir := filepath.Join(root, query)
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RunRecord{}, fmt.Errorf("run.json is missing")
		}
		var pathErr *os.PathError
		if errors.As(err, &pathErr) {
			return RunRecord{}, fmt.Errorf("cannot read run.json: %v", pathErr.Err)
		}
		return RunRecord{}, fmt.Errorf("cannot read run.json: %v", err)
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, err
	}
	if record.RunID != query {
		return RunRecord{}, fmt.Errorf("run id mismatch: record has %q", record.RunID)
	}
	if record.Result == nil {
		return RunRecord{}, fmt.Errorf("run result is missing")
	}
	if _, err := time.Parse(time.RFC3339, record.CreatedAt); err != nil {
		return RunRecord{}, fmt.Errorf("invalid created_at: %w", err)
	}
	// The embedded result is the canonical source for user-visible run state.
	record.Target = record.Result.RequestedTarget
	record.Status = record.Result.Status
	record.Path = dir
	return record, nil
}

func validRunID(value string) bool {
	if value == "" || strings.Contains(value, "..") {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_', '.':
			continue
		default:
			return false
		}
	}
	return true
}

func listableRunDirName(value string) bool {
	if !validRunID(value) {
		return false
	}
	if strings.HasPrefix(value, "run-") {
		return true
	}
	if len(value) < len("20060102-150405.000000000-000000") {
		return false
	}
	if value[len("20060102-150405.000000000")] != '-' {
		return false
	}
	_, err := time.Parse("20060102-150405.000000000", value[:len("20060102-150405.000000000")])
	return err == nil
}

func FindBySessionID(root string, sessionID string) (RunRecord, bool, error) {
	if sessionID == "" {
		return RunRecord{}, false, nil
	}
	records, err := List(root)
	if err != nil {
		var invalid *InvalidRecordsError
		if !errors.As(err, &invalid) {
			return RunRecord{}, false, err
		}
	}
	for _, record := range records {
		if record.Result != nil && record.Result.SessionID == sessionID {
			return record, true, nil
		}
	}
	return RunRecord{}, false, nil
}
