package testutil

import (
	"errors"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var fixtureKernel = syscall.NewLazyDLL("kernel32.dll")

func denyFileReads(path string) (func() error, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	lock := fixtureKernel.NewProc("LockFileEx")
	unlock := fixtureKernel.NewProc("UnlockFileEx")
	var offset syscall.Overlapped
	// An exclusive byte-range lock denies reads through other handles, even
	// from this process. Fail immediately instead of waiting on another lock.
	const flags = 3 // LOCKFILE_EXCLUSIVE_LOCK | LOCKFILE_FAIL_IMMEDIATELY
	const length = 0xffffffff
	ok, _, callErr := lock.Call(f.Fd(), flags, 0, length, length, uintptr(unsafe.Pointer(&offset)))
	runtime.KeepAlive(&offset)
	if ok == 0 {
		return nil, errors.Join(callErr, f.Close())
	}
	return func() error {
		ok, _, callErr := unlock.Call(f.Fd(), 0, length, length, uintptr(unsafe.Pointer(&offset)))
		runtime.KeepAlive(&offset)
		closeErr := f.Close()
		if ok == 0 {
			return errors.Join(callErr, closeErr)
		}
		return closeErr
	}, nil
}
