//go:build windows

package plugin

import (
	"os"
	"syscall"
)

// The runtime turns CTRL_C/CTRL_BREAK into SIGINT and CTRL_CLOSE/LOGOFF/
// SHUTDOWN into SIGTERM; without Notify for SIGTERM, closing the console
// ends the process before the Esc path restores it and cancels the finds.
func pickerShutdownSignals() []os.Signal {
	return []os.Signal{os.Interrupt, syscall.SIGTERM}
}
