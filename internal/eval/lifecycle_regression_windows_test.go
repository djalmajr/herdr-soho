//go:build windows

package eval

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// withJobFailure installs the fail-closed containment seams with the exact
// production signatures: job creation and/or assignment fail
// deterministically before the suspended root is resumed. These replace the
// earlier fault injection through unrelated DLL procedures.
func withJobFailure(t *testing.T, failCreate, failAssign bool) {
	t.Helper()
	origCreate, origAssign := winCreateOwnedJob, winAssignOwnedJobProcess
	t.Cleanup(func() {
		winCreateOwnedJob, winAssignOwnedJobProcess = origCreate, origAssign
	})
	if failCreate {
		winCreateOwnedJob = func() (syscall.Handle, error) {
			return 0, errors.New("injected job creation failure")
		}
	}
	if failAssign {
		winAssignOwnedJobProcess = func(job, process syscall.Handle) error {
			return errors.New("injected job assignment failure")
		}
	}
}

// lifecycleTestEvent is the subset of the REAL go tool test2json event
// schema (cmd/test2json TestEvent: Action, Package, Test, Elapsed, Output,
// OutputType) that the green-run assertions decode: the stream is verified
// against the actual emitter (locally probed), never against fabricated
// fields.
type lifecycleTestEvent struct {
	Action     string  `json:"Action"`
	Package    string  `json:"Package"`
	Test       string  `json:"Test"`
	Elapsed    float64 `json:"Elapsed"`
	Output     string  `json:"Output"`
	OutputType string  `json:"OutputType"`
}

// lifecycleFixtureBinary is the persistent fixture binary path the
// production consumer shape points at: lifecycleCompiledFixtureBinary
// creates it; the refusal tests point at the absent path by design — the
// refusal happens before resume, so the suspended root never runs it.
func lifecycleFixtureBinary(scratch string) string {
	return filepath.Join(scratch, ".eval-tmp", "lifecyclefixture.test.exe")
}

// lifecycleRunArgs is the production consumer's owned-run shape: the
// trusted native `go tool test2json <binary>` with explicit test flags
// (anchored single test, no cache, bounded test timeout) — the same
// composition probes.go runGoTests uses for each expected probe.
func lifecycleRunArgs(binary string) []string {
	return []string{
		"tool", "test2json", binary,
		"-test.run", "^TestLifecycleParent$",
		"-test.count", "1",
		"-test.v",
		"-test.timeout", "2m",
	}
}

// assertNoOwnedCaptureFiles proves the runner removed its owned output
// capture files (the scratch must be left with no helper files behind).
func assertNoOwnedCaptureFiles(t *testing.T, scratch string) {
	t.Helper()
	for _, name := range []string{winStdoutFileName, winStderrFileName} {
		if _, statErr := os.Stat(filepath.Join(scratch, name)); !os.IsNotExist(statErr) {
			t.Fatalf("owned capture file %s left behind in %s: %v", name, scratch, statErr)
		}
	}
}

// F-4: when the job with KILL_ON_JOB_CLOSE cannot be created, the run must
// fail closed BEFORE resuming the suspended process — no silent fallback to
// an uncontained tree, no execution marker, scratch removable. The command
// line uses the production consumer shape (go tool test2json <binary>) with
// the would-be binary path; the refusal precedes resume, so the suspended
// root never executes and the missing binary is never reached.
func TestLifecycleWindowsJobCreateFailureRefusesBeforeResume(t *testing.T) {
	scratch := lifecycleScratch(t)
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), filepath.Join(scratch, "child.pid"), marker, false, true)

	withJobFailure(t, true, false)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(lifecycleFixtureBinary(scratch)), env, stdout, stderr)
	if err == nil {
		t.Fatal("want the fail-closed refusal when the job cannot be created")
	}
	if !strings.Contains(err.Error(), "refusing to run without containment") {
		t.Fatalf("err = %v, want the pre-resume containment refusal", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("the suspended process ran despite the pre-resume refusal: the marker exists")
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the refusal: %v", err)
	}
}

