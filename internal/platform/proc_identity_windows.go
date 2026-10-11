//go:build windows

package platform

// procStartIdentity is the creation FILETIME ticks and image base name of
// ReadProcFull.
func procStartIdentity(pid int, env Env) (string, string, ProcLiveness) {
	started, name, live := ReadProcFull(pid, env)
	if live != ProcRunning {
		return "", "", live
	}
	return "windows:" + started, name, ProcRunning
}
