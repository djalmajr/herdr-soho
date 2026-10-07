//go:build !windows

package eval

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Unit tests of the bounded owned-group verification (the unix shutdown
// helper). The two seams — the kernel ESRCH probe and the ps snapshot read —
// are replaced with counting hooks, so the loop's contract (prompt empty
// group, failed-snapshot recovery, the ceiling's explicit non-claiming
// error, the zombie-only completion, the per-read deadline inside the
// budget) is tested without any real group state. The conservative table
// classifier is tested against crafted ps tables with real kernel confirms
// (the test's own pid exists; a pid above any kernel pid_max is ESRCH).

// withOwnedGroupVerifyHooks replaces both verification seams and restores
// them (even when an assertion fails).
func withOwnedGroupVerifyHooks(t *testing.T, kernel func(int) bool, snapshot func(int, time.Duration) bool) {
	t.Helper()
	oldKernel := ownedGroupEmptyByKernel
	oldSnapshot := ownedGroupMembersGone
	ownedGroupEmptyByKernel = kernel
	ownedGroupMembersGone = snapshot
	t.Cleanup(func() {
		ownedGroupEmptyByKernel = oldKernel
		ownedGroupMembersGone = oldSnapshot
	})
}

// Prompt normal empty-group completion: the kernel's ESRCH on the group
// probe proves the group empty in one pass — no ps read, no sleep, and no
// second group signal after the proof (the verification never signals at
// all: the group SIGKILL landed before it starts).
func TestOwnedGroupVerifyPromptEmptyGroup(t *testing.T) {
	kernelCalls, snapshotCalls := 0, 0
	withOwnedGroupVerifyHooks(t,
		func(int) bool { kernelCalls++; return true },
		func(int, time.Duration) bool { snapshotCalls++; return false })
	start := time.Now()
	if err := verifyOwnedGroupGone(777); err != nil {
		t.Fatalf("verifyOwnedGroupGone = %v, want nil for the proven-empty group", err)
	}
	if kernelCalls != 1 {
		t.Fatalf("kernel probe called %d times, want exactly 1 (the ESRCH proof)", kernelCalls)
	}
	if snapshotCalls != 0 {
		t.Fatalf("ps snapshot called %d times, want 0: the ESRCH proof needs no read", snapshotCalls)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("the empty-group completion took %s: it must return promptly", elapsed)
	}
}

// A failed (pending) snapshot read never proves the group empty; the loop
// retries until a read proves it.
func TestOwnedGroupVerifyFailedSnapshotThenGone(t *testing.T) {
	results := []bool{false, false, true}
	withOwnedGroupVerifyHooks(t,
		func(int) bool { return false }, // the kernel never reports ESRCH
		func(int, time.Duration) bool {
			next := results[0]
			results = results[1:]
			return next
		})
	if err := verifyOwnedGroupGone(777); err != nil {
		t.Fatalf("verifyOwnedGroupGone = %v, want nil once a read proves the group gone", err)
	}
	if len(results) != 0 {
		t.Fatalf("snapshot reads left unconsumed: %v", results)
	}
}

// A group that stays unverified until the budget fails with the explicit,
// non-claiming error, and the failure is bounded: the elapsed time is the
// ceiling plus at most one final read, never an open-ended wait.
func TestOwnedGroupVerifyCeilingExplicitError(t *testing.T) {
	withOwnedGroupVerifyHooks(t,
		func(int) bool { return false },
		func(int, time.Duration) bool { return false })
	start := time.Now()
	err := verifyOwnedGroupGone(777)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("verifyOwnedGroupGone = nil, want the explicit verification failure")
	}
	for _, want := range []string{"not proven exited", "no cleanup is claimed", "777"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not carry %q: the failure must be explicit and non-claiming", err, want)
		}
	}
	if elapsed < ownedGroupVerifyCeiling {
		t.Fatalf("the failure came after %s, before the %s budget: the ceiling was not honored", elapsed, ownedGroupVerifyCeiling)
	}
	if elapsed > ownedGroupVerifyCeiling+1500*time.Millisecond {
		t.Fatalf("the failure came after %s: the verification escaped its bounded budget", elapsed)
	}
}

