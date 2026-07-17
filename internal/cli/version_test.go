package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/buildinfo"
)

func TestVersionCommandJSON(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Main([]string{"version", "--format", "json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	var got buildinfo.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v output=%s", err, stdout.String())
	}
	if got.Version == "" || got.GoVersion == "" || got.Module == "" {
		t.Fatalf("identity=%+v", got)
	}
}

func TestVersionIsListedInTopLevelHelp(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := Main([]string{"--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
	if !bytes.Contains(stdout.Bytes(), []byte("ai-dispatch version")) {
		t.Fatalf("help=%s", stdout.String())
	}
}

func TestVersionCommandRejectsExtraArguments(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	if code := versionCommand([]string{"extra"}, &stdout, &stderr); code != 2 {
		t.Fatalf("code=%d stderr=%s", code, stderr.String())
	}
}
