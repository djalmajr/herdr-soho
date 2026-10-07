package eval

// Regressions for the reviewed measurement-integrity findings: stdout
// test2json-framing spoof (F1), worker-emitted timeout marker text (F2) and
// worker-tampered build configuration in the measurement scratch (F7). Each
// test runs the real package API or the real CLI binary against a tiny Go
// fixture with a deliberately hostile worker implementation.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The five hidden probes that fail against the broken Add (returns a); the
// order is the hidden declaration order, which is the expected failed order.
var wantBrokenFailed = []string{
	"TestAddOneTwo",
	"TestAddNegative",
	"TestAddIdentity",
	"TestAddCommutative",
	"TestAddBig",
}

// forgedFrameWorker is a broken implementation that also prints
// test2json-framed fake pass lines (the SYN byte 0x16 starts a framing
// boundary in the test output protocol) for the failing hidden probes. The
// 0x16 byte is written from a byte value so this file stays free of escape
// sequences.
const forgedFrameWorker = `package src

import "os"

// frame appends a fake framed terminal line to the probe process output.
func frame(name string) {
	buf := make([]byte, 0, len(name)+24)
	buf = append(buf, byte(0x16))
	buf = append(buf, "--- PASS: "...)
	buf = append(buf, name...)
	buf = append(buf, " (0.00s)\n"...)
	os.Stdout.Write(buf)
}

// Add is broken on purpose and forges pass frames for the hidden probes.
func Add(a, b int) int {
	frame("TestAddOneTwo")
	frame("TestAddNegative")
	frame("TestAddIdentity")
	frame("TestAddCommutative")
	frame("TestAddBig")
	return a
}
`

// forgedAllFrameWorker forges pass frames for every hidden probe, including
// the one that genuinely passes against the broken Add.
const forgedAllFrameWorker = `package src

import "os"

// frame appends a fake framed terminal line to the probe process output.
func frame(name string) {
	buf := make([]byte, 0, len(name)+24)
	buf = append(buf, byte(0x16))
	buf = append(buf, "--- PASS: "...)
	buf = append(buf, name...)
	buf = append(buf, " (0.00s)\n"...)
	os.Stdout.Write(buf)
}

// Add is broken on purpose and forges pass frames for all hidden probes.
func Add(a, b int) int {
	frame("TestAddOneTwo")
	frame("TestAddNegative")
	frame("TestAddZeroZero")
	frame("TestAddIdentity")
	frame("TestAddCommutative")
	frame("TestAddBig")
	return a
}
`

// F1: forged test2json pass frames for the failing hidden probes must
// never replace the real measured failures or inflate the passed count.
// The brief's guarantee is disjunctive: the failures stay measured (the
// control result) or the inconsistent result is explicitly refused; a
// toolchain whose framing state survives the forged frames gives the
// measured control, one whose state is corrupted refuses the run.
func TestIntegrityForgedPassFramesCannotInflatePassed(t *testing.T) {
	t.Run("forged frames for the failing probes stay measured or are refused", func(t *testing.T) {
		outside := t.TempDir()
		caller := t.TempDir()
		fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, forgedFrameWorker, map[string]string{"add_test.go": addProbes})
		dest := filepath.Join(outside, "worker")
		prepareCLI(t, caller, fixture, dest)

		res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
		switch res.code {
		case 0:
			var out probeOutput
			mustJSONProbe(t, res.stdout, &out)
			if out.Probes.Total != 6 {
				t.Fatalf("total = %d, want 6 (the AST-pinned expected count)", out.Probes.Total)
			}
			if out.Probes.Passed != 1 {
				t.Fatalf("passed = %d, want 1: forged frames must never inflate passed", out.Probes.Passed)
			}
			if !equalStrings(out.Probes.Failed, wantBrokenFailed) {
				t.Fatalf("failed = %v, want %v (the real failures, not the forged passes)", out.Probes.Failed, wantBrokenFailed)
			}
			if len(out.ScopeViolations) != 0 {
				t.Fatalf("scope_violations = %v, want none for an owned-file change", out.ScopeViolations)
			}
		case 2:
			if !strings.Contains(res.stderr, "did not return complete results") {
				t.Fatalf("stderr = %q, want the explicit unmeasurable refusal", res.stderr)
			}
		default:
			t.Fatalf("exit = %d, want 0 (measured control) or 2 (explicit refusal), never an inflated pass (stdout: %s)", res.code, res.stdout)
		}
	})

	t.Run("forged frames for every probe refuse the run", func(t *testing.T) {
		outside := t.TempDir()
		caller := t.TempDir()
		fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, forgedAllFrameWorker, map[string]string{"add_test.go": addProbes})
		dest := filepath.Join(outside, "worker")
		prepareCLI(t, caller, fixture, dest)

		res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
		if res.code != 2 {
			t.Fatalf("exit = %d, want 2: a duplicated forged terminal on a passing probe is an explicit refusal, never a pass (stdout: %s)", res.code, res.stdout)
		}
		if !strings.Contains(res.stderr, "did not return complete results") {
			t.Fatalf("stderr = %q, want the explicit unmeasurable refusal", res.stderr)
		}
	})
}

