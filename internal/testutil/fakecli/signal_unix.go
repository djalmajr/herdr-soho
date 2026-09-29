//go:build !windows

package fakecli

import (
	"os"
	"syscall"
)

var signalByName = map[string]syscall.Signal{
	"SIGHUP": syscall.SIGHUP, "SIGINT": syscall.SIGINT, "SIGQUIT": syscall.SIGQUIT,
	"SIGILL": syscall.SIGILL, "SIGTRAP": syscall.SIGTRAP, "SIGABRT": syscall.SIGABRT,
	"SIGBUS": syscall.SIGBUS, "SIGFPE": syscall.SIGFPE, "SIGKILL": syscall.SIGKILL,
	"SIGUSR1": syscall.SIGUSR1, "SIGSEGV": syscall.SIGSEGV, "SIGUSR2": syscall.SIGUSR2,
	"SIGPIPE": syscall.SIGPIPE, "SIGALRM": syscall.SIGALRM, "SIGTERM": syscall.SIGTERM,
}

func terminate(name string, exitCode int) {
	if signal, ok := signalByName[name]; ok {
		_ = syscall.Kill(os.Getpid(), signal)
	}
	os.Exit(exitCode)
}
