package cli

import (
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

type failureSummary struct {
	SchemaVersion   string          `json:"schema_version"`
	Since           string          `json:"since,omitempty"`
	Total           int             `json:"total"`
	Returned        int             `json:"returned"`
	IncludeDegraded bool            `json:"include_degraded"`
	ByProvider      map[string]int  `json:"by_provider"`
	ByTarget        map[string]int  `json:"by_target"`
	ByStatus        map[string]int  `json:"by_status"`
	ByFailureClass  map[string]int  `json:"by_failure_class"`
	ByFingerprint   map[string]int  `json:"by_fingerprint"`
	Records         []failureRecord `json:"records"`
}

type failureRecord struct {
	RunID           string `json:"run_id"`
	CreatedAt       string `json:"created_at"`
	TaskName        string `json:"task_name,omitempty"`
	Target          string `json:"target,omitempty"`
	RequestedTarget string `json:"requested_target,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	Status          string `json:"status,omitempty"`
	FailureClass    string `json:"failure_class,omitempty"`
	Degraded        bool   `json:"degraded"`
	DegradeReason   string `json:"degrade_reason,omitempty"`
	NextAction      string `json:"next_action,omitempty"`
	ExitCode        int    `json:"exit_code"`
	DurationMS      int64  `json:"duration_ms"`
	Fingerprint     string `json:"fingerprint"`
	StderrSummary   string `json:"stderr_summary,omitempty"`
	Path            string `json:"path,omitempty"`
}

func runsFailures(argv []string, stderr io.Writer) (failureSummary, error) {
	fs := flag.NewFlagSet("runs failures", flag.ContinueOnError)
	fs.SetOutput(stderr)
	status := fs.String("status", "", "filter by status")
	target := fs.String("target", "", "filter by target")
	taskName := fs.String("task-name", "", "filter by task name glob")
	failureClass := fs.String("failure-class", "", "filter by failure class")
	since := fs.String("since", "", "filter by RFC3339 timestamp or relative duration like 24h/7d")
	limit := fs.Int("limit", 50, "maximum number of failure records to return; 0 returns all")
	includeDegraded := fs.Bool("include-degraded", true, "include successful runs that degraded to a fallback provider")
	_ = fs.String("format", "json", "json")
	if err := fs.Parse(argv); err != nil {
		return failureSummary{}, err
	}
	filter := runstore.ListFilter{
		Status:       contract.Status(*status),
		Target:       *target,
		TaskNameGlob: *taskName,
		FailureClass: contract.FailureClass(*failureClass),
	}
	if *since != "" {
		t, err := parseSince(*since, time.Now())
		if err != nil {
			return failureSummary{}, fmt.Errorf("--since must be RFC3339 or relative duration like 24h/7d: %w", err)
		}
		filter.Since = t
	}
	records, err := runstore.ListFiltered("", filter)
	if err != nil {
		return failureSummary{}, err
	}
	summary := failureSummary{
		SchemaVersion:   "1.0",
		IncludeDegraded: *includeDegraded,
		ByProvider:      map[string]int{},
		ByTarget:        map[string]int{},
		ByStatus:        map[string]int{},
		ByFailureClass:  map[string]int{},
		ByFingerprint:   map[string]int{},
		Records:         []failureRecord{},
	}
	if *since != "" {
		summary.Since = *since
	}
	for _, record := range records {
		if record.Result == nil || !isFailureRecord(*record.Result, *includeDegraded) {
			continue
		}
		item := summarizeFailureRecord(record)
		summary.Total++
		increment(summary.ByProvider, firstNonEmpty(item.Provider, "unknown"))
		increment(summary.ByTarget, firstNonEmpty(item.RequestedTarget, item.Target, "unknown"))
		increment(summary.ByStatus, firstNonEmpty(item.Status, "unknown"))
		increment(summary.ByFailureClass, firstNonEmpty(item.FailureClass, "none"))
		increment(summary.ByFingerprint, firstNonEmpty(item.Fingerprint, "other"))
		if *limit == 0 || len(summary.Records) < *limit {
			summary.Records = append(summary.Records, item)
		}
	}
	summary.Returned = len(summary.Records)
	return summary, nil
}

func isFailureRecord(result contract.ProviderResult, includeDegraded bool) bool {
	if !result.OK {
		return true
	}
	return includeDegraded && result.Degraded
}

func summarizeFailureRecord(record runstore.RunRecord) failureRecord {
	result := record.Result
	failureClass := ""
	if result.FailureClass != nil {
		failureClass = string(*result.FailureClass)
	}
	return failureRecord{
		RunID:           record.RunID,
		CreatedAt:       record.CreatedAt,
		TaskName:        record.TaskName,
		Target:          record.Target,
		RequestedTarget: result.RequestedTarget,
		Provider:        result.ProviderUsed,
		Model:           result.ModelUsed,
		Status:          string(result.Status),
		FailureClass:    failureClass,
		Degraded:        result.Degraded,
		DegradeReason:   result.DegradeReason,
		NextAction:      string(result.NextAction),
		ExitCode:        result.ExitCode,
		DurationMS:      result.DurationMS,
		Fingerprint:     failureFingerprint(*result),
		StderrSummary:   summarizeStderr(result.Stderr, 220),
		Path:            record.Path,
	}
}

func failureFingerprint(result contract.ProviderResult) string {
	message := strings.ToLower(strings.Join([]string{
		result.Stderr,
		result.DegradeReason,
		string(result.Status),
		string(result.NextAction),
	}, "\n"))
	switch {
	case result.Degraded:
		return "degraded"
	case strings.Contains(message, "database is locked"):
		return "database_locked"
	case strings.Contains(message, "timed out") || result.Status == contract.StatusTimeout:
		return "timeout"
	case strings.Contains(message, "returned no successful result"):
		return "no_successful_result"
	case strings.Contains(message, "permission requested") && strings.Contains(message, "auto-reject"):
		return "permission_autoreject"
	case strings.Contains(message, "unknown model"):
		return "unknown_model"
	case strings.Contains(message, "executable file not found") || strings.Contains(message, "command not found"):
		return "missing_binary"
	case strings.Contains(message, "quota") || strings.Contains(message, "usage limit") || strings.Contains(message, "rate limit") || strings.Contains(message, "insufficient credits"):
		return "quota_or_limit"
	case strings.Contains(message, "error sending request") || strings.Contains(message, "connection closed") || strings.Contains(message, "no such host") || strings.Contains(message, "network"):
		return "network"
	default:
		return "other"
	}
}

func summarizeStderr(value string, limit int) string {
	value = strings.Join(strings.Fields(stripANSIEscapes(value)), " ")
	if limit <= 0 || len([]rune(value)) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

func stripANSIEscapes(value string) string {
	var builder strings.Builder
	for i := 0; i < len(value); i++ {
		if value[i] == 0x1b && i+1 < len(value) && value[i+1] == '[' {
			i += 2
			for i < len(value) {
				ch := value[i]
				if (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') {
					break
				}
				i++
			}
			continue
		}
		builder.WriteByte(value[i])
	}
	return builder.String()
}

func increment(values map[string]int, key string) {
	values[key]++
}

func parseSince(value string, now time.Time) (time.Time, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return time.Time{}, nil
	}
	if parsed, err := time.Parse(time.RFC3339, trimmed); err == nil {
		return parsed, nil
	}
	if strings.HasSuffix(trimmed, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(trimmed, "d"))
		if err != nil || days < 0 {
			return time.Time{}, fmt.Errorf("invalid day duration %q", value)
		}
		return now.AddDate(0, 0, -days), nil
	}
	duration, err := time.ParseDuration(trimmed)
	if err != nil {
		return time.Time{}, err
	}
	if duration < 0 {
		return time.Time{}, fmt.Errorf("duration must be positive")
	}
	return now.Add(-duration), nil
}
