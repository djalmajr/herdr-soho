//go:build !windows

package core

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// The two review probes of review-2-20261003T173031 (findings 1 and 2),
// now durable: fake ps answers, real sleep processes owned by the test
// (killed in the cleanup), StopProcRow as the entry point. The fake ps
// scripts sit first on PATH; ps/sleep/sh come from the system
// directories.

// TestReview92RootStartChangedDuringCollection: the consumer's registered
// identity read (lstart=,state=,comm=) gives the registered start, but
// the collection read (lstart=,ppid=,state=) gives another start. The
// root must not be signalled, StopProcRow must keep the line, and the
// stop must not be claimed.
func TestReview92RootStartChangedDuringCollection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix ps-based fixture; the windows round exercises the same contract")
	}
	cmd := exec.Command("/bin/sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	pid := cmd.Process.Pid

	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-A" ]; then printf '%d %d\n'; exit 0; fi
case "$2" in
 lstart=,state=,comm=) printf 'Sat Oct  3 00:00:00 2026 S sleep\n';;
 lstart=,ppid=,state=) if kill -0 %d 2>/dev/null; then printf 'Sat Oct  3 00:00:01 2026 %d S\n';fi;;
 pid=,state=) if kill -0 %d 2>/dev/null; then printf '%d S\n';fi;;
esac
`, pid, os.Getpid(), pid, os.Getpid(), pid, pid)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"PATH": dir + ":/usr/bin:/bin"}
	row := ProcRow{Pid: pid, Started: "Sat Oct  3 00:00:00 2026", Name: "sleep", Owner: "ghost"}
	if err := WriteProcs(dir, []ProcRow{row}); err != nil {
		t.Fatal(err)
	}
	result := StopProcRow(dir, row, env)
	t.Logf("owned pid=%d result=%+v", pid, result)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil || strings.Contains(result.Line, "stopped process") {
		t.Fatal("root was stopped despite changed start between registered identity check and collection")
	}
	if result.Dropped {
		t.Fatalf("StopProcRow = %+v; the line must stay on a divergent root", result)
	}
	if result.Err == nil || !strings.Contains(result.Err.Error(), fmt.Sprintf("process %d changed after the check", pid)) {
		t.Fatalf("StopProcRow = %+v; want the changed-after-the-check error", result)
	}
	rows, err := ReadProcs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Pid != pid {
		t.Fatalf("registry rows = %v; the line must stay registered", rows)
	}
}

// TestReview92UnreadableIdentityKeepsRegistration: a valid snapshot and a
// failed identity read (ps exits non-zero; the process is alive, so the
// kill(pid, 0) confirmation is not ESRCH). The read failure is not an
// absence: StopProcRow must report the error and keep the line, no
// signal, no drop.
func TestReview92UnreadableIdentityKeepsRegistration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the unix ps-based fixture; the windows round exercises the same contract")
	}
	cmd := exec.Command("/bin/sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	pid := cmd.Process.Pid

	dir := t.TempDir()
	script := fmt.Sprintf(`#!/bin/sh
if [ "$1" = "-A" ]; then printf '%d %d\n'; exit 0; fi
case "$2" in
 lstart=,state=,comm=) printf 'Sat Oct  3 00:00:00 2026 S sleep\n';;
 lstart=,ppid=,state=) echo 'identity query failed' >&2; : %d %d; exit 1;;
 pid=,state=) if kill -0 %d 2>/dev/null; then printf '%d S\n';fi;;
esac
`, pid, os.Getpid(), pid, os.Getpid(), pid, pid)
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"PATH": dir + ":/usr/bin:/bin"}
	row := ProcRow{Pid: pid, Started: "Sat Oct  3 00:00:00 2026", Name: "sleep", Owner: "ghost"}
	if err := WriteProcs(dir, []ProcRow{row}); err != nil {
		t.Fatal(err)
	}
	result := StopProcRow(dir, row, env)
	t.Logf("owned pid=%d result=%+v", pid, result)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("fixture not alive")
	}
	if result.Err == nil || result.Dropped {
		t.Fatal("unreadable root identity reported stopped and dropped while owned process is alive")
	}
	if !strings.Contains(result.Err.Error(), fmt.Sprintf("process %d is unreadable; not stopped", pid)) {
		t.Fatalf("StopProcRow = %+v; want the unreadable error", result)
	}
	rows, err := ReadProcs(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Pid != pid {
		t.Fatalf("registry rows = %v; the line must stay registered", rows)
	}
}

// TestProcRowStateUnknownWhenTheReadFails: a ps that fails every query
// (the process is alive) reads as unknown, never as gone — a failed read
// is not an absence.
func TestProcRowStateUnknownWhenTheReadFails(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	pid := cmd.Process.Pid

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ps"), []byte("#!/bin/sh\nexit 9\n"), 0700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{"PATH": dir + ":/usr/bin:/bin"}
	row := ProcRow{Pid: pid, Started: "Sat Oct  3 00:00:00 2026", Name: "sleep", Owner: "ghost"}
	if st := ProcRowState(row, env); st != "unknown" {
		t.Fatalf("ProcRowState = %q; want unknown (a failed read is not an absence)", st)
	}
	// The proven absence reads as gone: a dead, reaped pid.
	dead := exec.Command("/bin/sleep", "0.1")
	if err := dead.Start(); err != nil {
		t.Fatal(err)
	}
	deadPid := dead.Process.Pid
	if err := dead.Wait(); err != nil {
		t.Fatal(err)
	}
	deadRow := ProcRow{Pid: deadPid, Started: "Sat Oct  3 00:00:00 2026", Name: "sleep", Owner: "ghost"}
	envDead := platform.Env{"PATH": "/usr/bin:/bin"}
	if st := ProcRowState(deadRow, envDead); st != "gone" {
		t.Fatalf("ProcRowState(dead) = %q; want gone", st)
	}
}
