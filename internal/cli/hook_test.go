package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestNativeHookReminderAndOutsideHerdr(t *testing.T) {
	env, cwd := commandFixture(t)
	env["PATH"] = t.TempDir()
	for _, nowrite := range []string{"", "HERDR_SOHO_NOWRITE", "HERDR_AGENTS_NOWRITE"} {
		delete(env, "HERDR_SOHO_NOWRITE")
		delete(env, "HERDR_AGENTS_NOWRITE")
		if nowrite != "" {
			env[nowrite] = "1"
		}
		for _, mode := range []string{"reminder", "doctor"} {
			for _, value := range []string{"", "0"} {
				env["HERDR_ENV"] = value
				code, out, stderr := runIn(t, []string{"hook", mode}, env, cwd)
				if code != 0 || out != "" || stderr != "" {
					t.Fatalf("outside Herdr %s %s: %d %q %q", nowrite, mode, code, out, stderr)
				}
			}
		}
		env["HERDR_ENV"] = "1"
		code, out, stderr := runIn(t, []string{"hook", "reminder"}, env, cwd)
		if code != 0 || out != hookReminder+"\n" || stderr != "" {
			t.Fatalf("%s reminder: %d %q %q", nowrite, code, out, stderr)
		}
		code, out, stderr = runIn(t, []string{"hook", "doctor"}, env, cwd)
		if code != 0 || !strings.Contains(out, "herdr-soho doctor: ") || stderr != "" {
			t.Fatalf("%s doctor: %d %q %q", nowrite, code, out, stderr)
		}
	}
	delete(env, "HERDR_SOHO_NOWRITE")
	delete(env, "HERDR_AGENTS_NOWRITE")
	env["HERDR_ENV"] = "1"
	code, out, stderr := runIn(t, []string{"hook", "reminder"}, env, cwd)
	if code != 0 || out != hookReminder+"\n" || stderr != "" {
		t.Fatalf("reminder: %d %q %q", code, out, stderr)
	}
	for _, args := range [][]string{{"hook"}, {"hook", "other"}, {"hook", "doctor", "--fix"}} {
		code, out, stderr := runIn(t, args, env, cwd)
		if code != 2 || out != "" || !strings.Contains(stderr, "usage: hook") {
			t.Fatalf("invalid hook %v: %d %q %q", args, code, out, stderr)
		}
	}
}

func TestNativeDoctorHookUsesClaudeProject(t *testing.T) {
	env, project := commandFixture(t)
	configDir := filepath.Join(project, ".agents")
	if err := os.Mkdir(configDir, 0o700); err != nil {
		t.Fatal(err)
	}
	configFile := filepath.Join(configDir, "herdr-soho.conf")
	const config = "panes=9\n"
	if err := os.WriteFile(configFile, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	env["HERDR_ENV"], env["CLAUDE_PROJECT_DIR"], env["PATH"] = "1", project, t.TempDir()
	code, out, stderr := runIn(t, []string{"hook", "doctor"}, env, cwd)
	if code != 0 || !strings.Contains(out, "panes='9'") || stderr != "" {
		t.Fatalf("project doctor: %d %q %q", code, out, stderr)
	}
	data, err := os.ReadFile(configFile)
	if err != nil || string(data) != config {
		t.Fatalf("doctor changed project config: %q %v", data, err)
	}
	for _, dir := range []string{project, cwd} {
		for _, name := range []string{".herdr-soho", ".gitignore", ".claude"} {
			if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
				t.Fatalf("doctor created %s in %s: %v", name, dir, err)
			}
		}
	}
}

func TestNativeDoctorHookReadOnlyWarnings(t *testing.T) {
	env, cwd := commandFixture(t)
	env["HERDR_ENV"] = "1"
	env["PATH"] = t.TempDir()
	// No installed shell, Herdr, or agent CLI is available. Doctor warnings
	// must still be reported without creating state or failing startup.
	code, out, stderr := runIn(t, []string{"hook", "doctor"}, env, cwd)
	if code != 0 || out == "" || stderr != "" {
		t.Fatalf("doctor hook: %d %q %q", code, out, stderr)
	}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		if !strings.HasPrefix(line, "herdr-soho doctor: ") {
			t.Fatalf("hook leaked non-warning doctor output: %q", line)
		}
	}
	for _, name := range []string{".herdr-soho", ".gitignore", ".claude"} {
		if _, err := os.Stat(filepath.Join(cwd, name)); !os.IsNotExist(err) {
			t.Fatalf("read-only hook created %s: %v", name, err)
		}
	}
}

func TestNativeDoctorHookMissingSkillIsAdvisory(t *testing.T) {
	env := platform.Env{"HERDR_ENV": "1", "HERDR_SOHO_SKILL_DIR": filepath.Join(t.TempDir(), "missing"), "PATH": t.TempDir(), "HOME": t.TempDir(), "USERPROFILE": t.TempDir()}
	code, out, stderr := runIn(t, []string{"hook", "doctor"}, env, t.TempDir())
	if code != 0 || !strings.Contains(out, "herdr-soho doctor: ") || stderr != "" {
		t.Fatalf("missing skill hook: %d %q %q", code, out, stderr)
	}
}
