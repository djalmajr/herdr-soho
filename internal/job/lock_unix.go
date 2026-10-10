//go:build !windows

package job

import (
	"errors"
	"os"
	"syscall"
)

func flock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// tryFlock takes the exclusive lock without blocking: a lock held by
// another descriptor (in this process or another) is errStartLockHeld.
func tryFlock(f *os.File) error {
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return errStartLockHeld
		}
		return err
	}
	return nil
}

func funlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
