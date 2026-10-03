//go:build windows

package platform

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const windowsProcessQueryLimitedInformation = 0x1000

const windowsTH32CS_SNAPPROCESS = 0x00000002

// windowsFileTime is the FILETIME pair GetProcessTimes fills.
type windowsFileTime struct {
	Low  uint32
	High uint32
}

func (f windowsFileTime) ticks() int64 { return int64(f.High)<<32 | int64(f.Low) }

// windowsProcEntry32W is the PROCESSENTRY32W of the process snapshot used
// only to read the parent pid of one process (the image name field is
// never touched).
type windowsProcEntry32W struct {
	cbSize              uint32
	SizeOfMem           uint32
	UsageFlags          uint32
	th32ProcessID       uint32
	th32DefaultHeapID   uintptr
	th32ModuleID        uint32
	th32NumThreads      uint32
	th32ParentProcessID uint32
	PCPri               int32
	BasePriority        uint32
	DwFlags             uint32
	szExeFile           [260]uint16
}

var (
	windowsKernel32 = syscall.NewLazyDLL("kernel32.dll")

	windowsProcOpenProcess               = windowsKernel32.NewProc("OpenProcess")
	windowsProcGetProcessTimes           = windowsKernel32.NewProc("GetProcessTimes")
	windowsProcQueryFullProcessImageName = windowsKernel32.NewProc("QueryFullProcessImageNameW")
	windowsProcCreateToolhelp32Snapshot  = windowsKernel32.NewProc("CreateToolhelp32Snapshot")
	windowsProcProcess32First            = windowsKernel32.NewProc("Process32FirstW")
	windowsProcProcess32Next             = windowsKernel32.NewProc("Process32NextW")
	windowsProcCloseHandle               = windowsKernel32.NewProc("CloseHandle")
)

// ProcInfo reads the creation time and the image base name of one pid
// through kernel32 (OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION,
// GetProcessTimes, QueryFullProcessImageNameW). The creation time is
// stored as ISO-8601 UTC, so two reads of the same process compare equal.
// ok is false when the pid is not openable.
func ProcInfo(pid int, env Env) (string, string, bool) {
	handle, _, _ := windowsProcOpenProcess.Call(windowsProcessQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 {
		return "", "", false
	}
	defer windowsProcCloseHandle.Call(handle)
	var creation, exit, kernel, user windowsFileTime
	if _, _, err2 := windowsProcGetProcessTimes.Call(handle,
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); err2 != nil {
		return "", "", false
	}
	name, ok := windowsProcImageName(handle)
	if !ok {
		return "", "", false
	}
	return windowsCreationTime(creation), windowsBaseName(name), true
}

// windowsCreationTime renders a FILETIME creation time as ISO-8601 UTC
// (second precision, stable for the life of the process).
func windowsCreationTime(creation windowsFileTime) string {
	// FILETIME ticks are 100 ns since 1601-01-01; the Unix epoch is
	// 11644473600 s later.
	const epochDeltaTicks = 116444736000000000
	nanos := (creation.ticks() - epochDeltaTicks) * 100
	if nanos < 0 {
		nanos = 0
	}
	return time.Unix(0, nanos).UTC().Format("2006-01-02T15:04:05Z")
}

func windowsProcImageName(handle uintptr) (string, bool) {
	size := uint32(1024)
	for {
		buf := make([]uint16, size)
		written, _, err := windowsProcQueryFullProcessImageName.Call(handle, 0,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(size))
		if err != nil {
			return "", false
		}
		if written == 0 {
			return "", false
		}
		if uint32(written) < size {
			return syscall.UTF16ToString(buf[:written]), true
		}
		size *= 2
		if size > 1<<16 {
			return "", false
		}
	}
}

func windowsBaseName(value string) string {
	trimmed := strings.TrimRight(value, `\`)
	if i := strings.LastIndexAny(trimmed, `\/`); i >= 0 {
		return trimmed[i+1:]
	}
	return value
}

// ParentPID reads the parent of this process from the process snapshot
// (only the pid and parent-pid fields are read; the image name field is
// never used). -1 when the snapshot is unavailable.
func ParentPID() int {
	snap, _, err := windowsProcCreateToolhelp32Snapshot.Call(windowsTH32CS_SNAPPROCESS, 0)
	if snap == 0 || err != nil {
		return -1
	}
	defer windowsProcCloseHandle.Call(snap)
	entry := windowsProcEntry32W{}
	entry.cbSize = uint32(unsafe.Sizeof(entry))
	first, _, _ := windowsProcProcess32First.Call(snap, uintptr(unsafe.Pointer(&entry)))
	for first == 1 {
		if int(entry.th32ProcessID) == syscall.Getpid() {
			return int(entry.th32ParentProcessID)
		}
		first, _, _ = windowsProcProcess32Next.Call(snap, uintptr(unsafe.Pointer(&entry)))
	}
	return -1
}

// procExists probes the pid with a limited-information handle.
func procExists(pid int) bool {
	handle, _, err := windowsProcOpenProcess.Call(windowsProcessQueryLimitedInformation, 0, uintptr(pid))
	if handle == 0 || err != nil {
		return false
	}
	windowsProcCloseHandle.Call(handle)
	return true
}

// procAliveStates reports which of the pids is still running: one
// limited-information probe per pid (windows has no process table listing
// in this path; a stopped process's entry closes when its parent reaps
// it).
func procAliveStates(pids []int, env Env) map[int]bool {
	out := map[int]bool{}
	for _, p := range pids {
		out[p] = procExists(p)
	}
	return out
}

// procDescendants is unix-only (the windows stop is taskkill /T, which
// walks the tree in the OS); the test that names it skips on windows.
func procDescendants(pid int, env Env) ([]int, error) {
	return nil, errors.New("descendant collection is unix-only; windows stops with taskkill /T")
}

// StopProcessTree stops the windows process tree rooted at pid:
// `taskkill /PID <pid> /T`; after 3 s, when the pid is still there, the
// forceful `taskkill /PID <pid> /T /F`. An error is returned when the pid
// survives the forced kill.
func StopProcessTree(pid int, env Env) error {
	windowsTaskkill(pid, false, env)
	if windowsWaitGone(pid, 3*time.Second) {
		return nil
	}
	windowsTaskkill(pid, true, env)
	if windowsWaitGone(pid, time.Second) {
		return nil
	}
	return fmt.Errorf("process %d survived taskkill /F", pid)
}

func windowsTaskkill(pid int, force bool, env Env) {
	args := []string{"/PID", strconv.Itoa(pid), "/T"}
	if force {
		args = append(args, "/F")
	}
	_ = RunCli("taskkill", args, RunOptions{Env: env, TimeoutMs: 10_000})
}

func windowsWaitGone(pid int, ceiling time.Duration) bool {
	deadline := time.Now().Add(ceiling)
	for {
		if !procExists(pid) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
