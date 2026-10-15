package job

// auto_close_test.go pins the job's automatic close keys: job_auto_cleanup
// and job_human_input_minutes are known to ConfigKeyOk and validate in
// ConfigValueOk, and `job start` resolves them from the job checkout's
// configuration layers (the project file beats the user file, which beats
// the defaults), records them on the state once, in the run facts write,
// and never rewrites them; an invalid value fails the start with exit 2
// naming the key. A state without the fields (a job that predates them)
// reads as "on" and 5 minutes.

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// Both keys are known to ConfigKeyOk, stay out of ConfigScalarKeys (the
// scalar oracle's row order is frozen and the machine keys have their own
// loader), and validate their contract values in ConfigValueOk.
func TestAutoCloseKeysConfigValidation(t *testing.T) {
	env := platform.Env{}
	cwd := t.TempDir()
	for _, key := range []string{"job_auto_cleanup", "job_human_input_minutes"} {
		if !core.ConfigKeyOk(key) {
			t.Fatalf("ConfigKeyOk(%s) = false", key)
		}
		for _, scalar := range core.ConfigScalarKeys {
			if scalar == key {
				t.Fatalf("%s joined ConfigScalarKeys", key)
			}
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"on", true},
		{"off", true},
		{"ON", false},
		{"x", false},
		{"0", false},
		{"1", false},
		{"1440", false},
		{"1441", false},
		{"five", false},
	} {
		if got := core.ConfigValueOk("job_auto_cleanup", tc.value, env, cwd); got != tc.want {
			t.Errorf("ConfigValueOk(job_auto_cleanup, %q) = %t, want %t", tc.value, got, tc.want)
		}
	}
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"on", false},
		{"off", false},
		{"ON", false},
		{"x", false},
		{"0", false},
		{"1", true},
		{"1440", true},
		{"1441", false},
		{"five", false},
	} {
		if got := core.ConfigValueOk("job_human_input_minutes", tc.value, env, cwd); got != tc.want {
			t.Errorf("ConfigValueOk(job_human_input_minutes, %q) = %t, want %t", tc.value, got, tc.want)
		}
	}
	// The generic refusal still applies: a value with a banned character
	// is refused before the value shape is looked at.
	if core.ConfigValueOk("job_auto_cleanup", "on#x", env, cwd) {
		t.Fatal("a job_auto_cleanup value with a comment marker was accepted")
	}
	if core.ConfigValueOk("job_human_input_minutes", "10#x", env, cwd) {
		t.Fatal("a job_human_input_minutes value with a comment marker was accepted")
	}
}

// autoCloseStartFix wires one hermetic job start over an existing checkout
// (the project file's home) with a controlled defaults layer: defaultsBody
// is the temp skill's config.defaults, projectBody the checkout's
// .agents/soho.conf and userBody the user configuration file; an
// empty body leaves that layer without the keys.
func autoCloseStartFix(t *testing.T, defaultsBody, projectBody, userBody string) (Starter, *prepareFixture, string, *[]string) {
	t.Helper()
	f := newPrepareFixture(t, []fakecli.Rule{{Argv: []string{"auth", "status"}}})
	skill := t.TempDir()
	writeTeamFile(t, filepath.Join(skill, "config.defaults"), defaultsBody)
	f.env["HERDR_SOHO_SKILL_DIR"] = skill
	f.cloneCheckout()
	if projectBody != "" {
		writeTeamFile(t, filepath.Join(f.checkout(), ".agents", "soho.conf"), projectBody)
	}
	if userBody != "" {
		writeTeamFile(t, platform.UserConfigPath(platform.Current(), f.env), userBody)
	}
	stateRoot := t.TempDir()
	var friction []string
	return startStarter(t, f, startFake(), stateRoot, &friction), f, stateRoot, &friction
}

