package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopCatalogDiscoveryIsReadOnlyAndClosesBackend(t *testing.T) {
	host, closed, queued := desktopBackendFixture(t)
	t.Setenv("TEST_DESKTOP_MODEL_PAGING", "pages")
	models, err := DiscoverCLIModels(context.Background(), host.binary)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].ID != "gpt-6.1-sol" || models[1].ID != "gpt-6-astra" || !models[0].Default || len(models[0].Efforts) != 1 || models[0].Efforts[0] != "high" {
		t.Fatalf("models=%+v", models)
	}
	if _, err := os.Stat(closed); err != nil {
		t.Fatalf("temporary backend did not exit: %v", err)
	}
	for _, name := range []string{queued, filepath.Join(filepath.Dir(queued), "unexpected-turn"), filepath.Join(filepath.Dir(queued), "name")} {
		if _, err := os.Stat(name); !os.IsNotExist(err) {
			t.Fatalf("model discovery mutated a thread: %s %v", name, err)
		}
	}
}

func TestDesktopCatalogRejectsRepeatedCursor(t *testing.T) {
	host, closed, _ := desktopBackendFixture(t)
	t.Setenv("TEST_DESKTOP_MODEL_PAGING", "repeat")
	if _, err := DiscoverCLIModels(context.Background(), host.binary); err == nil || !strings.Contains(err.Error(), "repeated pagination cursor") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(closed); err != nil {
		t.Fatalf("failed discovery left backend open: %v", err)
	}
}

func TestDesktopCatalogFiltersHiddenAndRejectsMalformedData(t *testing.T) {
	page := []byte(`{"data":[{"id":"logical","model":"native","displayName":"Visible","supportedReasoningEfforts":[{"reasoningEffort":"high"}]},{"model":"hidden","hidden":true},{"id":""}],"nextCursor":null}`)
	models, next, err := decodeModelPage(page)
	if err != nil || next != "" || len(models) != 1 || models[0].ID != "native" {
		t.Fatalf("models=%+v next=%q err=%v", models, next, err)
	}
	for _, raw := range []string{`{}`, `{"data":null}`, `not-json`} {
		if _, _, err := decodeModelPage([]byte(raw)); err == nil {
			t.Fatalf("invalid catalog accepted: %s", raw)
		}
	}
	models, _, err = decodeModelPage([]byte(`{"data":[]}`))
	if err != nil || len(models) != 0 {
		t.Fatalf("empty successful catalog rejected: %v", err)
	}
}
