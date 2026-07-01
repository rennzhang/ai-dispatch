package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/dispatch"
	"github.com/rennzhang/ai-dispatch/internal/providers/antigravity"
	"github.com/rennzhang/ai-dispatch/internal/providers/claude"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runstore"
)

func Main(argv []string, stdout io.Writer, stderr io.Writer) int {
	return MainWithInput(argv, stdout, stderr, nil)
}

func MainWithInput(argv []string, stdout io.Writer, stderr io.Writer, stdin io.Reader) int {
	if len(argv) > 0 && argv[0] == "__claude-pty-driver" {
		return claude.RunPTYDriverCLI(argv[1:], stdout, stderr)
	}
	if len(argv) > 0 && argv[0] == "__agy-driver" {
		return antigravity.RunAgyDriverCLI(argv[1:], stdout, stderr)
	}
	args := append([]string(nil), argv...)
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}
	switch args[0] {
	case "config":
		return configCommand(args[1:], stdout, stderr)
	case "doctor":
		return doctor(args[1:], stdout, stderr)
	case "guide":
		return guide(args[1:], stdout, stderr)
	case "init":
		return initConfig(args[1:], stdout, stderr)
	case "models":
		return models(args[1:], stdout, stderr)
	case "runs":
		return runs(args[1:], stdout, stderr)
	case "send":
		return send(args[1:], stdout, stderr, stdin)
	case "resume":
		return resume(args[1:], stdout, stderr, stdin)
	case "skill":
		return skill(args[1:], stdout, stderr)
	default:
		return emitCLIError(stdout, stderr, false, fmt.Sprintf("unknown command: %s", args[0]), 2)
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "ai-dispatch go")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  ai-dispatch send <target> [prompt] [flags]")
	fmt.Fprintln(w, "  ai-dispatch resume --session-id <id> [--target <target>] [prompt] [flags]")
	fmt.Fprintln(w, "  ai-dispatch config path|show")
	fmt.Fprintln(w, "  ai-dispatch guide models")
	fmt.Fprintln(w, "  ai-dispatch runs list|show|tail|failures")
	fmt.Fprintln(w, "  ai-dispatch init [--claude-transport print|pty|auto|disabled]")
	fmt.Fprintln(w, "  ai-dispatch skill install --target codex|claude|all")
	fmt.Fprintln(w, "  ai-dispatch doctor --format json")
	fmt.Fprintln(w, "  ai-dispatch models --format json")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Common flags:")
	fmt.Fprintln(w, "  --json-result                  write ProviderResult JSON to stdout")
	fmt.Fprintln(w, "  --stream-progress              write progress NDJSON to stderr")
	fmt.Fprintln(w, "  --provider-opt key=value        provider option, e.g. claude.transport=print|pty|auto")
	fmt.Fprintln(w, "  --activity-timeout seconds      fail after no provider activity; 0 disables")
}

func initConfig(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	fs.SetOutput(stderr)
	claudeTransport := fs.String("claude-transport", "print", "print, pty, auto, or disabled")
	force := fs.Bool("force", false, "overwrite existing config")
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if !config.ValidClaudeTransport(*claudeTransport) {
		return emitCLIError(stdout, stderr, *format == "json", "--claude-transport must be print, pty, auto, or disabled", 2)
	}
	cfg, err := config.Init(config.InitOptions{
		ClaudeTransport: *claudeTransport,
		Force:           *force,
	})
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 1)
	}
	payload := map[string]any{
		"ok":               true,
		"home":             config.HomeDir(),
		"config":           config.ConfigPath(),
		"runs":             config.RunsDir(),
		"cache":            config.CacheDir(),
		"logs":             config.LogsDir(),
		"claude_transport": cfg.ClaudeTransport,
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintf(stdout, "ai-dispatch initialized at %s\n", config.HomeDir())
	fmt.Fprintf(stdout, "config: %s\n", config.ConfigPath())
	return 0
}