func requireAutoClose(t *testing.T, store *Store, auto string, min int) {
	t.Helper()
	snap, err := store.Snapshot("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if st := snap.State; st.AutoCleanup != auto || st.HumanInputMin != min {
		t.Fatalf("recorded auto_cleanup=%q human_input_min=%d, want %q and %d", st.AutoCleanup, st.HumanInputMin, auto, min)
	}
	raw, err := os.ReadFile(filepath.Join(store.Root, "jobs", "job-1", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	autoOk := strings.Contains(text, `"auto_cleanup":"`+auto+`"`) || strings.Contains(text, `"auto_cleanup": "`+auto+`"`)
	num := strconv.Itoa(min)
	minOk := strings.Contains(text, `"human_input_min":`+num) || strings.Contains(text, `"human_input_min": `+num)
	if !autoOk || !minOk {
		t.Errorf("state.json = %s, want auto_cleanup %q and human_input_min %d", raw, auto, min)
	}
}

// `job start` records the values the project file of the job's repository
// defines, ahead of the user file and the defaults.
func TestStartRecordsAutoCloseFromTheProjectFile(t *testing.T) {
	s, _, _, _ := autoCloseStartFix(t,
		"job_auto_cleanup=on\njob_human_input_minutes=5\n",
		"job_auto_cleanup=off\njob_human_input_minutes=30\n",
		"job_auto_cleanup=on\njob_human_input_minutes=5\n")
	_, store, err := s.Start(startRequest("main", briefA))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	requireAutoClose(t, store, "off", 30)
}

// When the project file does not define a key, the user file's value is
// recorded.
func TestStartRecordsAutoCloseFromTheUserFileWhenTheProjectDoesNotDefine(t *testing.T) {
	s, _, _, _ := autoCloseStartFix(t,
		"job_auto_cleanup=on\njob_human_input_minutes=5\n",
		"",
		"job_auto_cleanup=off\njob_human_input_minutes=45\n")
	_, store, err := s.Start(startRequest("main", briefA))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	requireAutoClose(t, store, "off", 45)
}

// Per key: the project file wins when it defines the key, the user file
// supplies the key it does not.
func TestStartProjectBeatsUserInTheAutoCloseLayers(t *testing.T) {
	s, _, _, _ := autoCloseStartFix(t,
		"",
		"job_auto_cleanup=off\n",
		"job_auto_cleanup=on\njob_human_input_minutes=15\n")
	_, store, err := s.Start(startRequest("main", briefA))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	requireAutoClose(t, store, "off", 15)
}

// When no file defines the keys, the defaults are recorded: the defaults
// layer's value when it defines the key (proving the layer is read), and
// the built-in default otherwise.
func TestStartRecordsAutoCloseDefaultsWhenNoFileDefines(t *testing.T) {
	t.Run("the defaults layer defines them", func(t *testing.T) {
		s, _, _, _ := autoCloseStartFix(t,
			"job_auto_cleanup=off\njob_human_input_minutes=7\n",
			"", "")
		_, store, err := s.Start(startRequest("main", briefA))
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		requireAutoClose(t, store, "off", 7)
	})
	t.Run("no layer defines them", func(t *testing.T) {
		s, _, _, _ := autoCloseStartFix(t, "", "", "")
		_, store, err := s.Start(startRequest("main", briefA))
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		requireAutoClose(t, store, "on", 5)
	})
}

// An invalid value in the project file fails the start with exit 2 naming
// the key, the job ends failed, and nothing is recorded.
func TestStartInvalidAutoCloseValueFailsWithExit2NamingTheKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		project string
		msg     string
	}{
		{"job_auto_cleanup", "job_auto_cleanup=banana\n", "job: invalid job_auto_cleanup 'banana'"},
		{"job_human_input_minutes", "job_human_input_minutes=1441\n", "job: invalid job_human_input_minutes '1441'"},
		{"job_human_input_minutes zero", "job_human_input_minutes=0\n", "job: invalid job_human_input_minutes '0'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _, _, _ := autoCloseStartFix(t,
				"job_auto_cleanup=on\njob_human_input_minutes=5\n",
				tc.project, "")
			_, store, err := s.Start(startRequest("main", briefA))
			var exit *platform.ExitError
			if !errors.As(err, &exit) || exit.Code != 2 || exit.Msg != tc.msg {
				t.Fatalf("Start = %v, want exit 2 %q", err, tc.msg)
			}
			if store == nil {
				t.Fatal("no store returned")
			}
			snap, err := store.Snapshot("job-1")
			if err != nil {
				t.Fatal(err)
			}
			st := snap.State
			if st.Status != StatusFailed {
				t.Fatalf("status = %q, want failed", st.Status)
			}
			if st.Motivo == nil || *st.Motivo != tc.msg {
				t.Fatalf("motivo = %v, want %q", st.Motivo, tc.msg)
			}
			raw, err := os.ReadFile(filepath.Join(store.Root, "jobs", "job-1", "state.json"))
			if err != nil {
				t.Fatal(err)
			}
			// The field names with their JSON quote and colon: the motivo
			// text carries the key without them.
			if strings.Contains(string(raw), `"auto_cleanup":`) || strings.Contains(string(raw), `"human_input_min":`) {
				t.Errorf("state.json = %s, want no automatic close fields after an invalid value", raw)
			}
		})
	}
}

