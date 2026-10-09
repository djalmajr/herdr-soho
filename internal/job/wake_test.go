package job

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// newWakeHookEnv installs the fake wake hook in its own directory and
// returns the hermetic env: PATH with only the fake directory, its own HOME
// and XDG_CONFIG_HOME, and the fakecli wiring (the pattern of
// prepare_test.go). The fake is the re-executed test binary, so nothing
// else is needed on the PATH.
func newWakeHookEnv(t *testing.T, rules []fakecli.Rule) (string, platform.Env) {
	t.Helper()
	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "wakehook", rules); err != nil {
		t.Fatalf("install the fake wake hook: %v", err)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	for _, dir := range []string{home, conf} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(nil, fakeDir) {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = conf
	return fakeDir, env
}

// wakeHookCalls reads the fake hook's call log; a missing log means the
// hook never ran.
func wakeHookCalls(t *testing.T, fakeDir string) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCalls(filepath.Join(fakeDir, "wakehook.calls.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read the wake hook call log: %v", err)
	}
	return calls
}

func wakeEvent(tipo string, refs map[string]string) Event {
	return Event{Seq: 7, TS: "2026-01-01T12:00:00-03:00", Tipo: tipo, Resumo: "short text", Refs: refs}
}

func wantDurations(t *testing.T, got, want []time.Duration) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("durations = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("durations = %v, want %v", got, want)
		}
	}
}

func wantFriction(t *testing.T, frictions []string, want string) {
	t.Helper()
	if len(frictions) != 1 || frictions[0] != want {
		t.Fatalf("friction = %v, want exactly one of %q", frictions, want)
	}
}

// TestWakeClassification covers every event type of the contract table,
// push with and without refs.motivo, and an unknown type: the hook runs
// exactly once for a wake type and never for the others.
func TestWakeClassification(t *testing.T) {
	for _, row := range []struct {
		name string
		tipo string
		refs map[string]string
		wake bool
	}{
		{"accepted", "accepted", nil, false},
		{"preparing", "preparing", nil, false},
		{"worker_spawned", "worker_spawned", nil, false},
		{"worker_done", "worker_done", nil, false},
		{"commit", "commit", nil, false},
		{"push", "push", nil, false},
		{"push_with_exit_ref", "push", map[string]string{"exit": "1"}, false},
		{"push_rejected", "push", map[string]string{"motivo": "rejected"}, true},
		{"push_empty_motivo", "push", map[string]string{"motivo": ""}, false},
		{"pr_opened", "pr_opened", nil, true},
		{"checkpoint", "checkpoint", nil, false},
		{"review_verdict", "review_verdict", nil, true},
		{"question", "question", nil, true},
		{"blocked", "blocked", nil, true},
		{"unblocked", "unblocked", nil, false},
		{"amend_received", "amend_received", nil, false},
		{"decision", "decision", nil, true},
		{"decision_acked", "decision_acked", nil, false},
		{"timeout_warning", "timeout_warning", nil, true},
		{"failure", "failure", nil, true},
		{"note", "note", nil, false},
		{"terminal", "terminal", nil, true},
		{"cleanup", "cleanup", nil, false},
		{"unknown_type", "elsewhere", nil, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true}})
			hook := WakeHook{Cmd: "wakehook", JobID: "job-1", Env: env}
			attempts := hook.Run(wakeEvent(row.tipo, row.refs))
			want := 0
			if row.wake {
				want = 1
			}
			if attempts != want {
				t.Fatalf("attempts = %d, want %d (tipo %q refs %v)", attempts, want, row.tipo, row.refs)
			}
			if calls := wakeHookCalls(t, fakeDir); len(calls) != want {
				t.Fatalf("hook calls = %d, want %d (tipo %q)", len(calls), want, row.tipo)
			}
		})
	}
}

// TestWakeEmptyCmd: a blank or empty Cmd runs nothing, for wake and
// non-wake events alike.
func TestWakeEmptyCmd(t *testing.T) {
	fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true}})
	for _, cmd := range []string{"", "   "} {
		hook := WakeHook{Cmd: cmd, JobID: "job-1", Env: env}
		if attempts := hook.Run(wakeEvent("question", nil)); attempts != 0 {
			t.Fatalf("Cmd %q ran %d attempts, want 0", cmd, attempts)
		}
	}
	if calls := wakeHookCalls(t, fakeDir); len(calls) != 0 {
		t.Fatalf("hook calls = %d, want 0", len(calls))
	}
}

