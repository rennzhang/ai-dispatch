package runtime

import (
	"strings"
	"testing"
)

func TestSanitizedEnvKeepsCursorPrefix(t *testing.T) {
	t.Setenv("CURSOR_API_KEY", "sk-test-cursor")
	t.Setenv("CURSOR_SOMETHING_ELSE", "value")
	env := SanitizedEnv(nil)
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "CURSOR_API_KEY=sk-test-cursor") {
		t.Fatalf("CURSOR_API_KEY missing from sanitized env: %q", joined)
	}
	if !strings.Contains(joined, "CURSOR_SOMETHING_ELSE=value") {
		t.Fatalf("CURSOR_SOMETHING_ELSE missing from sanitized env: %q", joined)
	}
}
