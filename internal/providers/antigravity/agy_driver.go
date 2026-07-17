package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
)

const defaultFlashLabel = "Gemini 3.5 Flash (Medium)"
const defaultProLabel = "Gemini 3.1 Pro (High)"
const legacyModelRecoveryJournalName = "settings.json.ai-dispatch-recovery"

var sessionRE = regexp.MustCompile(`Created conversation ([0-9a-f-]{36})`)
var modelRE = regexp.MustCompile(`(?i)selected model override to backend: label="([^"]+)"`)

type agyDriverConfig struct {
	Prompt       string
	PromptFile   string
	SessionID    string
	Model        string
	Project      string
	PrintTimeout string
	AgyBin       string
	AgyRoot      string
}

type legacyModelRecoveryJournal struct {
	Version  int    `json:"version"`
	Original []byte `json:"original"`
	Mode     uint32 `json:"mode"`
}

type agyProcessCleanup struct {
	once sync.Once
	run  func() error
	err  error
}

func (c *agyProcessCleanup) cleanup() error {
	c.once.Do(func() {
		c.err = c.run()
	})
	return c.err
}

func RunAgyDriverCLI(argv []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := parseAgyDriverArgs(argv, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runAgyDriverContext(ctx, cfg, stdout, stderr); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func parseAgyDriverArgs(argv []string, stderr io.Writer) (agyDriverConfig, error) {
	fs := flag.NewFlagSet("__agy-driver", flag.ContinueOnError)
	fs.SetOutput(stderr)
	prompt := fs.String("prompt", "", "prompt")
	promptFile := fs.String("prompt-file", "", "prompt file")
	sessionID := fs.String("session-id", "", "conversation id")
	model := fs.String("model", "", "model")
	project := fs.String("project", "", "project working directory")
	printTimeout := fs.String("print-timeout", "", "agy print timeout")
	agyBin := fs.String("agy-bin", "", "agy binary")
	agyRoot := fs.String("agy-root", "", "agy app data root")
	if err := fs.Parse(argv); err != nil {
		return agyDriverConfig{}, err
	}
	cfg := agyDriverConfig{
		Prompt:       *prompt,
		PromptFile:   *promptFile,
		SessionID:    *sessionID,
		Model:        *model,
		Project:      *project,
		PrintTimeout: *printTimeout,
		AgyBin:       *agyBin,
		AgyRoot:      *agyRoot,
	}
	if cfg.PromptFile != "" {
		data, err := os.ReadFile(cfg.PromptFile)
		if err != nil {
			return agyDriverConfig{}, fmt.Errorf("cannot read --prompt-file: %w", err)
		}
		cfg.Prompt = string(data)
	}
	if strings.TrimSpace(cfg.Prompt) == "" {
		return agyDriverConfig{}, errors.New("--prompt or --prompt-file is required")
	}
	return cfg, nil
}

func runAgyDriver(cfg agyDriverConfig, stdout io.Writer, stderr io.Writer) error {
	return runAgyDriverContext(context.Background(), cfg, stdout, stderr)
}

func runAgyDriverContext(ctx context.Context, cfg agyDriverConfig, stdout io.Writer, stderr io.Writer) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("agy canceled before start: %w", err)
	}
	agyBin, err := resolveAgyBinary(cfg.AgyBin)
	if err != nil {
		return err
	}
	root, err := agyRoot(cfg.AgyRoot)
	if err != nil {
		return err
	}
	if err := recoverLegacyModelSwitchContext(ctx, root); err != nil {
		return err
	}
	if cfg.SessionID != "" {
		if _, err := os.Stat(conversationPath(root, cfg.SessionID)); err != nil {
			return fmt.Errorf("agy conversation not found: %s", cfg.SessionID)
		}
	}
	modelLabel, err := resolveModelLabel(cfg.Model)
	if err != nil {
		return err
	}
	logFile, err := os.CreateTemp("", "ai-dispatch-agy-*.log")
	if err != nil {
		return err
	}
	logPath := logFile.Name()
	_ = logFile.Close()
	defer os.Remove(logPath)

	transcriptOffset := int64(0)
	if cfg.SessionID != "" {
		transcriptOffset = fileSize(transcriptPath(root, cfg.SessionID))
	}

	args := []string{"--dangerously-skip-permissions", "--log-file", logPath}
	if modelLabel != "" {
		args = append(args, "--model", modelLabel)
	}
	if cfg.PrintTimeout != "" {
		args = append(args, "--print-timeout", cfg.PrintTimeout)
	}
	if cfg.SessionID != "" {
		args = append(args, "--conversation", cfg.SessionID)
	}
	if cfg.Project != "" {
		args = append(args, "--project", cfg.Project)
	}
	args = append(args, "--print", cfg.Prompt)

	childStdout, childStderr, warnings, withErr := executeAgyBounded(ctx, agyBin, args)
	logText, logWarnings := readAgyLog(logPath)
	warnings = append(warnings, logWarnings...)
	sessionID := firstNonEmpty(cfg.SessionID, matchFirst(sessionRE, logText))
	modelObserved := firstNonEmpty(matchFirst(modelRE, logText), modelLabel)
	text := strings.TrimSpace(string(childStdout))
	if cfg.SessionID != "" {
		text, logWarnings = extractFinalTranscriptText(transcriptPath(root, sessionID), transcriptOffset)
		warnings = append(warnings, logWarnings...)
	} else if sessionID != "" {
		transcriptText, transcriptWarnings := extractFinalTranscriptText(transcriptPath(root, sessionID), transcriptOffset)
		warnings = append(warnings, transcriptWarnings...)
		if transcriptText != "" {
			text = transcriptText
		}
	}
	for _, warning := range warnings {
		emitJSON(stdout, map[string]any{"event": "warning", "message": warning})
	}
	if sessionID != "" {
		emitJSON(stdout, map[string]any{"event": "session_start", "session_id": sessionID, "model": modelObserved})
	}
	if text != "" {
		emitJSON(stdout, map[string]any{"event": "assistant_text", "text": text, "session_id": sessionID, "model": modelObserved})
	}
	emitJSON(stdout, map[string]any{"event": "done", "text": text, "session_id": sessionID, "model": modelObserved})
	childDiagnostic := strings.TrimSpace(string(childStderr))
	if withErr != nil {
		if logDiagnostic := actionableAgyLogError(logText); logDiagnostic != "" {
			childDiagnostic = joinDiagnosticLines(childDiagnostic, logDiagnostic)
		}
		if childDiagnostic != "" {
			fmt.Fprintln(stderr, childDiagnostic)
		}
		return fmt.Errorf("agy failed: %w", withErr)
	}
	if childDiagnostic != "" {
		fmt.Fprintln(stderr, childDiagnostic)
	}
	if text == "" {
		if summary := actionableAgyLogError(logText); summary != "" {
			return fmt.Errorf("agy completed without output; %s", summary)
		}
		return fmt.Errorf("agy completed without output; verify agy login and Chrome authorization before retrying")
	}
	return nil
}

