package plugin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

type pickerPIDRecorder struct {
	mu   sync.Mutex
	pids []int
}

func (r *pickerPIDRecorder) add(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pids = append(r.pids, pid)
}

func (r *pickerPIDRecorder) snapshot() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), r.pids...)
}

// pickerParallelFixture installs the picker's fake CLIs. The machine list is
// always slow, fast, middle. In gated mode each fake remote find blocks until
// its own release file appears before answering (the returned release function
// unblocks one), so the test - not the wall clock - imposes the completion
// order; in delay mode (gated=false) the fakes sleep delays[machine] ms as
// before.
// pickerParallelFixture installs the picker's fake CLI. The machine list is
// always slow, fast, middle. In gated mode each fake remote snapshot blocks
// until its own release file appears before answering (the returned release
// function unblocks one), so the test - not the wall clock - imposes the
// completion order; in delay mode (gated=false) the fakes sleep
// delays[machine] ms as before.
func pickerParallelFixture(t *testing.T, delays map[string]int, gated bool) (platform.Env, string, func(string)) {
	t.Helper()
	dir := t.TempDir()
	var releaseDir string
	if gated {
		releaseDir = filepath.Join(dir, "release")
		if err := os.MkdirAll(releaseDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	snapshot := func(name string) string {
		return fmt.Sprintf(`{"result":{"snapshot":{"workspaces":[{"workspace_id":"w1","label":"w1"}],"tabs":[{"tab_id":"w1:t1"}],"agents":[{"pane_id":"w1:p1","name":%q,"agent":"codex","agent_status":"idle"}],"panes":[{"pane_id":"w1:p1","workspace_id":"w1","tab_id":"w1:t1"}]}}}`, name)
	}
	rules := []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: snapshot("local")},
		{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"slow","enabled":true},{"label":"fast","enabled":true},{"label":"middle","enabled":true}]`},
	}
	for _, machine := range []string{"slow", "fast", "middle"} {
		rule := fakecli.Rule{Argv: []string{"--machine", machine, "api", "snapshot"}, Stdout: snapshot(machine), Delay: delays[machine]}
		if gated {
			rule.WaitFile = filepath.Join(releaseDir, "release-"+machine)
		}
		rules = append(rules, rule)
	}
	cliPath, err := fakecli.Install(t, dir, "herdr", rules)
	if err != nil {
		t.Fatal(err)
	}
	env := pickerParallelEnv(t, dir)
	release := func(string) {}
	if gated {
		release = func(machine string) {
			if err := os.WriteFile(filepath.Join(releaseDir, "release-"+machine), []byte("go"), 0o644); err != nil {
				t.Fatalf("release %s: %v", machine, err)
			}
		}
		t.Cleanup(func() {
			// Unblock finds still waiting after the test (a failed wait) so
			// the fakes exit instead of polling for their release file
			// forever.
			for _, machine := range []string{"slow", "fast", "middle"} {
				_ = os.WriteFile(filepath.Join(releaseDir, "release-"+machine), []byte("go"), 0o644)
			}
		})
	}
	return env, cliPath, release
}

// pickerParallelEnv builds the picker's environment for the fixture
// directory (the fake herdr is installed by pickerParallelFixture).
func pickerParallelEnv(t *testing.T, dir string) platform.Env {
	t.Helper()
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = filepath.Join(dir, "herdr")
	return env
}

func pickerStartWithPipes(t *testing.T, env platform.Env, cliPath string) (*os.File, *os.File, *os.File, <-chan int) {
	t.Helper()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "picker-parallel-")
	if err != nil {
		t.Fatal(err)
	}
	oldIn, oldOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = input, output
	t.Cleanup(func() {
		os.Stdin, os.Stdout = oldIn, oldOut
		_ = input.Close()
		_ = writer.Close()
		_ = output.Close()
	})
	finished := make(chan int, 1)
	go func() { finished <- RunPicker(env, platform.Current(), cliPath) }()
	return writer, output, input, finished
}

// waitForPickerRow waits (30 s) for the row to appear in the accumulated
// screen. The failure messages name the serial-execution hypothesis: with
// the release gates a run that snapshots the machines one by one in
// machine-list order (slow, fast, middle) blocks on the first, still
// unreleased machine and never shows the waited row; the per-call snapshot
// timeout is longer than the global deadline, so it cannot unmask the block
// in time.
func waitForPickerRow(t *testing.T, output *os.File, finished <-chan int, row string) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second) // returns as soon as the row shows; Windows process starts can take seconds
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(output.Name())
		if strings.Contains(string(data), row) {
			return
		}
		select {
		case code := <-finished:
			t.Fatalf("picker exited before the row %q appeared (serial remote finds?), code=%d, output=%q", row, code, string(data))
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(output.Name())
	t.Fatalf("after 30 s the row %q never appeared: a serial run in machine-list order blocks on an unreleased machine and hides it, output=%q", row, string(data))
}

func TestPickerRemoteFindsAppendInCompletionOrder(t *testing.T) {
	// JS: "loads enabled machines in parallel and appends each result as it completes"
	// The completion order is imposed by release gates, not by wall-clock
	// sleeps: each fake remote find blocks on its own release file, and the
	// machine list (slow, fast, middle) is in a different order than the
	// releases (fast, middle, slow), so appending is checked against the
	// order the test set.
	// Mutation captured: making remote searches serial starts "slow" first
	// (machine-list order) and blocks there, so "fast" never appears and the
	// first wait times out.
	env, cliPath, release := pickerParallelFixture(t, nil, true)
	writer, output, _, finished := pickerStartWithPipes(t, env, cliPath)
	for _, machine := range []string{"fast", "middle", "slow"} {
		release(machine)
		waitForPickerRow(t, output, finished, machine+"/w1:p1")
	}
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	screen := string(data)
	fast := strings.Index(screen, "fast/w1:p1")
	middle := strings.Index(screen, "middle/w1:p1")
	slow := strings.Index(screen, "slow/w1:p1")
	if !(fast >= 0 && middle > fast && slow > middle) {
		t.Fatalf("rows were not appended by completion order fast < middle < slow: fast=%d middle=%d slow=%d", fast, middle, slow)
	}
	if _, err := writer.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("picker code=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("picker did not close after Esc")
	}
}

func TestPickerCloseCancelsEveryRemoteFindPID(t *testing.T) {
	// JS: "Esc during a slow remote load exits without copying and kills the loads"
	// Mutation captured: omitting cancellation leaves recorded remote child PIDs running after close.
	env, cliPath, _ := pickerParallelFixture(t, map[string]int{"slow": 8000, "fast": 8000, "middle": 8000}, false)
	recorder := &pickerPIDRecorder{}
	oldObserver := pickerProcessStarted
	pickerProcessStarted = recorder.add
	defer func() { pickerProcessStarted = oldObserver }()
	writer, output, _, finished := pickerStartWithPipes(t, env, cliPath)
	deadline := time.Now().Add(30 * time.Second) // returns as soon as the condition holds; Windows process starts can take seconds
	for time.Now().Before(deadline) && len(recorder.snapshot()) < 5 {
		select {
		case code := <-finished:
			t.Fatalf("picker exited before all children started, code=%d", code)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	pids := recorder.snapshot()
	if len(pids) != 5 {
		t.Fatalf("recorded child PIDs=%v, want local find, machine list, and three remote finds", pids)
	}
	if _, err := writer.Write([]byte("\x1b")); err != nil {
		t.Fatal(err)
	}
	_ = writer.Close()
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("picker code=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("picker did not stop after Esc; screen=%q", output.Name())
	}
	for _, pid := range pids {
		if pickerPIDAlive(pid) {
			t.Errorf("child PID %d still alive after picker close", pid)
		}
	}
}

func TestPickerShutdownSignalCancelsEveryRemoteFindPID(t *testing.T) {
	// Mutation captured: removing the shutdown-signal select case leaves the picker running and its find children alive.
	env, cliPath, _ := pickerParallelFixture(t, map[string]int{"slow": 8000, "fast": 8000, "middle": 8000}, false)
	recorder := &pickerPIDRecorder{}
	oldObserver := pickerProcessStarted
	pickerProcessStarted = recorder.add
	defer func() { pickerProcessStarted = oldObserver }()
	input, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	output, err := os.CreateTemp(t.TempDir(), "picker-signal-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close(); _ = writer.Close(); _ = output.Close() })
	shutdown := make(chan os.Signal, 1)
	finished := make(chan int, 1)
	go func() { finished <- runPickerLoopWithSignals(env, "darwin", cliPath, input, output, false, shutdown) }()
	deadline := time.Now().Add(30 * time.Second) // returns as soon as the condition holds; Windows process starts can take seconds
	for time.Now().Before(deadline) && len(recorder.snapshot()) < 5 {
		select {
		case code := <-finished:
			t.Fatalf("picker exited before all children started, code=%d", code)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	pids := recorder.snapshot()
	if len(pids) != 5 {
		t.Fatalf("recorded child PIDs=%v, want local find, machine list, and three remote finds", pids)
	}
	shutdown <- os.Interrupt
	select {
	case code := <-finished:
		if code != 0 {
			t.Fatalf("picker code=%d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("picker did not stop after shutdown signal")
	}
	if _, err := input.Stat(); err != nil {
		t.Fatalf("picker closed stdin while handling shutdown signal: %v", err)
	}
	if _, err := writer.Write([]byte("x")); err != nil {
		t.Fatalf("stdin writer failed after picker returned: %v", err)
	}
	for _, pid := range pids {
		if pickerPIDAlive(pid) {
			t.Errorf("child PID %d still alive after shutdown signal", pid)
		}
	}
}
