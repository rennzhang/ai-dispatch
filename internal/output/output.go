package output

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func WriteFile(path string, result contract.ProviderResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	content := renderFrontmatter(result) + "\n" + result.Text
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0o600)
}

func renderFrontmatter(result contract.ProviderResult) string {
	lines := []string{
		"---",
		"ai_dispatch:",
		"  ts: " + time.Now().Format(time.RFC3339),
		"  schema_version: " + quote(result.SchemaVersion),
		"  provider: " + quote(result.ProviderUsed),
		"  requested_target: " + quote(result.RequestedTarget),
		"  model: " + quote(result.ModelUsed),
		"  status: " + quote(string(result.Status)),
		"  ok: " + strconv.FormatBool(result.OK),
	}
	if result.Degraded {
		lines = append(lines, "  degraded: true")
	}
	if result.DegradeReason != "" {
		lines = append(lines, "  degrade_reason: "+quote(result.DegradeReason))
	}
	if result.SessionID != "" {
		lines = append(lines, "  session_id: "+quote(result.SessionID))
	}
	lines = append(lines,
		"  duration_s: "+strconv.FormatFloat(float64(result.DurationMS)/1000.0, 'f', 2, 64),
		"  exit_code: "+strconv.Itoa(result.ExitCode),
		"  route_trace:",
	)
	if len(result.RouteTrace) == 0 {
		lines = append(lines, "    []")
	} else {
		for _, step := range result.RouteTrace {
			lines = append(lines, "    - "+quote(step))
		}
	}
	lines = append(lines, "---")
	return strings.Join(lines, "\n")
}

func quote(value string) string {
	if value == "" {
		return `""`
	}
	return strconv.Quote(value)
}