// A state without the fields (a job started before they existed) reads as
// "on" and 5 minutes in the two methods.
func TestStateWithoutAutoCloseFieldsReadsOnAndFive(t *testing.T) {
	legacy := State{}
	if !legacy.AutoCleanupOn() {
		t.Error("AutoCleanupOn() = false on a state without the fields, want true (on)")
	}
	if got := legacy.HumanInputWait(); got != 5*time.Minute {
		t.Errorf("HumanInputWait() = %v, want 5m", got)
	}
	for _, tc := range []struct {
		auto string
		min  int
		on   bool
		wait time.Duration
	}{
		{"on", 0, true, 5 * time.Minute},
		{"off", 0, false, 5 * time.Minute},
		{"on", 30, true, 30 * time.Minute},
		{"off", 1440, false, 1440 * time.Minute},
	} {
		st := State{AutoCleanup: tc.auto, HumanInputMin: tc.min}
		if got := st.AutoCleanupOn(); got != tc.on {
			t.Errorf("AutoCleanupOn(auto_cleanup=%q) = %t, want %t", tc.auto, got, tc.on)
		}
		if got := st.HumanInputWait(); got != tc.wait {
			t.Errorf("HumanInputWait(human_input_min=%d) = %v, want %v", tc.min, got, tc.wait)
		}
	}
}

// The values land once, in the run facts write, and are never rewritten to
// another value: every state.json write after the first one that holds
// them carries the same pair.
func TestStartRecordsAutoCloseOnceAndNeverRewritesIt(t *testing.T) {
	s, _, _, _ := autoCloseStartFix(t, "", "job_auto_cleanup=off\njob_human_input_minutes=30\n", "")
	writes := startStateWrites(t)
	if _, _, err := s.Start(startRequest("main", briefA)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	first := -1
	for i, w := range *writes {
		switch {
		case w.AutoCleanup == "" && w.HumanInputMin == 0:
			// Before the run facts write: the fields are absent.
		case first < 0:
			first = i
			if w.AutoCleanup != "off" || w.HumanInputMin != 30 {
				t.Fatalf("first write with the fields holds %q/%d, want off/30", w.AutoCleanup, w.HumanInputMin)
			}
		default:
			if w.AutoCleanup != "off" || w.HumanInputMin != 30 {
				t.Errorf("write %d holds %q/%d, want the recorded off/30 (never rewritten)", i, w.AutoCleanup, w.HumanInputMin)
			}
		}
	}
	if first < 0 {
		t.Fatalf("no state.json write recorded the fields (%d writes observed)", len(*writes))
	}
}
