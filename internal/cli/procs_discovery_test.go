package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// procs add discovery: a pid whose identity the single snapshot read could
// not resolve (the ps query failed and the kernel did not report the
// absence) is a friction (exit 4, a clear diagnostic, nothing registered),
// not the dead-process refusal (which the proven-gone pid keeps, exit 2).
// The registration test is the actual procs add consumer on an owned Go
// fixture process read through the real ps; the flagged-token native proof
// (the session-leader Ss shape the operator's capture processes had) is
// in internal/platform, apart from the deterministic fake-ps tier.
// Every process touched is an owned fixture, killed and reaped in a
// cleanup that also runs when an assertion fails; no foreign process is
// registered, signalled or altered.

// A failed ps read of a live pid must not read as a dead process: the
// refusal is the exit 2 of a proven-gone pid only. With the ps query
// failing (the fixture's PATH puts the fake dir first, so the registry
// helpers resolve the fake before the system ps) and the pid alive, the
// kernel does not report the absence: procs add exits 4 with a clear
// diagnostic and registers nothing; with the same failed ps and a dead
// pid, the kernel confirms the absence and the proven-gone refusal keeps
// its message and exit code.
func TestProcsAddUnreadableIdentityIsNotADeadProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the failed-ps read injection is Unix-specific; the windows three-way read is covered in internal/platform")
	}
	f := newProcsFixture(t)
	live := f.sleeper(t)
	dead := f.sleeperGone(t)
	if _, err := fakecli.Install(t, f.fakeDir, "ps", []fakecli.Rule{{AnyArgs: true, Code: 9}}); err != nil {
		t.Fatal(err)
	}
	t.Run("the live unreadable pid exits 4 with the clear diagnostic and registers nothing", func(t *testing.T) {
		code, _, errOut := f.run(t, "procs", "add", strconv.Itoa(live))
		if code != 4 {
			t.Fatalf("code=%d err=%q; want 4 (a failed read is not a dead process)", code, errOut)
		}
		want := "herdr-soho: procs add: the identity of pid " + strconv.Itoa(live) + " is unreadable (the system state read failed and the kernel did not report it gone); not registered"
		if !strings.Contains(errOut, want) {
			t.Fatalf("stderr = %q; want the clear unreadable diagnostic %q", errOut, want)
		}
		if _, err := os.Stat(f.registryPath()); !os.IsNotExist(err) {
			t.Fatalf("the registry exists after an unreadable read: %v", err)
		}
		if data, err := os.ReadFile(filepath.Join(f.state, "friction.log")); err != nil || !strings.Contains(string(data), "unreadable") {
			t.Fatalf("friction.log = %q (err %v); the friction must be recorded", string(data), err)
		}
	})
	t.Run("the dead pid keeps the proven-gone refusal (exit 2, no registry)", func(t *testing.T) {
		code, _, errOut := f.run(t, "procs", "add", strconv.Itoa(dead))
		if code != 2 {
			t.Fatalf("code=%d err=%q; want 2 (the kernel confirmed the absence)", code, errOut)
		}
		if !strings.Contains(errOut, "procs add: refusing "+strconv.Itoa(dead)+": no process with that pid is running") {
			t.Fatalf("stderr = %q; want the proven-gone refusal", errOut)
		}
		if _, err := os.Stat(f.registryPath()); !os.IsNotExist(err) {
			t.Fatalf("the registry exists after the refusal: %v", err)
		}
	})
}

// The actual procs add consumer on an owned Go fixture process read
// through the real ps: the single snapshot read takes the native state
// token as running, the registration lands with the exact lstart identity
// and the comm base name, the line re-checks as running, and the re-add
// replaces the line without duplicating it.
func TestProcsAddRegistersAnOwnedFixtureProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the ps read injection and the native lstart are unix; the windows read is the native kernel32 path")
	}
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	started, name, live := platform.ReadProcFull(pid, f.env)
	if live != platform.ProcRunning || started == "" || name == "" {
		t.Fatalf("ReadProcFull(%d) = (%q, %q, %v); the owned fixture must read as running with the native lstart", pid, started, name, live)
	}
	t.Logf("owned fixture %d: lstart %q name %q", pid, started, name)
	code, out, errOut := f.run(t, "procs", "add", strconv.Itoa(pid))
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("procs add: code=%d out=%q err=%q; the running fixture must register", code, out, errOut)
	}
	rows := f.procsRows(t)
	if len(rows) != 1 {
		t.Fatalf("registry rows = %v; want the one line", rows)
	}
	row := rows[0]
	if row.Pid != pid || row.Name != name || !platform.SameStarted(row.Started, started) {
		t.Fatalf("row = %+v; want pid=%d name=%q started=%q", row, pid, name, started)
	}
	if got := core.ProcRowState(row, f.env); got != "running" {
		t.Fatalf("the line state = %q; the running fixture must re-check as running", got)
	}
	// The re-add replaces the line without duplicating it.
	if code, _, _ := f.run(t, "procs", "add", strconv.Itoa(pid)); code != 0 {
		t.Fatalf("second add: code=%d", code)
	}
	if rows := f.procsRows(t); len(rows) != 1 {
		t.Fatalf("registry rows after the second add = %v; want still one line", rows)
	}
}
