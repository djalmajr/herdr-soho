//go:build !unix

package job

import "os"

// evidenceOpenFlags are the open flags of the bounded evidence read on
// windows (and any other non-unix system): plain read-only. Windows
// filesystems have no FIFOs, so there is no blocking open to defend
// against.
const evidenceOpenFlags = os.O_RDONLY
