//go:build !unix

package job

import "os"

// launchLockOpenFlags opens launch.lock; the symlink and file-kind checks
// around the open (openLaunchLock) apply on every platform.
const launchLockOpenFlags = os.O_CREATE | os.O_RDWR
