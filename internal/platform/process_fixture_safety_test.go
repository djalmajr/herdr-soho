package platform

// TestOwnedFixtureStopMismatchControl pins the fixture cleanup safety
// contract: the stop validates the original start identity immediately
// before the single owned signal, so a wrong recorded identity refuses
// the signal and the actual owned live child stays alive; a record that
// captured no identity is refused the same way (unknown is never
// signalled); the correct owned cleanup then removes the child with the
// single signal and the identity-aware read poll confirms the absence.
// TestOwnedFixtureStopAbsentControl pins the absent case: the absence of
// an exited child is confirmed by the identity-aware read and no signal
// is ever sent to a pid that is already gone.

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// startOwnedFixtureChild starts the fixture binary directly (not through
// RunCli) in one fixture role: the test owns the live exec.Cmd, the role
// needs no PATH (the hold roles only sleep and write nothing).
func startOwnedFixtureChild(t *testing.T, role string) *exec.Cmd {
	t.Helper()
	bin := t.TempDir()
	runCliFixtureProgram(t, bin, runCliFixtureName())
	cmd := exec.Command(filepath.Join(bin, runCliFixtureName()))
	cmd.Env = []string{runCliFixtureRoleEnv + "=" + role}
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the owned fixture child: %v", err)
	}
	t.Cleanup(func() { _ = cmd.Wait() })
	return cmd
}

// awaitFixtureRunning waits (bounded) until the platform's identity-aware
// read sees the pid running with a start identity, and returns it.
func awaitFixtureRunning(t *testing.T, pid int, env Env) string {
	t.Helper()
	until := time.Now().Add(5 * time.Second)
	lastStarted := ""
	for {
		started, live := ReadProc(pid, env)
		lastStarted = started
		if live == ProcRunning && started != "" {
			return started
		}
		if live == ProcGone {
			t.Fatalf("the owned fixture child %d exited before the identity read", pid)
		}
		if time.Now().After(until) {
			t.Fatalf("the identity read never saw fixture pid %d running (last live=%v started=%q)", pid, live, lastStarted)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestOwnedFixtureStopMismatchControl(t *testing.T) {
	env := fixturePsEnv()
	child := startOwnedFixtureChild(t, "hold-5000")
	pid := child.Process.Pid
	started := awaitFixtureRunning(t, pid, env)
	_, name, ok := ProcInfo(pid, env)
	if !ok {
		t.Fatalf("the owned child %d reads unreadable after it ran", pid)
	}
	correct := fixtureOwnedRecord{Pid: pid, Started: started, Name: name}
	// The cleanup is registered before any assertion: a failure below
	// must not leak the owned child (the stop re-validates the identity
	// and refuses rather than signal a recycled pid).
	t.Cleanup(func() {
		if outcome := stopOwnedFixture(t, correct, env); outcome != "absent" && outcome != "stopped" {
			t.Errorf("the cleanup could not stop the owned child: %s", outcome)
		}
	})
	// A wrong start identity: the signal must be refused and the actual
	// owned live child must stay alive.
	wrong := correct
	wrong.Started = "Sun Sep 07 09:08:07 2025"
	if outcome := stopOwnedFixture(t, wrong, env); outcome != "mismatch" {
		t.Fatalf("the wrong identity: outcome=%s want the refused mismatch", outcome)
	}
	if nowStarted, live := ReadProc(pid, env); live != ProcRunning || !SameStarted(nowStarted, correct.Started) {
		t.Fatalf("the refused stop signalled the owned child (live=%v started=%q want %q)", live, nowStarted, correct.Started)
	}
	wrongName := correct
	wrongName.Name = "unrelated-process"
	if outcome := stopOwnedFixture(t, wrongName, env); outcome != "mismatch" {
		t.Fatalf("the wrong executable name: outcome=%s want mismatch", outcome)
	}
	if nowStarted, live := ReadProc(pid, env); live != ProcRunning || !SameStarted(nowStarted, correct.Started) {
		t.Fatalf("the wrong-name stop signalled the owned child: live=%v started=%q", live, nowStarted)
	}
	// A record that captured no identity is refused too: unknown is
	// never signalled.
	if outcome := stopOwnedFixture(t, fixtureOwnedRecord{Pid: pid, Name: name}, env); outcome != "unknown" {
		t.Fatalf("the identity-less record: outcome=%s want the refused unknown", outcome)
	}
	if _, live := ReadProc(pid, env); live != ProcRunning {
		t.Fatalf("the identity-less stop signalled the owned child (live=%v)", live)
	}
	// The correct owned cleanup removes it: the single signal, the
	// absence confirmed by the identity-aware read.
	if outcome := stopOwnedFixture(t, correct, env); outcome != "stopped" {
		t.Fatalf("the correct cleanup: outcome=%s want stopped", outcome)
	}
	// A second stop sees the proven absence and signals nothing.
	if outcome := stopOwnedFixture(t, correct, env); outcome != "absent" {
		t.Fatalf("the second stop: outcome=%s want the confirmed absence", outcome)
	}
}

func TestOwnedFixtureStopAbsentControl(t *testing.T) {
	env := fixturePsEnv()
	child := startOwnedFixtureChild(t, "hold-300")
	pid := child.Process.Pid
	started := awaitFixtureRunning(t, pid, env)
	record := fixtureOwnedRecord{Pid: pid, Started: started, Name: "fixture"}
	// Let the child exit on its own (it is reaped by the test) and
	// confirm the absence by the identity-aware read: no signal is ever
	// sent to a pid that is already gone.
	if err := child.Wait(); err != nil {
		t.Fatalf("wait the owned child: %v", err)
	}
	until := time.Now().Add(5 * time.Second)
	for {
		_, live := ReadProc(pid, env)
		if live == ProcGone {
			break
		}
		if time.Now().After(until) {
			t.Fatalf("the absence of the exited child %d was never confirmed (live=%v)", pid, live)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if outcome := stopOwnedFixture(t, record, env); outcome != "absent" {
		t.Fatalf("the absent pid: outcome=%s want the no-signal absence", outcome)
	}
}
