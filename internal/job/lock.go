package job

import (
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
