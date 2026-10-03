//go:build windows

package platform

import (
	"syscall"
	"unsafe"
)

// statfsFree measures one existing path with GetDiskFreeSpaceExW. The API
// writes, in order: the bytes available to the caller (the user-free space,
// quotas included), the volume's total bytes and the volume's free bytes.
func statfsFree(path string) (free, total int64, ok bool) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	wpath, err := syscall.UTF16FromString(path)
	if err != nil {
		return 0, 0, false
	}
	var freeToCaller, totalBytes, freeBytes uint64
	ret, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&wpath[0])),
		uintptr(unsafe.Pointer(&freeToCaller)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&freeBytes)))
	if ret == 0 {
		return 0, 0, false
	}
	return int64(freeToCaller), int64(totalBytes), true
}

// Windows: swap is not measured, so the pressure check only ever sees the
// disk part.
func swapUsageReal(_ Env) (used, total int64, ok bool) {
	return 0, 0, false
}
