package core

import "fmt"

// WarnShortTimeout warns that a --timeout given in milliseconds is under a
// second: the usual mistake is seconds typed where milliseconds are expected
// (passing --timeout 45 meaning 45 s). The value is left as passed: the
// command keeps its behavior. It prints the command's usual warning line and
// records it in the command's friction log (logFile; an empty logFile keeps
// the warning on screen only).
func WarnShortTimeout(command, logFile string, ms int64) {
	if ms <= 0 || ms >= 1000 {
		return
	}
	Warn(fmt.Sprintf("%s: --timeout is in milliseconds; %d is under a second (for %d seconds pass %d)", command, ms, ms, ms*1000), logFile, command)
}
