//go:build !unix

package job

import "os"

// openEvidenceBase opens the trusted base of an evidence root (its own
// symlinks resolve). Systems without filesystem FIFOs (Windows) cannot
// block on the open.
func openEvidenceBase(base string) (*os.Root, error) {
	return os.OpenRoot(base)
}

// openEvidenceComponent opens one appended component through the parent's
// handle, which refuses a component that escapes the parent; the caller's
// Lstat check refuses a symlink.
func openEvidenceComponent(parent *os.Root, name string) (*os.Root, error) {
	return parent.OpenRoot(name)
}
