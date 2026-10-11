// Native probe-package dedupe regression: runGoTests must compile each
// probe package exactly once (first-seen order) while still executing every
// expected probe in its own owned process with unchanged pass/fail
// outcomes. The go invocations are observed through a spy GoExecutable:
// the test binary re-executed in an env-selected spy role (the same
// re-exec pattern as the lifecycle fixture) records argv and delegates to
// the REAL go, relaying stdio and the real exit code. The spy never
// fabricates a probe result: every measured outcome below is the real
// toolchain's. No shell, no interpreter.
package eval

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	compileSpyRunEnv = "HERDR_EVAL_COMPILE_SPY_RUN"
	compileSpyGoEnv  = "HERDR_EVAL_COMPILE_SPY_GO"
	compileSpyLogEnv = "HERDR_EVAL_COMPILE_SPY_LOG"
)

// spyInvocation is one recorded go argv observed by the spy role.
type spyInvocation struct {
	Run  string   `json:"run"`
	Argv []string `json:"argv"`
}

// The env-selected spy role: when the test binary is re-executed with the
// spy environment, it records its argv and delegates to the real go,
// streaming stdio straight through and exiting with the real exit code.
// Nothing the spy emits can be mistaken for a test2json event: it only
// relays the real go's bytes and status.
func init() {
	if os.Getenv(compileSpyRunEnv) == "" {
		return
	}
	realGo := os.Getenv(compileSpyGoEnv)
	if !filepath.IsAbs(realGo) || len(os.Args) < 2 {
		return
	}
	spyArgv := os.Args[1:]
	if logFile := os.Getenv(compileSpyLogEnv); logFile != "" {
		line, _ := json.Marshal(spyInvocation{Run: os.Getenv(compileSpyRunEnv), Argv: spyArgv})
		if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			_, _ = f.Write(append(line, '\n'))
			f.Close()
		}
	}
	cmd := exec.Command(realGo, spyArgv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = os.Environ()
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		code = 126
	}
	os.Exit(code)
}

// compileOnceProbes holds four probes in package go-add-fixture/probes
// (three real passes and one deliberately wrong expectation) and two probes
// in the distinct package go-add-fixture/probes/second (one real pass and
// one deliberately wrong expectation): six probes, two packages.
const compileOnceMainProbe = `package probes

import (
	"testing"

	"go-add-fixture/src"
)

func TestAddOneTwo(t *testing.T) {
	if got := src.Add(1, 2); got != 3 {
		t.Fatalf("Add(1, 2) = %d, want 3", got)
	}
}

func TestAddNegative(t *testing.T) {
	if got := src.Add(-1, -2); got != -3 {
		t.Fatalf("Add(-1, -2) = %d, want -3", got)
	}
}

func TestAddZeroZero(t *testing.T) {
	if got := src.Add(0, 0); got != 0 {
		t.Fatalf("Add(0, 0) = %d, want 0", got)
	}
}

func TestAddOneTwoIsFour(t *testing.T) {
	// Deliberately wrong expectation: a real per-probe failure.
	if got := src.Add(1, 2); got != 4 {
		t.Fatalf("Add(1, 2) = %d, want 4", got)
	}
}
`

const compileOnceSecondProbe = `package second

import (
	"testing"

	"go-add-fixture/src"
)

func TestSecondPass(t *testing.T) {
	if got := src.Add(7, 8); got != 15 {
		t.Fatalf("Add(7, 8) = %d, want 15", got)
	}
}

func TestSecondFail(t *testing.T) {
	// Deliberately wrong expectation: a real per-probe failure.
	if got := src.Add(1, 1); got != 3 {
		t.Fatalf("Add(1, 1) = %d, want 3", got)
	}
}
`

