//go:build !windows

package doctor

import "syscall"

var writableAccess = func(path string) error { return syscall.Access(path, 2) }

func isWritable(path string) bool { return writableAccess(path) == nil }