func configCommand(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch config path")
		fmt.Fprintln(stdout, "  ai-dispatch config show")
		return 0
	}
	switch argv[0] {
	case "path":
		fmt.Fprintln(stdout, config.ConfigPath())
		return 0
	case "show":
		cfg, err := config.Load()
		if err != nil {
			return emitCLIError(stdout, stderr, false, err.Error(), 1)
		}
		data, err := json.MarshalIndent(cfg, "", "  ")
		if err != nil {
			return emitCLIError(stdout, stderr, false, err.Error(), 1)
		}
		fmt.Fprintln(stdout, string(data))
		return 0
	default:
		fmt.Fprintf(stderr, "ai-dispatch config: unknown subcommand %q\n", argv[0])
		return 2
	}
}

func doctor(argv []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	cfg, cfgErr := config.Load()
	payload := map[string]any{
		"ok":                 true,
		"runtime":            "go",
		"provider_execution": providerExecutionStatus(),
		"contract":           "2.0",
		"home":               config.HomeDir(),
		"config_path":        config.ConfigPath(),
		"runs_dir":           config.RunsDir(),
		"cache_dir":          config.CacheDir(),
		"claude_env":         claudeEnvStatus(),
	}
	if cfgErr != nil {
		payload["ok"] = false
		payload["config_error"] = cfgErr.Error()
	} else {
		payload["config"] = cfg
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintln(stdout, "ai-dispatch go: ok")
	return 0
}

func providerExecutionStatus() string {
	if os.Getenv("AI_DISPATCH_GO_PROVIDER_EXECUTION") == "on" {
		return "enabled"
	}
	return "disabled_by_default"
}

func claudeEnvStatus() map[string]any {
	status := map[string]any{
		"api_env_present": false,
	}
	model := os.Getenv("ANTHROPIC_MODEL")
	baseURL := os.Getenv("ANTHROPIC_BASE_URL")
	if model != "" {
		status["model"] = model
	}
	if baseURL != "" {
		status["base_url"] = baseURL
	}
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if strings.TrimSpace(os.Getenv(key)) != "" {
			status["api_env_present"] = true
			break
		}
	}
	return status
}

func sanitizedClaudeProviderConfig(raw string) (model string, baseURL string, hasAPIBackend bool) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return "", "", false
	}
	env, ok := payload["env"].(map[string]any)
	if !ok {
		return "", "", false
	}
	if value, ok := env["ANTHROPIC_MODEL"].(string); ok {
		model = value
	}
	if value, ok := env["ANTHROPIC_BASE_URL"].(string); ok {
		baseURL = value
	}
	for _, key := range []string{"ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"} {
		if value, ok := env[key].(string); ok && strings.TrimSpace(value) != "" {
			hasAPIBackend = true
			break
		}
	}
	return model, baseURL, hasAPIBackend
}

func models(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) > 0 && argv[0] == "resolve" {
		return modelsResolve(argv[1:], stdout, stderr)
	}
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "text", "text or json")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	payload := map[string]any{
		"ok":      true,
		"targets": routing.SupportedTargets(),
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintln(stdout, strings.Join(payload["targets"].([]string), "\n"))
	return 0
}

