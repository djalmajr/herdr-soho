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

func pickerParallelFixture(t *testing.T, delays map[string]int) (platform.Env, string) {
	t.Helper()
	dir := t.TempDir()
	local := `{"ref":"local/w1:p1","machine":"local","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","name":"local","kind":"codex","status":"idle"}` + "\n"
	rules := []fakecli.Rule{{Argv: []string{"find", "--json"}, Stdout: local}}
	for _, machine := range []string{"slow", "fast", "middle"} {
		row := fmt.Sprintf(`{"ref":"%s/w1:p1","machine":"%s","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1","name":"%s","kind":"codex","status":"idle"}`+"\n", machine, machine, machine)
		rules = append(rules, fakecli.Rule{Argv: []string{"find", "--json", "--machine", machine}, Stdout: row, Delay: delays[machine]})
	}
	cliPath, err := fakecli.Install(t, dir, "herdr-soho", rules)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"slow","enabled":true},{"label":"fast","enabled":true},{"label":"middle","enabled":true}]`}})
	if err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(testutil.CleanEnv(t), dir, fakecli.EnvOptions{IncludeBasePath: true}) {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	env["HERDR_BIN_PATH"] = filepath.Join(dir, "herdr")
	return env, cliPath
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

func waitForPicker(t *testing.T, output *os.File, finished <-chan int, condition func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second) // returns as soon as the condition holds; Windows process starts can take seconds
	for time.Now().Before(deadline) {
		data, _ := os.ReadFile(output.Name())
		screen := string(data)
		if condition(screen) {
			return screen
		}
		select {
		case code := <-finished:
			t.Fatalf("picker exited before screen condition, code=%d, output=%q", code, screen)
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, _ := os.ReadFile(output.Name())
	t.Fatalf("timed out waiting for picker screen condition, output=%q", data)
	return ""
}

func TestPickerRemoteFindsAppendInCompletionOrder(t *testing.T) {
	// JS: "loads enabled machines in parallel and appends each result as it completes"
	// Mutation captured: making remote searches serial preserves machine-list order and fails this assertion.
	env, cliPath := pickerParallelFixture(t, map[string]int{"slow": 350, "fast": 60, "middle": 180})
	writer, output, _, finished := pickerStartWithPipes(t, env, cliPath)
	screen := waitForPicker(t, output, finished, func(screen string) bool {
		return strings.Contains(screen, "slow/w1:p1") && strings.Contains(screen, "fast/w1:p1") && strings.Contains(screen, "middle/w1:p1")
	})
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
	env, cliPath := pickerParallelFixture(t, map[string]int{"slow": 8000, "fast": 8000, "middle": 8000})
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
	env, cliPath := pickerParallelFixture(t, map[string]int{"slow": 8000, "fast": 8000, "middle": 8000})
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
