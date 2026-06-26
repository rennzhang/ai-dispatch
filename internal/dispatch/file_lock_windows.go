//go:build windows

package dispatch

func acquireFileLock(path string) (func(), error) {
	return func() {}, nil
}
