//go:build !windows

package eval

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Bounded owned-group verification.
//
// cmd.Wait reaps only the direct child of the owned run: a grandchild (the
// go toolchain's compile, a worker child) can still be inside the kernel's
// exit path when the runner returns — on macOS it reads a `?` (unknown
// Mach) ps state while it finishes. verifyOwnedGroupGone runs between the
// owned runner's final group SIGKILL and its return and proves, within a
// short budget, that the exact owned process group holds no live member:
// a reaped/missing group (the kernel's ESRCH on the group probe) and a
// proven zombie-only group complete; a live, exiting, unreadable or
// unverified member keeps the group pending until the budget, after which
// the runner returns an explicit error and does not claim cleanup. No
// fixed sleep is used as proof: every pass is a real kernel probe or a
// real ps snapshot. The verification never signals the group (the group
// SIGKILL landed before it starts); the snapshot reads a readonly
// whole-system PID/PGID/state table and classifies only the owned group's
// rows — no argv, no environment, and nothing outside the owned group is
// ever signalled.
const (
	// ownedGroupVerifyCeiling is the total budget for one verification:
	// short enough that a stuck group surfaces as a bounded explicit error
	// instead of stalling the caller, long enough that a normal post-
	// SIGKILL teardown (milliseconds to a second) completes inside it.
	ownedGroupVerifyCeiling = 2 * time.Second
	// ownedGroupVerifyReadCeil caps one ps snapshot read; a read starts
	// only with the time left in the ceiling, so the per-read deadline
	// never exceeds the overall budget.
	ownedGroupVerifyReadCeil = 300 * time.Millisecond
	// ownedGroupVerifyPoll spaces the reads: a poll interval, not a proof —
	// every pass re-proves the group against the kernel or a snapshot.
	ownedGroupVerifyPoll = 50 * time.Millisecond
	// ownedGroupWaitDelay bounds the owned ps read's pipe-release wait after
	// the read's own ps process has exited or been killed on the read
	// deadline: ps is a leaf process (no grandchildren holding the pipe
	// ends), so the kernel closes them at the kill and this delay is the
	// documented worst-case slack of one read, not a grace period. A read
	// can outlive its deadline by at most the kill latency plus this delay.
	ownedGroupWaitDelay = 200 * time.Millisecond
)

// Test hooks; the zero values keep the production behavior.
var (
	// ownedGroupEmptyByKernel probes the exact owned group with kill(-pgid,
	// 0) and reports true only on ESRCH, the kernel's proven-empty answer.
	// A nil result (a member exists) and EPERM (a zombie member, on
	// macOS) both report not-empty: neither proves the group empty.
	ownedGroupEmptyByKernel = func(pgid int) bool {
		return errors.Is(syscall.Kill(-pgid, 0), syscall.ESRCH)
	}
	// ownedGroupMembersGone snapshots the owned group (PID/PGID/state
	// only) and reports true only when every member proves its exit.
	ownedGroupMembersGone = func(pgid int, readCeil time.Duration) bool {
		return ownedGroupSnapshotGone(pgid, readCeil)
	}
)

// verifyOwnedGroupGone proves the owned group (pgid, the PID of this run's
// Setpgid child — the group leader, so pgid == pid) holds no live member,
// or fails with an explicit, non-claiming error at the budget. Once a pass
// proves the group empty the verification returns at once: it never sends
// another group signal after the proof.
func verifyOwnedGroupGone(pgid int) error {
	deadline := time.Now().Add(ownedGroupVerifyCeiling)
	for {
		if ownedGroupEmptyByKernel(pgid) {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return ownedGroupVerifyError(pgid)
		}
		readCeil := ownedGroupVerifyReadCeil
		if remaining < readCeil {
			readCeil = remaining
		}
		if ownedGroupMembersGone(pgid, readCeil) {
			return nil
		}
		if time.Now().After(deadline) {
			return ownedGroupVerifyError(pgid)
		}
		time.Sleep(ownedGroupVerifyPoll)
	}
}

