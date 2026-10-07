//go:build !windows

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Native process fixtures (P1): the test binary re-executes itself to play
// the long-lived processes the stop/identity contracts need. The old
// fixtures were an `sh -c "sleep 300 & wait"` tree and /usr/bin/python3
// signal-handling scripts; this file retires the shell and the interpreter
// with the same observable roles. Only os/exec, os/signal/syscall and
// native file/pipe synchronization are used: no shell, no Python/Node/Bun,
// no external sleep binary.
//
// The helper is started as the test binary itself with
// `-test.run=^TestProcFixtureHelper$` and the role in the environment
// (argv carries only test flags, so the role rides in env vars, test
// private).
const (
	procFixtureRoleEnv = "HERDR_PROC_FIXTURE_ROLE"
	procFixtureArg1Env = "HERDR_PROC_FIXTURE_ARG1"
	procFixtureArg2Env = "HERDR_PROC_FIXTURE_ARG2"

	procFixtureHelperTest = "TestProcFixtureHelper"
)

// TestProcFixtureHelper is the re-executed helper entry: when the role env
// is set the fixture role runs and the process exits; in the main test run
// the env is unset and the function returns without running anything.
func TestProcFixtureHelper(t *testing.T) {
	role := os.Getenv(procFixtureRoleEnv)
	if role == "" {
		return
	}
	os.Exit(runProcFixtureRole(role, os.Getenv(procFixtureArg1Env), os.Getenv(procFixtureArg2Env)))
}

// runProcFixtureRole runs one fixture role inside the re-executed test
// binary. Every role keeps default signal handling unless it says
// otherwise, so a real SIGTERM kills it and SIGKILL stops it.
func runProcFixtureRole(role, arg1, arg2 string) (code int) {
	switch role {
	case "sleeper":
		// A long-lived process that dies from a default SIGTERM (and from
		// SIGKILL): the owned stand-in for the old `sleep 300`.
		blockForever()
	case "tree":
		// A parent that owns exactly one sleeper child and waits on it:
		// the owned stand-in for `sh -c "sleep 300 & wait"`. The parent
		// dies from a default SIGTERM and it exits when its child stops
		// (wait returns), as the shell tree did, and the child reparents.
		child := procFixtureChild("sleeper", nil)
		_ = child.Wait()
	case "ignoreterm":
		// Installs SIG_IGN for SIGTERM with the real signal machinery
		// (the python fixture's signal.signal(SIGTERM, SIG_IGN)) and
		// prints the handshake only after the ignore is in place; then
		// stays alive like the fixture's `while True: time.sleep`.
		signal.Ignore(syscall.SIGTERM)
		fmt.Fprint(os.Stdout, "ready\n")
		blockForever()
	case "ignoreterm-child":
		// Ignores SIGTERM and keeps one sleeper child: the parent stays
		// alive after its child dies (the python fixture slept 300 s
		// without waiting on the child), so both TERM sends of a
		// two-level stop stay observable.
		signal.Ignore(syscall.SIGTERM)
		_ = procFixtureChild("sleeper", nil)
		blockForever()
	case "forkroot":
		// Spawns the heartbeat child through os/exec (the native stand-in
		// for the os.fork fixture), records the child's pid in arg1 and
		// stays alive (the python parent slept 300 s without waiting on
		// the child): the child's parent is this process until it dies.
		child := procFixtureChild("heartbeat", []string{arg2})
		if err := os.WriteFile(arg1, []byte(strconv.Itoa(child.Process.Pid)+"\n"), 0o600); err != nil {
			_ = child.Process.Kill()
			fmt.Fprintf(os.Stderr, "fixture forkroot: %v\n", err)
			return 1
		}
		blockForever()
	case "heartbeat":
		// Writes "<ppid> <unix ns>" to arg1 every 50 ms: the heartbeat the
		// review probes read to observe reparenting and to prove a
		// signal reached (or did not reach) the child.
		for {
			_ = os.WriteFile(arg1, []byte(fmt.Sprintf("%d %d\n", os.Getppid(), time.Now().UnixNano())), 0o600)
			time.Sleep(50 * time.Millisecond)
		}
	default:
		fmt.Fprintf(os.Stderr, "fixture: unknown role %q\n", role)
		return 127
	}
	return 0
}

// blockForever blocks the process for the rest of its life inside a real
// blocking syscall (a read on a pipe whose write end this process keeps
// open, so the read never returns and never reaches EOF). A Go process
// whose only goroutine is parked in select{} is a self-declared deadlock
// (the runtime prints the goroutines and exits 2), and a timer sleep does
// not hold a syscall thread either; a blocked read does. The default
// SIGTERM and SIGKILL still stop the process while it is blocked.
func blockForever() {
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: pipe: %v\n", err)
		os.Exit(1)
	}
	_ = w // held open for the life of the process: the read never returns.
	buf := make([]byte, 1)
	_, _ = r.Read(buf)
	os.Exit(1) // unreachable: the read blocks until a signal stops the process
}

// procFixtureChild starts the test binary in another fixture role from
// inside the helper process (its own environment plus the role markers).
func procFixtureChild(role string, args []string) *exec.Cmd {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture: executable: %v\n", err)
		os.Exit(1)
	}
	cmd := exec.Command(exe, "-test.run=^"+procFixtureHelperTest+"$")
	items := os.Environ()
	items = append(items, procFixtureRoleEnv+"="+role)
	for i, a := range args {
		items = append(items, fmt.Sprintf("HERDR_PROC_FIXTURE_ARG%d=%s", i+1, a))
	}
	cmd.Env = items
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "fixture: starting %s: %v\n", role, err)
		os.Exit(1)
	}
	return cmd
}

// fixtureSpawn starts the test binary in a fixture role under the test's
// env and owns the process for the whole test (killed and reaped in the
// cleanup, also when an assertion failed), so the test never leaves a
// process behind and never touches one it did not start.
func fixtureSpawn(t *testing.T, env Env, role string, args ...string) *exec.Cmd {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("locating the test binary: %v", err)
	}
	return fixtureSpawnExecutable(t, env, exe, role, args...)
}

func fixtureSpawnExecutable(t *testing.T, env Env, exe, role string, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(exe, "-test.run=^"+procFixtureHelperTest+"$")
	items := env.List()
	items = append(items, procFixtureRoleEnv+"="+role)
	for i, a := range args {
		items = append(items, fmt.Sprintf("HERDR_PROC_FIXTURE_ARG%d=%s", i+1, a))
	}
	cmd.Env = items
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting the %s fixture: %v", role, err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// beatPPID reads the current "<ppid> <unix ns>" heartbeat line (0 when
// unreadable).
func beatPPID(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	p, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0
	}
	return p
}

// waitBeatPPID polls the heartbeat file until it reports ppid want (true)
// or the ceiling passes (false): the initial-parent check without timing
// arithmetic.
func waitBeatPPID(t *testing.T, beat string, want int, ceiling time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(ceiling)
	for {
		if ppid := beatPPID(beat); ppid == want {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(20 * time.Millisecond)
	}
}
