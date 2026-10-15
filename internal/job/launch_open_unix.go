//go:build unix

package job

import (
	"os"
	"syscall"
)

// launchLockOpenFlags opens launch.lock without following a symlink and
// without blocking on a FIFO planted at the path.
const launchLockOpenFlags = os.O_CREATE | os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
