package cli

import (
	"bytes"
	"testing"
)

func TestExplicitTargetKeepsEveryPromptWord(t *testing.T) {
	for _, words := range [][]string{{"hello"}, {"hello", "world"}} {
		args := append([]string{"--target", "codex"}, words...)
		var stderr bytes.Buffer
		req, _, err := parseSend("send", args, &stderr)
		if err != nil {
			t.Fatal(err)
		}
		want := "hello"
		if len(words) > 1 {
			want = "hello world"
		}
		if req.Target != "codex" || req.Prompt != want {
			t.Fatalf("request=%+v", req)
		}
	}
}

func TestActivityUnsetSurvivesUntilSurfaceResolution(t *testing.T) {
	for _, command := range []string{"send", "resume"} {
		var stderr bytes.Buffer
		req, _, err := parseSend(command, []string{"--prompt-file", "prompt.txt"}, &stderr)
		if err != nil {
			t.Fatal(err)
		}
		if req.ActivityTimeoutSeconds != -1 {
			t.Fatalf("request=%+v", req)
		}
	}
}