// F-4: the same fail-closed contract for an assignment failure.
func TestLifecycleWindowsJobAssignFailureRefusesBeforeResume(t *testing.T) {
	scratch := lifecycleScratch(t)
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), filepath.Join(scratch, "child.pid"), marker, false, true)

	withJobFailure(t, false, true)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(lifecycleFixtureBinary(scratch)), env, stdout, stderr)
	if err == nil {
		t.Fatal("want the fail-closed refusal when the assign fails")
	}
	if !strings.Contains(err.Error(), "refusing to run without containment") {
		t.Fatalf("err = %v, want the pre-resume containment refusal", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatal("the suspended process ran despite the pre-resume refusal: the marker exists")
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the refusal: %v", err)
	}
}

// F-5: a green run that leaves a detached child executing the persistent
// fixture binary must return only after the owned job is drained (the
// ActiveProcesses==0 proof) — the runner-owned capture files are removed and
// the scratch is left with no helper files. On Windows a directory with a
// live process's cwd (or open file) inside it cannot be deleted, so a
// removable scratch after the return is the observable proof that the
// job's tree is gone before the caller cleans up. The go-test-run shape
// that failed natively (unlinking the throwaway test exe while the child
// still runs it) is gone: the child executes the persistent binary the
// production consumer shape compiled, which is never unlinked under it.
func TestLifecycleWindowsJobCleanupGreenRun(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	// Preparation BEFORE the run: compile the persistent fixture binary
	// the way the production consumer does (no unlink collision under the
	// detached child).
	binary := lifecycleCompiledFixtureBinary(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, false, false)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(binary), env, stdout, stderr)
	if err != nil {
		t.Fatalf("the fixture is a green run; runOwnedGoProcess = %v (stdout %q, stderr %q)", err, stdout.String(), stderr.String())
	}
	// Exact stdout/stderr separation and content: decode the REAL test2json
	// event stream (verified schema, no fabricated fields) and assert the
	// anchored test pass plus the terminal package pass; the green run
	// writes nothing to the stderr capture.
	var events []lifecycleTestEvent
	dec := json.NewDecoder(strings.NewReader(stdout.String()))
	for {
		var ev lifecycleTestEvent
		if err := dec.Decode(&ev); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("the owned stdout capture is not a JSON event stream: %v: %q", err, stdout.String())
		}
		events = append(events, ev)
	}
	if len(events) == 0 {
		t.Fatalf("the owned stdout capture is empty: %q", stdout.String())
	}
	if events[0].Action != "start" {
		t.Fatalf("the first test2json event is not start: %+v", events[0])
	}
	var sawRun, sawTestPass bool
	for _, ev := range events {
		if ev.Action == "run" && ev.Test == "TestLifecycleParent" {
			sawRun = true
		}
		if ev.Action == "pass" && ev.Test == "TestLifecycleParent" {
			sawTestPass = true
		}
	}
	if !sawRun {
		t.Fatalf("the anchored test run event is missing from the owned stdout capture: %q", stdout.String())
	}
	if !sawTestPass {
		t.Fatalf("the anchored test pass event is missing from the owned stdout capture: %q", stdout.String())
	}
	last := events[len(events)-1]
	if last.Action != "pass" || last.Test != "" {
		t.Fatalf("the terminal test2json event is not the package pass (Action pass, no Test): %+v (full stream %q)", last, stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("the green run wrote to the stderr capture: %q", stderr.String())
	}
	// The real test generation ran (the marker is written only by it).
	if data, statErr := os.ReadFile(marker); statErr != nil || string(data) != "ran" {
		t.Fatalf("the fixture marker is missing or wrong: %v %q", statErr, data)
	}
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Fatalf("detached child pid file missing: the fixture never reached the child")
	}
	if !waitWhile(5*time.Second, func() bool { return processAlive(pid) }) {
		t.Fatalf("detached child %d still holds the job after the green return (F-5)", pid)
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the green return (F-5): %v", err)
	}
}

