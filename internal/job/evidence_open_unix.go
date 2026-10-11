//go:build unix

package job

import "syscall"

// evidenceOpenFlags are the open flags of the bounded evidence read on
// unix: read-only and non-blocking, so a FIFO swapped in between the
// pre-open regular check and the open still cannot block the read.
const evidenceOpenFlags = syscall.O_RDONLY | syscall.O_NONBLOCK
