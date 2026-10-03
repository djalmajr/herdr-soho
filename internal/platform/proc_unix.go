//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// procStartRe splits one `ps -o lstart=,state=,comm=` line: the raw lstart
// field (day space-padded, LC_ALL=C abbreviations), the single-letter
// state and the rest of the line, the comm. The lstart is stored exactly
// as the system prints it, so two reads of the same process compare
// equal.
var procStartRe = regexp.MustCompile(`^([A-Z][a-z]{2} [A-Z][a-z]{2}  ?\d{1,2} \d{2}:\d{2}:\d{2} \d{4})\s+([A-Za-z])\s+(.+)$`)

// procLocale returns env with LC_ALL=C, so ps prints the date fields in
// the fixed English abbreviated form the parser expects.
func procLocale(env Env) Env {
	out := env.Clone()
	out["LC_ALL"] = "C"
	return out
}

// ProcInfo reads the start time and the command base name of one pid with
// `ps -o lstart=,state=,comm= -p <pid>` (LC_ALL=C, a 5 s ceiling): only
// that pid, never a command-line listing (it may carry credentials).
// started is the start time exactly as the system prints it, name the base
// name of the command. ok is false when the pid is not running — a zombie
// (a dead process left unreaped by its parent) is not running —, when ps
// is missing or times out, or when the line does not parse.
func ProcInfo(pid int, env Env) (string, string, bool) {
	r := RunCli("ps", []string{"-o", "lstart=,state=,comm=", "-p", strconv.Itoa(pid)}, RunOptions{Env: procLocale(env), TimeoutMs: 5000})
	if r.NotFound || r.TimedOut || r.Error != "" || r.Status == nil || *r.Status != 0 {
		return "", "", false
	}
	m := procStartRe.FindStringSubmatch(strings.TrimSpace(r.Stdout))
	if m == nil || m[2] == "Z" {
		return "", "", false
	}
	return m[1], basePath(m[3]), true
}