func executeAgyBounded(ctx context.Context, agyBin string, args []string) ([]byte, []byte, []string, error) {
	stdoutCapture := newBoundedHeadTailBuffer(agyStdoutHeadLimitBytes, agyStdoutTailLimitBytes)
	stderrCapture := newBoundedHeadTailBuffer(agyStderrHeadLimitBytes, agyStderrTailLimitBytes)
	cmd := exec.CommandContext(ctx, agyBin, args...)
	configureAgyProcess(cmd)
	cleanup := &agyProcessCleanup{run: func() error { return terminateAgyProcessTree(cmd) }}
	cmd.Cancel = cleanup.cleanup
	cmd.Stdout = stdoutCapture
	cmd.Stderr = stderrCapture
	runErr := cmd.Run()
	cleanupErr := cleanup.cleanup()
	if cleanupErr != nil {
		runErr = errors.Join(runErr, cleanupErr)
	}
	warnings := []string{}
	if stdoutCapture.Truncated() {
		warnings = append(warnings, stdoutCapture.truncationWarning("agy child stdout"))
	}
	if stderrCapture.Truncated() {
		warnings = append(warnings, stderrCapture.truncationWarning("agy child stderr"))
	}
	return stdoutCapture.Bytes(), stderrCapture.Bytes(), warnings, runErr
}

