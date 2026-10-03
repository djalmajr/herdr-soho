//go:build !windows

package platform

import (
	"os"
	"runtime"
	"syscall"
)

// statfsFree measures one existing path (syscall.Statfs, Bavail for the
// user-free space). A non-existing path is the caller's problem:
// diskFreeReal walks up to the first existing ancestor.
func statfsFree(path string) (free, total int64, ok bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, false
	}
	bsize := int64(st.Bsize)
	if bsize <= 0 {
		return 0, 0, false
	}
	return int64(st.Bavail) * bsize, int64(st.Blocks) * bsize, true
}

func swapUsageReal(env Env) (used, total int64, ok bool) {
	switch runtime.GOOS {
	case "darwin":
		// `sysctl -n vm.swapusage`: total = 47104.00M  used = 45517.00M
		// free = 1587.00M  (encrypted). A 2 s ceiling keeps a stuck
		// sysctl from delaying the command.
		r := RunCli("sysctl", []string{"-n", "vm.swapusage"}, RunOptions{Env: env, TimeoutMs: 2000})
		if r.NotFound || r.TimedOut || r.Error != "" || r.Status == nil || *r.Status != 0 {
			return 0, 0, false
		}
		return parseSwapSysctl(r.Stdout)
	case "linux":
		data, err := os.ReadFile("/proc/meminfo")
		if err != nil {
			return 0, 0, false
		}
		return parseMeminfoSwap(string(data))
	default:
		return 0, 0, false
	}
}