// TestWakeInput: stdin carries the exact events.jsonl line and the four
// HERDR_SOHO_JOB_* vars hold the exact values.
func TestWakeInput(t *testing.T) {
	fakeDir := t.TempDir()
	if _, err := fakecli.InstallWithOptions(t, fakeDir, "wakehook", []fakecli.Rule{{AnyArgs: true}}, fakecli.InstallOptions{
		CaptureStdin: true,
		CaptureEnv:   []string{"HERDR_SOHO_JOB_ID", "HERDR_SOHO_JOB_SEQ", "HERDR_SOHO_JOB_EVENT", "HERDR_SOHO_JOB_IDEMPOTENCY_KEY"},
	}); err != nil {
		t.Fatalf("install the capturing fake wake hook: %v", err)
	}
	env := platform.Env{}
	for _, item := range fakecli.Env(nil, fakeDir) {
		key, value, ok := strings.Cut(item, "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	for _, dir := range []string{home, conf} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	env["HOME"] = home
	env["XDG_CONFIG_HOME"] = conf

	// The event is published through the store so the stdin comparison is
	// against the exact events.jsonl line, not a re-rendering.
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatalf("start: %v", err)
	}
	ev, err := s.Append("job-1", EventIn{Tipo: "question", Resumo: "needs a decision", Refs: map[string]string{"agente": "orchestrator"}})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	hook := WakeHook{Cmd: "wakehook", JobID: "job-1", Env: env}
	if attempts := hook.Run(ev); attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
	calls := wakeHookCalls(t, fakeDir)
	if len(calls) != 1 {
		t.Fatalf("hook calls = %d, want 1", len(calls))
	}
	eventsRaw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(eventsRaw), "\n")
	if len(lines) != 3 || lines[2] != "" {
		t.Fatalf("events.jsonl has %d lines, want 2 lines plus the trailing newline", len(lines))
	}
	wantStdin := lines[1] + "\n"
	if calls[0].Stdin != wantStdin {
		t.Fatalf("stdin = %q, want the exact events.jsonl line %q", calls[0].Stdin, wantStdin)
	}
	marshaled, err := json.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	if calls[0].Stdin != string(marshaled)+"\n" {
		t.Fatalf("stdin = %q, want %q", calls[0].Stdin, string(marshaled)+"\n")
	}
	wantEnv := map[string]string{
		"HERDR_SOHO_JOB_ID":              "job-1",
		"HERDR_SOHO_JOB_SEQ":             "2",
		"HERDR_SOHO_JOB_EVENT":           "question",
		"HERDR_SOHO_JOB_IDEMPOTENCY_KEY": "job-1:2",
	}
	if len(calls[0].Env) != len(wantEnv) {
		t.Fatalf("captured env = %v, want %v", calls[0].Env, wantEnv)
	}
	for key, value := range wantEnv {
		if calls[0].Env[key] != value {
			t.Fatalf("env %s = %q, want %q", key, calls[0].Env[key], value)
		}
	}
}