// The zombie-only completion: on macOS the group probe answers EPERM (a
// zombie member cannot be signalled), not ESRCH — so the kernel probe alone
// can never prove such a group, and the snapshot read that sees only zombie
// members is the proof. It must complete on that single read.
func TestOwnedGroupVerifyZombieOnlyGroupCompletes(t *testing.T) {
	snapshotCalls := 0
	withOwnedGroupVerifyHooks(t,
		func(int) bool { return false }, // EPERM: not the ESRCH proof
		func(int, time.Duration) bool { snapshotCalls++; return true })
	if err := verifyOwnedGroupGone(777); err != nil {
		t.Fatalf("verifyOwnedGroupGone = %v, want nil for the proven zombie-only group", err)
	}
	if snapshotCalls != 1 {
		t.Fatalf("snapshot reads = %d, want 1: the zombie-only proof is a single read", snapshotCalls)
	}
}

// The consumer discriminates a reader that overruns its deadline, not just
// the passed timeout values: while the budget is wide the reads get the
// read ceiling and the final pass gets only the time left in the budget
// (never more), and a read that outlives its deadline (simulated here by
// sleeping past it, the way the real reader's kill + pipe-release slack
// lets a hung ps do) still ends the verification at the ceiling plus the
// documented slack — a bounded, explicit failure, never an endless wait.
func TestOwnedGroupVerifyPerReadDeadlineWithinBudget(t *testing.T) {
	readCeils := []time.Duration{}
	withOwnedGroupVerifyHooks(t,
		func(int) bool { return false },
		func(_ int, readCeil time.Duration) bool {
			readCeils = append(readCeils, readCeil)
			// The read overruns its whole deadline by the documented
			// kill/pipe slack: a hung reader outliving the deadline is the
			// case the consumer must discriminate.
			time.Sleep(readCeil + 250*time.Millisecond)
			return false
		})
	start := time.Now()
	err := verifyOwnedGroupGone(777)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("want the ceiling failure when every read stays pending")
	}
	for _, want := range []string{"not proven exited", "no cleanup is claimed"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q does not carry %q: the overrun failure must stay explicit and non-claiming", err, want)
		}
	}
	if len(readCeils) < 2 {
		t.Fatalf("only %d reads ran: the budget should admit several bounded reads", len(readCeils))
	}
	sawFull, sawShrunk := false, false
	for i, ceil := range readCeils {
		if ceil > ownedGroupVerifyCeiling {
			t.Fatalf("read %d got the deadline %s, beyond the %s budget", i, ceil, ownedGroupVerifyCeiling)
		}
		if ceil == ownedGroupVerifyReadCeil {
			sawFull = true
		}
		if ceil < ownedGroupVerifyReadCeil {
			sawShrunk = true
		}
	}
	if !sawFull || !sawShrunk {
		t.Fatalf("read deadlines %v: want the full read ceiling early and a budget-shrunk deadline late", readCeils)
	}
	// Bounded despite every read overrunning its deadline: the ceiling plus
	// at most one read's documented overrun slack (read ceiling + kill +
	// pipe-release delay), never an open-ended wait.
	if elapsed > ownedGroupVerifyCeiling+1500*time.Millisecond {
		t.Fatalf("the verification took %s: the consumer must stay bounded when a reader overruns its deadline", elapsed)
	}
}

// The real owned ps reader, end to end against the real ps: it resolves ps
// from the parent PATH, runs the PID/PGID/state snapshot under its own
// deadline (a hung read is killed and the pipes released within the
// documented slack — never a generic runner's five-second WaitDelay), and
// a table without a row for the queried group reports pending, never empty.
func TestOwnedGroupSnapshotReadBoundedRealPs(t *testing.T) {
	if _, err := exec.LookPath("ps"); err != nil {
		t.Fatalf("the documented native ps dependency does not resolve in the parent environment: %v", err)
	}
	start := time.Now()
	got := ownedGroupSnapshotGone(1<<30, 300*time.Millisecond)
	elapsed := time.Since(start)
	if got {
		t.Fatal("the real reader proved the absent group empty from one snapshot: a table without an owned-group row must stay pending")
	}
	if elapsed > time.Second {
		t.Fatalf("the real ps read took %s: the reader must be bounded by its own 300ms deadline plus the kill/pipe slack", elapsed)
	}
	// The documented kill/pipe cleanup path, exercised against the real ps:
	// a deadline far below ps's own runtime forces the read to kill and
	// reap its own ps and return pending promptly — bounded by the kill +
	// WaitDelay slack, never by a generic runner's five-second wait.
	start = time.Now()
	if got := ownedGroupSnapshotGone(1<<30, time.Millisecond); got {
		t.Fatal("the overrunning real read proved the absent group empty: an overrun must stay pending")
	} else if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("the overrunning real read took %s: the reader must kill and reap its own ps within the documented slack", elapsed)
	}
}

