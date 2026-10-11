//go:build unix

package job

import (
	"fmt"
	"os"
	"runtime"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// openEvidenceBase opens the trusted base of an evidence root (its own
// symlinks resolve) without ever blocking: see openEvidenceDir.
func openEvidenceBase(base string) (*os.Root, error) {
	f, err := os.OpenFile(base, platform.ReadOnlyNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	return openEvidenceDir(f)
}

// openEvidenceComponent opens one appended component through the parent's
// handle without ever blocking (a symlink resolving inside the parent is
// followed here and refused by the caller's Lstat check; one escaping the
// parent is refused by the handle): see openEvidenceDir.
func openEvidenceComponent(parent *os.Root, name string) (*os.Root, error) {
	f, err := parent.OpenFile(name, platform.ReadOnlyNonBlocking, 0)
	if err != nil {
		return nil, err
	}
	return openEvidenceDir(f)
}

// openEvidenceDir turns a handle opened with the non-blocking read mode
// into an os.Root only when the handle is a real directory: a FIFO, a
// device or a regular file in the directory's place is refused at once
// (os.OpenRoot and Root.OpenRoot open without O_NONBLOCK, so a FIFO there
// would hold the caller). The root is opened from the handle itself (the
// /dev/fd or /proc/self/fd entry of the open descriptor, which names the
// open directory, not a path that can be replaced) and must be the same
// directory as the handle. f is always closed.
func openEvidenceDir(f *os.File) (*os.Root, error) {
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return nil, errEvidenceOutside
	}
	fdPath := fmt.Sprintf("/dev/fd/%d", f.Fd())
	if runtime.GOOS == "linux" {
		fdPath = fmt.Sprintf("/proc/self/fd/%d", f.Fd())
	}
	r, err := os.OpenRoot(fdPath)
	if err != nil {
		return nil, errEvidenceOutside
	}
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(opened, info) {
		_ = r.Close()
		return nil, errEvidenceOutside
	}
	return r, nil
}
