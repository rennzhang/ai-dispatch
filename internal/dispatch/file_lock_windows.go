//go:build windows

package dispatch

import "context"

func acquireFileLock(ctx context.Context, path string) (func(), error) {
	return func() {}, nil
}