func readAgyLog(path string) (string, []string) {
	data, truncated, err := readBoundedHeadTailFile(path, agyLogHeadLimitBytes, agyLogTailLimitBytes)
	if err != nil {
		return "", []string{"agy log read failed: " + compactFileError(err)}
	}
	warnings := []string{}
	if truncated {
		warnings = append(warnings, fmt.Sprintf("agy log exceeded the %d-byte read limit; retained bounded head and final tail", agyLogHeadLimitBytes+agyLogTailLimitBytes))
	}
	return string(data), warnings
}

func resolveAgyBinary(explicit string) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return executableAgyBinary(explicit, "agy binary override")
	}
	if env := strings.TrimSpace(os.Getenv("AI_DISPATCH_AGY_BIN")); env != "" {
		return executableAgyBinary(env, "AI_DISPATCH_AGY_BIN override")
	}
	if path, err := exec.LookPath("agy"); err == nil {
		return path, nil
	}
	if home := os.Getenv("HOME"); home != "" {
		if path, err := executableAgyBinary(filepath.Join(home, ".local", "bin", "agy"), "agy fallback"); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("agy binary not found; install agy or set AI_DISPATCH_AGY_BIN")
}

func executableAgyBinary(candidate string, label string) (string, error) {
	if path, err := exec.LookPath(candidate); err == nil {
		return path, nil
	}
	if !strings.Contains(candidate, string(os.PathSeparator)) {
		return "", fmt.Errorf("%s is not executable or not found", label)
	}
	info, err := os.Stat(candidate)
	if err != nil || info.IsDir() || info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%s is not executable or not found", label)
	}
	return candidate, nil
}

func agyRoot(explicit string) (string, error) {
	root := explicit
	if root == "" {
		root = os.Getenv("AI_DISPATCH_AGY_APPDATA_DIR")
	}
	if root == "" {
		root = filepath.Join(os.Getenv("HOME"), ".gemini", "antigravity-cli")
	}
	root = expandHome(root)
	if info, err := os.Stat(root); err != nil || !info.IsDir() {
		return "", fmt.Errorf("agy root not found: %s", root)
	}
	return root, nil
}

func resolveModelLabel(model string) (string, error) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		// Empty means no --model override; agy uses its own default model.
		return "", nil
	}
	// Exact agy display labels (including effort tags) pass through unchanged.
	if agyEffortLabelRE.MatchString(trimmed) || strings.Contains(trimmed, " (") {
		return trimmed, nil
	}
	normalized := strings.ToLower(trimmed)
	switch normalized {
	case "flash", "gemini-3-flash", "gemini-3.1-flash", "gemini-3.5-flash", "google/gemini-3-flash-preview", "google/gemini-3.1-flash-preview", "google/gemini-3.5-flash", "google/antigravity-gemini-3-flash", "model_placeholder_m18", "model_placeholder_m132":
		return defaultFlashLabel, nil
	case "pro", "pro-high", "gemini-3-pro-high", "gemini-3.1-pro-high", "google/gemini-3-pro-preview", "google/gemini-3.1-pro-preview", "google/antigravity-gemini-3-pro-high", "model_placeholder_m37":
		return defaultProLabel, nil
	case "pro-low", "gemini-3-pro-low", "gemini-3.1-pro-low", "google/antigravity-gemini-3-pro-low":
		return "Gemini 3.1 Pro (Low)", nil
	default:
		return "", fmt.Errorf("agy has no verified model label mapping for %q", model)
	}
}

