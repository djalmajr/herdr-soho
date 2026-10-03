//go:build windows

package platform

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

const (
	windowsProcessQueryLimitedInformation = 0x1000
	windowsProcessTerminate               = 0x0001
	windowsTH32CS_SNAPPROCESS             = 0x00000002
	windowsErrorInsufficientBuffer        = 122
)

// windowsInvalidHandleValue is the handle Windows returns for a failed
// OpenProcess/CreateToolhelp32Snapshot; a successful handle is never zero
// or the invalid value.
const windowsInvalidHandleValue = ^uintptr(0)

// windowsFileTime is the FILETIME pair GetProcessTimes fills.
type windowsFileTime struct {
	Low  uint32
	High uint32
}

func (f windowsFileTime) ticks() int64 { return int64(f.High)<<32 | int64(f.Low) }

// procIdentity is one process's identity read by pid: its creation time
// in raw FILETIME ticks (100 ns, decimal; see windowsCreationIdentity)
// and whether the handle could be opened. The windows parentage comes
// from the Toolhelp32 snapshot, not from this read.
type procIdentity struct {
	started string
	running bool
}

// Test hooks; the zero values keep the production behavior.
// procStopAfterSnapshot runs between the process snapshot and the
// identity reads; procReadIdentity replaces the by-pid identity read
// (the collection pass and every pre-signal re-read); procSendTerm
// replaces the per-pid TERM (taskkill /PID, without /T).
var (
	procStopAfterSnapshot func(root int, env Env)
	procReadIdentity      = procReadIdentityReal
	procSendTerm          = windowsTaskkill
)

var (
	windowsKernel32 = syscall.NewLazyDLL("kernel32.dll")

	windowsProcOpenProcess               = windowsKernel32.NewProc("OpenProcess")
	windowsProcGetProcessTimes           = windowsKernel32.NewProc("GetProcessTimes")
	windowsProcQueryFullProcessImageName = windowsKernel32.NewProc("QueryFullProcessImageNameW")
	windowsProcCreateToolhelp32Snapshot  = windowsKernel32.NewProc("CreateToolhelp32Snapshot")
	windowsProcProcess32First            = windowsKernel32.NewProc("Process32FirstW")
	windowsProcProcess32Next             = windowsKernel32.NewProc("Process32NextW")
	windowsProcCloseHandle               = windowsKernel32.NewProc("CloseHandle")
	windowsProcTerminateProcess          = windowsKernel32.NewProc("TerminateProcess")
	windowsProcGetExitCodeProcess        = windowsKernel32.NewProc("GetExitCodeProcess")
)

// Every kernel32 call decides success by the return value, never by the
// error: syscall.LazyProc.Call always returns a non-nil error (the
// GetLastError snapshot), even on success. The error is read only when
// the return value indicates failure, as the cause.

// windowsOpenProcess opens the pid with the given access; a zero or
// INVALID_HANDLE_VALUE handle is the failure.
func windowsOpenProcess(access uint32, pid int) (uintptr, error) {
	handle, _, err := windowsProcOpenProcess.Call(uintptr(access), 0, uintptr(pid))
	if handle == 0 || handle == windowsInvalidHandleValue {
		return 0, err
	}
	return handle, nil
}

// windowsCreationTicks reads the creation time (FILETIME ticks of 100 ns)
// through the given handle; GetProcessTimes returns BOOL.
func windowsCreationTicks(handle uintptr) (int64, error) {
	var creation, exit, kernel, user windowsFileTime
	done, _, err := windowsProcGetProcessTimes.Call(handle,
		uintptr(unsafe.Pointer(&creation)), uintptr(unsafe.Pointer(&exit)),
		uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user)))
	if done == 0 {
		return 0, err
	}
	return creation.ticks(), nil
}

