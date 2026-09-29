//go:build !windows

package platform

import (
	"os/exec"
	"syscall"
)

func runWindowsCmdTreeKill(_ string, _ CmdInvocationResult, _ RunOptions) (RunResult, bool) {
	return RunResult{}, false
}

func processSignal(exitErr *exec.ExitError) string {
	status, ok := exitErr.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() {
		return ""
	}
	name := map[syscall.Signal]string{
		syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT",
		syscall.SIGILL: "SIGILL", syscall.SIGTRAP: "SIGTRAP", syscall.SIGABRT: "SIGABRT",
		syscall.SIGBUS: "SIGBUS", syscall.SIGFPE: "SIGFPE", syscall.SIGKILL: "SIGKILL",
		syscall.SIGUSR1: "SIGUSR1", syscall.SIGSEGV: "SIGSEGV", syscall.SIGUSR2: "SIGUSR2",
		syscall.SIGPIPE: "SIGPIPE", syscall.SIGALRM: "SIGALRM", syscall.SIGTERM: "SIGTERM",
		syscall.SIGURG: "SIGURG", syscall.SIGSTOP: "SIGSTOP", syscall.SIGTSTP: "SIGTSTP",
		syscall.SIGCONT: "SIGCONT", syscall.SIGCHLD: "SIGCHLD", syscall.SIGTTIN: "SIGTTIN",
		syscall.SIGTTOU: "SIGTTOU", syscall.SIGIO: "SIGIO", syscall.SIGXCPU: "SIGXCPU",
		syscall.SIGXFSZ: "SIGXFSZ", syscall.SIGVTALRM: "SIGVTALRM", syscall.SIGPROF: "SIGPROF",
		syscall.SIGWINCH: "SIGWINCH", syscall.SIGSYS: "SIGSYS",
	}[status.Signal()]
	if name != "" {
		return name
	}
	return ""
}

func errnoName(errno syscall.Errno) string {
	return errnoNames[errno]
}

var errnoNames = func() map[syscall.Errno]string {
	entries := []struct {
		errno syscall.Errno
		name  string
	}{
		{syscall.E2BIG, "E2BIG"}, {syscall.EACCES, "EACCES"}, {syscall.EADDRINUSE, "EADDRINUSE"},
		{syscall.EADDRNOTAVAIL, "EADDRNOTAVAIL"}, {syscall.EAFNOSUPPORT, "EAFNOSUPPORT"}, {syscall.EAGAIN, "EAGAIN"},
		{syscall.EALREADY, "EALREADY"}, {syscall.EBADF, "EBADF"}, {syscall.EBADMSG, "EBADMSG"},
		{syscall.EBUSY, "EBUSY"}, {syscall.ECANCELED, "ECANCELED"}, {syscall.ECHILD, "ECHILD"},
		{syscall.ECONNABORTED, "ECONNABORTED"}, {syscall.ECONNREFUSED, "ECONNREFUSED"}, {syscall.ECONNRESET, "ECONNRESET"},
		{syscall.EDEADLK, "EDEADLK"}, {syscall.EDESTADDRREQ, "EDESTADDRREQ"}, {syscall.EDOM, "EDOM"},
		{syscall.EDQUOT, "EDQUOT"}, {syscall.EEXIST, "EEXIST"}, {syscall.EFAULT, "EFAULT"},
		{syscall.EFBIG, "EFBIG"}, {syscall.EHOSTUNREACH, "EHOSTUNREACH"}, {syscall.EIDRM, "EIDRM"},
		{syscall.EILSEQ, "EILSEQ"}, {syscall.EINPROGRESS, "EINPROGRESS"}, {syscall.EINTR, "EINTR"},
		{syscall.EINVAL, "EINVAL"}, {syscall.EIO, "EIO"}, {syscall.EISDIR, "EISDIR"},
		{syscall.ELOOP, "ELOOP"}, {syscall.EMFILE, "EMFILE"}, {syscall.EMLINK, "EMLINK"},
		{syscall.EMSGSIZE, "EMSGSIZE"}, {syscall.EMULTIHOP, "EMULTIHOP"}, {syscall.ENAMETOOLONG, "ENAMETOOLONG"},
		{syscall.ENETDOWN, "ENETDOWN"}, {syscall.ENETRESET, "ENETRESET"}, {syscall.ENETUNREACH, "ENETUNREACH"},
		{syscall.ENFILE, "ENFILE"}, {syscall.ENOBUFS, "ENOBUFS"}, {syscall.ENODATA, "ENODATA"},
		{syscall.ENODEV, "ENODEV"}, {syscall.ENOENT, "ENOENT"}, {syscall.ENOEXEC, "ENOEXEC"},
		{syscall.ENOLCK, "ENOLCK"}, {syscall.ENOLINK, "ENOLINK"}, {syscall.ENOMEM, "ENOMEM"},
		{syscall.ENOMSG, "ENOMSG"}, {syscall.ENOPROTOOPT, "ENOPROTOOPT"}, {syscall.ENOSPC, "ENOSPC"},
		{syscall.ENOSR, "ENOSR"}, {syscall.ENOSTR, "ENOSTR"}, {syscall.ENOSYS, "ENOSYS"},
		{syscall.ENOTCONN, "ENOTCONN"}, {syscall.ENOTDIR, "ENOTDIR"}, {syscall.ENOTEMPTY, "ENOTEMPTY"},
		{syscall.ENOTSOCK, "ENOTSOCK"}, {syscall.ENOTSUP, "ENOTSUP"}, {syscall.ENOTTY, "ENOTTY"},
		{syscall.ENXIO, "ENXIO"}, {syscall.EOPNOTSUPP, "EOPNOTSUPP"}, {syscall.EOVERFLOW, "EOVERFLOW"},
		{syscall.EPFNOSUPPORT, "EPFNOSUPPORT"}, {syscall.EPIPE, "EPIPE"}, {syscall.EPROTO, "EPROTO"},
		{syscall.EPROTONOSUPPORT, "EPROTONOSUPPORT"}, {syscall.EPROTOTYPE, "EPROTOTYPE"}, {syscall.ERANGE, "ERANGE"},
		{syscall.EROFS, "EROFS"}, {syscall.ESPIPE, "ESPIPE"}, {syscall.ESRCH, "ESRCH"},
		{syscall.ETIME, "ETIME"}, {syscall.ETIMEDOUT, "ETIMEDOUT"}, {syscall.ETXTBSY, "ETXTBSY"},
		{syscall.EWOULDBLOCK, "EWOULDBLOCK"}, {syscall.EXDEV, "EXDEV"},
	}
	result := make(map[syscall.Errno]string, len(entries))
	for _, entry := range entries {
		if _, exists := result[entry.errno]; !exists {
			result[entry.errno] = entry.name
		}
	}
	return result
}()

func setWindowsInvocation(_ *exec.Cmd, _ CmdInvocationResult) {}
