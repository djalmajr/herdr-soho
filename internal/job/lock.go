package job

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// localLocks serializes goroutines in this process. BSD flock is
// per-process, so a second flock from another goroutine would succeed
// while the first still holds the critical section. The OS lock still
// covers other processes and is released when this process exits.
var localLocks sync.Map

func mutexFor(path string) *sync.Mutex {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	v, _ := localLocks.LoadOrStore(key, &sync.Mutex{})
	return v.(*sync.Mutex)
}

func withLock(dir string, fn func() error) error {
	path := filepath.Join(dir, "lock")
	mu := mutexFor(path)
	mu.Lock()
	defer mu.Unlock()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = flock(f); err != nil {
		return err
	}
	defer func() { _ = funlock(f) }()
	return fn()
}

// errStartLockHeld is the distinct "held" result of a start-lock try: the
// lock is taken by another live starter (or by this process through a
// separate descriptor). It is not a failure.
var errStartLockHeld = errors.New("job: the start lock is held")

// tryStartLock opens <dir>/start.lock (mode 0600) and takes its OS lock
// without blocking: a held lock returns errStartLockHeld, any other
// failure the underlying error. The file is separate from the job lock
// and takes no mutexFor: each try opens its own descriptor, and the OS
// lock on a separately opened file conflicts within one process (flock is
// per open file, LockFileEx per file name), so a second starter or the
// recovery in this same process sees the held result. The caller releases
// it with closeStartLock on every path: the OS drops the lock when the
// process dies, which is the crash signal the recovery reads.
func tryStartLock(dir string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(dir, "start.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := tryFlock(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// closeStartLock releases and closes the start lock.
func closeStartLock(f *os.File) {
	_ = funlock(f)
	_ = f.Close()
}
