//go:build !windows

package plugin

import (
	"os"
	"syscall"
)

func pickerShutdownSignals() []os.Signal {
	return []os.Signal{syscall.SIGTERM, syscall.SIGHUP}
}
