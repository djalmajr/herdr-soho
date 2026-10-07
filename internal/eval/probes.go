package eval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ProbeSummary carries the observed probe outcome; its JSON fields mirror
// the legacy run-probes.mjs probes object.
type ProbeSummary struct {
	Passed int      `json:"passed"`
	Total  int      `json:"total"`
	Failed []string `json:"failed"`
}

// ProbeResult is the probes result object; its JSON fields mirror the legacy
// run-probes.mjs stdout payload.
type ProbeResult struct {
	Fixture         string       `json:"fixture"`
	Probes          ProbeSummary `json:"probes"`
	ScopeViolations []string     `json:"scope_violations"`
	ManifestOK      bool         `json:"manifest_ok"`
}

const (
	// probeTestTimeout is the legacy test execution deadline passed to
	// `go test -timeout`.
	probeTestTimeout = "30s"
	// probeProcessLimit bounds the whole measurement (the probe package
	// compilation plus every anchored probe execution) so a wedged toolchain
	// cannot hang the runner.
	probeProcessLimit = 2 * time.Minute
	// boundedOutputCap mirrors the legacy maxBuffer: captured process output
	// beyond this size is dropped instead of growing memory without bound.
	boundedOutputCap = 10 << 20
	// diagnosticTailLimit bounds how much runner output a failure message
	// carries.
	diagnosticTailLimit = 4096
)

// defaultModuleRE matches fixture directory names that are safe as the
// default module path of the generated go.mod.
var defaultModuleRE = regexp.MustCompile(`^[A-Za-z0-9._~-]+$`)

// Probes validates the fixture manifest and the prepare metadata, computes
// scope violations against the completed worker destination, then measures
// the worker source in a fresh owned temporary directory with the fixture's
// hidden Go tests. The worker copy is never modified and the temporary
// directory is removed on every path.
func Probes(ctx context.Context, o Options) (ProbeResult, error) {
	if err := ctx.Err(); err != nil {
		return ProbeResult{}, err
	}
	fixture, err := readFixtureManifest(o.Fixture)
	if err != nil {
		return ProbeResult{}, err
	}
	repository, err := resolveRepository(o.Repository)
	if err != nil {
		return ProbeResult{}, err
	}
	dest, err := checkDestination(repository, o.Destination, true)
	if err != nil {
		return ProbeResult{}, err
	}
	if err := checkPrepareMetadata(dest, filepath.Base(fixture.Root)); err != nil {
		return ProbeResult{}, err
	}
	probeFiles, err := listProbeFiles(filepath.Join(fixture.Root, "probes"))
	if err != nil {
		return ProbeResult{}, err
	}
	if len(probeFiles) == 0 {
		return ProbeResult{}, fmt.Errorf("fixture has no hidden *_test.go probes")
	}
	owned, err := ownedPaths(filepath.Join(fixture.Root, "brief.md"))
	if err != nil {
		return ProbeResult{}, err
	}
	worker, err := workerFiles(dest)
	if err != nil {
		return ProbeResult{}, err
	}
	scopeViolations := compareScope(dest, fixture.Manifest, owned, worker)
	summary, err := runHiddenProbes(ctx, o, fixture, dest, worker, probeFiles, owned)
	if err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{
		Fixture:         filepath.Base(fixture.Root),
		Probes:          summary,
		ScopeViolations: scopeViolations,
		ManifestOK:      true,
	}, nil
}

// checkPrepareMetadata mirrors the legacy .eval-run.json validation: the
// fixture name must match, manifest_ok must be the boolean true and
// prepared_at must be present as a string.
func checkPrepareMetadata(dest, fixtureName string) error {
	missing := fmt.Errorf("destination is missing valid prepare metadata for this fixture")
	raw, err := os.ReadFile(filepath.Join(dest, ".eval-run.json"))
	if err != nil {
		return missing
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return missing
	}
	if meta["fixture"] != fixtureName || meta["manifest_ok"] != true {
		return missing
	}
	if _, ok := meta["prepared_at"].(string); !ok {
		return missing
	}
	return nil
}