// F-4/F-5 cancellation: a cancel of a running owned job must terminate the
// whole job, bound the drain (no INFINITE wait), and leave the detached
// child gone and the scratch removable. The preparation (persistent
// binary compile) occurs BEFORE the cancel elapsed/PID-ready timeout
// starts, so the 60s assertion measures only the run, the cancel and the
// bounded drain — never a cold build.
func TestLifecycleWindowsCancelKillsOwnedJob(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	// Preparation BEFORE the cancel elapsed/PID-ready timeout starts.
	binary := lifecycleCompiledFixtureBinary(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, true, false)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			// Cancel only when the child is provably ALIVE (pid file AND the
			// child-written ready marker), not on a Start-derived PID alone.
			if readPIDFile(pidFile) > 0 {
				if _, err := os.Stat(marker + "-child-ready"); err == nil {
					time.Sleep(300 * time.Millisecond)
					cancel()
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		cancel()
	}()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	start := time.Now()
	err := runOwnedGoProcess(ctx, lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(binary), env, stdout, stderr)
	if err == nil {
		t.Fatal("want the cancellation to surface as an owned-process error")
	}
	if elapsed := time.Since(start); elapsed > 60*time.Second {
		t.Fatalf("cancellation took %s: the bounded drain must not wait indefinitely (F-4)", elapsed)
	}
	// Capture on the killed path: the caller's stdout buffer is written
	// before the return and holds the test2json run event; the stderr
	// capture stays separate.
	if !strings.Contains(stdout.String(), `"Action":"run"`) {
		t.Fatalf("the killed-run stdout capture lost the test2json run event: %q", stdout.String())
	}
	if strings.Contains(stderr.String(), `"Action":"run"`) {
		t.Fatalf("the test2json stream leaked into the stderr capture: %q", stderr.String())
	}
	pid := readPIDFile(pidFile)
	if pid <= 0 {
		t.Fatalf("detached child pid file missing: the cancel never fired mid-test")
	}
	if !waitWhile(5*time.Second, func() bool { return processAlive(pid) }) {
		t.Fatalf("detached child %d still alive after the bounded cancellation (F-4/F-5)", pid)
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the cancellation (F-5): %v", err)
	}
}

// ABI: a green run must actually reach startup through the native command
// line and environment block, and the child must observe EXACTLY the
// requested argv and environment entries. The hostile argument set
// exercises every quoting branch: a plain token, spaces, tabs, interior
// backslashes, a trailing backslash (MSYS/Windows path form), embedded
// quotes, a trailing quote, a lone backslash, and the empty argument. The
// environment block is explicit (CREATE_UNICODE_ENVIRONMENT), so the child
// sees exactly the requested entries — nothing inherited, nothing dropped.
func TestLifecycleWindowsStartupEnvAndArgs(t *testing.T) {
	scratch := lifecycleScratch(t)
	echoBin := lifecycleEchoBinary(t, scratch)
	echoFile := filepath.Join(scratch, "echo.txt")
	args := []string{
		"plain",
		"a b",
		"tab\there",
		`back\slash`,
		`C:\temp\`,
		`he said "hi"`,
		`end"`,
		`\`,
		"",
	}
	env := []string{
		"PATH=" + filepath.Dir(echoBin),
		"GOMAXPROCS=1",
		`LIFECYCLE_A=a=b c`,
		`LIFECYCLE_B=say "q"`,
		`LIFECYCLE_C=C:\x\`,
		"HERDR_EVAL_LIFECYCLE_ECHO=" + echoFile,
	}

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), echoBin, scratch, args, env, stdout, stderr)
	if err != nil {
		t.Fatalf("the echo run is green; runOwnedGoProcess = %v (stderr %q)", err, stderr.String())
	}
	data, err := os.ReadFile(echoFile)
	if err != nil {
		t.Fatalf("the echo binary never ran (no output file): %v", err)
	}
	lines := strings.Split(string(data), "\n")
	if lines[len(lines)-1] != "" {
		t.Fatalf("the echo output must end with a newline: %q", data)
	}
	lines = lines[:len(lines)-1]
	var gotArgs []string
	var gotEnv []string
	for _, line := range lines {
		kind, value, ok := strings.Cut(line, "\t")
		if !ok {
			t.Fatalf("malformed echo line %q", line)
		}
		switch kind {
		case "ARG":
			gotArgs = append(gotArgs, value)
		case "ENV":
			gotEnv = append(gotEnv, value)
		default:
			t.Fatalf("unknown echo line %q", line)
		}
	}
	// argv: the executable plus exactly the hostile arguments, in order.
	if len(gotArgs) != len(args)+1 {
		t.Fatalf("argv length = %d (%v), want %d", len(gotArgs), gotArgs, len(args)+1)
	}
	for i, want := range args {
		if gotArgs[i+1] != want {
			t.Fatalf("argv[%d] = %q, want %q (the command line quoting broke the argument)", i+1, gotArgs[i+1], want)
		}
	}
	// env: exactly the requested entries with their exact values.
	wantEnv := make(map[string]string, len(env))
	for _, e := range env {
		k, v, _ := strings.Cut(e, "=")
		wantEnv[k] = v
	}
	if len(gotEnv) != len(wantEnv) {
		t.Fatalf("env length = %d (%v), want %d (%v)", len(gotEnv), gotEnv, len(wantEnv), wantEnv)
	}
	for _, e := range gotEnv {
		k, v, ok := strings.Cut(e, "=")
		if !ok {
			t.Fatalf("malformed env entry %q", e)
		}
		if want, present := wantEnv[k]; !present || want != v {
			t.Fatalf("env %q = %q, want %q (the environment block is wrong)", k, v, want)
		}
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the green echo run: %v", err)
	}
}

// The run must leave no helper goroutines and no helper files behind: the
// file capture is synchronous after the drain proof (no copy goroutines
// exist to outlive the return), and the owned capture files are removed on
// the success path.
func TestLifecycleWindowsNoHelperGoroutinesAndFilesAfterReturn(t *testing.T) {
	scratch := lifecycleScratch(t)
	pidFile := filepath.Join(scratch, "child.pid")
	marker := filepath.Join(scratch, "marker")
	writeLifecycleFixture(t, scratch)
	binary := lifecycleCompiledFixtureBinary(t, scratch)
	env := lifecycleEnv(lifecycleScratchEnv(scratch), pidFile, marker, false, false)

	before := runtime.NumGoroutine()
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	if err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(binary), env, stdout, stderr); err != nil {
		t.Fatalf("the fixture is a green run; runOwnedGoProcess = %v (stderr %q)", err, stderr.String())
	}
	// Let any forbidden lingering goroutine surface itself.
	time.Sleep(250 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("runOwnedGoProcess leaked %d goroutine(s) (before %d, after %d)", after-before, before, after)
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the green return: %v", err)
	}
}

// Control: an early capture-create failure is returned (not a silent
// fallback) and the central owning cleanup leaves no owned file behind:
// pre-creating the stdout capture name as a directory makes the runner's
// OpenFile fail before CreateProcess, after the first create boundary is
// passed by exactly this failing create. The stderr capture file must not
// exist and no helper goroutine may survive.
func TestLifecycleWindowsEarlyCaptureCreateFailureCleansUp(t *testing.T) {
	scratch := lifecycleScratch(t)
	writeLifecycleFixture(t, scratch)
	if err := os.Mkdir(filepath.Join(scratch, winStdoutFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	before := runtime.NumGoroutine()
	err := runOwnedGoProcess(context.Background(), lifecycleGoExecutable(t), scratch,
		lifecycleRunArgs(lifecycleFixtureBinary(scratch)), lifecycleScratchEnv(scratch), stdout, stderr)
	if err == nil {
		t.Fatal("want the honest capture-create failure")
	}
	if !strings.Contains(err.Error(), "create stdout capture file") {
		t.Fatalf("err = %v, want the capture-create refusal", err)
	}
	if _, statErr := os.Stat(filepath.Join(scratch, winStderrFileName)); !os.IsNotExist(statErr) {
		t.Fatalf("the stderr capture file was left behind: %v", statErr)
	}
	if info, statErr := os.Stat(filepath.Join(scratch, winStdoutFileName)); statErr != nil || !info.IsDir() {
		t.Fatalf("capture cleanup removed the pre-existing directory: %v", statErr)
	}
	time.Sleep(100 * time.Millisecond)
	if after := runtime.NumGoroutine(); after > before {
		t.Fatalf("the early failure leaked %d goroutine(s) (before %d, after %d)", after-before, before, after)
	}
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the early capture-create failure: %v", err)
	}
}

// failingWriter refuses every write with the given error: the bounded
// consumer boundary for the capture-time writer-refusal control.
type failingWriter struct{ err error }

func (w *failingWriter) Write(p []byte) (int, error) { return 0, w.err }

type lifecycleShortWriter struct{}

func (lifecycleShortWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestLifecycleWindowsCaptureOutputRejectsShortWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	if err := os.WriteFile(path, []byte("captured output"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := captureOneOutput(path, lifecycleShortWriter{}); err == nil || !strings.Contains(err.Error(), "short write") {
		t.Fatalf("partial output accepted: %v", err)
	}
}

// Control: an injected job drain failure and a capture-time writer refusal
// are COMBINED in the returned error, not one masking the other (the jobErr
// branch must join the capture error): the echo binary produces a small
// (in-cap) capture, so the writer refusal — not a size overflow — is the
// capture error. The owned capture files are cleaned up on the error path.
func TestLifecycleWindowsCombinedJobAndCaptureErrors(t *testing.T) {
	scratch := lifecycleScratch(t)
	echoBin := lifecycleEchoBinary(t, scratch)
	echoFile := filepath.Join(scratch, "echo.txt")
	env := []string{
		"PATH=" + filepath.Dir(echoBin),
		"HERDR_EVAL_LIFECYCLE_ECHO=" + echoFile,
	}
	origQuery := winQueryOwnedJobAccounting
	t.Cleanup(func() { winQueryOwnedJobAccounting = origQuery })
	winQueryOwnedJobAccounting = func(job syscall.Handle) (winJobObjectBasicAccountingInformation, error) {
		return winJobObjectBasicAccountingInformation{}, errors.New("injected query failure (combined control)")
	}
	refuse := errors.New("injected bounded consumer refusal")
	stdout, stderr := &failingWriter{err: refuse}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), echoBin, scratch, []string{echoBin, "one", "two"}, env, stdout, stderr)
	if err == nil {
		t.Fatal("want the combined job and capture error")
	}
	if !strings.Contains(err.Error(), "injected query failure (combined control)") {
		t.Fatalf("the job drain error is missing from the combined error: %v", err)
	}
	if !strings.Contains(err.Error(), "injected bounded consumer refusal") {
		t.Fatalf("the capture writer error was discarded by the jobErr branch: %v", err)
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the combined error: %v", err)
	}
}

// lifecycleOverflowSource is the bounded-refusal probe: it holds a scratch
// file open (so a removable scratch is proof the job terminated it),
// writes past the tightly documented disk allowance (21 MiB > 2 x
// boundedOutputCap = 20 MiB), and then stays alive — the runner must
// detect the overflow while the process is running, refuse the run
// honestly, and terminate the whole job.
const lifecycleOverflowSource = `package main

import (
	"os"
	"time"
)

func main() {
	var held *os.File
	if h := os.Getenv("HERDR_EVAL_LIFECYCLE_OVERFLOW_HELD"); h != "" {
		f, err := os.OpenFile(h, os.O_CREATE|os.O_WRONLY, 0o644)
		if err != nil {
			os.Exit(1)
		}
		held = f
	}
	chunk := make([]byte, 1<<20)
	total := 0
	for total < 21<<20 {
		n, err := os.Stdout.Write(chunk)
		total += n
		if err != nil {
			return
		}
	}
	if held != nil {
		_ = held
		// A live sleeping loop (never select{}): the overflow holder must
		// stay alive across the job kill — a self-deadlocking holder would
		// let the scratch removal pass without proving the job killed it.
		for {
			time.Sleep(30 * time.Second)
		}
	}
}
`

// lifecycleOverflowBinary builds the bounded-refusal probe inside the
// scratch (hermetic: toolchain-local, proxy off, the runner-owned GOCACHE
// selection of lifecycleBuildEnv).
func lifecycleOverflowBinary(t *testing.T, scratch string) string {
	t.Helper()
	dir := filepath.Join(scratch, "overflow")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module lifecycleoverflow\n\ngo 1.25\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(lifecycleOverflowSource), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "overflow.exe")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	// Owned process cleanup: the explicit bounded context kills the build
	// on deadline (exec.CommandContext), and WaitDelay bounds the wait
	// after the kill — never an unbounded wait.
	cmd := exec.CommandContext(ctx, lifecycleGoExecutable(t), "build", "-o", bin, ".")
	cmd.Dir = dir
	cmd.Env = lifecycleBuildEnv(scratch)
	cmd.WaitDelay = 30 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the overflow probe: %v: %s", err, out)
	}
	return bin
}

// Output overflow: a process that writes past the tightly documented disk
// allowance (2 x boundedOutputCap) must produce an honest bounded refusal
// (never a truncated-but-scored pass); the overflow process held the
// scratch (open file + cwd), so a removable scratch after the refusal
// proves the job terminated the whole tree. The owned capture files are
// removed on the refusal path.
func TestLifecycleWindowsOutputOverflowBoundedRefusal(t *testing.T) {
	scratch := lifecycleScratch(t)
	bin := lifecycleOverflowBinary(t, scratch)
	held := filepath.Join(scratch, "overflow-held")
	env := append(lifecycleScratchEnv(scratch), "HERDR_EVAL_LIFECYCLE_OVERFLOW_HELD="+held)

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	err := runOwnedGoProcess(context.Background(), bin, scratch, []string{bin}, env, stdout, stderr)
	if err == nil {
		t.Fatal("want the honest bounded refusal for oversized output")
	}
	if !strings.Contains(err.Error(), "output exceeded the bounded disk cap") {
		t.Fatalf("err = %v, want the bounded overflow refusal", err)
	}
	// Combined job/capture error control: the injected query failure (the
	// drain error) and the capture overflow refusal are BOTH returned —
	// the jobErr branch joins the capture error instead of discarding it.
	origQuery := winQueryOwnedJobAccounting
	t.Cleanup(func() { winQueryOwnedJobAccounting = origQuery })
	winQueryOwnedJobAccounting = func(job syscall.Handle) (winJobObjectBasicAccountingInformation, error) {
		return winJobObjectBasicAccountingInformation{}, errors.New("injected query failure (overflow combined control)")
	}
	stdout2, stderr2 := &bytes.Buffer{}, &bytes.Buffer{}
	err2 := runOwnedGoProcess(context.Background(), bin, scratch, []string{bin}, env, stdout2, stderr2)
	if err2 == nil {
		t.Fatal("want the combined job and capture error on the overflow path")
	}
	if !strings.Contains(err2.Error(), "injected query failure (overflow combined control)") {
		t.Fatalf("the job drain error is missing from the combined error: %v", err2)
	}
	if !strings.Contains(err2.Error(), "over the bounded") {
		t.Fatalf("the capture overflow refusal was discarded by the jobErr branch: %v", err2)
	}
	assertNoOwnedCaptureFiles(t, scratch)
	if err := os.RemoveAll(scratch); err != nil {
		t.Fatalf("scratch not removable after the bounded overflow refusal (job not terminated?): %v", err)
	}
}

// Native: a query on an ACTUAL empty owned job (real kernel32 job created
// by the production path, production query seam) must validate the verified
// primary class-1 contract: 48-byte layout with the returned size of 48 as
// the ABI evidence, and ActiveProcesses 0. finishOwnedJob-style single
// ownership is the norm: the test zeroes its mutable job handle after it
// is closed so the cleanup never double-closes.
func TestLifecycleWindowsEmptyOwnedJobAccountingProbe(t *testing.T) {
	if got := unsafe.Sizeof(winJobObjectBasicAccountingInformation{}); got != 48 {
		t.Fatalf("accounting struct size = %d, want 48 (verified primary layout)", got)
	}
	job, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create the real owned job: %v", err)
	}
	t.Cleanup(func() {
		if job != 0 {
			_ = syscall.CloseHandle(job)
		}
	})
	info, err := winQueryOwnedJobAccounting(job)
	if err != nil {
		t.Fatalf("query the empty owned job failed: %v", err)
	}
	if info.ActiveProcesses != 0 {
		t.Fatalf("ActiveProcesses = %d, want 0 on the empty owned job", info.ActiveProcesses)
	}
	if info.TotalProcesses != 0 {
		t.Fatalf("TotalProcesses = %d, want 0 on the empty owned job", info.TotalProcesses)
	}
	if err := terminateOwnedJob(job); err != nil {
		t.Fatalf("terminate the empty job: %v", err)
	}
	_ = syscall.CloseHandle(job)
	job = 0
}

// Native: a STABLE accounting snapshot with ActiveProcesses > 0 must NOT be
// accepted as drained (a sleeping live process has unchanged counters): the
// drain must run to the bounded deadline and report the timeout. The
// positive control proves ActiveProcesses == 0 is the accepted proof.
// finishOwnedJob owns the job close on every path; the test zeroes its
// mutable job handle before cleanup/assertions.
func TestLifecycleWindowsStableSnapshotWithActiveProcessIsNotDrained(t *testing.T) {
	origQuery := winQueryOwnedJobAccounting
	t.Cleanup(func() { winQueryOwnedJobAccounting = origQuery })

	// Stable snapshot, one active process: never drained.
	job, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create the real owned job: %v", err)
	}
	t.Cleanup(func() {
		if job != 0 {
			_ = syscall.CloseHandle(job)
		}
	})
	winQueryOwnedJobAccounting = func(job syscall.Handle) (winJobObjectBasicAccountingInformation, error) {
		return winJobObjectBasicAccountingInformation{TotalProcesses: 1, ActiveProcesses: 1}, nil
	}
	start := time.Now()
	err = finishOwnedJob(job) // finishOwnedJob owns the job close on every path
	job = 0
	if err == nil {
		t.Fatal("a stable snapshot with ActiveProcesses=1 was accepted as drained: want the timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, want the bounded drain timeout", err)
	}
	if elapsed := time.Since(start); elapsed < winJobDrainTimeout-2*time.Second {
		t.Fatalf("drain gave up after %s: the stable Active>0 snapshot must run to the bounded deadline (%s)", elapsed, winJobDrainTimeout)
	}

	// Positive control: ActiveProcesses == 0 is the drain proof.
	job2, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create the real owned job: %v", err)
	}
	t.Cleanup(func() {
		if job2 != 0 {
			_ = syscall.CloseHandle(job2)
		}
	})
	winQueryOwnedJobAccounting = func(job syscall.Handle) (winJobObjectBasicAccountingInformation, error) {
		return winJobObjectBasicAccountingInformation{}, nil
	}
	if err := finishOwnedJob(job2); err != nil {
		t.Fatalf("ActiveProcesses=0 must be accepted as drained: %v", err)
	}
	job2 = 0
}

// Native: termination and query failures are returned as real errors, not
// declared successful cleanup. finishOwnedJob owns the job close on every
// path; the test zeroes its mutable job handle before cleanup/assertions.
func TestLifecycleWindowsQueryAndTerminateFailuresPropagate(t *testing.T) {
	origQuery, origTerminate := winQueryOwnedJobAccounting, winTerminateOwnedJob
	t.Cleanup(func() {
		winQueryOwnedJobAccounting, winTerminateOwnedJob = origQuery, origTerminate
	})

	job, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create the real owned job: %v", err)
	}
	t.Cleanup(func() {
		if job != 0 {
			_ = syscall.CloseHandle(job)
		}
	})
	winTerminateOwnedJob = func(job syscall.Handle) error {
		return errors.New("injected termination failure")
	}
	if err := finishOwnedJob(job); err == nil || !strings.Contains(err.Error(), "injected termination failure") {
		t.Fatalf("termination failure not propagated: %v", err)
	}
	job = 0

	job2, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create the real owned job: %v", err)
	}
	t.Cleanup(func() {
		if job2 != 0 {
			_ = syscall.CloseHandle(job2)
		}
	})
	winTerminateOwnedJob = func(job syscall.Handle) error { return nil }
	winQueryOwnedJobAccounting = func(job syscall.Handle) (winJobObjectBasicAccountingInformation, error) {
		return winJobObjectBasicAccountingInformation{}, errors.New("injected query failure")
	}
	if err := finishOwnedJob(job2); err == nil || !strings.Contains(err.Error(), "injected query failure") {
		t.Fatalf("query failure not propagated: %v", err)
	}
	job2 = 0
}
