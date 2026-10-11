//go:build unix

package job

import (
	"os"
	"syscall"
)

// openNoFollow opens an existing path (never a create) without following a
// symlink at it and without blocking on a FIFO planted there: read-only, or
// read-write when write is set.
func openNoFollow(path string, write bool) (*os.File, error) {
	mode := os.O_RDONLY
	if write {
		mode = os.O_RDWR
	}
	return os.OpenFile(path, mode|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
