package job

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

const (
	teamOrg  = "example-org"
	teamRepo = "example-repo"
)

func TestTeam(t *testing.T) {
	t.Run("repository file wins over default and project", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "panes=2\nlane.build.roles=implementer\n")
		f.writeTeam(t, "default", "panes=3\nlane.build.roles=implementer\n")
		f.writeProject(t, "panes=4\nlane.review.roles=reviewer\n")
		got := f.must(t, nil)
		if got.Source != "teams/example-org/example-repo.conf" || got.File != f.repoFile() {
			t.Fatalf("source %q file %q", got.Source, got.File)
		}
		if value(t, got, "panes") != "2" || value(t, got, "lane.review.roles") != "" {
			t.Fatalf("pairs = %#v", got.Pairs)
		}
	})

	t.Run("default file is second", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "default", "panes=3\nlane.build.roles=implementer\n")
		f.writeProject(t, "panes=4\nlane.review.roles=reviewer\n")
		got := f.must(t, nil)
		if got.Source != "teams/default.conf" || got.File != f.defaultFile() || value(t, got, "panes") != "3" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("project config is third only with lane roles", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeProject(t, "panes=4\nlane.review.roles=reviewer\n")
		got := f.must(t, nil)
		if got.Source != ".agents/herdr-soho.conf" || value(t, got, "lane.review.roles") != "reviewer" || value(t, got, "panes") != "4" {
			t.Fatalf("got %+v", got)
		}
		if _, ok := pair(got, "lane.build.roles"); ok {
			t.Fatal("preset build lane was invented")
		}
	})

	t.Run("project without lane roles exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeProject(t, "panes=4\nlane.review.kind=grok\n")
		f.wantCode(t, nil, 2)
	})

	t.Run("legacy project config is not a team", func(t *testing.T) {
		f := newTeamFix(t)
		writeTeamFile(t, filepath.Join(f.repo, ".agents", "herdr-agents.conf"), "lane.build.roles=implementer\n")
		f.wantCode(t, nil, 2)
	})

	t.Run("no configuration exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		f.wantCode(t, nil, 2)
	})

	t.Run("present repo file without lane roles does not fall through", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "panes=2\n")
		f.writeTeam(t, "default", "panes=3\nlane.build.roles=implementer\n")
		f.wantCode(t, nil, 2)
	})

	t.Run("brief overrides apply last", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "panes=4\nlane.build.roles=implementer\nlane.review.roles=reviewer\nlane.review.effort=low\n")
		overrides := map[string]string{
			"lane.review.effort": "high",
			"lane.sec.roles":     "security-reviewer",
			"lane.sec.panes":     "1",
		}
		got := f.must(t, overrides)
		if value(t, got, "lane.review.effort") != "high" || value(t, got, "panes") != "4" || value(t, got, "lane.sec.roles") != "security-reviewer" || value(t, got, "lane.sec.panes") != "1" {
			t.Fatalf("pairs = %#v", got.Pairs)
		}
		wantKeys := []string{"lane.review.effort", "lane.sec.panes", "lane.sec.roles"}
		if !reflect.DeepEqual(got.OverrideKeys, wantKeys) {
			t.Fatalf("override keys = %#v", got.OverrideKeys)
		}
		if got.Pairs[0].Key != "panes" || got.Pairs[len(got.Pairs)-1].Key != "lane.sec.roles" {
			t.Fatalf("pair order = %#v", got.Pairs)
		}
	})

	t.Run("override can supply the only lane roles", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "default", "panes=2\n")
		got := f.must(t, map[string]string{"lane.build.roles": "implementer"})
		if got.Source != "teams/default.conf" || value(t, got, "lane.build.roles") != "implementer" || value(t, got, "panes") != "2" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("overrides do not create a source", func(t *testing.T) {
		f := newTeamFix(t)
		f.wantCode(t, map[string]string{"lane.build.roles": "implementer"}, 2)
	})

	t.Run("unknown key exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "nope=1\nlane.build.roles=implementer\n")
		f.writeTeam(t, "default", "panes=3\nlane.build.roles=implementer\n")
		err := f.wantCode(t, nil, 2)
		if !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("err = %v", err)
		}
		f = newTeamFix(t)
		f.writeTeam(t, "repo", "lane.build.roles=implementer\n")
		err = f.wantCode(t, map[string]string{"nope": "1"}, 2)
		if !strings.Contains(err.Error(), "unknown key") {
			t.Fatalf("override err = %v", err)
		}
	})

	t.Run("invalid value exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "panes=9\nlane.build.roles=implementer\n")
		f.writeTeam(t, "default", "panes=2\nlane.build.roles=implementer\n")
		err := f.wantCode(t, nil, 2)
		if !strings.Contains(err.Error(), "invalid value") {
			t.Fatalf("err = %v", err)
		}
		f = newTeamFix(t)
		f.writeTeam(t, "default", "lane.build.roles=implementer\n")
		err = f.wantCode(t, map[string]string{"lane.review.effort": "extreme"}, 2)
		if !strings.Contains(err.Error(), "invalid value") {
			t.Fatalf("override err = %v", err)
		}
		f = newTeamFix(t)
		f.writeTeam(t, "default", "lane.build.roles=implementer\n")
		_, err = f.resolve(map[string]string{"panes": ""})
		if teamCode(err) != 2 {
			t.Fatalf("empty override code = %d err = %v", teamCode(err), err)
		}
	})

	t.Run("quotes comments and last key win", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "# note\npanes=\"2\"\npanes=3\nlane.build.roles=implementer # trailing\n")
		got := f.must(t, nil)
		if value(t, got, "panes") != "3" || value(t, got, "lane.build.roles") != "implementer" || len(got.Pairs) != 2 {
			t.Fatalf("pairs = %#v", got.Pairs)
		}
		if got.OverrideKeys == nil {
			t.Fatal("override keys are nil")
		}
	})

	t.Run("crlf team file", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "default", "panes=2\r\nlane.build.roles=implementer\r\n")
		got := f.must(t, nil)
		if value(t, got, "panes") != "2" {
			t.Fatalf("pairs = %#v", got.Pairs)
		}
	})

	t.Run("windows path uses APPDATA", func(t *testing.T) {
		root := t.TempDir()
		appdata := filepath.Join(root, "roaming")
		home := filepath.Join(root, "home")
		repo := filepath.Join(root, "checkout")
		if err := os.MkdirAll(repo, 0o700); err != nil {
			t.Fatal(err)
		}
		env := platform.Env{
			"APPDATA": appdata, "USERPROFILE": home, "HOME": home,
			"HERDR_SOHO_SKILL_DIR": skillDir(t), "PATH": filepath.Join(root, "bin"),
		}
		file := filepath.Join(appdata, "herdr-soho", "teams", teamOrg, teamRepo+".conf")
		writeTeamFile(t, file, "panes=3\nlane.build.roles=implementer\n")
		writeTeamFile(t, filepath.Join(home, ".config", "herdr-soho", "teams", teamOrg, teamRepo+".conf"), "panes=4\nlane.build.roles=implementer\n")
		got, err := ResolveTeam("win32", env, repo, teamOrg, teamRepo, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got.File != file || got.Source != "teams/example-org/example-repo.conf" || value(t, got, "panes") != "3" {
			t.Fatalf("got file %q source %q pairs %#v", got.File, got.Source, got.Pairs)
		}
		if filepath.Dir(platform.UserConfigPath("win32", env)) != filepath.Join(appdata, "herdr-soho") {
			t.Fatalf("user config dir = %s", platform.UserConfigPath("win32", env))
		}
	})

	t.Run("shared parser treats bom as whitespace", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "repo", "\uFEFFpanes=2\nlane.build.roles=implementer\n")
		got := f.must(t, nil)
		if value(t, got, "panes") != "2" {
			t.Fatalf("leading bom: pairs = %#v", got.Pairs)
		}
		f = newTeamFix(t)
		f.writeTeam(t, "default", "panes=2\uFEFF\nlane.build.roles=implementer\n")
		got = f.must(t, nil)
		if value(t, got, "panes") != "2" {
			t.Fatalf("trailing bom: pairs = %#v", got.Pairs)
		}
		f = newTeamFix(t)
		f.writeProject(t, "\uFEFFlane.build.roles=implementer\n")
		got = f.must(t, nil)
		if got.Source != ".agents/herdr-soho.conf" || value(t, got, "lane.build.roles") != "implementer" {
			t.Fatalf("bom project: got %+v", got)
		}
	})

	t.Run("non-file team path exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		if err := os.MkdirAll(f.repoFile(), 0o700); err != nil {
			t.Fatal(err)
		}
		f.writeTeam(t, "default", "panes=2\nlane.build.roles=implementer\n")
		f.wantCode(t, nil, 2)
	})

	t.Run("invalid org or repo exits 2", func(t *testing.T) {
		f := newTeamFix(t)
		f.writeTeam(t, "default", "lane.build.roles=implementer\n")
		for _, tc := range []struct{ org, repo string }{
			{org: "..", repo: teamRepo},
			{org: "a/b", repo: teamRepo},
			{org: teamOrg, repo: "a/b"},
			{org: teamOrg, repo: "bad repo"},
			{org: "", repo: teamRepo},
		} {
			_, err := ResolveTeam("linux", f.env, f.repo, tc.org, tc.repo, nil)
			if teamCode(err) != 2 {
				t.Fatalf("org %q repo %q err %v", tc.org, tc.repo, err)
			}
		}
	})
}

