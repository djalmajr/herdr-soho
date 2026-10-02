package core_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/cli"
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestCleanHeldRosterLockRecordsFriction(t *testing.T) {
	// Mutation captured: a plain lock timeout exits clean without an error row in friction.log.
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	skill := filepath.Join(root, "skill")
	state := filepath.Join(root, "state", "ws")
	for _, dir := range []string{bin, skill, state} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte("ghost\tpane\tgrok\timplementer\txai\t0\t/tmp\tnow\t\t\t\t\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(state, "agents.lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{
		"HERDR_ENV": "1", "HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_SOHO_FAKECLI_CONFIG": bin,
		"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": root, "PATH": bin, "TMPDIR": root,
	}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var stdout, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &stdout, &stderr
	defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
	if code := cli.Run([]string{"clean"}, env); code != 4 {
		t.Fatalf("clean exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	log, err := os.ReadFile(filepath.Join(state, "friction.log"))
	if err != nil || !strings.Contains(string(log), "error(exit 4)\tclean\troster lock ") {
		t.Fatalf("friction.log=%q err=%v", log, err)
	}
}

// D3 (appliance): new friction.log lines are written in UTC with the Z
// suffix; --since keeps reading the legacy zone-less lines as local time.
// The host's zone is fixed with an injected time.Location (FixedZone UTC-3,
// no DST), so the mixed-line filtering is deterministic on any machine.
func TestFrictionUtc(t *testing.T) {
	t.Run("friction_utc_new_lines_carry_utc_z", func(t *testing.T) {
		// Mutation captured: formatting the line with NowISO (local, zone-less)
		// writes "2026-10-01T22:03:56" instead of "2026-10-02T01:03:56Z".
		fixed := time.Date(2026, 10, 2, 1, 3, 56, 0, time.UTC) // 2026-10-01T22:03:56 at UTC-3
		oldNow := platform.Now
		platform.Now = func() time.Time { return fixed }
		t.Cleanup(func() { platform.Now = oldNow })
		oldLocal := time.Local
		time.Local = time.FixedZone("UTC-3", -3*3600)
		t.Cleanup(func() { time.Local = oldLocal })

		logFile := filepath.Join(t.TempDir(), "friction.log")
		oldErr := platform.Stderr
		platform.Stderr = &bytes.Buffer{}
		t.Cleanup(func() { platform.Stderr = oldErr })
		// frictionLog prefers the package-level friction command set by the CLI
		// entry (the earlier clean test in this binary leaves it "clean"), so
		// pin it for the duration of the test.
		core.SetFrictionCommand("wait")
		t.Cleanup(func() { core.SetFrictionCommand("") })

		core.Warn("legacy format line", logFile, "wait")
		core.RecordFrictionError("boom", 4, logFile, "wait")

		data, err := os.ReadFile(logFile)
		if err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 {
			t.Fatalf("friction.log=%q", data)
		}
		if want := "2026-10-02T01:03:56Z\twarning\twait\tlegacy format line"; lines[0] != want {
			t.Fatalf("warning line=%q, want %q", lines[0], want)
		}
		if want := "2026-10-02T01:03:56Z\terror(exit 4)\twait\tboom"; lines[1] != want {
			t.Fatalf("error line=%q, want %q", lines[1], want)
		}
		if strings.Contains(string(data), "2026-10-01T22:03:56") {
			t.Fatalf("local time leaked into a new line: %q", data)
		}
	})
	t.Run("friction_utc_since_mixes_local_and_utc_lines", func(t *testing.T) {
		// --since 2026-10-01T23:00:00Z against a mix of legacy local lines
		// (UTC-3 here) and new UTC lines: the kept set is decided in absolute
		// time, so no `date` conversion is needed to check the answer.
		oldLocal := time.Local
		time.Local = time.FixedZone("UTC-3", -3*3600)
		t.Cleanup(func() { time.Local = oldLocal })

		root := t.TempDir()
		bin := filepath.Join(root, "bin")
		skill := filepath.Join(root, "skill")
		state := filepath.Join(root, "state", "ws")
		for _, dir := range []string{bin, skill, state} {
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}}); err != nil {
			t.Fatal(err)
		}
		lines := []string{
			"2026-10-01T22:03:56\twarning\twait\tlegacy local kept",              // 2026-10-02T01:03:56Z -> kept
			"2026-10-01T19:59:59\twarning\twait\tlegacy local dropped",           // 2026-10-01T22:59:59Z -> dropped
			"2026-10-01T22:59:59Z\tnote\tfriction\tnew utc dropped",              // before the cutoff -> dropped
			"2026-10-02T00:00:00Z\tnote\tfriction\tnew utc kept",                 // after the cutoff -> kept
			"2026-10-01T20:00:00\twarning\tdispatch\tlegacy local at the cutoff", // 2026-10-01T23:00:00Z == cutoff -> kept (inclusive)
		}
		logPath := filepath.Join(state, "friction.log")
		if err := os.WriteFile(logPath, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{
			"HERDR_ENV": "1", "HERDR_SOHO_DIR": filepath.Join(root, "state"),
			"HERDR_SOHO_SKILL_DIR": skill, "HERDR_WORKSPACE_ID": "ws", "HOME": root, "PATH": bin, "TMPDIR": root,
		}
		oldOut, oldErr := platform.Stdout, platform.Stderr
		var stdout, stderr bytes.Buffer
		platform.Stdout, platform.Stderr = &stdout, &stderr
		t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })

		code := cli.Run([]string{"friction", "--since", "2026-10-01T23:00:00Z"}, env)
		if code != 0 {
			t.Fatalf("friction --since exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
		want := "friction log (" + logPath + "): timestamp, level, command, message\n" +
			lines[0] + "\n" +
			lines[3] + "\n" +
			lines[4] + "\n"
		if stdout.String() != want {
			t.Fatalf("filtered=%q, want %q", stdout.String(), want)
		}
	})
}