// TestProbePackagesCompileOnce proves the dedupe contract against the real
// Go toolchain: exactly one `go test -c` per probe package (two for the
// two-package fixture, first-seen order kept), one `go tool test2json`
// execution per probe against its package's single compiled binary, and
// the unchanged per-probe pass/fail outcomes.
func TestProbePackagesCompileOnce(t *testing.T) {
	goBin := goExecutableForTest(t)
	spyBin, err := os.Executable()
	if err != nil {
		t.Fatalf("test binary path: %v", err)
	}

	outside := t.TempDir()
	caller := t.TempDir()
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{
		"add_test.go":         compileOnceMainProbe,
		"second/more_test.go": compileOnceSecondProbe,
	})
	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)

	spyLog := filepath.Join(t.TempDir(), "spy.log")
	runID := fmt.Sprintf("compile-once-%d", time.Now().UnixNano())
	t.Setenv(compileSpyRunEnv, runID)
	t.Setenv(compileSpyGoEnv, goBin)
	t.Setenv(compileSpyLogEnv, spyLog)
	tempRoot := t.TempDir()
	result, err := Probes(context.Background(), Options{
		Fixture:      fixture,
		Destination:  dest,
		Repository:   caller,
		TempRoot:     tempRoot,
		GoExecutable: spyBin,
	})
	if err != nil {
		// Report the observed compile count per package on every outcome —
		// a watchdog kill stops the measurement mid-run, and the spy log
		// still holds the invocations that did happen.
		reportCompileEvidence(t, spyLog, runID)
		t.Fatalf("probes: %v", err)
	}

	// Outcomes measured by the real toolchain through the spy: four real
	// passes, two real failures, in expected (first-seen) order.
	if result.Probes.Passed != 4 || result.Probes.Total != 6 {
		t.Fatalf("probes = passed %d / total %d, want 4/6", result.Probes.Passed, result.Probes.Total)
	}
	if !equalStrings(result.Probes.Failed, []string{"TestAddOneTwoIsFour", "TestSecondFail"}) {
		t.Fatalf("failed = %v, want the two deliberately wrong probes in expected order", result.Probes.Failed)
	}

	invocations := readSpyLog(t, spyLog, runID)
	var compiles, executions []spyInvocation
	for _, inv := range invocations {
		switch {
		case len(inv.Argv) >= 2 && inv.Argv[0] == "test" && inv.Argv[1] == "-c":
			compiles = append(compiles, inv)
		case len(inv.Argv) >= 2 && inv.Argv[0] == "tool" && inv.Argv[1] == "test2json":
			executions = append(executions, inv)
		default:
			t.Fatalf("unexpected go invocation through the spy: %v", inv.Argv)
		}
	}

	// Exactly one compile per probe package, first-seen order kept.
	if len(compiles) != 2 {
		t.Fatalf("go test -c invocations = %d, want exactly one per probe package (2); per package: %s:\n%v",
			len(compiles), compileTally(compiles), argvList(compiles))
	}
	suffix := ""
	if runtime.GOOS == "windows" {
		suffix = ".exe"
	}
	assertCompile := func(index int, wantBin, wantDir string) {
		argv := compiles[index].Argv
		if len(argv) != 5 {
			t.Fatalf("compile %d argv = %v, want [test -c -o <binary> <dir>]", index, argv)
		}
		if filepath.Base(argv[3]) != wantBin || argv[4] != wantDir {
			t.Fatalf("compile %d = %v, want -o %s dir %s", index, argv, wantBin, wantDir)
		}
	}
	assertCompile(0, "probe-bin-0"+suffix, "./probes")
	assertCompile(1, "probe-bin-1"+suffix, "./probes/second")

	// One anchored execution per expected probe, in expected order, each
	// against its package's single compiled binary.
	wantRuns := []string{
		"^TestAddOneTwo$", "^TestAddNegative$", "^TestAddZeroZero$", "^TestAddOneTwoIsFour$",
		"^TestSecondPass$", "^TestSecondFail$",
	}
	wantBinary := map[string]string{
		"TestAddOneTwo":       "probe-bin-0" + suffix,
		"TestAddNegative":     "probe-bin-0" + suffix,
		"TestAddZeroZero":     "probe-bin-0" + suffix,
		"TestAddOneTwoIsFour": "probe-bin-0" + suffix,
		"TestSecondPass":      "probe-bin-1" + suffix,
		"TestSecondFail":      "probe-bin-1" + suffix,
	}
	if len(executions) != 6 {
		t.Fatalf("go tool test2json invocations = %d, want one per probe (6):\n%v", len(executions), argvList(executions))
	}
	for i, want := range wantRuns {
		argv := executions[i].Argv
		var runFlag, binary string
		for j := 0; j+1 < len(argv); j++ {
			switch argv[j] {
			case "-test.run":
				runFlag = argv[j+1]
			}
			if j == 2 {
				binary = argv[j]
			}
		}
		if runFlag != want {
			t.Fatalf("execution %d -test.run = %q, want %q (argv %v)", i, runFlag, want, argv)
		}
		name := strings.Trim(runFlag, "^$")
		if filepath.Base(binary) != wantBinary[name] {
			t.Fatalf("execution %d (%s) binary = %s, want %s", i, name, binary, wantBinary[name])
		}
	}
}