// windowsImageName reads the executable path with QueryFullProcessImageNameW:
// the fourth argument is a pointer to a uint32 size (in: capacity in
// wide characters, out: the characters written), and the buffer is
// decoded over the written size. On ERROR_INSUFFICIENT_BUFFER the buffer
// doubles until 32768 characters.
func windowsImageName(handle uintptr) (string, error) {
	size := uint32(1024)
	for {
		buf := make([]uint16, size)
		done, _, err := windowsProcQueryFullProcessImageName.Call(handle, 0,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)))
		if done == 0 {
			if errno, ok := err.(syscall.Errno); ok && errno == windowsErrorInsufficientBuffer {
				size *= 2
				if size > 32768 {
					return "", err
				}
				continue
			}
			return "", err
		}
		return syscall.UTF16ToString(buf[:size]), nil
	}
}

// windowsBaseName is the last segment of an executable path.
func windowsBaseName(value string) string {
	trimmed := strings.TrimRight(value, `\`)
	if i := strings.LastIndexAny(trimmed, `\/`); i >= 0 {
		return trimmed[i+1:]
	}
	return value
}

// ProcInfo reads the creation time and the image base name of one pid
// through kernel32 (OpenProcess with PROCESS_QUERY_LIMITED_INFORMATION,
// GetProcessTimes, QueryFullProcessImageNameW). The creation time is the
// raw FILETIME as decimal ticks of 100 ns: the full precision the API
// offers, so two reads of the same process compare equal and two distinct
// creations within one second do not. ok is false when the pid is not
// openable.
func ProcInfo(pid int, env Env) (string, string, bool) {
	handle, err := windowsOpenProcess(windowsProcessQueryLimitedInformation, pid)
	if err != nil {
		return "", "", false
	}
	defer windowsProcCloseHandle.Call(handle)
	if !windowsStillActive(handle) {
		return "", "", false
	}
	ticks, err := windowsCreationTicks(handle)
	if err != nil {
		return "", "", false
	}
	name, err := windowsImageName(handle)
	if err != nil {
		return "", "", false
	}
	return strconv.FormatInt(ticks, 10), windowsBaseName(name), true
}

// windowsParentSnapshot returns, from one Toolhelp32 process snapshot,
// the parent of every pid in the table (syscall.ProcessEntry32 is the
// PROCESSENTRY32W layout; only the pid and parent-pid fields are read —
// the image name field is never used). A zero or INVALID_HANDLE_VALUE
// snapshot handle and a BOOL 0 from Process32FirstW are failures.
func windowsParentSnapshot() (map[int]int, error) {
	snap, _, err := windowsProcCreateToolhelp32Snapshot.Call(windowsTH32CS_SNAPPROCESS, 0)
	if snap == 0 || snap == windowsInvalidHandleValue {
		return nil, err
	}
	defer windowsProcCloseHandle.Call(snap)
	out := map[int]int{}
	entry := syscall.ProcessEntry32{}
	entry.Size = uint32(unsafe.Sizeof(entry))
	done, _, firstErr := windowsProcProcess32First.Call(snap, uintptr(unsafe.Pointer(&entry)))
	if done == 0 {
		return nil, firstErr
	}
	for {
		out[int(entry.ProcessID)] = int(entry.ParentProcessID)
		done, _, err = windowsProcProcess32Next.Call(snap, uintptr(unsafe.Pointer(&entry)))
		if done == 0 {
			break
		}
	}
	return out, nil
}

// ParentPID reads the parent of this process from the process snapshot.
// -1 when the snapshot is unavailable or the entry was not found.
func ParentPID() int {
	parentOf, err := windowsParentSnapshot()
	if err != nil {
		return -1
	}
	parent, ok := parentOf[syscall.Getpid()]
	if !ok {
		return -1
	}
	return parent
}

// procDescendants lists the descendants of pid (every generation) from a
// Toolhelp32 process snapshot.
func procDescendants(pid int, env Env) ([]int, error) {
	parentOf, err := windowsParentSnapshot()
	if err != nil {
		return nil, err
	}
	children := map[int][]int{}
	for child, parent := range parentOf {
		children[parent] = append(children[parent], child)
	}
	out := []int{}
	seen := map[int]bool{pid: true}
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range children[cur] {
			if seen[child] {
				continue
			}
			seen[child] = true
			out = append(out, child)
			queue = append(queue, child)
		}
	}
	return out, nil
}

// procReadIdentityReal reads one pid's creation time with a
// limited-information handle; the read succeeds only when the handle
// opens (a stopped process's entry is gone).
func procReadIdentityReal(pid int, env Env) (procIdentity, bool) {
	handle, err := windowsOpenProcess(windowsProcessQueryLimitedInformation, pid)
	if err != nil {
		return procIdentity{}, false
	}
	defer windowsProcCloseHandle.Call(handle)
	ticks, err := windowsCreationTicks(handle)
	if err != nil {
		return procIdentity{}, false
	}
	return procIdentity{started: strconv.FormatInt(ticks, 10), running: windowsStillActive(handle)}, true
}

// windowsStillActiveCode is GetExitCodeProcess's STILL_ACTIVE (259).
const windowsStillActiveCode = 259

// windowsStillActive reports whether the process behind handle is still
// running. An exited process stays openable while any handle to it is
// open (its parent's, for one), so OpenProcess alone does not prove it
// runs: only the STILL_ACTIVE exit code does. A failed query reads as
// not running.
func windowsStillActive(handle uintptr) bool {
	var code uint32
	ok, _, _ := windowsProcGetExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&code)))
	return ok != 0 && code == windowsStillActiveCode
}

// procExists probes the pid with a limited-information handle and its
// exit code.
func procExists(pid int) bool {
	handle, err := windowsOpenProcess(windowsProcessQueryLimitedInformation, pid)
	if err != nil {
		return false
	}
	defer windowsProcCloseHandle.Call(handle)
	return windowsStillActive(handle)
}

// procAliveStates reports which of the pids is still openable: one
// limited-information probe per pid (a stopped process's entry closes
// when its parent reaps it).
func procAliveStates(pids []int, env Env) map[int]bool {
	out := map[int]bool{}
	for _, p := range pids {
		out[p] = procExists(p)
	}
	return out
}

// StopProcessTree stops the windows process tree rooted at pid in three
// phases, mirroring the unix helper.
//
// Collection: a Toolhelp32 pid/ppid snapshot, then — for every
// candidate, by pid — the creation time (OpenProcess and
// GetProcessTimes). Only the root verified by its creation time and the
// pids still children of an already-verified pid are kept; a pid that
// reparented or vanished leaves the list and is never signalled.
//
// TERM: from the deepest descendant to the root, the root last, a
// taskkill /PID <pid> (without /T, so only the verified pid is reached)
// after the pre-signal re-read of the creation time.
//
// KILL: after up to 3 s, the survivors only, through
// OpenProcess(PROCESS_TERMINATE|PROCESS_QUERY_LIMITED_INFORMATION),
// re-reading the creation time through the same handle and calling
// TerminateProcess only when it still matches.
func StopProcessTree(pid int, env Env) error {
	parentOf, err := windowsParentSnapshot()
	if err != nil {
		return err
	}
	if procStopAfterSnapshot != nil {
		procStopAfterSnapshot(pid, env)
	}
	members, depths := windowsTreeMembers(pid, parentOf)
	captured := map[int]string{}
	verified := map[int]bool{}
	for _, member := range members { // root first, shallow to deep
		id, ok := procReadIdentity(member, env)
		if !ok || !id.running {
			continue
		}
		if member != pid {
			parent := parentOf[member]
			if !verified[parent] {
				continue
			}
		}
		captured[member] = id.started
		verified[member] = true
	}
	targets := windowsVerifiedTargets(members, depths, verified)
	if len(targets) == 0 {
		return nil
	}
	for _, target := range targets {
		id, ok := procReadIdentity(target, env)
		if !ok || !id.running || id.started != captured[target] {
			continue
		}
		if err := procSendTerm(target, env); err != nil {
			// The final liveness check decides; the KILL round escalates.
		}
	}
	if windowsWaitGone(targets, 3*time.Second) {
		return nil
	}
	for _, target := range windowsStillRunning(targets) {
		id, ok := procReadIdentity(target, env)
		if !ok || !id.running || id.started != captured[target] {
			continue
		}
		if err := windowsTerminate(target, captured[target]); err != nil {
			// The final liveness check decides.
		}
	}
	if windowsWaitGone(targets, time.Second) {
		return nil
	}
	return fmt.Errorf("the tree of %d survived TerminateProcess (still running: %s)", pid, strings.Join(windowsInts(windowsStillRunning(targets)), ", "))
}

// windowsTreeMembers returns the root and its snapshot descendants (BFS
// order, root first) with each member's depth in the tree.
func windowsTreeMembers(pid int, parentOf map[int]int) ([]int, map[int]int) {
	children := map[int][]int{}
	for child, parent := range parentOf {
		children[parent] = append(children[parent], child)
	}
	members := []int{pid}
	depths := map[int]int{pid: 0}
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range children[cur] {
			if _, seen := depths[child]; seen {
				continue
			}
			depths[child] = depths[cur] + 1
			members = append(members, child)
			queue = append(queue, child)
		}
	}
	return members, depths
}

// windowsVerifiedTargets orders the verified members deepest first (the
// root last), pid ascending within a depth.
func windowsVerifiedTargets(members []int, depths map[int]int, verified map[int]bool) []int {
	out := []int{}
	for _, member := range members {
		if verified[member] {
			out = append(out, member)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if depths[out[i]] != depths[out[j]] {
			return depths[out[i]] > depths[out[j]]
		}
		return out[i] < out[j]
	})
	return out
}

// windowsStillRunning lists the pids still openable.
func windowsStillRunning(pids []int) []int {
	out := []int{}
	for _, pid := range pids {
		if procExists(pid) {
			out = append(out, pid)
		}
	}
	return out
}

// windowsTerminate opens the pid with PROCESS_TERMINATE (plus
// PROCESS_QUERY_LIMITED_INFORMATION), re-reads the creation time through
// the same handle and calls TerminateProcess only when it still matches
// the captured value; TerminateProcess returns BOOL.
func windowsTerminate(pid int, captured string) error {
	handle, err := windowsOpenProcess(windowsProcessTerminate|windowsProcessQueryLimitedInformation, pid)
	if err != nil {
		return err
	}
	defer windowsProcCloseHandle.Call(handle)
	ticks, err := windowsCreationTicks(handle)
	if err != nil {
		return err
	}
	if strconv.FormatInt(ticks, 10) != captured {
		return fmt.Errorf("process %d changed its identity before the terminate", pid)
	}
	done, _, termErr := windowsProcTerminateProcess.Call(handle, 0)
	if done == 0 {
		return termErr
	}
	return nil
}

func windowsInts(values []int) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strconv.Itoa(v)
	}
	return out
}

// windowsTaskkill sends the per-pid TERM: taskkill /PID <pid>, without
// /T, so only the pid verified by the pre-signal re-read is reached.
func windowsTaskkill(pid int, env Env) error {
	r := RunCli("taskkill", []string{"/PID", strconv.Itoa(pid)}, RunOptions{Env: env, TimeoutMs: 10_000})
	if r.NotFound || r.TimedOut || r.Error != "" || r.Status == nil || *r.Status != 0 {
		return fmt.Errorf("taskkill /PID %d failed", pid)
	}
	return nil
}

// windowsWaitGone polls every 100 ms until none of the pids is openable
// or the ceiling is reached.
func windowsWaitGone(pids []int, ceiling time.Duration) bool {
	deadline := time.Now().Add(ceiling)
	for {
		left := false
		for _, p := range pids {
			if procExists(p) {
				left = true
				break
			}
		}
		if !left {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
}