type teamFix struct {
	repo string
	env  platform.Env
}

func newTeamFix(t *testing.T) teamFix {
	t.Helper()
	root := t.TempDir()
	repo := filepath.Join(root, "checkout")
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	for _, dir := range []string{repo, home, conf, filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return teamFix{
		repo: repo,
		env: platform.Env{
			"HOME": home, "USERPROFILE": home, "XDG_CONFIG_HOME": conf,
			"HERDR_SOHO_SKILL_DIR": skillDir(t), "PATH": filepath.Join(root, "bin"),
		},
	}
}

func (f teamFix) repoFile() string {
	return filepath.Join(filepath.Dir(platform.UserConfigPath("linux", f.env)), "teams", teamOrg, teamRepo+".conf")
}

func (f teamFix) defaultFile() string {
	return filepath.Join(filepath.Dir(platform.UserConfigPath("linux", f.env)), "teams", "default.conf")
}

func (f teamFix) writeTeam(t *testing.T, which, body string) {
	t.Helper()
	path := f.defaultFile()
	if which == "repo" {
		path = f.repoFile()
	}
	writeTeamFile(t, path, body)
}

func (f teamFix) writeProject(t *testing.T, body string) {
	t.Helper()
	writeTeamFile(t, filepath.Join(f.repo, ".agents", "herdr-soho.conf"), body)
}

func (f teamFix) resolve(overrides map[string]string) (Team, error) {
	return ResolveTeam("linux", f.env, f.repo, teamOrg, teamRepo, overrides)
}

func (f teamFix) must(t *testing.T, overrides map[string]string) Team {
	t.Helper()
	got, err := f.resolve(overrides)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return got
}

func (f teamFix) wantCode(t *testing.T, overrides map[string]string, code int) error {
	t.Helper()
	_, err := f.resolve(overrides)
	if teamCode(err) != code {
		t.Fatalf("code = %d, err = %v", teamCode(err), err)
	}
	return err
}

func writeTeamFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func value(t *testing.T, team Team, key string) string {
	t.Helper()
	got, ok := pair(team, key)
	if !ok {
		return ""
	}
	return got
}

func pair(team Team, key string) (string, bool) {
	for _, item := range team.Pairs {
		if item.Key == key {
			return item.Value, true
		}
	}
	return "", false
}

func teamCode(err error) int {
	var exit *platform.ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return -1
}

func skillDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "skills", "herdr-soho")
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("skill directory %q: %v", dir, err)
	}
	return dir
}
