package runstore

import (
	"encoding/json"
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

func DefaultRoot() string {
	return config.RunsDir()
}

func NewRunID(now time.Time) string {
	return now.Format("20060102-150405.000")
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
	dir := filepath.Join(root, runID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	resultPath := filepath.Join(dir, "result.json")
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(resultPath, append(data, '\n'), 0o600); err != nil {
		return err
	}
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
	return os.WriteFile(filepath.Join(dir, "run.json"), append(meta, '\n'), 0o600)
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
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		record, err := Read(root, entry.Name())
		if err != nil {
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
	dir := filepath.Join(root, query)
	data, err := os.ReadFile(filepath.Join(dir, "run.json"))
	if err != nil {
		return RunRecord{}, err
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, err
	}
	if record.Path == "" {
		record.Path = dir
	}
	return record, nil
}

func FindBySessionID(root string, sessionID string) (RunRecord, bool, error) {
	if sessionID == "" {
		return RunRecord{}, false, nil
	}
	records, err := List(root)
	if err != nil {
		return RunRecord{}, false, err
	}
	for _, record := range records {
		if record.Result != nil && record.Result.SessionID == sessionID {
			return record, true, nil
		}
	}
	return RunRecord{}, false, nil
}
