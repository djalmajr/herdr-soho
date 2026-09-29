//go:build !windows

package setup

import "syscall"

const accessWrite = 2 // POSIX W_OK.

func localDirectoryWritable(path string) bool {
	return syscall.Access(path, accessWrite) == nil
}
