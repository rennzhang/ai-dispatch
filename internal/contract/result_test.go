package contract

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderResultGoldenSuccess(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "results", "success.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result ProviderResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if !result.OK || result.Status != StatusSuccess || result.NextAction != NextDone {
		t.Fatalf("unexpected success result: %+v", result)
	}
	if result.SchemaVersion != "2.0" {
		t.Fatalf("schema version = %q", result.SchemaVersion)
	}
}

func TestProviderResultGoldenInputError(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "results", "input-error.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var result ProviderResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.OK || result.Status != StatusError || result.FailureClass == nil || *result.FailureClass != FailureInput {
		t.Fatalf("unexpected input error result: %+v", result)
	}
	if result.NextAction != NextFixInput {
		t.Fatalf("next_action = %q", result.NextAction)
	}
}