// compareScope ports the legacy scope comparison: manifest paths whose bytes
// or type changed, and added paths, count as violations unless the brief's
// Owned files list them; owned paths are never violations.
func compareScope(dest string, manifest map[string]string, owned map[string]bool, worker []WorkerEntry) []string {
	actual := make(map[string]WorkerEntry, len(worker))
	for _, entry := range worker {
		actual[entry.Relative] = entry
	}
	violations := map[string]bool{}
	for relative, expected := range manifest {
		item, present := actual[relative]
		if !present || item.Type != "file" {
			if !owned[relative] {
				violations[relative] = true
			}
			continue
		}
		hash, err := sha256File(filepath.Join(dest, filepath.FromSlash(relative)))
		if err != nil || hash != expected {
			if !owned[relative] {
				violations[relative] = true
			}
		}
	}
	for relative, item := range actual {
		_, listed := manifest[relative]
		if !listed && !owned[relative] {
			violations[relative] = true
		}
		if item.Type != "file" && listed && !owned[relative] {
			violations[relative] = true
		}
	}
	return sortedKeys(violations)
}

// listProbeFiles discovers the hidden probe tests: regular *_test.go files
// under the fixture probes tree, refusing any symlink in the tree.
func listProbeFiles(probeRoot string) ([]string, error) {
	probeStat, err := os.Stat(probeRoot)
	if err != nil || !probeStat.IsDir() {
		return nil, fmt.Errorf("probe tree is missing: %s", probeRoot)
	}
	files := []string{}
	err = walkTree(probeRoot, probeRoot, false, func(absolute, relative string, entry os.DirEntry) error {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("probe tree contains a symlink: %s", relative)
		}
		if entry.Type().IsRegular() && strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, absolute)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// runHiddenProbes copies the completed worker source and the hidden probes
// into a fresh owned temporary directory outside the worker destination,
// writes a minimal go.mod when the fixture does not supply one, and runs
// the Go test executable there. Non-owned manifest files are copied from the
// immutable verified fixture bytes (never from the worker destination), so
// a tampered toolchain or build configuration is a scope violation that must
// not reach the compiler. The worker copy is never modified and the
// temporary directory is removed on every path.
func runHiddenProbes(ctx context.Context, o Options, fixture Fixture, dest string, worker []WorkerEntry, probeFiles []string, owned map[string]bool) (ProbeSummary, error) {
	tempRoot := o.TempRoot
	if tempRoot == "" {
		tempRoot = os.TempDir()
	}
	temporary, err := os.MkdirTemp(tempRoot, "herdr-eval-probes-")
	if err != nil {
		return ProbeSummary{}, fmt.Errorf("could not create probe temporary directory: %v", err)
	}
	defer func() {
		_ = os.RemoveAll(temporary)
	}()
	tempReal, err := filepath.EvalSymlinks(temporary)
	if err != nil {
		return ProbeSummary{}, err
	}
	destReal, err := filepath.EvalSymlinks(dest)
	if err != nil {
		return ProbeSummary{}, err
	}
	if within(destReal, tempReal) {
		return ProbeSummary{}, fmt.Errorf("probe temporary directory must not be inside the worker destination")
	}
	completed := make(map[string]WorkerEntry, len(worker))
	for _, entry := range worker {
		completed[entry.Relative] = entry
	}
	for relative, expectedHash := range fixture.Manifest {
		target := filepath.Join(temporary, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return ProbeSummary{}, err
		}
		if owned[relative] {
			// The worker's own source is the measurement target: use the
			// completed worker copy.
			entry, present := completed[relative]
			if !present || entry.Type != "file" {
				// A deleted or type-changed owned path is a scope violation
				// reported above; the missing file makes the measurement
				// compile fail, which is the honest unmeasurable result.
				continue
			}
			if err := copyFile(filepath.Join(dest, filepath.FromSlash(relative)), target); err != nil {
				return ProbeSummary{}, err
			}
			continue
		}
		// Non-owned manifest files (go.mod/go.sum, brief.md, ...) use the
		// immutable verified fixture bytes, checked against the manifest
		// hash, so an altered toolchain or build configuration in the worker
		// destination never reaches the compiler.
		source := filepath.Join(fixture.Root, filepath.FromSlash(relative))
		if hash, err := sha256File(source); err != nil || hash != expectedHash {
			return ProbeSummary{}, fmt.Errorf("fixture bytes do not match the verified manifest for %s", relative)
		}
		if err := copyFile(source, target); err != nil {
			return ProbeSummary{}, err
		}
	}
	probeRoot := filepath.Join(fixture.Root, "probes")
	tempProbeFiles := make([]string, 0, len(probeFiles))
	for _, probeFile := range probeFiles {
		relative, err := filepath.Rel(probeRoot, probeFile)
		if err != nil {
			return ProbeSummary{}, err
		}
		target := filepath.Join(temporary, "probes", filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return ProbeSummary{}, err
		}
		if err := copyFile(probeFile, target); err != nil {
			return ProbeSummary{}, err
		}
		tempProbeFiles = append(tempProbeFiles, target)
	}
	if _, supplied := fixture.Manifest["go.mod"]; !supplied {
		if err := writeDefaultGoMod(temporary, fixture.Root); err != nil {
			return ProbeSummary{}, err
		}
	}
	modulePath, err := modulePathFromGoMod(filepath.Join(temporary, "go.mod"))
	if err != nil {
		return ProbeSummary{}, err
	}
	expected, err := deriveExpectedProbes(modulePath, temporary, tempProbeFiles)
	if err != nil {
		return ProbeSummary{}, err
	}
	// Owned scratch for the Go toolchain's temporary work (TMPDIR/TEMP/TMP)
	// and for the owned compiled probe binaries: cancelled go-build work and
	// the binaries are removed with the owned temporary directory.
	if err := os.MkdirAll(filepath.Join(temporary, ".eval-tmp"), 0o700); err != nil {
		return ProbeSummary{}, err
	}
	return runGoTests(ctx, o, temporary, modulePath, expected)
}

// copyFile copies src to dst, creating intermediate directories of dst only
// where the caller made them; used inside the runner-owned temporary copy.
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// writeDefaultGoMod creates the minimal module file for fixtures that do not
// supply their own go.mod. The module path is the fixture directory name
// when it is safe as an import path element, so hidden probes can import the
// worker package deterministically.
func writeDefaultGoMod(temporary, fixtureRoot string) error {
	name := filepath.Base(fixtureRoot)
	if !defaultModuleRE.MatchString(name) {
		name = "eval-fixture"
	}
	return os.WriteFile(filepath.Join(temporary, "go.mod"), []byte("module "+name+"\n\ngo 1.25\n"), 0o644)
}

// runGoTests measures the isolated copy with the hidden probes. Each hidden
// probe package is compiled once into an owned native test binary, and each
// expected top-level probe is then executed individually in its own owned
// process through the trusted native Go tool test2json framing, with an
// anchored escaped -test.run and the same bounded parent deadline and process
// containment as the legacy run. The per-probe process exit status is
// authoritative; a pass additionally requires exactly one expected probe run
// and a consistent unique terminal sequence, so duplicate, forged or
// out-of-order terminals can never produce a pass. Measured failed probes
// are a successful measurement; compiler, infrastructure, timeout and
// cancellation outcomes are errors.
func runGoTests(ctx context.Context, o Options, temporary, modulePath string, expected []expectedProbe) (ProbeSummary, error) {
	goExecutable := o.GoExecutable
	if goExecutable == "" {
		resolved, err := exec.LookPath("go")
		if err != nil {
			return ProbeSummary{}, fmt.Errorf("go toolchain not found: %v", err)
		}
		goExecutable = resolved
	} else if !filepath.IsAbs(goExecutable) {
		return ProbeSummary{}, fmt.Errorf("GoExecutable must be a trusted absolute path: %q", goExecutable)
	}
	if len(expected) == 0 {
		return ProbeSummary{}, fmt.Errorf("test runner did not return complete results (status 0)")
	}
	runCtx, cancel := context.WithTimeout(ctx, probeProcessLimit)
	defer cancel()
	env := compilerTempEnv(temporary)
	workDir := filepath.Join(temporary, ".eval-tmp")
	// Compile each probe package once into an owned native test binary. The
	// go build cache is keyed by content, so the per-probe executions never
	// rebuild the package; the owned binaries are removed with the scratch.
	binaries := map[string]string{}
	packageOrder := []string{}
	for _, probe := range expected {
		if binaries[probe.pkg] == "" {
			packageOrder = append(packageOrder, probe.pkg)
		}
	}
	for i, pkg := range packageOrder {
		dir := strings.TrimPrefix(strings.TrimPrefix(pkg, modulePath), "/")
		if dir == "" {
			dir = "."
		}
		name := fmt.Sprintf("probe-bin-%d", i)
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(workDir, name)
		args := []string{"test", "-c", "-o", binary, "./" + dir}
		stdout := newBoundedWriter(boundedOutputCap)
		stderr := newBoundedWriter(boundedOutputCap)
		runErr := runOwnedGoProcess(runCtx, goExecutable, temporary, args, env, stdout, stderr)
		if runErr != nil {
			return ProbeSummary{}, fmt.Errorf("test runner did not return complete results (status %s): %s",
				statusString(runErr), diagnosticTail(stderr.bytes(), stdout.bytes()))
		}
		binaries[pkg] = binary
	}
	summary := ProbeSummary{Failed: []string{}}
	for i, probe := range expected {
		args := []string{
			"tool", "test2json", binaries[probe.pkg],
			"-test.run", "^" + regexp.QuoteMeta(probe.test) + "$",
			"-test.count", "1",
			"-test.v",
			"-test.timeout", probeTestTimeout,
		}
		stdout := newBoundedWriter(boundedOutputCap)
		stderr := newBoundedWriter(boundedOutputCap)
		runErr := runOwnedGoProcess(runCtx, goExecutable, temporary, args, env, stdout, stderr)
		outcome, err := classifyProbeRun(stdout.bytes(), stderr.bytes(), runErr, probe.test, runCtx.Err() != nil)
		if err != nil {
			return ProbeSummary{}, err
		}
		switch outcome {
		case outcomePass:
			summary.Passed++
		case outcomeSkip:
			// A skipped probe counts toward the total, not toward passed.
		case outcomeFail:
			summary.Failed = append(summary.Failed, probe.test)
		case outcomeCrash:
			// The probe process died in a runtime panic: complete this probe
			// and every remaining expected probe as failed (legacy padding
			// parity) and stop, the owned suite process is gone.
			for _, rest := range expected[i:] {
				summary.Failed = append(summary.Failed, rest.test)
			}
			summary.Total = len(expected)
			return summary, nil
		}
	}
	summary.Total = len(expected)
	return summary, nil
}

// probeOutcome is the verdict of one anchored single-probe execution.
type probeOutcome int

const (
	outcomePass probeOutcome = iota
	outcomeSkip
	outcomeFail
	outcomeCrash
	outcomeUnmeasurable
)

// classifyProbeRun judges one anchored probe execution against the trusted
// test2json event stream and the process exit status. The exit status is
// authoritative for failure; a pass additionally requires exactly one run of
// the anchored probe and a unique, consistent terminal sequence (the
// test-level terminal plus the package summary). A duplicate, forged or
// out-of-order terminal can never produce a pass; the last terminal event
// never wins. The runtime timeout and panic signatures are only trusted in
// combination with the missing package summary and the exit status, never as
// free worker-emitted text.
func classifyProbeRun(stdout, stderr []byte, runErr error, testName string, ctxFired bool) (probeOutcome, error) {
	unmeasurable := func() error {
		return fmt.Errorf("test runner did not return complete results (status %s): %s",
			statusString(runErr), diagnosticTail(stderr, stdout))
	}
	if ctxFired {
		return outcomeUnmeasurable, unmeasurable()
	}
	code, known := processExitCode(runErr)
	if !known {
		// The owned process tree was killed (cancellation, bounded deadline)
		// or did not report a usable exit status.
		return outcomeUnmeasurable, unmeasurable()
	}
	events, err := scanProbeEvents(stdout, testName)
	if err != nil {
		return outcomeUnmeasurable, err
	}
	if code == 0 {
		switch {
		case events.run == 1 && events.pass == 1 && events.skip == 0 && events.fail == 0 &&
			events.packagePass && !events.packageFail:
			return outcomePass, nil
		case events.run == 1 && events.skip == 1 && events.pass == 0 && events.fail == 0 &&
			events.packagePass && !events.packageFail:
			return outcomeSkip, nil
		default:
			// Exit zero without the unique consistent terminal sequence: a
			// forged or truncated result, never a measured pass.
			return outcomeUnmeasurable, unmeasurable()
		}
	}
	if events.run == 0 {
		// The anchored probe never started: a toolchain or infrastructure
		// failure of the owned process, not a measured probe failure.
		return outcomeUnmeasurable, unmeasurable()
	}
	switch {
	case events.packageFail:
		// The test binary completed with its package failure summary: the
		// exit status is authoritative, forged or duplicate terminals
		// included.
		return outcomeFail, nil
	case events.timeoutSignature:
		// The Go runtime timeout event with the probe result missing: a
		// genuine timeout, not a worker-emitted substring.
		return outcomeUnmeasurable, unmeasurable()
	case events.panicSignature && events.fail == 1:
		// The framework marked the probe failed and the runtime panic
		// aborted the binary before the package summary: the probe crashed.
		return outcomeCrash, nil
	default:
		// Any other non-zero exit without a package summary is still a
		// measured failure of the anchored probe: the exit status is
		// authoritative and no worker-emitted event can turn it into a pass.
		return outcomeFail, nil
	}
}

// probeEvents is the counted subset of one anchored probe's test2json stream
// that the verdict needs.
type probeEvents struct {
	run              int
	pass             int
	fail             int
	skip             int
	packagePass      bool
	packageFail      bool
	timeoutSignature bool
	panicSignature   bool
}

// scanProbeEvents streams the trusted test2json JSON of one anchored
// single-probe execution under the bounded output cap and counts the events
// for the expected probe. Per-probe isolation makes the test name the
// identity: events for other test names (worker-forged) are ignored, never
// merged. A line beyond the cap or a line that does not parse is an explicit
// refusal, never a silently dropped event.
func scanProbeEvents(stdout []byte, testName string) (probeEvents, error) {
	var events probeEvents
	scanner := bufio.NewScanner(bytes.NewReader(stdout))
	scanner.Buffer(make([]byte, 0, 64*1024), boundedOutputCap)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var event testEvent
		if err := json.Unmarshal(line, &event); err != nil {
			// A JSON line that does not parse inside the bounded stream is a
			// truncation signal, never a silently dropped event.
			return probeEvents{}, fmt.Errorf("test runner stream contains an invalid JSON event (possible bounded output truncation): %s",
				diagnosticTail(line, stdout))
		}
		switch event.Action {
		case "output":
			if strings.Contains(event.Output, "panic: test timed out after") {
				events.timeoutSignature = true
			}
			if strings.Contains(event.Output, "panic:") {
				events.panicSignature = true
			}
		case "run", "pass", "fail", "skip":
			if event.Test == "" {
				switch event.Action {
				case "pass":
					events.packagePass = true
				case "fail":
					events.packageFail = true
				}
				continue
			}
			if event.Test != testName {
				continue // a forged or foreign test name, never merged
			}
			switch event.Action {
			case "run":
				events.run++
			case "pass":
				events.pass++
			case "fail":
				events.fail++
			case "skip":
				events.skip++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return probeEvents{}, fmt.Errorf("test runner stream line exceeded the %d MiB bounded output cap: %s",
				boundedOutputCap>>20, diagnosticTail(stdout, stdout))
		}
		return probeEvents{}, fmt.Errorf("test runner stream could not be read: %v: %s", err, diagnosticTail(stdout, stdout))
	}
	return events, nil
}

// processExitCode extracts the owned process exit status: 0 on success, the
// numeric code on a normal exit, known=false when the tree was killed by a
// signal or reported no usable status.
func processExitCode(runErr error) (int, bool) {
	if runErr == nil {
		return 0, true
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		if code := exitErr.ExitCode(); code >= 0 {
			return code, true
		}
		return 0, false
	}
	var owned *ownedProcessResult
	if errors.As(runErr, &owned) {
		if owned.killed {
			return 0, false
		}
		return owned.code, true
	}
	return 0, false
}

// compilerTempEnv returns the child environment for the owned go
// invocations: the Go toolchain's temporary work directories (TMPDIR on
// Unix, TMP/TEMP on Windows) point at the runner-owned scratch inside the
// probe temporary copy, the build settings are clean (no operator GOFLAGS,
// no toolchain download, no module proxy), and no network or download path
// is reachable from the compiler.
func compilerTempEnv(temporary string) []string {
	workScratch := filepath.Join(temporary, ".eval-tmp")
	env := os.Environ()
	overrides := map[string]string{
		"TMPDIR":      workScratch,
		"TEMP":        workScratch,
		"TMP":         workScratch,
		"GOTOOLCHAIN": "local",
		"GOPROXY":     "off",
		"GOFLAGS":     "",
	}
	for i, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		if value, ok := overrides[kv[:eq]]; ok {
			env[i] = kv[:eq+1] + value
		}
	}
	for key, value := range overrides {
		env = append(env, key+"="+value)
	}
	return env
}

