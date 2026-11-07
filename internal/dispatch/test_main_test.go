package dispatch

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "dispatch-tests-*")
	if err != nil {
		panic(err)
	}
	os.Setenv("AI_DISPATCH_HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
