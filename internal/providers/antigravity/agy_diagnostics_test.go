package antigravity

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
	"github.com/rennzhang/ai-dispatch/internal/runtime"
)

func TestActionableAgyLogErrorReturnsOnlyCanonicalMessages(t *testing.T) {
	tests := []struct {
		name string
		log  string
		want string
	}{
		{
			name: "region",
			log:  "INFO auth session user@example.com\nERROR User location is not supported token=secret /Users/private/profile",
			want: "Antigravity is not available in the current region",
		},
		{
			name: "login",
			log:  "ERROR not logged in for user@example.com token=secret",
			want: "Antigravity is not logged in; complete agy login and Chrome authorization before retrying",
		},
		{
			name: "account",
			log:  "ERROR account is not eligible: user@example.com /Users/private/profile",
			want: "Antigravity account is not eligible for this model or service",
		},
		{
			name: "benign auth noise",
			log:  "INFO auth session established for user@example.com via browser /Users/private/profile",
			want: "",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := actionableAgyLogError(test.log)
			if got != test.want {
				t.Fatalf("got=%q want=%q", got, test.want)
			}
			for _, sensitive := range []string{"user@example.com", "secret", "/Users/private"} {
				if strings.Contains(got, sensitive) {
					t.Fatalf("canonical diagnostic leaked %q: %q", sensitive, got)
				}
			}
		})
	}
}

func TestAgyRegionFailureAppendsSanitizedLogDiagnosticAndClassifiesConfig(t *testing.T) {
	root := t.TempDir()
	fakeAgy := filepath.Join(root, "agy")
	script := `#!/bin/sh
set -eu
log_file=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    --log-file) log_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
cat > "$log_file" <<'LOG'
INFO auth session established for secret@example.com via /Users/private/profile
ERROR User location is not supported token=private-token secret@example.com
LOG
printf '%s\n' 'Agent execution terminated due to error' >&2
exit 1
`
	if err := os.WriteFile(fakeAgy, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := RunAgyDriverCLI([]string{
		"--agy-bin", fakeAgy,
		"--agy-root", root,
		"--model", "pro",
		"--prompt", "hello",
	}, &stdout, &stderr)
	if code == 0 {
		t.Fatalf("expected fake region failure: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}
	for _, want := range []string{
		"Agent execution terminated due to error",
		"Antigravity is not available in the current region",
	} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr=%q missing %q", stderr.String(), want)
		}
	}
	for _, sensitive := range []string{"secret@example.com", "private-token", "/Users/private"} {
		if strings.Contains(stderr.String(), sensitive) {
			t.Fatalf("stderr leaked %q: %q", sensitive, stderr.String())
		}
	}

	result := (Provider{}).Parse(runtime.RunResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: code,
		Error:    "exit status 1",
	}, providers.BuildRequest{Target: routing.DispatchTarget{
		Requested: "gemini-pro",
		Provider:  "antigravity",
		Model:     "pro",
	}})
	if result.OK || result.FailureClass == nil || *result.FailureClass != contract.FailureConfig {
		t.Fatalf("result=%+v", result)
	}
	if !strings.Contains(result.Stderr, "not available in the current region") {
		t.Fatalf("result stderr lost actionable region diagnostic: %q", result.Stderr)
	}
}
