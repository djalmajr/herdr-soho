//go:build windows

package fakecli

import "os"

var windowsSignalNumber = map[string]int{
	"SIGHUP": 1, "SIGINT": 2, "SIGQUIT": 3, "SIGILL": 4, "SIGTRAP": 5, "SIGABRT": 6,
	"SIGFPE": 8, "SIGKILL": 9, "SIGUSR1": 10, "SIGSEGV": 11, "SIGUSR2": 12,
	"SIGPIPE": 13, "SIGALRM": 14, "SIGTERM": 15,
}

func terminate(name string, exitCode int) {
	if number := windowsSignalNumber[name]; number != 0 {
		os.Exit(128 + number)
	}
	os.Exit(exitCode)
}
