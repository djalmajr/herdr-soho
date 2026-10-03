//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"regexp"
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

// procDescendants lists the descendants of pid (every generation) from a
// `ps -A -o pid=,ppid=` snapshot: only the pid/ppid pair, never command
// lines. A ps failure is an error, not an empty tree.
func procDescendants(pid int, env Env) ([]int, error) {
	r := RunCli("ps", []string{"-A", "-o", "pid=,ppid="}, RunOptions{Env: procLocale(env), TimeoutMs: 5000})
	if r.NotFound || r.TimedOut || r.Error != "" || r.Status == nil || *r.Status != 0 {
		return nil, fmt.Errorf("ps -A failed (exit %s)", statusText(r))
	}
	byParent := map[int][]int{}
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
		byParent[parent] = append(byParent[parent], child)
	}
	out := []int{}
	seen := map[int]bool{pid: true}
	queue := []int{pid}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, child := range byParent[cur] {
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

func statusText(r RunResult) string {
	if r.Status == nil {
		return "none"
	}
	return strconv.Itoa(*r.Status)
}

// StopProcessTree stops the unix process tree rooted at pid: the
// descendants are SIGTERMed first, then the pid; after up to 3 s the
// survivors are SIGKILLed. An error is returned when the tree listing
// fails or anything of the tree survives the kill.
func StopProcessTree(pid int, env Env) error {
	descendants, err := procDescendants(pid, env)
	if err != nil {
		return err
	}
	targets := append(descendants, pid)
	for _, t := range targets {
		if killErr := syscall.Kill(t, syscall.SIGTERM); killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
			if t == pid {
				return fmt.Errorf("SIGTERM to %d failed: %v", t, killErr)
			}
		}
	}
	if waitTreeGone(targets, 3*time.Second, env) {
		return nil
	}
	for _, t := range targets {
		_ = syscall.Kill(t, syscall.SIGKILL)
	}
	if waitTreeGone(targets, time.Second, env) {
		return nil
	}
	alive := procAliveStates(targets, env)
	left := []int{}
	for _, t := range targets {
		if alive[t] {
			left = append(left, t)
		}
	}
	return fmt.Errorf("the tree of %d survived SIGKILL (still running: %s)", pid, strings.Join(mapInts(left), ", "))
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
