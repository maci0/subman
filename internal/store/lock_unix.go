//go:build unix

package store

import (
	"os"
	"syscall"
)

// lockFile takes an exclusive advisory lock on f, blocking until it can.
// An interrupted call is retried; closing f releases the lock.
func lockFile(f *os.File) error {
	for {
		err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			return err
		}
	}
}
