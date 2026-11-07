package codex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

type appServer struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	decoder *json.Decoder
	encoder *json.Encoder
	nextID  int
}

// A metadata or queue-only backend exits before the GUI consumes any input.
// A persistent daemon would retain the thread and run its queued turn itself.
func startAppServer(ctx context.Context, host desktopHost) (*appServer, error) {
	cmd := exec.CommandContext(ctx, host.binary, "app-server")
	prepareDesktopBackendProcess(cmd)
	cmd.WaitDelay = time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, err
	}
	cmd.Stderr = io.Discard
	cmd.Cancel = func() error {
		_ = stdin.Close()
		_ = stdout.Close()
		return stopDesktopBackendProcess(cmd)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("start temporary Codex Desktop backend: %w", err)
	}
	return &appServer{cmd: cmd, stdin: stdin, stdout: stdout, decoder: json.NewDecoder(stdout), encoder: json.NewEncoder(stdin), nextID: 1}, nil
}

func (s *appServer) Close() {
	defer stopDesktopBackendProcess(s.cmd)
	_ = s.stdin.Close()
	_ = s.stdout.Close()
	done := make(chan struct{})
	go func() {
		_ = s.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		_ = stopDesktopBackendProcess(s.cmd)
		<-done
	}
}

func desktopThreadTitle(prompt, threadID string) string {
	prompt = desktopPromptBody(prompt)
	line, _, _ := strings.Cut(prompt, "\n")
	count := 0
	for index := range line {
		if count == 64 {
			line = line[:index]
			break
		}
		count++
	}
	if line == "" {
		line = "ai-dispatch"
	}
	return line + " · " + threadID[:min(8, len(threadID))]
}

func (s *appServer) requireCold(ctx context.Context) error {
	raw, err := s.call(ctx, "thread/loaded/list", map[string]any{})
	if err != nil {
		return err
	}
	var loaded struct {
		Data []string `json:"data"`
	}
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return fmt.Errorf("read loaded Desktop threads: %w", err)
	}
	if loaded.Data == nil || len(loaded.Data) != 0 {
		return fmt.Errorf("the temporary queue backend is not confirmed cold; it must not execute Desktop input")
	}
	return nil
}

func newDesktopMessageID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	raw[6] = raw[6]&0x0f | 0x40
	raw[8] = raw[8]&0x3f | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], raw[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], raw[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], raw[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], raw[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], raw[10:16])
	return string(encoded[:]), nil
}
