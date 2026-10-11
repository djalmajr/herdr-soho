//go:build unix

package platform

import "syscall"

// ReadOnlyNonBlocking is the open mode of a read that must never block on
// a FIFO: read-only and non-blocking. On a regular file the flag changes
// nothing.
const ReadOnlyNonBlocking = syscall.O_RDONLY | syscall.O_NONBLOCK
