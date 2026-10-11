package job

import (
	"errors"
	"io"
	"os"
)

// errNoFollowUnsupported is openNoFollow on a system that is neither unix
// nor Windows: a path cannot be opened there without following links, so
// the start lock probe and the private reads fail closed.
var errNoFollowUnsupported = errors.New("job: a private file cannot be opened without following links on this system")

// readPrivateFile reads path only as a regular file of at most max bytes
// that is the file the path names: an Lstat, a no-follow and non-blocking
// open of the existing path (openNoFollow), the handle's own kind and size,
// and os.SameFile between the Lstat, the handle and a fresh Lstat; the read
// itself is bounded. A missing path is reported with ok false and no
// error; a refused kind, size or identity is errLaunchFile.
func readPrivateFile(path string, max int64) (data []byte, ok bool, err error) {
	pre, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if !pre.Mode().IsRegular() || pre.Size() > max {
		return nil, false, errLaunchFile
	}
	f, err := openNoFollow(path, false)
	if err != nil {
		return nil, false, errLaunchFile
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > max || !os.SameFile(pre, info) {
		return nil, false, errLaunchFile
	}
	post, err := os.Lstat(path)
	if err != nil || !post.Mode().IsRegular() || !os.SameFile(info, post) {
		return nil, false, errLaunchFile
	}
	data, err = io.ReadAll(io.LimitReader(f, max+1))
	if err != nil || int64(len(data)) > max {
		return nil, false, errLaunchFile
	}
	return data, true, nil
}

// probeStartLock reports whether the OS lock of the start.lock at path is
// held: a held lock is a live starter, a free lock is a starter that is
// gone. The probe opens path read-write with openNoFollow (never a
// create), and only when the opened handle is a regular file of
// at most maxLaunchBytes that is the file the path named when pre was
// read (an os.SameFile against the handle's own Stat and a fresh Lstat)
// does it try the lock; a refused kind is errLaunchFile, and an open
// error (a missing path included) is returned as is. The try is
// non-blocking: the lock either is held, is free (released and reported
// as held=false) or fails with the underlying error.
func probeStartLock(path string, pre os.FileInfo) (held bool, err error) {
	f, err := openNoFollow(path, true)
	if err != nil {
		return false, err
	}
	info, statErr := f.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxLaunchBytes || !os.SameFile(pre, info) {
		_ = f.Close()
		return false, errLaunchFile
	}
	post, lstatErr := os.Lstat(path)
	if lstatErr != nil || !post.Mode().IsRegular() || !os.SameFile(info, post) {
		_ = f.Close()
		return false, errLaunchFile
	}
	err = tryFlock(f)
	if err == nil {
		_ = funlock(f)
		_ = f.Close()
		return false, nil
	}
	_ = f.Close()
	if errors.Is(err, errStartLockHeld) {
		return true, nil
	}
	return false, err
}
