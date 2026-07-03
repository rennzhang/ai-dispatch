package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/dispatch"
	"github.com/rennzhang/ai-dispatch/internal/providers/antigravity"
	"github.com/rennzhang/ai-dispatch/internal/providers/claude"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/setup"
)

// lastSetupResult holds the result of the most recent setup.Ensure() call
// in this process. It is non-nil only when send/resume created config state, so
// injectFirstRun can attach setup details to the result.
var lastSetupResult *setup.Result

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
	// `runs` only needs a runs/ directory, not a full provider setup.
	// send/resume perform setup after flag parsing so help and input errors stay
	// side-effect free.
	switch args[0] {
	case "runs":
		// Ensure runs directory exists without triggering full config setup.
		_ = os.MkdirAll(config.RunsDir(), 0o755)
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
	case "preferences":
		return preferences(args[1:], stdout, stderr)
	case "providers":
		return providersCommand(args[1:], stdout, stderr)
	case "runs":
		return runs(args[1:], stdout, stderr)
	case "send":
		return send(args[1:], stdout, stderr, stdin)
	case "resume":
		return resume(args[1:], stdout, stderr, stdin)
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
	fmt.Fprintln(w, "  ai-dispatch init [--claude-transport print|pty|auto|disabled] [--force]")
	fmt.Fprintln(w, "  ai-dispatch providers scan [--refresh] [--format json]")
	fmt.Fprintln(w, "  ai-dispatch doctor --format json")
	fmt.Fprintln(w, "  ai-dispatch models --format json")
	fmt.Fprintln(w, "  ai-dispatch preferences path|show|open")
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
	if err := validateFormat(*format); err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 2)
	}
	result, err := setup.Run(setup.Options{
		ClaudeTransport: *claudeTransport,
		Force:           *force,
		ScanProviders:   true,
	})
	if err != nil {
		return emitCLIError(stdout, stderr, *format == "json", err.Error(), 1)
	}
	payload := map[string]any{
		"ok":               true,
		"home":             config.HomeDir(),
		"config":           config.ConfigPath(),
		"runs":             config.RunsDir(),
		"logs":             config.LogsDir(),
		"claude_transport": result.Config.ClaudeTransport,
		"first_run":        result.FirstRun,
	}
	if len(result.Config.Providers) > 0 {
		payload["providers"] = result.Config.Providers
	}
	if *format == "json" {
		return writeJSON(stdout, payload, 0)
	}
	fmt.Fprintf(stdout, "ai-dispatch initialized at %s\n", config.HomeDir())
	fmt.Fprintf(stdout, "config: %s\n", config.ConfigPath())
	for name, ps := range result.Config.Providers {
		status := "unavailable"
		if ps.Available {
			status = "available"
		}
		fmt.Fprintf(stdout, "provider %s: %s\n", name, status)
	}
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
	if strings.TrimSpace(req.Prompt) == "" && req.PromptFile == "" {
		return emitCLIError(stdout, stderr, jsonResult, "Either prompt, --prompt-file, or stdin input is required", 2)
	}
	if code, ok := ensureExecutionSetup(jsonResult, stdout, stderr); !ok {
		return code
	}
	result := dispatch.ExecuteWithOptions(req, dispatch.Options{ProgressWriter: progressWriter(req, stderr)})
	result = injectFirstRun(result, jsonResult, stderr)
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
	if strings.TrimSpace(req.Prompt) == "" && req.PromptFile == "" {
		return emitCLIError(stdout, stderr, jsonResult, "Either prompt, --prompt-file, or stdin input is required", 2)
	}
	if code, ok := ensureExecutionSetup(jsonResult, stdout, stderr); !ok {
		return code
	}
	result := dispatch.ExecuteWithOptions(req, dispatch.Options{ProgressWriter: progressWriter(req, stderr)})
	result = injectFirstRun(result, jsonResult, stderr)
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

func ensureExecutionSetup(jsonResult bool, stdout io.Writer, stderr io.Writer) (int, bool) {
	lastSetupResult = nil
	if config.StateMissing() {
		fmt.Fprintln(stderr, "ai-dispatch: 首次调用，正在初始化配置...")
	}
	br, err := setup.Ensure()
	if err != nil {
		return emitCLIError(stdout, stderr, jsonResult, fmt.Sprintf("config setup failed: %v", err), 1), false
	}
	if br.Summary != nil {
		printSetupSummary(stderr, br.Summary)
		lastSetupResult = &br
		fmt.Fprintln(stderr, "ai-dispatch: 继续执行任务...")
	}
	return 0, true
}

