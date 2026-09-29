//go:build windows

package herdr

var windowsSignals = map[int]string{1: "SIGHUP", 2: "SIGINT", 3: "SIGQUIT", 4: "SIGILL", 5: "SIGTRAP", 6: "SIGABRT", 8: "SIGFPE", 9: "SIGKILL", 10: "SIGUSR1", 11: "SIGSEGV", 12: "SIGUSR2", 13: "SIGPIPE", 14: "SIGALRM", 15: "SIGTERM"}

func signalName(number int) string { return windowsSignals[number] }

func signalNumber(name string) int {
	for number, signal := range windowsSignals {
		if signal == name {
			return number
		}
	}
	return 0
}