// TestWakeArgv: argv is strings.Fields(Cmd) with the extra arguments passed
// verbatim, and there is no shell to interpret them.
func TestWakeArgv(t *testing.T) {
	t.Run("extra arguments pass verbatim", func(t *testing.T) {
		fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{Argv: []string{"--flag", "value"}}})
		hook := WakeHook{Cmd: "wakehook --flag value", JobID: "job-1", Env: env}
		if attempts := hook.Run(wakeEvent("question", nil)); attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
		calls := wakeHookCalls(t, fakeDir)
		if len(calls) != 1 || !sameArgv(calls[0].Argv, []string{"--flag", "value"}) {
			t.Fatalf("argv = %v, want [--flag value]", calls)
		}
	})
	t.Run("shell metacharacters are literal arguments", func(t *testing.T) {
		// A shell would spawn subshells or drop these characters; the fake
		// matches only the literal fields, so a rule hit proves no shell
		// ran.
		fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{Argv: []string{"a;b", "c|d", "$(x)", "`y`"}}})
		hook := WakeHook{Cmd: "wakehook a;b c|d $(x) `y`", JobID: "job-1", Env: env}
		var frictions []string
		hook.Friction = func(msg string) { frictions = append(frictions, msg) }
		if attempts := hook.Run(wakeEvent("question", nil)); attempts != 1 {
			t.Fatalf("attempts = %d, want 1", attempts)
		}
		calls := wakeHookCalls(t, fakeDir)
		want := []string{"a;b", "c|d", "$(x)", "`y`"}
		if len(calls) != 1 || !sameArgv(calls[0].Argv, want) {
			t.Fatalf("argv = %v, want %v", calls, want)
		}
		if len(frictions) != 0 {
			t.Fatalf("friction = %v, want none (the literal-argv rule matched)", frictions)
		}
	})
	t.Run("an absolute command path runs without a PATH lookup", func(t *testing.T) {
		fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{Argv: []string{"--flag"}}})
		matches, err := filepath.Glob(filepath.Join(fakeDir, "wakehook*"))
		if err != nil || len(matches) == 0 {
			t.Fatalf("fake hook not found in %s: %v", fakeDir, err)
		}
		exe := ""
		for _, match := range matches {
			if base := filepath.Base(match); base == "wakehook" || base == "wakehook.exe" {
				exe = match
			}
		}
		if exe == "" || strings.ContainsAny(exe, " \t") {
			t.Skipf("no space-free absolute fake path: %v", matches)
		}
		env["PATH"] = t.TempDir()
		var frictions []string
		hook := WakeHook{Cmd: exe + " --flag", JobID: "job-1", Env: env, Sleep: func(time.Duration) {}, Friction: func(msg string) { frictions = append(frictions, msg) }}
		if attempts := hook.Run(wakeEvent("question", nil)); attempts != 1 {
			t.Fatalf("attempts = %d, want 1 (friction %v)", attempts, frictions)
		}
		calls := wakeHookCalls(t, fakeDir)
		if len(calls) != 1 || !sameArgv(calls[0].Argv, []string{"--flag"}) {
			t.Fatalf("argv = %v, want [--flag]", calls)
		}
	})
}

// TestWakeRetry: retries are bounded, the sleeps come from the injected
// sleeper (recorded, never really slept), and the final failure logs one
// friction.
func TestWakeRetry(t *testing.T) {
	t.Run("an always-failing hook runs 4 attempts and logs one exit friction", func(t *testing.T) {
		fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true, Code: 3}})
		var sleeps []time.Duration
		var frictions []string
		hook := WakeHook{
			Cmd:      "wakehook fail",
			JobID:    "job-1",
			Env:      env,
			Sleep:    func(d time.Duration) { sleeps = append(sleeps, d) },
			Friction: func(msg string) { frictions = append(frictions, msg) },
		}
		attempts := hook.Run(wakeEvent("failure", nil))
		if attempts != 4 {
			t.Fatalf("attempts = %d, want 4", attempts)
		}
		if calls := wakeHookCalls(t, fakeDir); len(calls) != 4 {
			t.Fatalf("hook calls = %d, want 4", len(calls))
		}
		wantDurations(t, sleeps, []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second})
		wantFriction(t, frictions, "job: wake hook failed for job-1:7 after 4 attempts (exit 3)")
	})
	t.Run("a hook that fails once then succeeds stops at the second attempt", func(t *testing.T) {
		fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{
			{Argv: []string{"once"}, Call: 1, Code: 3},
			{Argv: []string{"once"}, Call: 2, Code: 0},
		})
		var sleeps []time.Duration
		var frictions []string
		hook := WakeHook{
			Cmd:      "wakehook once",
			JobID:    "job-1",
			Env:      env,
			Sleep:    func(d time.Duration) { sleeps = append(sleeps, d) },
			Friction: func(msg string) { frictions = append(frictions, msg) },
		}
		attempts := hook.Run(wakeEvent("failure", nil))
		if attempts != 2 {
			t.Fatalf("attempts = %d, want 2", attempts)
		}
		if calls := wakeHookCalls(t, fakeDir); len(calls) != 2 {
			t.Fatalf("hook calls = %d, want 2", len(calls))
		}
		wantDurations(t, sleeps, []time.Duration{10 * time.Second})
		if len(frictions) != 0 {
			t.Fatalf("friction = %v, want none after a success", frictions)
		}
	})
}

