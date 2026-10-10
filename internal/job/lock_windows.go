//go:build windows

package job

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

var (
	procLockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	procUnlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

const lockfileExclusiveLock = 0x00000002

// lockfileFailImmediately is LOCKFILE_FAIL_IMMEDIATELY: a locked file
// fails the call instead of blocking it.
const lockfileFailImmediately = 0x00000001

// errLockViolation is ERROR_LOCK_VIOLATION: another handle holds the lock
// and LOCKFILE_FAIL_IMMEDIATELY was set.
var errLockViolation = syscall.Errno(33)

func flock(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r == 0 {
		if err == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return err
	}
	return nil
}

func funlock(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procUnlockFileEx.Call(
		f.Fd(),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r == 0 {
		if err == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return err
	}
	return nil
}

// tryFlock takes the exclusive lock without blocking: a file locked by
// another handle (in this process or another) is errStartLockHeld.
func tryFlock(f *os.File) error {
	var ol syscall.Overlapped
	r, _, err := procLockFileEx.Call(
		f.Fd(),
		uintptr(lockfileExclusiveLock|lockfileFailImmediately),
		0,
		1,
		0,
		uintptr(unsafe.Pointer(&ol)),
	)
	if r == 0 {
		if errors.Is(err, errLockViolation) {
			return errStartLockHeld
		}
		if err == syscall.Errno(0) {
			return syscall.EINVAL
		}
		return err
	}
	return nil
}