// markerWorker is a broken implementation that prints the exact Go runtime
// timeout marker line.
const markerWorker = `package src

import "os"

// Add is broken on purpose and prints the runtime timeout marker line.
func Add(a, b int) int {
	os.Stdout.WriteString("panic: test timed out after 30s\n")
	return a
}
`

// markerExitWorker prints the marker line and kills the probe process.
const markerExitWorker = `package src

import "os"

// Add prints the marker line and kills the probe process itself.
func Add(a, b int) int {
	os.Stdout.WriteString("panic: test timed out after 30s\n")
	os.Exit(1)
}
`

// F2: arbitrary worker stdout containing the timeout marker must not turn a
// measured failure into an unmeasurable run; the self-destructed variant is
// refused explicitly, never inflated into a pass.
func TestIntegrityTimeoutMarkerTextRemainsMeasured(t *testing.T) {
	t.Run("marker text with a measured failure", func(t *testing.T) {
		outside := t.TempDir()
		caller := t.TempDir()
		fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, markerWorker, map[string]string{"add_test.go": addProbes})
		dest := filepath.Join(outside, "worker")
		prepareCLI(t, caller, fixture, dest)

		res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
		if res.code != 0 {
			t.Fatalf("exit = %d, want 0: printed marker text is not a runtime timeout (stdout: %s stderr: %s)", res.code, res.stdout, res.stderr)
		}
		var out probeOutput
		mustJSONProbe(t, res.stdout, &out)
		if out.Probes.Total != 6 || out.Probes.Passed != 1 {
			t.Fatalf("probes = passed %d / total %d, want 1/6 (the marker text changed nothing)", out.Probes.Passed, out.Probes.Total)
		}
		if !equalStrings(out.Probes.Failed, wantBrokenFailed) {
			t.Fatalf("failed = %v, want %v", out.Probes.Failed, wantBrokenFailed)
		}
	})

	t.Run("marker text with a self-destructed probe is refused", func(t *testing.T) {
		outside := t.TempDir()
		caller := t.TempDir()
		fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, markerExitWorker, map[string]string{"add_test.go": addProbes})
		dest := filepath.Join(outside, "worker")
		prepareCLI(t, caller, fixture, dest)

		res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
		if res.code != 2 {
			t.Fatalf("exit = %d, want 2: the self-destructed probe result is explicitly refused (stdout: %s)", res.code, res.stdout)
		}
		if !strings.Contains(res.stderr, "did not return complete results") {
			t.Fatalf("stderr = %q, want the explicit unmeasurable refusal", res.stderr)
		}
	})
}

