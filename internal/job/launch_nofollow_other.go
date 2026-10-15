//go:build !unix && !windows

package job

import "os"

// openNoFollow refuses: these systems cannot open a path without following
// links, so every no-follow read or probe fails closed (a start lock is
// never probed, a launch record or a managed state is never read).
func openNoFollow(path string, write bool) (*os.File, error) {
	return nil, errNoFollowUnsupported
}