func modelsResolve(argv []string, stdout io.Writer, stderr io.Writer) int {
	argv = reorderInterspersedFlags(argv)
	fs := flag.NewFlagSet("models resolve", flag.ContinueOnError)
	fs.SetOutput(stderr)
	format := fs.String("format", "json", "json")
	model := fs.String("model", "", "explicit model")
	if err := fs.Parse(argv); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	args := fs.Args()
	if len(args) != 1 {
		fmt.Fprintln(stderr, "ai-dispatch models resolve: expected exactly one target")
		return 2
	}
	target, err := routing.Resolve(args[0], *model)
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	return writeJSON(stdout, target, 0)
}

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
		if err != nil {
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
	RunID           string                `json:"run_id"`
	CreatedAt       string                `json:"created_at"`
	TaskName        string                `json:"task_name,omitempty"`
	Target          string                `json:"target,omitempty"`
	Status          contract.Status       `json:"status,omitempty"`
	ProviderUsed    string                `json:"provider_used,omitempty"`
	ModelUsed       string                `json:"model_used,omitempty"`
	RequestedTarget string                `json:"requested_target,omitempty"`
	Degraded        bool                  `json:"degraded,omitempty"`
	DegradeReason   string                `json:"degrade_reason,omitempty"`
	FailureClass    contract.FailureClass `json:"failure_class,omitempty"`
	NextAction      contract.NextAction   `json:"next_action,omitempty"`
	ExitCode        int                   `json:"exit_code,omitempty"`
	DurationMS      int64                 `json:"duration_ms,omitempty"`
	WarningsCount   int                   `json:"warnings_count,omitempty"`
	StderrBytes     int                   `json:"stderr_bytes,omitempty"`
	OutputFile      string                `json:"output_file,omitempty"`
	Path            string                `json:"path"`
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

func send(argv []string, stdout io.Writer, stderr io.Writer, stdin io.Reader) int {
	req, jsonResult, err := parseSend("send", argv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return emitCLIError(stdout, stderr, jsonResult, err.Error(), 2)
	}
	if req.Target == "" {
		return emitCLIError(stdout, stderr, jsonResult, "target is required", 2)
	}
	if err := prepareRequest(&req, stdin); err != nil {
		return emitCLIError(stdout, stderr, jsonResult, err.Error(), 2)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return emitCLIError(stdout, stderr, jsonResult, "Either prompt, --prompt-file, or stdin input is required", 2)
	}
	result := dispatch.ExecuteWithOptions(req, dispatch.Options{ProgressWriter: progressWriter(req, stderr)})
	if jsonResult {
		return writeProviderResult(stdout, result)
	}
	if result.OK {
		if result.Text != "" && result.OutputFile == "" {
			fmt.Fprintln(stdout, result.Text)
		}
		return 0
	}
	fmt.Fprintln(stderr, result.Stderr)
	return result.ExitCode
}

func resume(argv []string, stdout io.Writer, stderr io.Writer, stdin io.Reader) int {
	req, jsonResult, err := parseSend("resume", argv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return emitCLIError(stdout, stderr, jsonResult, err.Error(), 2)
	}
	if req.SessionID == "" {
		return emitCLIError(stdout, stderr, jsonResult, "--session-id is required", 2)
	}
	if err := prepareRequest(&req, stdin); err != nil {
		return emitCLIError(stdout, stderr, jsonResult, err.Error(), 2)
	}
	if strings.TrimSpace(req.Prompt) == "" {
		return emitCLIError(stdout, stderr, jsonResult, "Either prompt, --prompt-file, or stdin input is required", 2)
	}
	result := dispatch.ExecuteWithOptions(req, dispatch.Options{ProgressWriter: progressWriter(req, stderr)})
	if jsonResult {
		return writeProviderResult(stdout, result)
	}
	fmt.Fprintln(stderr, result.Stderr)
	return 3
}

func prepareRequest(req *contract.DispatchRequest, stdin io.Reader) error {
	if req.CWD != "" {
		info, err := os.Stat(req.CWD)
		if err != nil {
			return fmt.Errorf("--cwd is not accessible: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("--cwd must be a directory")
		}
	}
	if req.PromptFile != "" {
		data, err := os.ReadFile(req.PromptFile)
		if err != nil {
			return fmt.Errorf("cannot read --prompt-file: %w", err)
		}
		if len(data) > 2_000_000 {
			return fmt.Errorf("--prompt-file is too large; keep it under 2MB")
		}
		req.Prompt = string(data)
		return validatePromptFile(req.Prompt)
	}
	if strings.TrimSpace(req.Prompt) != "" {
		return validatePrompt(req.Prompt)
	}
	if stdin == nil {
		return nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return fmt.Errorf("cannot read stdin: %w", err)
	}
	req.Prompt = string(data)
	return validatePrompt(req.Prompt)
}

func progressWriter(req contract.DispatchRequest, stderr io.Writer) io.Writer {
	if !req.StreamProgress {
		return nil
	}
	return stderr
}

func validatePrompt(prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "-" {
		return fmt.Errorf("prompt '-' is ambiguous; pipe stdin or provide real prompt text")
	}
	if len([]rune(trimmed)) == 1 {
		return fmt.Errorf("prompt is too short")
	}
	if len([]rune(prompt)) > 20000 {
		return fmt.Errorf("inline prompt is too large; use --prompt-file")
	}
	return nil
}

func validatePromptFile(prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("--prompt-file is empty")
	}
	if trimmed == "-" {
		return fmt.Errorf("prompt '-' is ambiguous; pipe stdin or provide real prompt text")
	}
	return nil
}

func parseSend(command string, argv []string, stderr io.Writer) (contract.DispatchRequest, bool, error) {
	if err := rejectRetiredArgs(argv); err != nil {
		return contract.DispatchRequest{}, containsArg(argv, "--json-result"), err
	}
	argv = reorderInterspersedFlags(argv)
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(stderr)
	promptFile := fs.String("prompt-file", "", "read prompt from file")
	outputFile := fs.String("output-file", "", "write response body to file")
	outputFileShort := fs.String("o", "", "write response body to file")
	model := fs.String("model", "", "provider model override")
	modelShort := fs.String("m", "", "provider model override")
	targetFlag := fs.String("target", "", "target")
	cwd := fs.String("cwd", "", "working directory")
	sessionID := fs.String("session-id", "", "session id")
	sessionProvider := fs.String("session-provider", "", "session provider")
	jsonResult := fs.Bool("json-result", false, "write ProviderResult JSON to stdout")
	streamProgress := fs.Bool("stream-progress", false, "write progress NDJSON to stderr")
	timeout := fs.Int("timeout", 0, "wall-clock timeout seconds; 0 disables")
	timeoutShort := fs.Int("t", 0, "wall-clock timeout seconds; 0 disables")
	activityTimeout := fs.Int("activity-timeout", -1, "seconds without provider activity before failure; 0 disables")
	callerEnv := fs.String("caller-env", os.Getenv("AI_DISPATCH_CALLER_ENV"), "caller env")
	callerProvider := fs.String("caller-provider", os.Getenv("AI_DISPATCH_CALLER_PROVIDER"), "caller provider")
	callerModule := fs.String("caller-module", os.Getenv("AI_DISPATCH_CALLER_MODULE"), "caller module")
	taskName := fs.String("task-name", "", "task name")
	noMetaHeader := fs.Bool("no-meta-header", false, "disable meta header")
	providerOpts := multiFlag{}
	fs.Var(&providerOpts, "provider-opt", "provider option, e.g. claude.transport=print|pty|auto")
	if err := fs.Parse(argv); err != nil {
		return contract.DispatchRequest{}, false, err
	}
	args := fs.Args()
	req := contract.DispatchRequest{
		Command:         command,
		PromptFile:      *promptFile,
		OutputFile:      firstNonEmpty(*outputFile, *outputFileShort),
		Model:           firstNonEmpty(*model, *modelShort),
		CWD:             *cwd,
		SessionID:       *sessionID,
		SessionProvider: *sessionProvider,
		JSONResult:      *jsonResult,
		StreamProgress:  *streamProgress,
		TimeoutSeconds:  firstNonZero(*timeout, *timeoutShort),
		TaskName:        *taskName,
		CallerEnv:       *callerEnv,
		CallerProvider:  *callerProvider,
		CallerModule:    *callerModule,
		NoMetaHeader:    *noMetaHeader,
		ProviderOpts:    parseProviderOpts(providerOpts),
	}
	req.Target = *targetFlag
	if command == "resume" {
		if len(args) > 0 {
			req.Prompt = strings.Join(args, " ")
		}
	} else {
		if len(args) > 0 && req.Target == "" {
			req.Target = args[0]
		}
		if len(args) > 1 {
			req.Prompt = strings.Join(args[1:], " ")
		}
	}
	req.ActivityTimeoutSeconds = defaultActivityTimeoutSeconds(*activityTimeout, req.Target, req.Model)
	return req, *jsonResult, nil
}

func rejectRetiredArgs(argv []string) error {
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		name := arg
		if before, _, ok := strings.Cut(arg, "="); ok {
			name = before
		}
		switch name {
		case "--transport", "--claude-transport":
			return fmt.Errorf("%s was removed; use --provider-opt claude.transport=print|pty|auto", name)
		case "--persist-session":
			return fmt.Errorf("%s was removed; sessions persist by provider result and resume with --session-id", name)
		case "--fallback":
			return fmt.Errorf("%s was removed; fallback is owned by route policy", name)
		case "--model-family":
			return fmt.Errorf("%s was removed; use an explicit target or --model", name)
		}
	}
	return nil
}

