//go:build windows

package fakecli

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

func lock(path string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(path, 0o700)
		if err == nil {
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			// Windows can report ACCESS_DENIED when another process removes the
			// lock directory between this mkdir and the filesystem check. Treat
			// it as contention; the bounded retry below covers that race.
			if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
				return nil, err
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for %s", path)
		}
		time.Sleep(time.Millisecond)
	}
}
