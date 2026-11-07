package codex

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/providers"
)

func desktopBackendFixture(t *testing.T) (desktopHost, string, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin, state := t.TempDir(), t.TempDir()
	t.Setenv("PATH", bin)
	t.Setenv("TEST_DESKTOP_HELPER", executable)
	t.Setenv("TEST_DESKTOP_STATE", state)
	t.Setenv("AI_DISPATCH_HOME", state)
	t.Setenv("TEST_DESKTOP_BACKEND", "1")
	backend := filepath.Join(bin, "codex")
	if err := os.WriteFile(backend, []byte("#!/bin/sh\nexec \"$TEST_DESKTOP_HELPER\" -test.run '^TestDesktopBackendProcess$'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The GUI is outside this ownership test; only its successful preparation is stubbed.
	ui := "#!/bin/sh\nexec \"$TEST_DESKTOP_HELPER\" -test.run '^TestDesktopUIReceiptProcess$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "osascript"), []byte(ui), 0o755); err != nil {
		t.Fatal(err)
	}
	return desktopHost{binary: backend, bundle: "/test/ChatGPT.app", pid: 42}, filepath.Join(state, "closed"), filepath.Join(state, "queued")
}

func TestDesktopBackendProcess(t *testing.T) {
	if os.Getenv("TEST_DESKTOP_BACKEND") != "1" {
		return
	}
	state := os.Getenv("TEST_DESKTOP_STATE")
	decoder, encoder := json.NewDecoder(os.Stdin), json.NewEncoder(os.Stdout)
	loaded := false
	for {
		var request rpcMessage
		if err := decoder.Decode(&request); err != nil {
			if !errors.Is(err, io.EOF) {
				os.Exit(90)
			}
			break
		}
		if len(request.ID) == 0 {
			continue
		}
		var result any = map[string]any{}
		var failure string
		switch request.Method {
		case "initialize":
		case "model/list":
			if os.Getenv("TEST_DESKTOP_MODEL_DESCENDANT") == "1" {
				child := exec.Command(os.Args[0], "-test.run", "^TestDesktopCatalogDescendant$")
				child.Stdout, child.Stderr = os.Stdout, os.Stderr
				if err := child.Start(); err != nil {
					os.Exit(95)
				}
				for {
					time.Sleep(time.Second)
				}
			}
			var params struct {
				Cursor string `json:"cursor"`
			}
			_ = json.Unmarshal(request.Params, &params)
			_ = os.WriteFile(filepath.Join(state, "catalog-read"), nil, 0o600)
			model := map[string]any{"model": "gpt-6.1-sol", "displayName": "Sol", "isDefault": true,
				"supportedReasoningEfforts": []map[string]string{{"reasoningEffort": "high"}}, "inputModalities": []string{"text", "image"}}
			var next any
			if os.Getenv("TEST_DESKTOP_MODEL_PAGING") != "" && params.Cursor == "" {
				next = "next"
			}
			if os.Getenv("TEST_DESKTOP_MODEL_PAGING") == "repeat" {
				next = "next"
			}
			if params.Cursor != "" {
				model["model"] = "gpt-6-astra"
			}
			result = map[string]any{"data": []any{model}, "nextCursor": next}
		case "thread/start", "thread/resume":
			loaded = true
			result = map[string]any{"thread": map[string]any{"id": "thread-42"}}
		case "thread/name/set":
			var params struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(request.Params, &params)
			_ = os.WriteFile(filepath.Join(state, "name"), []byte(params.Name), 0o600)
		case "thread/read":
			name, err := os.ReadFile(filepath.Join(state, "name"))
			if err != nil {
				name = []byte("Desktop Test")
			}
			result = map[string]any{"thread": map[string]any{"name": string(name)}}
		case "thread/loaded/list":
			ids := []string{}
			if loaded {
				ids = append(ids, "thread-42")
			}
			result = map[string]any{"data": ids}
		case "thread/queue/add":
			_ = os.WriteFile(filepath.Join(state, "queued"), nil, 0o600)
			if loaded {
				_ = os.WriteFile(filepath.Join(state, "unexpected-turn"), nil, 0o600)
			}
		case "turn/start":
			_ = os.WriteFile(filepath.Join(state, "unexpected-turn"), nil, 0o600)
			failure = "the metadata or queue backend must not run a turn"
		default:
			failure = "unexpected backend request"
		}
		response := map[string]any{"id": request.ID, "result": result}
		if failure != "" {
			delete(response, "result")
			response["error"] = map[string]any{"code": -32602, "message": failure}
		}
		if err := encoder.Encode(response); err != nil {
			os.Exit(91)
		}
	}
	if err := os.WriteFile(filepath.Join(state, "closed"), nil, 0o600); err != nil {
		os.Exit(92)
	}
	os.Exit(0)
}

func TestDesktopUIReceiptProcess(t *testing.T) {
	if os.Getenv("TEST_DESKTOP_BACKEND") != "1" {
		return
	}
	if os.Getenv("TEST_DESKTOP_UI_REJECT") == "1" {
		os.Exit(93)
	}
	var settings struct {
		Title  string `json:"title"`
		PID    int    `json:"pid"`
		Bundle string `json:"bundle"`
		URL    string `json:"url"`
	}
	if json.Unmarshal([]byte(os.Args[len(os.Args)-1]), &settings) != nil {
		os.Exit(94)
	}
	pid := settings.PID
	if pid <= 0 || os.Getenv("TEST_DESKTOP_UI_WRONG_INSTANCE") == "1" {
		pid = 1
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"pid": pid, "title": settings.Title, "preset": "GUI test boundary", "bundle": settings.Bundle, "url": settings.URL})
	os.Exit(0)
}

func TestDesktopColdQueueCannotExecuteInputInSender(t *testing.T) {
	host, closed, queued := desktopBackendFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := queueDesktopMessage(ctx, host, providers.BuildRequest{SessionID: "thread-42"}, stamped("COLD INPUT"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(queued); err != nil {
		t.Fatalf("input was not persisted: %v", err)
	}
	if _, err := os.Stat(closed); err != nil {
		t.Fatalf("sender backend remains open: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(closed), "unexpected-turn")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("sender consumed the GUI input: %v", err)
	}
}

func TestDesktopWarmBackendIsRejectedBeforeQueueing(t *testing.T) {
	host, _, queued := desktopBackendFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client, err := startAppServer(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.initialize(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := client.startThread(ctx, providers.BuildRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := client.requireCold(ctx); err == nil {
		t.Fatal("a backend owning a thread must not accept GUI delivery")
	}
	if _, err := os.Stat(queued); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("warm sender persisted input: %v", err)
	}
}

func TestDesktopRejectedGUISettingsPreventQueueing(t *testing.T) {
	host, _, queued := desktopBackendFixture(t)
	t.Setenv("TEST_DESKTOP_UI_REJECT", "1")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := queueDesktopMessage(ctx, host, providers.BuildRequest{SessionID: "thread-42"}, stamped("DO NOT SEND"), nil); err == nil {
		t.Fatal("failed GUI configuration must reject delivery")
	}
	if _, err := os.Stat(queued); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected GUI input was queued: %v", err)
	}
}