// readSpyLog parses the spy log: one JSON line per recorded go invocation,
// all of this run.
func readSpyLog(t *testing.T, path, runID string) []spyInvocation {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("spy log missing (the spy role never ran): %v", err)
	}
	var invocations []spyInvocation
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var inv spyInvocation
		if err := json.Unmarshal([]byte(line), &inv); err != nil {
			t.Fatalf("spy log line %q does not parse: %v", line, err)
		}
		if inv.Run != runID {
			t.Fatalf("spy log run = %q, want %q", inv.Run, runID)
		}
		invocations = append(invocations, inv)
	}
	return invocations
}

// reportCompileEvidence reads the spy log and reports the observed
// `go test -c` count per probe package with the recorded argv, on every
// outcome — including Probes errors. A run killed by the bounded
// watchdog may have stopped mid-measurement; the evidence is reported as
// observed, never completed.
func reportCompileEvidence(t *testing.T, path, runID string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Logf("compile evidence: spy log unreadable (%v): the spy role never ran, or the run died before its first go invocation", err)
		return
	}
	counts := map[string]int{}
	argvs := map[string][]string{}
	var order []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var inv spyInvocation
		if err := json.Unmarshal([]byte(line), &inv); err != nil || inv.Run != runID {
			continue
		}
		if len(inv.Argv) < 5 || inv.Argv[0] != "test" || inv.Argv[1] != "-c" || inv.Argv[2] != "-o" {
			continue
		}
		dir := inv.Argv[4]
		if _, ok := counts[dir]; !ok {
			order = append(order, dir)
		}
		counts[dir]++
		argvs[dir] = append(argvs[dir], strings.Join(inv.Argv, " "))
	}
	if len(counts) == 0 {
		t.Logf("compile evidence: no `go test -c` invocations recorded before this outcome")
		return
	}
	for _, dir := range order {
		if counts[dir] == 1 {
			t.Logf("compile evidence: 1 compile of %s (as wanted)", dir)
		} else {
			t.Errorf("observed %d compiles of %s, want 1", counts[dir], dir)
		}
		t.Logf("compile evidence argv for %s:%s", dir, strings.Join(argvs[dir], "\n"))
	}
}

// compileTally renders the per-package `go test -c` counts for a failure
// message: e.g. "4 of ./probes, 2 of ./probes/second".
func compileTally(compiles []spyInvocation) string {
	counts := map[string]int{}
	var order []string
	for _, inv := range compiles {
		var dir string
		if len(inv.Argv) >= 5 && inv.Argv[0] == "test" && inv.Argv[1] == "-c" && inv.Argv[2] == "-o" {
			dir = inv.Argv[4]
		} else {
			dir = "(unparsed argv: " + strings.Join(inv.Argv, " ") + ")"
		}
		if _, ok := counts[dir]; !ok {
			order = append(order, dir)
		}
		counts[dir]++
	}
	parts := make([]string, 0, len(order))
	for _, dir := range order {
		parts = append(parts, fmt.Sprintf("%d of %s", counts[dir], dir))
	}
	return strings.Join(parts, ", ")
}

// argvList renders recorded invocations for a failure message.
func argvList(invocations []spyInvocation) string {
	parts := make([]string, 0, len(invocations))
	for _, inv := range invocations {
		parts = append(parts, strings.Join(inv.Argv, " "))
	}
	return strings.Join(parts, "\n")
}
