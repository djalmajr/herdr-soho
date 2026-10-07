//go:build !windows

package install

import "errors"

// DefaultUserPathStore refuses on non-Windows hosts: the user-PATH update
// (the old installers' -AddToPath opt-in) is Windows-only. The CLI rejects
// --add-to-path before any effect on these platforms; this store is the
// fail-closed second layer.
func DefaultUserPathStore() (UserPathStore, error) {
	return nil, errors.New("install: the user PATH update is only supported on Windows")
}