func recoverLegacyModelSwitchContext(ctx context.Context, root string) error {
	journalPath := legacyModelRecoveryJournalPath(root)
	if _, err := os.Stat(journalPath); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("cannot inspect legacy agy settings recovery journal: %w", err)
	}

	lockPath := filepath.Join(root, "settings.json.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("cannot open legacy agy settings recovery lock: %w", err)
	}
	defer lock.Close()
	if err := flockContext(ctx, lock); err != nil {
		return fmt.Errorf("cannot acquire legacy agy settings recovery lock: %w", err)
	}
	defer funlock(lock)
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("legacy agy settings recovery canceled: %w", err)
	}
	return recoverLegacyModelSwitchLocked(root)
}

func legacyModelRecoveryJournalPath(root string) string {
	return filepath.Join(root, legacyModelRecoveryJournalName)
}

func recoverLegacyModelSwitchLocked(root string) error {
	journalPath := legacyModelRecoveryJournalPath(root)
	data, err := os.ReadFile(journalPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot read legacy agy settings recovery journal: %w", err)
	}
	var journal legacyModelRecoveryJournal
	if err := json.Unmarshal(data, &journal); err != nil || journal.Version != 1 || len(journal.Original) == 0 {
		if err == nil {
			err = errors.New("unsupported or empty journal")
		}
		return fmt.Errorf("legacy agy settings recovery journal is invalid: %w", err)
	}
	settingsPath := filepath.Join(root, "settings.json")
	current, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("cannot safely recover legacy agy settings.json; journal preserved: %w", err)
	}
	// Version 1 recorded only the original bytes, not the temporary bytes that
	// ai-dispatch applied. A changed file therefore cannot be attributed safely.
	if !bytes.Equal(current, journal.Original) {
		return errors.New("legacy agy settings recovery journal cannot verify the current settings.json was written by ai-dispatch; settings and journal were preserved unchanged")
	}
	if err := os.Remove(journalPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("cannot remove legacy agy settings recovery journal: %w", err)
	}
	if err := syncDirectory(root); err != nil {
		return fmt.Errorf("cannot durably remove legacy agy settings recovery journal: %w", err)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func compactFileError(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Err.Error()
	}
	return err.Error()
}

func conversationPath(root string, sessionID string) string {
	return filepath.Join(root, "conversations", sessionID+".pb")
}

func transcriptPath(root string, sessionID string) string {
	return filepath.Join(root, "brain", sessionID, ".system_generated", "logs", "transcript.jsonl")
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func matchFirst(re *regexp.Regexp, text string) string {
	match := re.FindStringSubmatch(text)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func actionableAgyLogError(text string) string {
	lower := strings.ToLower(text)
	switch {
	case containsAgyLogPhrase(lower,
		"user location is not supported",
		"location is not supported",
		"unsupported region",
		"not available in your region",
		"country, region, or territory is not supported",
	):
		return "Antigravity is not available in the current region"
	case containsAgyLogPhrase(lower,
		"not logged in",
		"login required",
		"please log in",
		"requires login",
	):
		return "Antigravity is not logged in; complete agy login and Chrome authorization before retrying"
	case containsAgyLogPhrase(lower,
		"account is ineligible",
		"account ineligible",
		"account is not eligible",
		"account not eligible",
		"not eligible for antigravity",
		"not available for your account",
	):
		return "Antigravity account is not eligible for this model or service"
	default:
		return ""
	}
}

func containsAgyLogPhrase(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func joinDiagnosticLines(left string, right string) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "":
		return right
	case right == "", strings.Contains(left, right):
		return left
	default:
		return left + "\n" + right
	}
}

func emitJSON(w io.Writer, payload map[string]any) {
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintln(w, string(data))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func expandHome(path string) string {
	if path == "~" {
		return os.Getenv("HOME")
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), strings.TrimPrefix(path, "~/"))
	}
	return path
}
