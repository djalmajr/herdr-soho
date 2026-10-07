package eval

// Regressions for the three reviewed measurement findings (F1 process-tree
// cancellation, F2 expected-probe measurement, F3 bounded JSON stream), run
// against the real package API and the real go toolchain with native Go
// children. The pre-fix failures are the reviewer's reproduced controls
// (go-review-eval-20261006T100614.probe.log: R1/R3/R4); these tests are the
// guards that must fail without the fix.

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// readPIDFile reads a pid file written by a probe; zero when absent.
func readPIDFile(path string) int {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	return pid
}

// waitWhile polls the condition with a bounded deadline and reports whether
// it cleared before the deadline.
func waitWhile(deadline time.Duration, cond func() bool) bool {
	end := time.Now().Add(deadline)
	for cond() {
		if time.Now().After(end) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return true
}

// F1: a real context cancellation after the native Go test binary and its
// Go re-exec child handshake must terminate the whole owned tree within a
// bounded deadline and remove the owned temporary work.
func TestRegressionCancelKillsOwnedProcessTree(t *testing.T) {
	outside := t.TempDir()
	caller := t.TempDir()
	pidDir := t.TempDir()
	binFile := filepath.Join(pidDir, "testbinary.pid")
	childFile := filepath.Join(pidDir, "child.pid")
	probe := `package probes

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
	"time"
)

func init() {
	// When re-executed with the marker, the test binary acts as the
	// native Go child: it waits and exits, no test framework involved.
	if os.Getenv("EVAL_REGRESSION_CHILD") == "1" {
		time.Sleep(55 * time.Second)
		os.Exit(0)
	}
}

func TestParentThenChild(t *testing.T) {
	if err := os.WriteFile(` + strconv.Quote(binFile) + `, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin)
	cmd.Env = append(os.Environ(), "EVAL_REGRESSION_CHILD=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(` + strconv.Quote(childFile) + `, []byte(strconv.Itoa(cmd.Process.Pid)), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Second)
}
`
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{"regression_test.go": probe})
	dest := filepath.Join(outside, "worker")
	if _, err := Prepare(context.Background(), Options{Fixture: fixture, Destination: dest, Repository: caller}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	tempRoot := t.TempDir()
	o := Options{Fixture: fixture, Destination: dest, Repository: caller, TempRoot: tempRoot}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			if readPIDFile(binFile) > 0 && readPIDFile(childFile) > 0 {
				time.Sleep(300 * time.Millisecond)
				cancel()
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	_, err := Probes(ctx, o)
	elapsed := time.Since(start)
	bin, child := readPIDFile(binFile), readPIDFile(childFile)
	if err == nil || !strings.Contains(err.Error(), "did not return complete results") {
		t.Fatalf("err = %v, want the cancellation refusal", err)
	}
	if bin <= 0 || child <= 0 {
		t.Fatalf("handshake PIDs missing (testBinary %d, child %d): the cancel never fired mid-test", bin, child)
	}
	if elapsed > 20*time.Second {
		t.Fatalf("elapsed = %s, want a bounded cancellation", elapsed)
	}
	// The whole owned tree (go, the test binary, the re-exec child) is gone
	// within a bounded deadline.
	if !waitWhile(5*time.Second, func() bool { return processAlive(bin) || processAlive(child) }) {
		t.Fatalf("owned processes still alive: testBinary %d alive=%v child %d alive=%v",
			bin, processAlive(bin), child, processAlive(child))
	}
	// The owned temporary work is removed.
	if !waitWhile(5*time.Second, func() bool {
		entries, _ := os.ReadDir(tempRoot)
		return len(entries) != 0
	}) {
		entries, _ := os.ReadDir(tempRoot)
		t.Fatalf("owned work not removed: %d entries left in the temporary root", len(entries))
	}
}

// F2: a panic in the second of six expected probes must keep the fixed N=6
// (with the missing probes completed as failed), not a truncated 1/2.
func TestRegressionPanicRetainsExpectedTotal(t *testing.T) {
	panicky := strings.Replace(addProbes, `if got := src.Add(-1, -2); got != -3 {`,
		`var m map[string]int
		m["x"] = 1
		if got := src.Add(-1, -2); got != -3 {`, 1)
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{"add_test.go": panicky})
	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)

	res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
	if res.code != 0 {
		t.Fatalf("exit = %d, want 0 for a measured failed package (stdout: %s stderr: %s)", res.code, res.stdout, res.stderr)
	}
	var out probeOutput
	mustJSONProbe(t, res.stdout, &out)
	if out.Probes.Total != 6 {
		t.Fatalf("total = %d, want 6 (the expected probe count, not the observed prefix)", out.Probes.Total)
	}
	if out.Probes.Passed != 1 {
		t.Fatalf("passed = %d, want 1 (only the probe before the panic)", out.Probes.Passed)
	}
	wantFailed := []string{
		"TestAddNegative",
		"TestAddZeroZero",
		"TestAddIdentity",
		"TestAddCommutative",
		"TestAddBig",
	}
	if !equalStrings(out.Probes.Failed, wantFailed) {
		t.Fatalf("failed = %v, want %v", out.Probes.Failed, wantFailed)
	}
}

