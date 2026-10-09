//go:build windows

package job

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	procLockFileEx   = syscall.NewLazyDLL("kernel32.dll").NewProc("LockFileEx")
	procUnlockFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("UnlockFileEx")
)

const lockfileExclusiveLock = 0x00000002

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
