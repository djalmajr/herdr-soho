package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func runTM10Env(t *testing.T, omitted string, extra platform.Env) (string, string, int) {
	t.Helper()
	env, _ := commandFixture(t)
	bin := t.TempDir()
	rules := map[string][]fakecli.Rule{
		"git":   {{ArgvPrefix: true, Argv: []string{"-C"}, Stdout: "abc1234 2026-09-24\n"}},
		"herdr": {{Argv: []string{"--version"}, Stdout: "herdr 9.9.9\n"}},
		"grok":  {{Argv: []string{"--version"}, Stdout: "grok 8.8.8\n"}},
		"agy":   {{Argv: []string{"--version"}, Stdout: "partial\n", Code: 1}},
	}
	for name, set := range rules {
		if name == omitted {
			continue
		}
		if _, err := fakecli.Install(t, bin, name, set); err != nil {
			t.Fatal(err)
		}
	}
	clean := platform.Env{}
	for _, entry := range fakecli.Env(testutil.CleanEnv(t), bin) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			clean[key] = value
		}
	}
	for k, v := range env {
		clean[k] = v
	}
	clean["PATH"] = bin
	clean["HOME"], clean["USERPROFILE"] = t.TempDir(), t.TempDir()
	clean["XDG_CONFIG_HOME"] = filepath.Join(t.TempDir(), "config")
	clean["TMPDIR"] = t.TempDir()
	clean["HERDR_SOHO_SKILL_DIR"] = testSkillDir(t)
	for k, v := range extra {
		clean[k] = v
	}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	code := Run([]string{"env"}, clean)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return out.String(), stderr.String(), code
}

func TestTM10EnvMirrors(t *testing.T) {
	t.Run(`JS: "env: a kind whose --version fails prints ? (even when it printed first)"`, func(t *testing.T) {
		out, stderr, code := runTM10Env(t, "", platform.Env{})
		if code != 0 || stderr != "" || !strings.Contains(out, "kind agy: ?") {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
	})
	t.Run(`JS: "env: herdr missing prints unknown"`, func(t *testing.T) {
		out, stderr, code := runTM10Env(t, "herdr", nil)
		if code != 0 || stderr != "" || !strings.Contains(out, "herdr: unknown\n") {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
	})
	t.Run(`without git the first line is the binary's version (the JS prints unversioned)`, func(t *testing.T) {
		out, stderr, code := runTM10Env(t, "git", nil)
		want := "herdr-soho: " + strings.TrimPrefix(strings.TrimSpace(versionText(version, vcsRevision())), "herdr-soho ") + "\n"
		if code != 0 || stderr != "" || !strings.HasPrefix(out, want) || strings.Contains(out, "unversioned") {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
	})
	t.Run(`JS: "env: the runtime line names the executing runtime"`, func(t *testing.T) {
		out, stderr, code := runTM10Env(t, "", nil)
		if code != 0 || stderr != "" || !strings.Contains(out, "runtime: go go") {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
	})
	t.Run(`JS: "env: the config line reflects the effective config (env overrides)"`, func(t *testing.T) {
		out, stderr, code := runTM10Env(t, "", platform.Env{"HERDR_SOHO_LAYOUT": "tab", "HERDR_SOHO_APPROVALS": "full"})
		if code != 0 || stderr != "" || !strings.Contains(out, "config: layout=tab reuse_workers=on approvals=full") {
			t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
		}
	})
}

func TestEnvPrintsTheSkillDirAndItsOrigin(t *testing.T) {
	fixtureEnv, _ := commandFixture(t)
	dir := fixtureEnv.Get("HERDR_SOHO_SKILL_DIR")
	if dir == "" {
		t.Fatal("the command fixture sets no HERDR_SOHO_SKILL_DIR")
	}
	out, stderr, code := runTM10Env(t, "", nil)
	if code != 0 || stderr != "" || !strings.Contains(out, "\nskill dir: "+dir+" (HERDR_SOHO_SKILL_DIR)\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out, stderr)
	}
}
