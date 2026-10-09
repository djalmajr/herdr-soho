package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestMachineConfig(t *testing.T) {
	t.Run("shipped defaults and no retention key", func(t *testing.T) {
		data, err := os.ReadFile(filepath.Join(skillDir(t), "config.defaults"))
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		for _, line := range []string{
			"machine_label=",
			"job_orgs=",
			"job_repos_root=~/repo.git",
			"job_timeout=120",
			"job_wake_cmd=",
		} {
			if !strings.Contains(text, line) {
				t.Errorf("config.defaults missing %q", line)
			}
		}
		for _, banned := range []string{"job_keep_hours", "retencao_ate", "retention"} {
			if strings.Contains(text, banned) {
				t.Errorf("config.defaults contains %q", banned)
			}
		}
	})

	t.Run("known keys and timeout bounds", func(t *testing.T) {
		env := platform.Env{"HERDR_SOHO_SKILL_DIR": skillDir(t)}
		cwd := t.TempDir()
		for _, key := range []string{"machine_label", "job_orgs", "job_repos_root", "job_timeout", "job_wake_cmd"} {
			if !core.ConfigKeyOk(key) {
				t.Errorf("ConfigKeyOk(%q) = false", key)
			}
		}
		if core.ConfigKeyOk("job_keep_hours") || core.ConfigKeyOk("nope") || !core.ConfigKeyOk("layout") {
			t.Fatal("existing key recognition changed")
		}
		for _, key := range core.ConfigScalarKeys {
			if key == "machine_label" || key == "job_timeout" {
				t.Fatalf("%s joined ConfigScalarKeys", key)
			}
		}
		for _, tc := range []struct {
			key, value string
			want       bool
		}{
			{"job_timeout", "1", true},
			{"job_timeout", "120", true},
			{"job_timeout", "1440", true},
			{"job_timeout", "0", false},
			{"job_timeout", "1441", false},
			{"job_timeout", "90m", false},
			{"machine_label", "machine-a", true},
			{"machine_label", "a#b", false},
			{"job_orgs", "example-org, other-org", true},
			{"job_repos_root", "~/repo.git", true},
			{"job_wake_cmd", "echo hi", true},
			{"max_workers", "0", true},
			{"max_workers", "-1", false},
			{"panes", "4", true},
			{"panes", "5", false},
		} {
			if got := core.ConfigValueOk(tc.key, tc.value, env, cwd); got != tc.want {
				t.Errorf("ConfigValueOk(%q, %q) = %t, want %t", tc.key, tc.value, got, tc.want)
			}
		}
	})

	t.Run("defaults expand the checkout root", func(t *testing.T) {
		f := newMachineFix(t)
		got := f.must(t, "linux")
		if got.Label != "" || len(got.Orgs) != 0 || got.TimeoutMin != 120 || got.WakeCmd != "" {
			t.Fatalf("defaults = %+v", got)
		}
		if got.ReposRoot != filepath.Join(f.home, "repo.git") {
			t.Fatalf("repos root = %q", got.ReposRoot)
		}
		if err := got.LabelMatches(""); err != nil {
			t.Fatal(err)
		}
		if err := got.LabelMatches("machine-a"); teamCode(err) != 2 {
			t.Fatalf("empty label vs brief: %v", err)
		}
		if teamCode(got.AllowsOrg("example-org")) != 2 {
			t.Fatal("empty job_orgs allowed an organization")
		}
	})

	t.Run("user file and environment", func(t *testing.T) {
		f := newMachineFix(t)
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "machine_label=from-file\njob_orgs=example-org, other-org\njob_repos_root=/data/repos\njob_timeout=90\njob_wake_cmd=echo hi\n")
		got := f.must(t, "linux")
		if got.Label != "from-file" || got.TimeoutMin != 90 || got.WakeCmd != "echo hi" || got.ReposRoot != "/data/repos" {
			t.Fatalf("user = %+v", got)
		}
		if err := got.AllowsOrg("example-org"); err != nil {
			t.Fatal(err)
		}
		if err := got.AllowsOrg("other-org"); err != nil {
			t.Fatal(err)
		}
		if teamCode(got.AllowsOrg("nope")) != 2 {
			t.Fatal("unexpected org was allowed")
		}
		if err := got.LabelMatches("from-file"); err != nil {
			t.Fatal(err)
		}
		if teamCode(got.LabelMatches("machine-b")) != 2 {
			t.Fatal("mismatched label was accepted")
		}
		if err := got.Admit("", "example-org", "example-repo"); err != nil {
			t.Fatal(err)
		}
		if teamCode(got.Admit("other", "example-org", "example-repo")) != 2 {
			t.Fatal("admit accepted a different machine")
		}
		if teamCode(got.Admit("", "example-org", "bad repo")) != 2 {
			t.Fatal("admit accepted a bad repo")
		}
		if teamCode(got.Admit("", "missing", "example-repo")) != 2 {
			t.Fatal("admit accepted an org outside job_orgs")
		}
	})

	t.Run("invalid timeout and organizations", func(t *testing.T) {
		f := newMachineFix(t)
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "job_timeout=0\n")
		if _, err := LoadMachine("linux", f.env); teamCode(err) != 2 {
			t.Fatalf("timeout 0: %v", err)
		}
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "job_timeout=1441\n")
		if _, err := LoadMachine("linux", f.env); teamCode(err) != 2 {
			t.Fatalf("timeout 1441: %v", err)
		}
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "job_orgs=example-org,,other\n")
		if _, err := LoadMachine("linux", f.env); teamCode(err) != 2 {
			t.Fatalf("empty org token: %v", err)
		}
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "job_timeout=1440\njob_orgs=example-org\n")
		got := f.must(t, "linux")
		if got.TimeoutMin != 1440 || teamCode(got.AllowsOrg("example-org")) != -1 {
			t.Fatalf("upper bound = %+v", got)
		}
	})

	t.Run("windows checkout root uses USERPROFILE", func(t *testing.T) {
		f := newMachineFix(t)
		got := f.must(t, "win32")
		if got.ReposRoot != filepath.Join(f.profile, "repo.git") {
			t.Fatalf("windows root = %q", got.ReposRoot)
		}
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), `job_repos_root=%USERPROFILE%\repo.git`+"\n")
		got = f.must(t, "win32")
		if got.ReposRoot != filepath.Join(f.profile, "repo.git") {
			t.Fatalf("USERPROFILE form = %q", got.ReposRoot)
		}
	})
	t.Run("project session and environment never change the user file", func(t *testing.T) {
		f := newMachineFix(t)
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "machine_label=user-label\njob_orgs=user-org\njob_repos_root=/data/user\njob_timeout=77\njob_wake_cmd=echo user\n")
		writeTeamFile(t, filepath.Join(f.cwd, ".agents", "herdr-soho.conf"), "machine_label=project-label\njob_orgs=project-org\njob_repos_root=/data/project\njob_timeout=66\njob_wake_cmd=echo project\n")
		writeTeamFile(t, filepath.Join(f.cwd, ".herdr-soho", "ws1", "session.conf"), "machine_label=session-label\njob_orgs=session-org\njob_repos_root=/data/session\njob_timeout=55\njob_wake_cmd=echo session\n")
		env := platform.Env{}
		for name, value := range f.env {
			env[name] = value
		}
		env["HERDR_WORKSPACE_ID"] = "ws1"
		env["HERDR_SOHO_MACHINE_LABEL"] = "env-label"
		env["HERDR_SOHO_JOB_ORGS"] = "env-org"
		env["HERDR_SOHO_JOB_REPOS_ROOT"] = "/data/env"
		env["HERDR_SOHO_JOB_TIMEOUT"] = "45"
		env["HERDR_SOHO_JOB_WAKE_CMD"] = "echo env"
		got, err := LoadMachine("linux", env)
		if err != nil {
			t.Fatal(err)
		}
		if got.Label != "user-label" || len(got.Orgs) != 1 || got.Orgs[0] != "user-org" || got.ReposRoot != "/data/user" || got.TimeoutMin != 77 || got.WakeCmd != "echo user" {
			t.Fatalf("machine = %+v", got)
		}
		if err := got.Admit("", "user-org", "example-repo"); err != nil {
			t.Fatal(err)
		}
		if teamCode(got.Admit("", "project-org", "example-repo")) != 2 {
			t.Fatal("project-supplied organization was admitted")
		}
		if teamCode(got.Admit("", "env-org", "example-repo")) != 2 {
			t.Fatal("environment-supplied organization was admitted")
		}
	})

	t.Run("project only job_orgs leaves the allowlist empty", func(t *testing.T) {
		f := newMachineFix(t)
		writeTeamFile(t, platform.UserConfigPath("linux", f.env), "machine_label=user-label\n")
		writeTeamFile(t, filepath.Join(f.cwd, ".agents", "herdr-soho.conf"), "job_orgs=other-org\n")
		got := f.must(t, "linux")
		if len(got.Orgs) != 0 {
			t.Fatalf("orgs = %#v", got.Orgs)
		}
		if teamCode(got.Admit("", "other-org", "example-repo")) != 2 {
			t.Fatal("project-supplied organization was admitted")
		}
	})

	t.Run("environment alone leaves all defaults", func(t *testing.T) {
		f := newMachineFix(t)
		f.env["HERDR_SOHO_MACHINE_LABEL"] = "env-label"
		f.env["HERDR_SOHO_JOB_ORGS"] = "env-org"
		f.env["HERDR_SOHO_JOB_REPOS_ROOT"] = "/data/env"
		f.env["HERDR_SOHO_JOB_TIMEOUT"] = "45"
		f.env["HERDR_SOHO_JOB_WAKE_CMD"] = "echo env"
		got := f.must(t, "linux")
		if got.Label != "" || len(got.Orgs) != 0 || got.ReposRoot != filepath.Join(f.home, "repo.git") || got.TimeoutMin != 120 || got.WakeCmd != "" {
			t.Fatalf("machine = %+v", got)
		}
	})

	t.Run("go defaults match config.defaults", func(t *testing.T) {
		pairs, err := core.ReadConfigFilePairs(filepath.Join(skillDir(t), "config.defaults"))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, pair := range pairs {
			got[pair.Key] = pair.Value
		}
		for key, want := range map[string]string{
			"machine_label":  defaultMachineLabel,
			"job_orgs":       defaultJobOrgs,
			"job_repos_root": defaultJobReposRoot,
			"job_timeout":    defaultJobTimeout,
			"job_wake_cmd":   defaultJobWakeCmd,
		} {
			if value := got[key]; value != want {
				t.Errorf("config.defaults %s = %q, want %q", key, value, want)
			}
		}
	})

	t.Run("appdata user file wins when xdg is absent", func(t *testing.T) {
		root := t.TempDir()
		appdata := filepath.Join(root, "roaming")
		home := filepath.Join(root, "home")
		profile := filepath.Join(root, "profile")
		env := platform.Env{
			"APPDATA": appdata, "HOME": home, "USERPROFILE": profile,
			"HERDR_SOHO_SKILL_DIR": skillDir(t), "PATH": filepath.Join(root, "bin"),
		}
		newUser := filepath.Join(appdata, "herdr-soho", "config")
		writeTeamFile(t, newUser, "machine_label=from-appdata\njob_orgs=appdata-org\n")
		writeTeamFile(t, filepath.Join(home, ".config", "herdr-soho", "config"), "machine_label=from-home\n")
		writeTeamFile(t, filepath.Join(profile, "AppData", "Roaming", "herdr-soho", "config"), "machine_label=from-profile\n")
		got, err := LoadMachine("win32", env)
		if err != nil {
			t.Fatal(err)
		}
		if got.Label != "from-appdata" || len(got.Orgs) != 1 || got.Orgs[0] != "appdata-org" {
			t.Fatalf("machine = %+v", got)
		}
		if err := os.Remove(newUser); err != nil {
			t.Fatal(err)
		}
		writeTeamFile(t, filepath.Join(appdata, "herdr-agents", "config"), "machine_label=from-legacy\n")
		got, err = LoadMachine("win32", env)
		if err != nil {
			t.Fatal(err)
		}
		if got.Label != "from-legacy" {
			t.Fatalf("legacy = %+v", got)
		}
		if platform.UserConfigPath("win32", env) != newUser {
			t.Fatalf("UserConfigPath changed: %s", platform.UserConfigPath("win32", env))
		}
	})
}

type machineFix struct {
	cwd, home, profile string
	env                platform.Env
}

func newMachineFix(t *testing.T) machineFix {
	t.Helper()
	root := t.TempDir()
	f := machineFix{
		cwd:     filepath.Join(root, "checkout"),
		home:    filepath.Join(root, "home"),
		profile: filepath.Join(root, "profile"),
	}
	for _, dir := range []string{f.cwd, f.home, f.profile, filepath.Join(root, "xdg"), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.env = platform.Env{
		"HOME": f.home, "USERPROFILE": f.profile, "XDG_CONFIG_HOME": filepath.Join(root, "xdg"),
		"HERDR_SOHO_SKILL_DIR": skillDir(t), "PATH": filepath.Join(root, "bin"),
	}
	return f
}

func (f machineFix) must(t *testing.T, osName string) Machine {
	t.Helper()
	got, err := LoadMachine(osName, f.env)
	if err != nil {
		t.Fatalf("LoadMachine: %v", err)
	}
	return got
}