// ownedGroupSnapshotGone reads one bounded native ps snapshot of the
// process table with PID/PGID/state only (a readonly whole-system table —
// argv and environment are never read; only the owned group's rows are
// classified, and nothing outside the owned group is signalled) and
// reports true only when the snapshot proves every member of the owned
// group exited: a zombie base state is a dead member, a known live base
// state keeps the group pending, and any other state — including `?`, the
// macOS in-exit unknown — is confirmed against the kernel, where only
// ESRCH proves the member is gone. A missing, failed, timed-out, empty or
// malformed snapshot proves nothing: it reports pending. A table without a
// row for the owned group does not claim the group empty either — the next
// pass's kernel probe settles it (or the budget ends with the explicit
// error).
//
// The read is genuinely bounded by its own deadline: the deadline is the
// read slice of the overall verification ceiling that the caller passes
// (min of the time left and the read ceiling), and when it fires the
// context kills the ps process this read started (the exec default cancel
// is a SIGKILL) and WaitDelay bounds the pipe-release wait after that
// kill — one read can therefore outlive its deadline by at most the kill
// latency plus ownedGroupWaitDelay, never a generic runner's five-second
// slack. The reader resolves ps from the runner's own (parent) PATH, not
// the intentionally restricted child environment, and reaps its own ps
// (cmd.Wait) on every path: no stray process, no signal to anything
// outside this read.
func ownedGroupSnapshotGone(pgid int, readCeil time.Duration) bool {
	if readCeil <= 0 {
		return false
	}
	// A ps that does not resolve in the parent environment is a failed
	// read, never an empty group.
	psPath, err := exec.LookPath("ps")
	if err != nil {
		return false
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(readCeil))
	defer cancel()
	cmd := exec.CommandContext(ctx, psPath, "-A", "-o", "pid=,pgid=,state=")
	cmd.WaitDelay = ownedGroupWaitDelay
	cmd.Env = ownedGroupReadEnv()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return false
	}
	if err := cmd.Wait(); err != nil {
		// ps failed, was killed on the read deadline, or the pipes did not
		// release in time: the read proves nothing and stays pending.
		return false
	}
	return ownedGroupRowsGone(stdout.String(), pgid)
}

// ownedGroupReadEnv is the runner's own (parent) environment with LC_ALL=C
// so ps prints in the fixed form the parser expects; it never inherits the
// intentionally restricted child environment of the owned run.
func ownedGroupReadEnv() []string {
	env := os.Environ()
	for i, kv := range env {
		if strings.HasPrefix(kv, "LC_ALL=") {
			env[i] = "LC_ALL=C"
			return env
		}
	}
	return append(env, "LC_ALL=C")
}

// ownedGroupRowsGone is the conservative classifier of one ps snapshot
// table (pid/pgid/state rows) for the owned group: it reports true only
// when at least one row belongs to the group and every one of its rows
// proves exit (a zombie base state, or a state the kernel confirms as
// ESRCH). A valid row of another group is foreign and is skipped — never
// classified as owned and never signalled — including the PGID-0 kernel
// thread rows a Linux readonly native snapshot lists. A row that does not
// parse (wrong field count), a pid that does not decode or is not
// positive, a group that does not decode or is negative, and a table with
// no owned-group row all report pending: a failed or partial read is never
// folded into "empty".
func ownedGroupRowsGone(table string, pgid int) bool {
	owned := 0
	for _, line := range strings.Split(strings.TrimRight(table, "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			return false
		}
		pid, err1 := strconv.Atoi(fields[0])
		group, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil || pid <= 0 || group < 0 {
			return false
		}
		// A valid row of another group — including the PGID-0 kernel
		// threads Linux lists — is foreign: skipped, not owned, and never
		// signalled.
		if group != pgid {
			continue
		}
		owned++
		state := fields[2]
		if strings.HasPrefix(state, "Z") {
			continue // a zombie is a dead member, not running
		}
		if ownedGroupStateLive(state) {
			return false
		}
		// Unknown state (including `?`): the kernel decides. ESRCH is the
		// proven exit; a live or denied (EPERM) pid keeps the group
		// pending — unknown is never labeled gone.
		if !errors.Is(syscall.Kill(pid, 0), syscall.ESRCH) {
			return false
		}
	}
	return owned > 0
}

// ownedGroupStateLive reports whether a ps state token's base letter is a
// live state, mirroring the platform liveness reader's letter set (R, S,
// D, T, W, X, I, U, or the tracing-stop t; the modifier letters ps appends
// never change the liveness). Any other base letter — or an empty token —
// is not a positive live read: it is an unknown state, confirmed against
// the kernel instead of classified.
func ownedGroupStateLive(state string) bool {
	if state == "" {
		return false
	}
	switch state[0] {
	case 'R', 'S', 'D', 'T', 'W', 'X', 'I', 'U', 't':
		return true
	}
	return false
}

// ownedGroupVerifyError is the explicit, non-claiming verification failure:
// the final kill has been sent, but at the budget the group still carried a
// live, exiting, unreadable or unverified member. It states that cleanup
// is not claimed; it never erases or relabels the run's own error.
func ownedGroupVerifyError(pgid int) error {
	return fmt.Errorf("owned process group %d not proven exited within %v after the final kill: a member was still live, exiting, unreadable or unverified at the ceiling; no cleanup is claimed", pgid, ownedGroupVerifyCeiling)
}