// printSetupSummary writes a human-readable initialization summary to
// stderr so the calling Agent (and user) can see what was created.
func printSetupSummary(stderr io.Writer, s *setup.Summary) {
	fmt.Fprintln(stderr, "ai-dispatch: 配置初始化完成")
	fmt.Fprintf(stderr, "  运行时目录: %s\n", s.HomeDir)
	fmt.Fprintf(stderr, "  配置文件: %s\n", s.ConfigPath)
	fmt.Fprintf(stderr, "  用户偏好: %s\n", s.PreferencesPath)
	fmt.Fprintf(stderr, "  Claude transport: %s\n", s.ClaudeTransport)
	if len(s.Providers) == 0 {
		return
	}
	fmt.Fprintln(stderr, "  Provider 探测:")
	for _, name := range []string{"claude", "codex", "opencode", "antigravity"} {
		ps, ok := s.Providers[name]
		if !ok {
			continue
		}
		if ps.Available {
			version := ps.Version
			if version == "" {
				version = "unknown"
			}
			fmt.Fprintf(stderr, "    %s: available (%s)\n", name, version)
		} else {
			fmt.Fprintf(stderr, "    %s: unavailable\n", name)
		}
	}
}

// injectFirstRun stamps the first ProviderResult only when this process
// actually created config state before executing send/resume.
func injectFirstRun(result contract.ProviderResult, jsonResult bool, stderr io.Writer) contract.ProviderResult {
	if lastSetupResult == nil || lastSetupResult.Summary == nil {
		return result
	}
	result.FirstRun = true
	result.FirstRunHint = setup.FirstRunHint
	s := lastSetupResult.Summary
	providers := make(map[string]contract.ProviderSetupSummary, len(s.Providers))
	for name, ps := range s.Providers {
		providers[name] = contract.ProviderSetupSummary{
			Available:         ps.Available,
			Version:           ps.Version,
			CatalogModelCount: ps.CatalogModelCount,
			Error:             ps.Error,
		}
	}
	result.FirstRunSetup = &contract.FirstRunSetupInfo{
		InitializedAt:   s.InitializedAt,
		HomeDir:         s.HomeDir,
		ConfigPath:      s.ConfigPath,
		PreferencesPath: s.PreferencesPath,
		ClaudeTransport: s.ClaudeTransport,
		Providers:       providers,
	}
	if !jsonResult {
		fmt.Fprintln(stderr, setup.FirstRunHint)
	}
	lastSetupResult = nil
	return result
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
		return validatePromptFile(string(data))
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
	timeout := fs.Int("timeout", -1, "wall-clock timeout seconds; 0 disables")
	timeoutShort := fs.Int("t", -1, "wall-clock timeout seconds; 0 disables")
	activityTimeout := fs.Int("activity-timeout", -1, "seconds without provider activity before failure; 0 disables")
	taskName := fs.String("task-name", "", "task name")
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
		TimeoutSeconds:  defaultFixedTimeoutSeconds(firstNonNegative(*timeout, *timeoutShort)),
		TaskName:        *taskName,
	}
	parsedProviderOpts, err := parseProviderOpts(providerOpts)
	if err != nil {
		return contract.DispatchRequest{}, *jsonResult, err
	}
	req.ProviderOpts = parsedProviderOpts
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
		case "--caller-env", "--caller-provider", "--caller-module", "--no-meta-header":
			return fmt.Errorf("%s was removed; caller metadata is no longer part of the dispatch request", name)
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

func defaultFixedTimeoutSeconds(flagValue int) int {
	if flagValue >= 0 {
		return flagValue
	}
	return 1800
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
	case "--json-result", "--stream-progress":
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
		"--activity-timeout", "--task-name", "--provider-opt", "--format":
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

func parseProviderOpts(values []string) (map[string]map[string]string, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := map[string]map[string]string{}
	for _, value := range values {
		left, right, ok := strings.Cut(value, "=")
		if !ok {
			return nil, fmt.Errorf("--provider-opt must use provider.key=value")
		}
		provider, key, ok := strings.Cut(left, ".")
		if !ok || provider == "" || key == "" {
			return nil, fmt.Errorf("--provider-opt must use provider.key=value")
		}
		if !validProviderOpt(provider, key) {
			return nil, fmt.Errorf("unsupported provider option %s.%s", provider, key)
		}
		if out[provider] == nil {
			out[provider] = map[string]string{}
		}
		out[provider][key] = right
	}
	return out, nil
}

func validProviderOpt(provider string, key string) bool {
	switch provider {
	case "claude":
		return key == "transport"
	case "opencode":
		return key == "format"
	case "antigravity":
		return key == "bin" || key == "root"
	default:
		return false
	}
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

func firstNonNegative(values ...int) int {
	for _, value := range values {
		if value >= 0 {
			return value
		}
	}
	return -1
}
