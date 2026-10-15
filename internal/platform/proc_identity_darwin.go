//go:build darwin

package platform

import (
	"encoding/binary"
	"strconv"
	"syscall"
	"unsafe"
)

// darwinKinfoProcSize is sizeof(struct kinfo_proc) on 64-bit macOS; the
// offsets below are those of its leading struct extern_proc: p_starttime
// (struct timeval: tv_sec int64, tv_usec int32) at 0, p_stat at 36, p_pid
// at 40 and p_comm (MAXCOMLEN+1 bytes) at 243. The record's own p_pid must
// equal the pid asked for, so a layout that does not match reads as
// unknown, never as another process's identity.
const (
	darwinKinfoProcSize = 648
	darwinOffStat       = 36
	darwinOffPid        = 40
	darwinOffComm       = 243
	darwinCommLen       = 17
	darwinStatZombie    = 5
)

func procStartIdentity(pid int, env Env) (string, string, ProcLiveness) {
	mib := [4]int32{1, 14, 1, int32(pid)} // CTL_KERN, KERN_PROC, KERN_PROC_PID
	buf := make([]byte, darwinKinfoProcSize)
	n := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL, uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0, 0)
	if errno != 0 {
		return "", "", ProcUnknown
	}
	if n == 0 {
		// No such process: the kernel returns no record.
		return "", "", ProcGone
	}
	if n != darwinKinfoProcSize || int32(binary.LittleEndian.Uint32(buf[darwinOffPid:])) != int32(pid) {
		return "", "", ProcUnknown
	}
	if buf[darwinOffStat] == darwinStatZombie {
		return "", "", ProcGone
	}
	sec := int64(binary.LittleEndian.Uint64(buf[0:8]))
	usec := int64(int32(binary.LittleEndian.Uint32(buf[8:12])))
	comm := buf[darwinOffComm : darwinOffComm+darwinCommLen]
	end := 0
	for end < len(comm) && comm[end] != 0 {
		end++
	}
	if sec <= 0 || end == 0 {
		return "", "", ProcUnknown
	}
	return "darwin:" + strconv.FormatInt(sec, 10) + "." + strconv.FormatInt(usec, 10), string(comm[:end]), ProcRunning
}
