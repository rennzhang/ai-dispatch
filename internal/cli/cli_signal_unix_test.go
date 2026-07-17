//go:build darwin || linux

package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
)

func TestCLISignalCancellation(t *testing.T) {
	for name, signal := range map[string]syscall.Signal{
		"sigint":  syscall.SIGINT,
		"sigterm": syscall.SIGTERM,
		"sighup":  syscall.SIGHUP,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			readyFile := filepath.Join(root, "provider.ready")
			providerPIDFile := filepath.Join(root, "provider.pid")
			childPIDFile := filepath.Join(root, "provider-child.pid")
			grokBin := filepath.Join(root, "grok")
			script := "#!/bin/sh\nset -eu\nprintf '%s' \"$$\" > " + shellQuote(providerPIDFile) + "\nsleep 300 &\nchild=$!\nprintf '%s' \"$child\" > " + shellQuote(childPIDFile) + "\ntrap 'kill \"$child\" 2>/dev/null || true; wait \"$child\" 2>/dev/null || true; exit 0' TERM INT HUP\nprintf ready > " + shellQuote(readyFile) + "\nwait \"$child\"\n"
			if err := os.WriteFile(grokBin, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			configPath := filepath.Join(root, "config.json")
			writeCLIConfig(t, configPath, `{"version":1,"claude_transport":"print","models":{},"providers":{}}`)
			if err := os.WriteFile(filepath.Join(root, "preferences.md"), []byte("# test preferences\n"), 0o600); err != nil {
				t.Fatal(err)
			}

			cmd := exec.Command(os.Args[0], "-test.run=^TestCLISignalHelper$")
			cmd.Env = append(os.Environ(),
				"AI_DISPATCH_CLI_SIGNAL_HELPER=1",
				"AI_DISPATCH_GO_PROVIDER_EXECUTION=on",
				"AI_DISPATCH_HOME="+root,
				"AI_DISPATCH_CONFIG="+configPath,
				"AI_DISPATCH_RUNS_DIR="+filepath.Join(root, "runs"),
				"AI_DISPATCH_GROK_BIN="+grokBin,
			)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_, _ = cmd.Process.Wait()
				}
			})
			if !waitForSignalHelper(readyFile) {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatalf("provider helper did not become ready: stdout=%s stderr=%s", stdout.String(), stderr.String())
			}
			if err := cmd.Process.Signal(signal); err != nil {
				t.Fatal(err)
			}
			err := cmd.Wait()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
				t.Fatalf("signal=%v err=%v stdout=%s stderr=%s", signal, err, stdout.String(), stderr.String())
			}

			var result contract.ProviderResult
			if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
				t.Fatalf("decode result: %v stdout=%s stderr=%s", err, stdout.String(), stderr.String())
			}
			if result.OK || result.Status != contract.StatusError || result.ExitCode != 130 || result.NextAction != contract.NextDone {
				t.Fatalf("result=%+v", result)
			}
			if result.Stderr != "dispatch canceled" || result.FailureClass != nil {
				t.Fatalf("cancellation contract drifted: %+v", result)
			}
			if result.ProviderUsed != "grok" || len(result.RouteSteps) != 1 || result.Degraded {
				t.Fatalf("signal cancellation must not fall back: %+v", result)
			}
			for _, pidFile := range []string{providerPIDFile, childPIDFile} {
				pid := readSignalPID(t, pidFile)
				if err := syscall.Kill(pid, 0); err == nil {
					t.Fatalf("signal=%v left provider process %d alive", signal, pid)
				}
			}
		})
	}
}

func readSignalPID(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("invalid pid %q: %v", data, err)
	}
	return pid
}

func TestCLISignalHelper(t *testing.T) {
	if os.Getenv("AI_DISPATCH_CLI_SIGNAL_HELPER") != "1" {
		return
	}
	if err := os.Setenv("AI_DISPATCH_GO_PROVIDER_EXECUTION", "on"); err != nil {
		os.Exit(2)
	}
	code := MainWithInput([]string{"send", "grok", "hello", "--json-result", "--timeout", "0"}, os.Stdout, os.Stderr, nil)
	os.Exit(code)
}

func waitForSignalHelper(path string) bool {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
