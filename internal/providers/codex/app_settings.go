package codex

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
)

//go:embed app_settings.js
var desktopSettingsScript string

// Configure the GUI's own thread. A separate backend cannot update its owner.
func configureDesktopThread(ctx context.Context, host desktopHost, req providers.BuildRequest, title string) error {
	settings, err := json.Marshal(struct {
		PID    int             `json:"pid"`
		Bundle string          `json:"bundle"`
		Title  string          `json:"title"`
		Model  string          `json:"model"`
		Effort contract.Effort `json:"effort"`
		Fast   bool            `json:"fast"`
	}{host.pid, host.bundle, title, req.Target.Model, req.Effort, req.Fast})
	if err != nil {
		return err
	}
	out, err := exec.CommandContext(ctx, "osascript", "-l", "JavaScript", "-e", desktopSettingsScript, string(settings)).CombinedOutput()
	if err != nil {
		return fmt.Errorf("configure Codex Desktop through Accessibility; no input was queued: %s", firstNonEmpty(strings.TrimSpace(string(out)), err.Error()))
	}
	var receipt struct {
		PID    int    `json:"pid"`
		Title  string `json:"title"`
		Preset string `json:"preset"`
	}
	if err := json.Unmarshal(out, &receipt); err != nil || receipt.PID <= 0 || (host.pid > 0 && receipt.PID != host.pid) || receipt.Title != title || receipt.Preset == "" {
		return fmt.Errorf("Codex Desktop settings were not verified; no input was queued")
	}
	return nil
}

func validateDesktopTurnSettings(data []byte, turnID string, req providers.BuildRequest) error {
	explicitEffort := req.Effort != "" && req.Effort != contract.EffortAuto
	if req.Target.Model == "" && !explicitEffort && req.CWD == "" && !req.Fast {
		return nil
	}
	found := false
	for len(data) > 0 {
		var line []byte
		line, data, _ = bytes.Cut(data, []byte("\n"))
		var event struct {
			Type    string `json:"type"`
			Payload struct {
				TurnID      string `json:"turn_id"`
				Model       string `json:"model"`
				Effort      string `json:"effort"`
				CWD         string `json:"cwd"`
				ServiceTier string `json:"service_tier"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &event) != nil || event.Type != "turn_context" || event.Payload.TurnID != turnID {
			continue
		}
		found = true
		actual := event.Payload
		if req.Target.Model != "" && actual.Model != req.Target.Model {
			return fmt.Errorf("Desktop model mismatch: requested %q, actual %q", req.Target.Model, actual.Model)
		}
		if explicitEffort && actual.Effort != string(req.Effort) {
			return fmt.Errorf("Desktop effort mismatch: requested %q, actual %q", req.Effort, actual.Effort)
		}
		if req.CWD != "" && (actual.CWD == "" || filepath.Clean(actual.CWD) != filepath.Clean(req.CWD)) {
			return fmt.Errorf("Desktop workspace mismatch: requested %q, actual %q", req.CWD, actual.CWD)
		}
		if req.Fast && actual.ServiceTier != "priority" || !req.Fast && actual.ServiceTier == "priority" {
			return fmt.Errorf("Desktop speed mismatch: requested fast=%t, actual service tier %q", req.Fast, actual.ServiceTier)
		}
	}
	if !found {
		return fmt.Errorf("Desktop execution has no matching turn_context; requested settings were not verified")
	}
	return nil
}