const workerShadowTest = `package src

import "testing"

// Same name as the first hidden probe, and it fails: the hidden results
// must not be overwritten or inflated by the worker package.
func TestAddOneTwo(t *testing.T) {
	if got := Add(1, 2); got != 99 {
		t.Fatalf("worker shadow: Add(1, 2) = %d, want 99", got)
	}
}
`

// F2: a worker-visible test with the same name as a hidden probe (in a
// different package) must not overwrite or inflate the hidden results.
func TestRegressionWorkerSameNameIgnored(t *testing.T) {
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := filepath.Join(outside, "go-add-fixture")
	writeTreeFile(t, fixture, "brief.md", briefMD)
	writeTreeFile(t, fixture, "src/add.go", referenceAdd)
	writeTreeFile(t, fixture, "src/add_test.go", workerShadowTest)
	writeTreeFile(t, fixture, "probes/add_test.go", addProbes)
	writeTreeFile(t, fixture, "ANSWER-KEY.md", "reviewer-only answer key — never copied\n")
	manifest := ""
	for _, rel := range []string{"brief.md", "src/add.go", "src/add_test.go"} {
		hash, err := sha256File(filepath.Join(fixture, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("manifest hash %s: %v", rel, err)
		}
		manifest += hash + "  " + rel + "\n"
	}
	writeTreeFile(t, fixture, "MANIFEST.sha256", manifest)
	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)

	res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
	if res.code != 0 {
		t.Fatalf("exit = %d, want 0 for a fully measured probe package (stdout: %s stderr: %s)", res.code, res.stdout, res.stderr)
	}
	var out probeOutput
	mustJSONProbe(t, res.stdout, &out)
	if out.Probes.Total != 6 || out.Probes.Passed != 6 {
		t.Fatalf("probes = passed %d / total %d, want 6/6 (the worker package result is not a hidden result)", out.Probes.Passed, out.Probes.Total)
	}
	if len(out.Probes.Failed) != 0 {
		t.Fatalf("failed = %v, want none (the worker's same-named failure is ignored)", out.Probes.Failed)
	}
}

// F3: a probe that dumps 2 MiB of output without newlines buries the
// test-level terminal frame inside the oversized stream line, so even the
// trusted test2json framing keeps only the package summary; the runner must
// refuse to measure (exit 2) instead of reporting a silent false pass.
func TestRegressionHugeProbeOutputIsRefusedNotSilent(t *testing.T) {
	// The full probe with the 2 MiB write in the first test; a raw string
	// keeps the generated source free of escape sequences.
	const longProbes = `package probes

import (
	"os"
	"strings"
	"testing"

	"go-add-fixture/src"
)

func TestAddOneTwo(t *testing.T) {
	os.Stdout.WriteString(strings.Repeat("x", 2<<20))
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

func TestAddIdentity(t *testing.T) {
	if got := src.Add(0, 5); got != 5 {
		t.Fatalf("Add(0, 5) = %d, want 5", got)
	}
}

func TestAddCommutative(t *testing.T) {
	if src.Add(1, 2) != src.Add(2, 1) {
		t.Fatal("Add is not commutative")
	}
}

func TestAddBig(t *testing.T) {
	if got := src.Add(1000, 2000); got != 3000 {
		t.Fatalf("Add(1000, 2000) = %d, want 3000", got)
	}
}
`
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{"add_test.go": longProbes})
	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)

	res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
	if res.code != 2 {
		t.Fatalf("exit = %d, want 2 (no silent false pass over the corrupted terminal sequence; stdout: %s stderr: %s)", res.code, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stderr, "did not return complete results") {
		t.Fatalf("stderr = %q, want the explicit unmeasurable refusal", res.stderr)
	}
}

