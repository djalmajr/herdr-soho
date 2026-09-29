//go:build !windows

package herdr

import "syscall"

var signalNames = map[int]string{
	int(syscall.SIGHUP): "SIGHUP", int(syscall.SIGINT): "SIGINT", int(syscall.SIGQUIT): "SIGQUIT",
	int(syscall.SIGILL): "SIGILL", int(syscall.SIGTRAP): "SIGTRAP", int(syscall.SIGABRT): "SIGABRT",
	int(syscall.SIGBUS): "SIGBUS", int(syscall.SIGFPE): "SIGFPE", int(syscall.SIGKILL): "SIGKILL",
	int(syscall.SIGUSR1): "SIGUSR1", int(syscall.SIGSEGV): "SIGSEGV", int(syscall.SIGUSR2): "SIGUSR2",
	int(syscall.SIGPIPE): "SIGPIPE", int(syscall.SIGALRM): "SIGALRM", int(syscall.SIGTERM): "SIGTERM",
	int(syscall.SIGURG): "SIGURG", int(syscall.SIGSTOP): "SIGSTOP", int(syscall.SIGTSTP): "SIGTSTP",
	int(syscall.SIGCONT): "SIGCONT", int(syscall.SIGCHLD): "SIGCHLD", int(syscall.SIGTTIN): "SIGTTIN",
	int(syscall.SIGTTOU): "SIGTTOU", int(syscall.SIGIO): "SIGIO", int(syscall.SIGXCPU): "SIGXCPU",
	int(syscall.SIGXFSZ): "SIGXFSZ", int(syscall.SIGVTALRM): "SIGVTALRM", int(syscall.SIGPROF): "SIGPROF",
	int(syscall.SIGWINCH): "SIGWINCH", int(syscall.SIGSYS): "SIGSYS",
}

func signalName(number int) string { return signalNames[number] }

func signalNumber(name string) int {
	for number, signal := range signalNames {
		if signal == name {
			return number
		}
	}
	return 0
}
