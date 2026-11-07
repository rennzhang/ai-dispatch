package codex

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/rennzhang/ai-dispatch/internal/contract"
	"github.com/rennzhang/ai-dispatch/internal/providers"
	"github.com/rennzhang/ai-dispatch/internal/routing"
)

// Explicit opt-in UI acceptance. It changes only model settings on the named
// existing thread: no thread creation, queue/add, turn/start or model inference.
func TestDesktopSettingsLiveMediumThenHigh(t *testing.T) {
	if os.Getenv("AI_DISPATCH_TEST_SETTINGS_ONLY") != "on" {
		t.Skip("explicit model-settings UI acceptance only")
	}
	session := os.Getenv("AI_DISPATCH_TEST_SETTINGS_SESSION")
	if session == "" {
		t.Fatal("AI_DISPATCH_TEST_SETTINGS_SESSION is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	host, err := resolveDesktopHost(ctx)
	if err != nil {
		t.Fatal(err)
	}
	client, err := startAppServer(ctx, host)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.initialize(ctx); err != nil {
		client.Close()
		t.Fatal(err)
	}
	raw, err := client.call(ctx, "thread/read", map[string]any{"threadId": session, "includeTurns": false})
	client.Close()
	if err != nil {
		t.Fatal(err)
	}
	var metadata struct {
		Thread struct {
			Name string `json:"name"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &metadata); err != nil || metadata.Thread.Name == "" {
		t.Fatal("test thread has no persisted name")
	}
	req := providers.BuildRequest{SessionID: session, Target: routing.DispatchTarget{Provider: "codex", Model: "gpt-6.1-sol"}, Fast: false}
	for _, effort := range []contract.Effort{contract.EffortMedium, contract.EffortHigh} {
		if err := openDesktopThread(ctx, host, session, false); err != nil {
			t.Fatal(err)
		}
		req.Effort = effort
		if err := configureDesktopThread(ctx, host, req, metadata.Thread.Name); err != nil {
			t.Fatalf("pid=%d effort=%s: %v", host.pid, effort, err)
		}
		t.Logf("pid=%d configured model=gpt-6.1-sol effort=%s fast=false without queueing", host.pid, effort)
	}
}