// basePath is filepath.Base without importing path/filepath in the hot
// error paths: the comm is a path on some systems, and only its last
// segment is the name.
func basePath(value string) string {
	trimmed := strings.TrimRight(value, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}

// ParentPID is the pid of the process that invoked this herdr-soho (the
// caller's shell), for the procs add refusal.
func ParentPID() int { return os.Getppid() }

// procIdentity is one process's identity read by pid: its start time
// (empty when the table does not print one), the current parent pid when
// the line carries it, and whether it is running (a zombie is not).
type procIdentity struct {
	started   string
	ppid      int
	ppidKnown bool
	running   bool
}

// Test hooks; the zero values keep the production behavior.
// procStopAfterSnapshot runs between the pid/ppid snapshot and the
// identity reads; procReadIdentity replaces the by-pid identity read
// (the collection pass and every pre-signal re-read); procSendTerm
// replaces the TERM send.
var (
	procStopAfterSnapshot func(root int, env Env)
	procReadIdentity      = procReadIdentityReal
	procSendTerm          = func(pid int, env Env) error { return syscall.Kill(pid, syscall.SIGTERM) }
)

// procReadIdentityReal reads one pid's start time, current parent pid and
// state with `ps -o lstart=,ppid=,state= -p <pid>` (LC_ALL=C, a 5 s
// ceiling): only those fields, never a command line. The lstart is
// re-joined with single spaces, a normalization that is stable for the
// life of the process, so two reads of the same process compare equal.
// ok is false when the pid is not in the table or the line does not parse;
// a pid whose line does not print a start time or a parent (a reduced
// table) reads both as unknown and keeps only the state.
func procReadIdentityReal(pid int, env Env) (procIdentity, bool) {
	r := RunCli("ps", []string{"-o", "lstart=,ppid=,state=", "-p", strconv.Itoa(pid)}, RunOptions{Env: procLocale(env), TimeoutMs: 5000})
	if r.NotFound || r.TimedOut || r.Error != "" {
		return procIdentity{}, false
	}
	for _, line := range strings.Split(strings.TrimSpace(r.Stdout), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		state := fields[len(fields)-1]
		if len(fields) == 2 && isAllDigit(fields[0]) {
			// The line carries a pid and a state: it counts only when it
			// is the queried pid's own line (a table row of another pid is
			// not this pid's answer).
			if linePid, err := strconv.Atoi(fields[0]); err != nil || linePid != pid {
				continue
			}
			return procIdentity{running: state != "Z"}, true
		}
		ppid, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil {
			continue
		}
		return procIdentity{
			started:   strings.Join(fields[:len(fields)-2], " "),
			ppid:      ppid,
			ppidKnown: true,
			running:   state != "Z",
		}, true
	}
	return procIdentity{}, false
}

func isAllDigit(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// procParentSnapshot returns, from one `ps -A -o pid=,ppid=` call, the
// parent of every pid in the table: only the pid/ppid pair, never command
// lines. A ps failure is an error, not an empty table.
func procParentSnapshot(env Env) (map[int]int, error) {
	r := RunCli("ps", []string{"-A", "-o", "pid=,ppid="}, RunOptions{Env: procLocale(env), TimeoutMs: 5000})
	if r.NotFound || r.TimedOut || r.Error != "" || r.Status == nil || *r.Status != 0 {
		return nil, fmt.Errorf("ps -A failed (exit %s)", statusText(r))
	}
	out := map[int]int{}
	for _, line := range strings.Split(r.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		child, err1 := strconv.Atoi(fields[0])
		parent, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil {
			continue
		}
		out[child] = parent
	}
	return out, nil
}

// procDescendants lists the descendants of pid (every generation) from a
// pid/ppid snapshot.
func procDescendants(pid int, env Env) ([]int, error) {
	parentOf, err := procParentSnapshot(env)
	if err != nil {
		return nil, err
	}
	children := procChildren(parentOf)
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

func procChildren(parentOf map[int]int) map[int][]int {
	children := map[int][]int{}
	for child, parent := range parentOf {
		children[parent] = append(children[parent], child)
	}
	return children
}

func statusText(r RunResult) string {
	if r.Status == nil {
		return "none"
	}
	return strconv.Itoa(*r.Status)
}

// StopProcessTree stops the unix process tree rooted at pid in three
// phases.
//
// Collection: a pid/ppid snapshot, then — for every candidate, by pid —
// the start time and the current parent. Only the root verified by its
// start time and the pids still children of an already-verified pid are
// kept; a pid that reparented or vanished leaves the list and is never
// signalled.
//
// TERM: from the deepest descendant to the root, the root last.
// Immediately before each signal the pid's start time is read again and
// the signal goes only when it still matches the collected one.
//
// KILL: after up to 3 s, the survivors only, with the same re-read before
// each signal. A pid that lost its parent because the parent got the TERM
// is still the same process by its start time and is escalated.
//
// An error is returned when the snapshot fails or anything of the tree
// survives the kill. The unix lstart has second precision: in practice a
// pid is not recycled within the same second, and the re-read before each
// signal shrinks the window a recycled pid could hide behind.
func StopProcessTree(pid int, env Env) error {
	parentOf, err := procParentSnapshot(env)
	if err != nil {
		return err
	}
	if procStopAfterSnapshot != nil {
		procStopAfterSnapshot(pid, env)
	}
	members, depths := procTreeMembers(pid, parentOf)
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
			if id.ppidKnown && id.ppid != parent {
				continue // reparented since the snapshot
			}
		}
		captured[member] = id.started
		verified[member] = true
	}
	targets := procVerifiedTargets(members, depths, verified)
	if len(targets) == 0 {
		return nil
	}
	for _, target := range targets {
		id, ok := procReadIdentity(target, env)
		if !ok || !id.running || id.started != captured[target] {
			continue
		}
		if err := procSendTerm(target, env); err != nil && !errors.Is(err, syscall.ESRCH) {
			// The final liveness check decides; the KILL round escalates.
		}
	}
	if waitTreeGone(targets, 3*time.Second, env) {
		return nil
	}
	for _, target := range procStillRunning(targets, env) {
		id, ok := procReadIdentity(target, env)
		if !ok || !id.running || id.started != captured[target] {
			continue
		}
		_ = syscall.Kill(target, syscall.SIGKILL)
	}
	if waitTreeGone(targets, time.Second, env) {
		return nil
	}
	return fmt.Errorf("the tree of %d survived SIGKILL (still running: %s)", pid, strings.Join(mapInts(procStillRunning(targets, env)), ", "))
}

// procTreeMembers returns the root and its snapshot descendants (BFS
// order, root first) with each member's depth in the tree.
func procTreeMembers(pid int, parentOf map[int]int) ([]int, map[int]int) {
	children := procChildren(parentOf)
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

// procVerifiedTargets orders the verified members deepest first (the root
// last), pid ascending within a depth.
func procVerifiedTargets(members []int, depths map[int]int, verified map[int]bool) []int {
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

// procStillRunning lists, one by-pid read at a time, the pids still
// running (a zombie is not running).
func procStillRunning(pids []int, env Env) []int {
	out := []int{}
	for _, pid := range pids {
		if id, ok := procReadIdentity(pid, env); ok && id.running {
			out = append(out, pid)
		}
	}
	return out
}

// procAliveStates reports, from one `ps -o pid=,state=` call, which of the
// pids is still running; a zombie (a dead process left unreaped) is not
// running. ps exits non-zero when some of the pids are not there, but the
// printed lines are still the answer: an unprinted pid is gone. When ps
// itself fails (missing, timeout), every pid reads as alive: a stop is
// never claimed on a doubt.
func procAliveStates(pids []int, env Env) map[int]bool {
	out := map[int]bool{}
	if len(pids) == 0 {
		return out
	}
	r := RunCli("ps", []string{"-o", "pid=,state=", "-p", strings.Join(mapInts(pids), ",")}, RunOptions{Env: procLocale(env), TimeoutMs: 2000})
	if r.NotFound || r.TimedOut || r.Error != "" {
		for _, p := range pids {
			out[p] = true
		}
		return out
	}
	for _, line := range strings.Split(r.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		out[pid] = fields[1] != "Z"
	}
	return out
}

// waitTreeGone polls every 100 ms until none of the pids is running (a
// zombie is not running) or the ceiling is reached.
func waitTreeGone(targets []int, ceiling time.Duration, env Env) bool {
	deadline := time.Now().Add(ceiling)
	for {
		living := procAliveStates(targets, env)
		left := false
		for _, t := range targets {
			if living[t] {
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

func mapInts(values []int) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = strconv.Itoa(v)
	}
	return out
}
