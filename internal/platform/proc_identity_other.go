//go:build unix && !darwin && !linux

package platform

// procStartIdentity on the other unix systems is the ps lstart text of
// ReadProcFull: one-second precision.
func procStartIdentity(pid int, env Env) (string, string, ProcLiveness) {
	started, name, live := ReadProcFull(pid, env)
	if live != ProcRunning {
		return "", "", live
	}
	return "ps:" + started, name, ProcRunning
}