// TestWakeTimeout: each attempt is bounded by Timeout; the whole Run with a
// zero-duration sleeper finishes well under the 5 s the fake would take per
// attempt, and the cause is timeout.
func TestWakeTimeout(t *testing.T) {
	fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true, Delay: 5000}})
	var frictions []string
	hook := WakeHook{
		Cmd:      "wakehook slow",
		JobID:    "job-1",
		Env:      env,
		Timeout:  200 * time.Millisecond,
		Sleep:    func(time.Duration) {},
		Friction: func(msg string) { frictions = append(frictions, msg) },
	}
	start := time.Now()
	attempts := hook.Run(wakeEvent("failure", nil))
	elapsed := time.Since(start)
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if calls := wakeHookCalls(t, fakeDir); len(calls) != 4 {
		t.Fatalf("hook calls = %d, want 4", len(calls))
	}
	if elapsed >= 10*time.Second {
		t.Fatalf("Run took %s, want under 10s (4 x 200ms attempts)", elapsed)
	}
	wantFriction(t, frictions, "job: wake hook failed for job-1:7 after 4 attempts (timeout)")
}

// TestWakeMissingExecutable: a hook that is not on the PATH fails every
// attempt with the not found cause, stays bounded, and does not panic.
func TestWakeMissingExecutable(t *testing.T) {
	fakeDir, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true}})
	var sleeps []time.Duration
	var frictions []string
	hook := WakeHook{
		Cmd:      "wakehook-missing fail",
		JobID:    "job-1",
		Env:      env,
		Sleep:    func(d time.Duration) { sleeps = append(sleeps, d) },
		Friction: func(msg string) { frictions = append(frictions, msg) },
	}
	attempts := hook.Run(wakeEvent("failure", nil))
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if calls := wakeHookCalls(t, fakeDir); len(calls) != 0 {
		t.Fatalf("the missing executable was invoked %d times", len(calls))
	}
	wantDurations(t, sleeps, []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second})
	wantFriction(t, frictions, "job: wake hook failed for job-1:7 after 4 attempts (not found)")
}

// TestWakeFailingHookNeverChangesJob: a failing hook on one of the job's
// events leaves state.json and events.jsonl byte-identical.
func TestWakeFailingHookNeverChangesJob(t *testing.T) {
	_, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true, Code: 4}})
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatalf("start: %v", err)
	}
	ev, err := s.Append("job-1", EventIn{Tipo: "blocked", Resumo: "dialog needed", Refs: map[string]string{"agente": "orchestrator"}})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	dir, err := Dir(root, "job-1")
	if err != nil {
		t.Fatal(err)
	}
	stateBefore, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	hook := WakeHook{Cmd: "wakehook fail", JobID: "job-1", Env: env, Sleep: func(time.Duration) {}}
	if attempts := hook.Run(ev); attempts != 4 {
		t.Fatalf("the failing hook ran %d attempts, want 4", attempts)
	}
	stateAfter, err := os.ReadFile(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateBefore, stateAfter) {
		t.Fatalf("state.json changed: %s -> %s", stateBefore, stateAfter)
	}
	if !bytes.Equal(eventsBefore, eventsAfter) {
		t.Fatalf("events.jsonl changed: %s -> %s", eventsBefore, eventsAfter)
	}
}

// TestWakeScheduleComesOnlyFromSleep: with a no-op sleeper the whole run
// stays under 1 s although the default delays total 130 s, so Run computes
// no wall-clock deadline of its own; the recorded delays prove the schedule
// is exactly the injected Sleep calls. A fake clock that goes backwards is
// irrelevant for the same reason: nothing in the schedule reads the wall
// clock.
func TestWakeScheduleComesOnlyFromSleep(t *testing.T) {
	_, env := newWakeHookEnv(t, []fakecli.Rule{{AnyArgs: true, Code: 5}})
	var sleeps []time.Duration
	hook := WakeHook{
		Cmd:   "wakehook fast-fail",
		JobID: "job-1",
		Env:   env,
		Sleep: func(d time.Duration) { sleeps = append(sleeps, d) },
	}
	start := time.Now()
	attempts := hook.Run(wakeEvent("failure", nil))
	elapsed := time.Since(start)
	if attempts != 4 {
		t.Fatalf("attempts = %d, want 4", attempts)
	}
	if elapsed >= time.Second {
		t.Fatalf("Run took %s with a no-op sleeper, want under 1s apart from the attempts", elapsed)
	}
	wantDurations(t, sleeps, []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second})
}