// noOwnedGroupPID is a pid no kernel on this run will ever hand out: above
// Linux's 2^22-1 pid_max ceiling and far beyond macOS pid allocation, so
// the kernel confirm for it is ESRCH by construction.
const noOwnedGroupPID = 1 << 30

// assertESRCHPID pins the test's premise: the confirm pid really is absent.
func assertESRCHPID(t *testing.T, pid int) {
	t.Helper()
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("test premise broken: pid %d is not absent (kill(0) = %v)", pid, err)
	}
}

func TestOwnedGroupRowsGoneClassifiesOwnedGroup(t *testing.T) {
	self := os.Getpid()
	assertESRCHPID(t, noOwnedGroupPID)
	cases := []struct {
		name  string
		table string
		want  bool
	}{
		{"zombie-only group is gone", "100 77 Z\n101 77 Z+\n", true},
		{"flagged zombie is gone", "100 77 Zs\n", true},
		{"live member keeps the group pending", "100 77 R\n101 77 S+\n", false},
		{"any live member among zombies keeps the group pending", "100 77 Z\n101 77 D\n", false},
		// The discriminating unknown: a `?` (unknown Mach) state is never
		// labeled gone while the kernel still reports the member existing.
		{"unknown state with a live pid stays pending", fmt.Sprintf("%d 77 ?E\n", self), false},
		{"unknown state confirmed ESRCH by the kernel is gone", fmt.Sprintf("%d 77 ?\n", noOwnedGroupPID), true},
		{"unknown state with an EPERM-class pid stays pending", "1 77 ?\n", false},
		{"mixed zombies and kernel-confirmed unknowns is gone", fmt.Sprintf("100 77 Z\n%d 77 ?\n", noOwnedGroupPID), true},
		{"rows of foreign groups alone do not claim the group empty", "100 1 S\n101 42 S\n", false},
		// The actual native Linux row shape: a readonly native
		// PID/PGID/state snapshot lists kernel threads as valid PGID-0
		// rows (e.g. the rows "2 0 S" and "4 0 I" of the reference
		// snapshot). Foreign PGID-0 rows must not refuse the table and
		// must not count as owned rows — skipped, never classified,
		// never signalled.
		{"foreign PGID0 rows plus owned zombies completes", "2 0 S\n4 0 I\n100 77 Z\n101 77 Zs\n", true},
		{"foreign PGID0 rows plus an owned live member stay pending", "2 0 S\n4 0 I\n100 77 R\n", false},
		{"foreign PGID0 rows plus an owned unknown member stay pending", fmt.Sprintf("2 0 S\n4 0 I\n%d 77 ?E\n", self), false},
		{"only foreign PGID0 rows do not claim the group empty", "2 0 S\n4 0 I\n", false},
		{"an empty table does not claim the group empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownedGroupRowsGone(tc.table, 77); got != tc.want {
				t.Fatalf("ownedGroupRowsGone = %v for %q, want %v: the read must classify the owned group conservatively", got, tc.table, tc.want)
			}
		})
	}
}

// A malformed or partial table proves nothing: it is pending, never empty.
// Valid foreign rows (including PGID-0 kernel threads) do not make a table
// malformed; invalid pid/group values still refuse it.
func TestOwnedGroupRowsGoneRejectsMalformedTables(t *testing.T) {
	self := os.Getpid()
	malformed := []string{
		"garbage line\n",
		"100 77\n",                             // two fields
		"100 77 S extra\n",                     // four fields
		"10x 77 S\n",                           // pid does not decode
		"100 7z S\n",                           // group does not decode
		"2 -1 S\n",                             // a negative group is invalid
		"0 0 S\n",                              // a non-positive pid is invalid
		fmt.Sprintf("%d 77 S\nbroken\n", self), // a good row plus a broken one
	}
	for i, table := range malformed {
		if ownedGroupRowsGone(table, 77) {
			t.Fatalf("case %d: the malformed table %q proved the group gone: a partial read must stay pending", i, table)
		}
	}
}

// The kernel probe's contract, pinned against the real kernel: a group that
// cannot exist is ESRCH (proven empty), the test's own group is not.
func TestOwnedGroupKernelProbeContract(t *testing.T) {
	if !ownedGroupEmptyByKernel(1 << 30) {
		t.Fatal("the absent group must read as ESRCH-proven empty")
	}
	if ownedGroupEmptyByKernel(syscall.Getpgrp()) {
		t.Fatal("the live own group must never read as proven empty")
	}
}