// F3: one JSON line up to the bounded cap must parse (the pre-fix scanner
// stopped at 1 MiB and silently dropped the rest of the stream). The stream
// is one anchored probe execution: exactly one run, one unique pass and the
// package summary, so it classifies as a measured pass.
func TestRegressionOversizeJSONLineParses(t *testing.T) {
	stdout := []byte(`{"Time":"t","Action":"run","Package":"m/probes","Test":"TestA"}`)
	stdout = append(stdout, 0x0a)
	line := []byte(`{"Time":"t","Action":"pass","Package":"m/probes","Test":"TestA","Output":"`)
	line = append(line, strings.Repeat("y", 5<<20)...)
	// The Output field ends with a JSON string newline (backslash, n),
	// then the object and the line close; byte values keep this file free
	// of escape sequences.
	line = append(line, 0x5c, 0x6e, '"', '}', 0x0a)
	stdout = append(stdout, line...)
	stdout = append(stdout, []byte(`{"Time":"t","Action":"pass","Package":"m/probes","Elapsed":0.1}`)...)
	stdout = append(stdout, 0x0a)
	outcome, err := classifyProbeRun(stdout, nil, nil, "TestA", false)
	if err != nil {
		t.Fatalf("err = %v, want a measured result inside the bounded cap", err)
	}
	if outcome != outcomePass {
		t.Fatalf("outcome = %v, want the measured pass with the oversize line parsed", outcome)
	}
}

// F3: a line beyond the bounded cap must fail explicitly, never silently.
func TestRegressionOverlongLineIsExplicitlyUnmeasurable(t *testing.T) {
	stdout := []byte(`{"Time":"t","Action":"pass","Package":"m/probes","Test":"TestA","Output":"`)
	stdout = append(stdout, strings.Repeat("y", 11<<20)...)
	stdout = append(stdout, '"', '"', 0x0a)
	_, err := classifyProbeRun(stdout, nil, nil, "TestA", false)
	if err == nil || !strings.Contains(err.Error(), "exceeded the 10 MiB bounded output cap") {
		t.Fatalf("err = %v, want the explicit bounded cap refusal", err)
	}
}

// F3: a JSON line that does not parse must fail explicitly.
func TestRegressionInvalidJSONLineIsExplicit(t *testing.T) {
	stdout := []byte(`{"Time":"t","Action":"pass","Package":"m/probes","Test":`)
	stdout = append(stdout, 0x0a)
	_, err := classifyProbeRun(stdout, nil, nil, "TestA", false)
	if err == nil || !strings.Contains(err.Error(), "invalid JSON event") {
		t.Fatalf("err = %v, want the explicit invalid JSON event refusal", err)
	}
}

// F3: an explicit GoExecutable must be a trusted absolute path before the
// runner launches anything.
func TestRegressionRejectsNonAbsoluteGoExecutable(t *testing.T) {
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{"add_test.go": addProbes})
	dest := filepath.Join(outside, "worker")
	if _, err := Prepare(context.Background(), Options{Fixture: fixture, Destination: dest, Repository: caller}); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	_, err := Probes(context.Background(), Options{
		Fixture:      fixture,
		Destination:  dest,
		Repository:   caller,
		GoExecutable: "go",
	})
	if err == nil || !strings.Contains(err.Error(), "trusted absolute path") {
		t.Fatalf("err = %v, want the trusted absolute path refusal", err)
	}
}
