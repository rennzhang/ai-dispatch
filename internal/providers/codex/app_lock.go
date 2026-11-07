package codex

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type desktopLock struct{ file *os.File }

func desktopLockKey(host desktopHost, scope string) string {
	return fmt.Sprintf("%s:%s:%d:%s", host.bundle, host.binary, host.pid, scope)
}

func acquireDesktopLock(ctx context.Context, key string, wait bool) (*desktopLock, error) {
	home := os.Getenv("AI_DISPATCH_HOME")
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		home = filepath.Join(userHome, ".ai-dispatch")
	}
	root := filepath.Join(home, "desktop-locks")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(root, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(key))))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		if err := ctx.Err(); err != nil {
			_ = file.Close()
			return nil, err
		}
		locked, err := tryDesktopFileLock(file)
		if err != nil {
			_ = file.Close()
			return nil, err
		}
		if locked {
			return &desktopLock{file: file}, nil
		}
		if !wait {
			_ = file.Close()
			return nil, fmt.Errorf("another dispatch already owns this Desktop session (app_session_busy); no input was queued")
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (lock *desktopLock) Close() {
	_ = unlockDesktopFile(lock.file)
	_ = lock.file.Close()
}

func (lock *desktopLock) pending() (string, error) {
	if _, err := lock.file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	raw, err := io.ReadAll(io.LimitReader(lock.file, 128))
	return strings.TrimSpace(string(raw)), err
}

func (lock *desktopLock) setPending(id string) error {
	if err := lock.file.Truncate(0); err != nil {
		return err
	}
	if _, err := lock.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.WriteString(lock.file, id); err != nil {
		return err
	}
	return lock.file.Sync()
}

// Retain an unconfirmed delivery across cancellation and pickup timeouts. A
// retry must observe its terminal turn before it can enqueue another message.
func acquireDesktopSession(ctx context.Context, _ desktopHost, sessionID string) (*desktopLock, error) {
	sessions, err := codexSessionsDir()
	if err != nil {
		return nil, err
	}
	lock, err := acquireDesktopLock(ctx, sessions+":session:"+sessionID, false)
	if err != nil {
		return nil, err
	}
	pending, err := lock.pending()
	if err == nil && pending != "" {
		err = confirmPreviousDesktopDelivery(sessionID, pending)
	}
	if err == nil {
		err = lock.setPending("")
	}
	if err != nil {
		lock.Close()
		return nil, err
	}
	return lock, nil
}

func confirmPreviousDesktopDelivery(sessionID, dispatchID string) error {
	anchor, err := findRollout(sessionID)
	if err != nil && !errors.Is(err, errRolloutNotFound) {
		return err
	}
	if err == nil {
		paths, readErr := rolloutHistoryPaths(sessionID, anchor)
		if readErr != nil {
			return readErr
		}
		var history []byte
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			history = append(history, data...)
			history = append(history, '\n')
		}
		for _, turn := range userTurns(history) {
			if desktopPromptID(turn.text) != dispatchID {
				continue
			}
			if _, ended := terminalByID(history, turn.turnID); ended {
				return nil
			}
		}
	}
	return fmt.Errorf("previous Desktop delivery %s has no confirmed terminal turn (app_delivery_pending); check the same Desktop thread before retrying; no input was queued", dispatchID)
}
