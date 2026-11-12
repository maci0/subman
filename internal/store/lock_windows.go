//go:build windows

package store

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes an exclusive lock on the whole of f, blocking until it can.
// Closing f releases the lock.
func lockFile(f *os.File) error {
	return windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK,
		0,
		1,
		0,
		new(windows.Overlapped),
	)
}
