package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

func runs(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "ai-dispatch runs: expected list, show, tail, or failures")
		return 2
	}
	switch argv[0] {
	case "list":
		filter, err := parseRunsListFlags(argv[1:], stderr)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(stderr, "ai-dispatch runs list:", err)
			return 2
		}
		records, err := runstore.ListFiltered("", filter)
		if err := surfaceInvalidRunRecords(err, stderr); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch runs list:", err)
			return 1
		}
		return writeJSON(stdout, summarizeRunRecords(records), 0)
	case "failures":
		payload, err := runsFailures(argv[1:], stderr)
		if err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0
			}
			fmt.Fprintln(stderr, "ai-dispatch runs failures:", err)
			return 2
		}
		return writeJSON(stdout, payload, 0)
	case "show", "tail":
		if len(argv) < 2 {
			fmt.Fprintf(stderr, "ai-dispatch runs %s: missing run id\n", argv[0])
			return 2
		}
		record, err := runstore.Read("", argv[1])
		if err != nil {
			fmt.Fprintf(stderr, "ai-dispatch runs %s: %v\n", argv[0], err)
			return 1
		}
		return writeJSON(stdout, record, 0)
	default:
		fmt.Fprintf(stderr, "ai-dispatch runs: unknown subcommand %q\n", argv[0])
		return 2
	}
}

func surfaceInvalidRunRecords(err error, stderr io.Writer) error {
	if err == nil {
		return nil
	}
	var invalid *runstore.InvalidRecordsError
	if !errors.As(err, &invalid) {
		return err
	}
	for _, record := range invalid.Records {
		fmt.Fprintf(stderr, "ai-dispatch runs: skipped invalid run record %s: %s\n", record.RunID, record.Error)
	}
	return nil
}

func parseRunsListFlags(argv []string, stderr io.Writer) (runstore.ListFilter, error) {
	fs := flag.NewFlagSet("runs list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	status := fs.String("status", "", "filter by status")
	target := fs.String("target", "", "filter by target")
	taskName := fs.String("task-name", "", "filter by task name glob")
	failureClass := fs.String("failure-class", "", "filter by failure class")
	since := fs.String("since", "", "filter by RFC3339 timestamp or relative duration like 24h/7d")
	limit := fs.Int("limit", 0, "maximum number of records")
	if err := fs.Parse(argv); err != nil {
		return runstore.ListFilter{}, err
	}
	filter := runstore.ListFilter{
		Status:       contract.Status(*status),
		Target:       *target,
		TaskNameGlob: *taskName,
		FailureClass: contract.FailureClass(*failureClass),
		Limit:        *limit,
	}
	if *since != "" {
		t, err := parseSince(*since, time.Now())
		if err != nil {
			return runstore.ListFilter{}, fmt.Errorf("--since must be RFC3339 or relative duration like 24h/7d: %w", err)
		}
		filter.Since = t
	}
	return filter, nil
}

type runListSummary struct {
	RunID                string                `json:"run_id"`
	CreatedAt            string                `json:"created_at"`
	TaskName             string                `json:"task_name,omitempty"`
	Target               string                `json:"target,omitempty"`
	Status               contract.Status       `json:"status,omitempty"`
	ProviderUsed         string                `json:"provider_used,omitempty"`
	ModelUsed            string                `json:"model_used,omitempty"`
	RequestedTarget      string                `json:"requested_target,omitempty"`
	Degraded             bool                  `json:"degraded,omitempty"`
	DegradeReason        string                `json:"degrade_reason,omitempty"`
	RequestedEffort      contract.Effort       `json:"requested_effort,omitempty"`
	AppliedEffort        contract.Effort       `json:"applied_effort,omitempty"`
	EffortFallbackReason string                `json:"effort_fallback_reason,omitempty"`
	FailureClass         contract.FailureClass `json:"failure_class,omitempty"`
	NextAction           contract.NextAction   `json:"next_action,omitempty"`
	ExitCode             int                   `json:"exit_code,omitempty"`
	DurationMS           int64                 `json:"duration_ms,omitempty"`
	WarningsCount        int                   `json:"warnings_count,omitempty"`
	StderrBytes          int                   `json:"stderr_bytes,omitempty"`
	OutputFile           string                `json:"output_file,omitempty"`
	Path                 string                `json:"path"`
}

func summarizeRunRecords(records []runstore.RunRecord) []runListSummary {
	summaries := make([]runListSummary, 0, len(records))
	for _, record := range records {
		summaries = append(summaries, summarizeRunRecord(record))
	}
	return summaries
}

func summarizeRunRecord(record runstore.RunRecord) runListSummary {
	summary := runListSummary{
		RunID:     record.RunID,
		CreatedAt: record.CreatedAt,
		TaskName:  record.TaskName,
		Target:    record.Target,
		Status:    record.Status,
		Path:      record.Path,
	}
	if record.Result == nil {
		return summary
	}
	result := record.Result
	summary.ProviderUsed = result.ProviderUsed
	summary.ModelUsed = result.ModelUsed
	summary.RequestedTarget = result.RequestedTarget
	summary.Degraded = result.Degraded
	summary.DegradeReason = result.DegradeReason
	summary.RequestedEffort = result.RequestedEffort
	summary.AppliedEffort = result.AppliedEffort
	summary.EffortFallbackReason = result.EffortFallbackReason
	if result.FailureClass != nil {
		summary.FailureClass = *result.FailureClass
	}
	summary.NextAction = result.NextAction
	summary.ExitCode = result.ExitCode
	summary.DurationMS = result.DurationMS
	summary.WarningsCount = len(result.Warnings)
	summary.StderrBytes = len(result.Stderr)
	summary.OutputFile = result.OutputFile
	return summary
}