// testEvent is the subset of a test2json event line the runner needs. The
// package identity fields are intentionally absent: per-probe isolation makes
// the anchored test name the identity, and a forged package field must not
// matter.
type testEvent struct {
	Action string `json:"Action"`
	Output string `json:"Output"`
	Test   string `json:"Test"`
}

// statusString renders the child exit status for diagnostics: "0" on
// success, the numeric code on a normal exit, the runtime error otherwise.
func statusString(err error) string {
	if err == nil {
		return "0"
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() >= 0 {
		return strconv.Itoa(exitErr.ExitCode())
	}
	var owned *ownedProcessResult
	if errors.As(err, &owned) {
		if owned.killed {
			return "signal: killed"
		}
		return strconv.Itoa(owned.code)
	}
	return err.Error()
}

// ownedProcessResult is the exit status of the owned process tree on
// platforms that do not go through os/exec (the native Windows job-object
// runner). A killed tree (cancellation, bounded deadline or job kill) is a
// signal outcome; anything else carries the root process exit code.
type ownedProcessResult struct {
	code   int
	killed bool
}

func (r *ownedProcessResult) Error() string {
	if r.killed {
		return "signal: killed"
	}
	return fmt.Sprintf("exit status %d", r.code)
}

// diagnosticTail returns the bounded tail of stderr, or of stdout when
// stderr is empty, for failure messages.
func diagnosticTail(stderr, stdout []byte) string {
	data := bytes.TrimSpace(stderr)
	if len(data) == 0 {
		data = bytes.TrimSpace(stdout)
	}
	if len(data) > diagnosticTailLimit {
		data = data[len(data)-diagnosticTailLimit:]
	}
	return string(data)
}

// boundedWriter captures process output up to a cap and silently drops the
// excess, mirroring the legacy maxBuffer bound without growing memory.
type boundedWriter struct {
	buf  bytes.Buffer
	drop int
	cap  int
}

func newBoundedWriter(cap int) *boundedWriter {
	return &boundedWriter{cap: cap}
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if remaining := w.cap - w.buf.Len(); remaining > 0 {
		if len(p) <= remaining {
			w.buf.Write(p)
			return len(p), nil
		}
		w.buf.Write(p[:remaining])
		w.drop += len(p) - remaining
		return len(p), nil
	}
	w.drop += len(p)
	return len(p), nil
}

func (w *boundedWriter) bytes() []byte { return w.buf.Bytes() }
