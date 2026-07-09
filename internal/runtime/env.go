package runtime

import (
	"os"
	"sort"
	"strings"
)

var allowedEnvKeys = map[string]bool{
	"HOME":                true,
	"PATH":                true,
	"USER":                true,
	"LOGNAME":             true,
	"SHELL":               true,
	"TMPDIR":              true,
	"TMP":                 true,
	"TEMP":                true,
	"TERM":                true,
	"LANG":                true,
	"LC_ALL":              true,
	"LC_CTYPE":            true,
	"SSL_CERT_FILE":       true,
	"SSL_CERT_DIR":        true,
	"SSH_AUTH_SOCK":       true,
	"XDG_CONFIG_HOME":     true,
	"XDG_DATA_HOME":       true,
	"XDG_CACHE_HOME":      true,
	"CODEX_HOME":          true,
	"OPENCODE_CONFIG_DIR": true,
}

var allowedEnvPrefixes = []string{
	"ANTHROPIC_",
	"CLAUDE_",
	"CODEX_",
	"OPENAI_",
	"OPENCODE_",
	"GOOGLE_",
	"GEMINI_",
	"GROK_",
	"XAI_",
}

// SanitizedEnv returns the default environment for provider subprocesses. It
// keeps auth/config variables providers commonly need, but avoids passing the
// caller's entire shell environment to model-controlled tooling.
func SanitizedEnv(overrides map[string]string) []string {
	env := map[string]string{}
	for _, item := range os.Environ() {
		key, value, ok := strings.Cut(item, "=")
		if !ok || key == "" || !envAllowed(key) {
			continue
		}
		env[key] = value
	}
	for key, value := range overrides {
		if key == "" {
			continue
		}
		env[key] = value
	}
	if env["TERM"] == "dumb" {
		env["TERM"] = "xterm-256color"
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key+"="+env[key])
	}
	return out
}

func envAllowed(key string) bool {
	if allowedEnvKeys[key] {
		return true
	}
	for _, prefix := range allowedEnvPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
