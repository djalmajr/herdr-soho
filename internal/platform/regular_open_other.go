//go:build !unix

package platform

import "os"

// ReadOnlyNonBlocking is plain read-only on systems without filesystem
// FIFOs (Windows), where an open of a path cannot block on one.
const ReadOnlyNonBlocking = os.O_RDONLY
