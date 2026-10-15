//go:build unix

package job

import "syscall"

func makeTestFIFO(path string) error { return syscall.Mkfifo(path, 0o600) }

const testHasFIFO = true