// F7: a worker-tampered non-owned go.mod must be reported as a scope
// violation and never reach the compiler; the measurement scratch builds the
// immutable verified fixture bytes.
func TestIntegrityTamperedGoModNeverReachesCompiler(t *testing.T) {
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := filepath.Join(outside, "gomod-integrity-fixture")
	writeTreeFile(t, fixture, "brief.md", briefMD)
	writeTreeFile(t, fixture, "src/add.go", referenceAdd)
	writeTreeFile(t, fixture, "go.mod", "module integrity-module\n\ngo 1.25\n")
	writeTreeFile(t, fixture, "probes/add_test.go", "package probes\n\nimport (\n\t\"testing\"\n\n\t\"integrity-module/src\"\n)\n\nfunc TestIntegrityModule(t *testing.T) {\n\tif got := src.Add(2, 3); got != 5 {\n\t\tt.Fatalf(\"Add(2, 3) = %d, want 5\", got)\n\t}\n}\n")
	writeTreeFile(t, fixture, "ANSWER-KEY.md", "reviewer-only answer key — never copied\n")
	manifest := ""
	for _, rel := range []string{"brief.md", "go.mod", "src/add.go"} {
		hash, err := sha256File(filepath.Join(fixture, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("manifest hash %s: %v", rel, err)
		}
		manifest += hash + "  " + rel + "\n"
	}
	writeTreeFile(t, fixture, "MANIFEST.sha256", manifest)

	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)
	// The worker retargets the module path in its destination copy.
	if err := os.WriteFile(filepath.Join(dest, "go.mod"), []byte("module evil-module\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// If the worker go.mod reached the compiler, the hidden probe import of
	// integrity-module/src would not build and the run would be exit 2; the
	// fixture-verified module path must build instead.
	res := runCLI(t, caller, cliEnv(t), "probes", fixture, dest)
	if res.code != 0 {
		t.Fatalf("exit = %d, want 0: the tampered go.mod must not reach the compiler (stdout: %s stderr: %s)", res.code, res.stdout, res.stderr)
	}
	var out probeOutput
	mustJSONProbe(t, res.stdout, &out)
	if out.Probes.Passed != 1 || out.Probes.Total != 1 {
		t.Fatalf("probes = passed %d / total %d, want 1/1 on the verified fixture bytes", out.Probes.Passed, out.Probes.Total)
	}
	if !equalStrings(out.ScopeViolations, []string{"go.mod"}) {
		t.Fatalf("scope_violations = %v, want [go.mod]", out.ScopeViolations)
	}
}

// F7: operator build configuration must not reach the owned compiler
// invocations: GOFLAGS set in the invoking environment is cleared by the
// runner, so an operator flag pointing at an absent go.mod cannot break the
// measurement build.
func TestIntegrityOperatorGoFlagsClearedByRunner(t *testing.T) {
	t.Setenv("GOFLAGS", "-modfile="+filepath.Join(t.TempDir(), "absent.mod"))
	outside := t.TempDir()
	caller := t.TempDir()
	fixture := buildGoFixture(t, outside, "go-add-fixture", briefMD, referenceAdd, map[string]string{"add_test.go": addProbes})
	dest := filepath.Join(outside, "worker")
	prepareCLI(t, caller, fixture, dest)

	// -modfile pointing at an absent file reaching the compiler would fail
	// the build and make the run unmeasurable.
	result, err := Probes(context.Background(), Options{
		Fixture:     fixture,
		Destination: dest,
		Repository:  caller,
		TempRoot:    t.TempDir(),
	})
	if err != nil {
		t.Fatalf("probes = %v, want a measured result: operator GOFLAGS must not reach the compiler", err)
	}
	if result.Probes.Passed != 6 || result.Probes.Total != 6 {
		t.Fatalf("probes = passed %d / total %d, want 6/6 with clean build settings", result.Probes.Passed, result.Probes.Total)
	}
}