func containsArg(argv []string, target string) bool {
	for _, arg := range argv {
		if arg == target {
			return true
		}
	}
	return false
}

func defaultActivityTimeoutSeconds(flagValue int, target string, model string) int {
	if flagValue >= 0 {
		return flagValue
	}
	resolved, err := routing.Resolve(target, model)
	if err == nil && resolved.Provider == "opencode" {
		return 300
	}
	return 180
}

func reorderInterspersedFlags(argv []string) []string {
	flags := []string{}
	positionals := []string{}
	for i := 0; i < len(argv); i++ {
		arg := argv[i]
		if isBoolFlag(arg) {
			flags = append(flags, arg)
			continue
		}
		if isValueFlag(arg) {
			flags = append(flags, arg)
			if !strings.Contains(arg, "=") && i+1 < len(argv) {
				flags = append(flags, argv[i+1])
				i++
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...)
}

func isBoolFlag(arg string) bool {
	switch arg {
	case "--json-result", "--stream-progress", "--no-meta-header":
		return true
	default:
		return false
	}
}

func isValueFlag(arg string) bool {
	name := arg
	if before, _, ok := strings.Cut(arg, "="); ok {
		name = before
	}
	switch name {
	case "--prompt-file", "--output-file", "-o", "--model", "-m", "--cwd",
		"--target", "--session-id", "--session-provider", "--timeout", "-t",
		"--activity-timeout", "--caller-env", "--caller-provider",
		"--caller-module", "--task-name", "--provider-opt", "--format":
		return true
	default:
		return false
	}
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func parseProviderOpts(values []string) map[string]map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := map[string]map[string]string{}
	for _, value := range values {
		left, right, ok := strings.Cut(value, "=")
		if !ok {
			continue
		}
		provider, key, ok := strings.Cut(left, ".")
		if !ok || provider == "" || key == "" {
			continue
		}
		if out[provider] == nil {
			out[provider] = map[string]string{}
		}
		out[provider][key] = right
	}
	return out
}

func emitCLIError(stdout io.Writer, stderr io.Writer, jsonResult bool, message string, exitCode int) int {
	if jsonResult {
		result := contract.ErrorResult(contract.StatusError, contract.FailureInput, message, exitCode)
		return writeProviderResult(stdout, result)
	}
	fmt.Fprintln(stderr, "ai-dispatch: error:", message)
	return exitCode
}

func writeProviderResult(stdout io.Writer, result contract.ProviderResult) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(result); err != nil {
		return 1
	}
	if result.ExitCode != 0 {
		return result.ExitCode
	}
	if !result.OK {
		return 1
	}
	return 0
}

func writeJSON(stdout io.Writer, payload any, exitCode int) int {
	enc := json.NewEncoder(stdout)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		return 1
	}
	return exitCode
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
