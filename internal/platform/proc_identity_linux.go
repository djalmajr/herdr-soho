//go:build linux

package platform

import (
	"os"
	"strconv"
	"strings"
)

// procStartIdentity reads /proc/<pid>/stat once: the command name between
// the first "(" and the last ")", the state, and field 22 (starttime, in
// clock ticks since boot), qualified by the boot id
// (/proc/sys/kernel/random/boot_id), so a start time from another boot
// never equals one from this boot.
func procStartIdentity(pid int, env Env) (string, string, ProcLiveness) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", ProcGone
		}
		return "", "", ProcUnknown
	}
	text := string(data)
	open, close := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 0 || close < open {
		return "", "", ProcUnknown
	}
	name := text[open+1 : close]
	rest := strings.Fields(text[close+1:])
	// rest[0] is field 3 (state); starttime is field 22, rest[19].
	if len(rest) < 20 || name == "" {
		return "", "", ProcUnknown
	}
	if rest[0] == "Z" || rest[0] == "X" || rest[0] == "x" {
		return "", "", ProcGone
	}
	if _, err := strconv.ParseUint(rest[19], 10, 64); err != nil {
		return "", "", ProcUnknown
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	bootID := strings.TrimSpace(string(boot))
	if err != nil || bootID == "" {
		return "", "", ProcUnknown
	}
	return "linux:" + bootID + ":" + rest[19], name, ProcRunning
}
