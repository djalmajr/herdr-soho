//go:build !unix

package job

import "os"

// launchLockOpenFlags opens launch.lock. The no-follow and non-blocking
// open flags are requested on unix systems only; elsewhere the Lstat
// pre-check and the post-open regular and SameFile checks (openLaunchLock)
// apply, and an entry swapped after the Lstat may be opened (followed)
// before the identity check refuses it, not proven on Windows.
const launchLockOpenFlags = os.O_CREATE | os.O_RDWR
