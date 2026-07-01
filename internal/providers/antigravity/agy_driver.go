package antigravity

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
)

const defaultFlashLabel = "Gemini 3.5 Flash (Medium)"
const defaultProLabel = "Gemini 3.1 Pro (High)"

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

func RunAgyDriverCLI(argv []string, stdout io.Writer, stderr io.Writer) int {
	cfg, err := parseAgyDriverArgs(argv, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := runAgyDriver(cfg, stdout, stderr); err != nil {
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
	agyBin, err := resolveAgyBinary(cfg.AgyBin)
	if err != nil {
		return err
	}
	root, err := agyRoot(cfg.AgyRoot)
	if err != nil {
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

	var childStdout []byte
	var childStderr []byte
	withErr := withTemporaryModel(root, modelLabel, func() error {
		cmd := exec.Command(agyBin, args...)
		var outBuf bytes.Buffer
		var errBuf bytes.Buffer
		cmd.Stdout = &outBuf
		cmd.Stderr = &errBuf
		err := cmd.Run()
		childStdout = outBuf.Bytes()
		childStderr = errBuf.Bytes()
		return err
	})

	logText := readText(logPath)
	sessionID := firstNonEmpty(cfg.SessionID, matchFirst(sessionRE, logText))
	modelObserved := firstNonEmpty(matchFirst(modelRE, logText), modelLabel)
	text := strings.TrimSpace(string(childStdout))
	if sessionID != "" {
		if transcriptText := extractFinalTranscriptText(transcriptPath(root, sessionID), transcriptOffset); transcriptText != "" {
			text = transcriptText
		}
	}
	if sessionID != "" {
		emitJSON(stdout, map[string]any{"event": "session_start", "session_id": sessionID, "model": modelObserved})
	}
	if text != "" {
		emitJSON(stdout, map[string]any{"event": "assistant_text", "text": text, "session_id": sessionID, "model": modelObserved})
	}
	emitJSON(stdout, map[string]any{"event": "done", "text": text, "session_id": sessionID, "model": modelObserved})
	if len(childStderr) > 0 {
		fmt.Fprint(stderr, strings.TrimSpace(string(childStderr)))
	}
	if withErr != nil {
		if strings.TrimSpace(string(childStderr)) == "" {
			fmt.Fprint(stderr, summarizeLog(logText))
		}
		return fmt.Errorf("agy failed: %w", withErr)
	}
	if text == "" {
		if summary := summarizeLog(logText); summary != "" {
			return fmt.Errorf("agy completed without output; %s", summary)
		}
		return fmt.Errorf("agy completed without output; verify agy login and Chrome authorization before retrying")
	}
	return nil
}

func resolveAgyBinary(explicit string) (string, error) {
	candidates := []string{
		explicit,
		os.Getenv("AI_DISPATCH_AGY_BIN"),
		"agy",
		filepath.Join(os.Getenv("HOME"), ".local", "bin", "agy"),
	}
	for _, candidate := range candidates {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if path, err := exec.LookPath(candidate); err == nil {
			return path, nil
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("agy binary not found; install agy or set AI_DISPATCH_AGY_BIN")
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
	normalized := strings.ToLower(strings.TrimSpace(model))
	switch normalized {
	case "", "flash", "gemini-3-flash", "gemini-3.1-flash", "gemini-3.5-flash", "google/gemini-3-flash-preview", "google/gemini-3.1-flash-preview", "google/gemini-3.5-flash", "google/antigravity-gemini-3-flash", "model_placeholder_m18", "model_placeholder_m132":
		return defaultFlashLabel, nil
	case "pro", "pro-high", "gemini-3-pro-high", "gemini-3.1-pro-high", "google/gemini-3-pro-preview", "google/gemini-3.1-pro-preview", "google/antigravity-gemini-3-pro-high", "model_placeholder_m37":
		return defaultProLabel, nil
	case "pro-low", "gemini-3-pro-low", "gemini-3.1-pro-low", "google/antigravity-gemini-3-pro-low":
		return "Gemini 3.1 Pro (Low)", nil
	default:
		return "", fmt.Errorf("agy backend has no verified settings mapping for model %q", model)
	}
}

func withTemporaryModel(root string, modelLabel string, fn func() error) error {
	settingsPath := filepath.Join(root, "settings.json")
	lockPath := filepath.Join(root, "settings.json.lock")
	lock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := flock(lock); err != nil {
		return err
	}
	defer funlock(lock)

	original, err := os.ReadFile(settingsPath)
	if err != nil {
		return fmt.Errorf("cannot read agy settings.json: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(original, &payload); err != nil {
		return fmt.Errorf("agy settings.json is not valid JSON: %w", err)
	}
	changed := strings.TrimSpace(fmt.Sprint(payload["model"])) != modelLabel
	if changed {
		payload["model"] = modelLabel
		data, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(settingsPath, data, 0o600); err != nil {
			return err
		}
		defer os.WriteFile(settingsPath, original, 0o600)
	}
	return fn()
}

func extractFinalTranscriptText(path string, startOffset int64) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	if startOffset > 0 && startOffset < int64(len(data)) {
		data = data[startOffset:]
	}
	text := ""
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item["source"] != "MODEL" || item["type"] != "PLANNER_RESPONSE" {
			continue
		}
		if calls, ok := item["tool_calls"].([]any); ok && len(calls) > 0 {
			continue
		}
		if content, ok := item["content"].(string); ok && strings.TrimSpace(content) != "" {
			text = strings.TrimSpace(content)
		}
	}
	return text
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

func readText(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func matchFirst(re *regexp.Regexp, text string) string {
	match := re.FindStringSubmatch(text)
	if len(match) > 1 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

func summarizeLog(text string) string {
	lines := []string{}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "error") || strings.Contains(lower, "auth") || strings.Contains(lower, "login") || strings.Contains(lower, "oauth") || strings.Contains(lower, "browser") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > 5 {
		lines = lines[len(lines)-5:]
	}
	return strings.Join(lines, "\n")
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

func flock(file *os.File) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX)
}

func funlock(file *os.File) {
	if runtime.GOOS == "windows" {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
}
