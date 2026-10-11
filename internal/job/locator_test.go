package job

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// winAbs stands in for the production abs in the win32 subtests: it accepts
// the Windows absolute shapes filepath.Abs refuses off Windows (a drive
// letter or a UNC prefix) and returns them untouched, so the subtests pin
// the selector behavior on the test host.
func winAbs(value string) (string, error) {
	if filepath.IsAbs(value) {
		return filepath.Clean(value), nil
	}
	if strings.HasPrefix(value, `\\`) || (len(value) >= 2 && value[1] == ':') {
		return value, nil
	}
	return "", errors.New("winAbs: not an absolute path: " + value)
}

// locatorCode returns the job exit code of an error, or -1 for any other
// shape: locator refusals must be the job *ExitError of code 2.
func locatorCode(err error) int {
	var exit *ExitError
	if errors.As(err, &exit) {
		return exit.Code
	}
	return -1
}

// wantLocatorMap fails when got differs from want in any key or value.
func wantLocatorMap(t *testing.T, got map[string]string, want map[string]string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("locator map = %v, want exactly %v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("locator map[%q] = %q, want %q (map %v)", key, got[key], value, got)
		}
	}
}

// TestSupervisorLocator pins the supervisorLocatorEnv allowlist: exactly
// XDG_CONFIG_HOME always, set to the base directory of the starter's own
// selection, plus the home variable only when reposRootHome is true.
func TestSupervisorLocator(t *testing.T) {
	t.Run("POSIX default without XDG", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{"HOME": home}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": filepath.Join(home, ".config")})
	})

	t.Run("POSIX home forwarded only when reposRootHome", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{"HOME": home}
		got, err := supervisorLocatorEnv("linux", env, true, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{
			"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
			"HOME":            home,
		})
	})

	t.Run("XDG absolute", func(t *testing.T) {
		home, xdg := t.TempDir(), t.TempDir()
		env := platform.Env{"HOME": home, "XDG_CONFIG_HOME": xdg}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": xdg})
	})

	t.Run("XDG relative normalized by absLocator", func(t *testing.T) {
		cwd := t.TempDir()
		t.Chdir(cwd)
		env := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": filepath.Join("rel cfg", "home")}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": filepath.Join(cwd, "rel cfg", "home")})
	})

	t.Run("spaces preserved except cleaning", func(t *testing.T) {
		env := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": "/cfg base/with spaces/./extra"}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": "/cfg base/with spaces/extra"})
	})

	t.Run("win32 XDG set", func(t *testing.T) {
		env := platform.Env{"XDG_CONFIG_HOME": `C:\xdg home`}
		got, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": `C:\xdg home`})
	})

	t.Run("win32 APPDATA only pins XDG to the APPDATA value", func(t *testing.T) {
		env := platform.Env{"APPDATA": `C:\app data`, "HOME": t.TempDir()}
		got, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": `C:\app data`})
	})

	t.Run("win32 neither XDG nor APPDATA falls back to USERPROFILE AppData Roaming", func(t *testing.T) {
		env := platform.Env{"USERPROFILE": `C:\users\me`}
		got, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		// The brief's rule: the base directory of the starter's own
		// selection, made absolute by the injected abs.
		base := filepath.Dir(filepath.Dir(platform.UserConfigPath("win32", env)))
		want, err := winAbs(base)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": want})
		// Joined the way platform.UserConfigPath joins on the test host:
		// the drive-letter segment keeps its backslash, the rest uses the
		// host separator.
		wantExplicit := `C:\users\me` + string(filepath.Separator) + "AppData" + string(filepath.Separator) + "Roaming"
		if got["XDG_CONFIG_HOME"] != wantExplicit {
			t.Fatalf("XDG_CONFIG_HOME = %q, want %q", got["XDG_CONFIG_HOME"], wantExplicit)
		}
	})

	t.Run("win32 USERPROFILE forwarded only when reposRootHome", func(t *testing.T) {
		env := platform.Env{"USERPROFILE": `C:\users\me`}
		base := filepath.Dir(filepath.Dir(platform.UserConfigPath("win32", env)))
		wantXDG, err := winAbs(base)
		if err != nil {
			t.Fatal(err)
		}
		got, err := supervisorLocatorEnv("win32", env, true, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{
			"XDG_CONFIG_HOME": wantXDG,
			"USERPROFILE":     `C:\users\me`,
		})
		gotOff, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, gotOff, map[string]string{"XDG_CONFIG_HOME": wantXDG})
	})

	t.Run("allowlist holds only the documented keys", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{
			"HOME":                  home,
			"GH_TOKEN":              "gh-secret",
			"AWS_SECRET_ACCESS_KEY": "aws-secret",
			"HERDR_SOHO_DIR":        "/state/soho",
			"HERDR_SOHO_STATE_DIR":  "/state/soho2",
			"APPDATA":               `C:\appdata`,
			"PATH":                  "/usr/bin:/bin",
			"SSH_AUTH_SOCK":         "/run/user/1000/ssh-agent.sock",
		}
		for _, reposRootHome := range []bool{false, true} {
			got, err := supervisorLocatorEnv("linux", env, reposRootHome, absLocator)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"XDG_CONFIG_HOME": filepath.Join(home, ".config")}
			if reposRootHome {
				want["HOME"] = home
			}
			wantLocatorMap(t, got, want)
			for _, secret := range []string{
				env["GH_TOKEN"], env["AWS_SECRET_ACCESS_KEY"], env["HERDR_SOHO_DIR"],
				env["HERDR_SOHO_STATE_DIR"], env["APPDATA"], env["PATH"], env["SSH_AUTH_SOCK"],
			} {
				for _, value := range got {
					if value == secret {
						t.Fatalf("locator map forwards a non-locator value: %q", value)
					}
				}
			}
		}
	})

	t.Run("refusals exit with code 2", func(t *testing.T) {
		home := t.TempDir()
		absFail := func(string) (string, error) { return "", errors.New("abs failure") }
		long4096 := "/" + strings.Repeat("a", 4095)
		long4097 := "/" + strings.Repeat("a", 4096)
		for _, tc := range []struct {
			name   string
			env    platform.Env
			abs    func(string) (string, error)
			home   bool
			accept bool
		}{
			{"XDG with NUL byte", platform.Env{"HOME": home, "XDG_CONFIG_HOME": "bad\x00dir"}, absLocator, false, false},
			{"XDG with DEL byte", platform.Env{"HOME": home, "XDG_CONFIG_HOME": "bad\x7fdir"}, absLocator, false, false},
			{"4097-byte XDG", platform.Env{"HOME": home, "XDG_CONFIG_HOME": long4097}, absLocator, false, false},
			{"abs error", platform.Env{"HOME": home, "XDG_CONFIG_HOME": t.TempDir()}, absFail, false, false},
			{"4096-byte XDG accepted at the boundary", platform.Env{"HOME": home, "XDG_CONFIG_HOME": long4096}, absLocator, false, true},
			{"HOME with a control byte refused when forwarded", platform.Env{"HOME": "h\x01ome", "XDG_CONFIG_HOME": t.TempDir()}, absLocator, true, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				got, err := supervisorLocatorEnv("linux", tc.env, tc.home, tc.abs)
				if tc.accept {
					if err != nil {
						t.Fatalf("accepted value refused: %v", err)
					}
					wantLocatorMap(t, got, map[string]string{"XDG_CONFIG_HOME": long4096})
					return
				}
				if err == nil {
					t.Fatalf("refused value returned a map: %v", got)
				}
				if locatorCode(err) != 2 {
					t.Fatalf("refusal code = %d, want 2 (%v)", locatorCode(err), err)
				}
				if got != nil {
					t.Fatalf("refusal returned a partial map: %v", got)
				}
			})
		}
	})
}

// TestLocatorValue pins the locatorValue normalization contract: empty,
// control-byte and over-long values are refused, and abs errors are refused
// the same way; absLocator resolves against the process working directory
// and cleans the result.
func TestLocatorValue(t *testing.T) {
	absIdentity := func(value string) (string, error) { return value, nil }
	absFail := func(string) (string, error) { return "", errors.New("abs failure") }
	absDirty := func(value string) (string, error) { return value + "\x01", nil }
	for _, tc := range []struct {
		name string
		raw  string
		abs  func(string) (string, error)
		want string
		ok   bool
	}{
		{"empty refused", "", absIdentity, "", false},
		{"NUL byte refused", "a\x00b", absIdentity, "", false},
		{"tab byte refused", "a\tb", absIdentity, "", false},
		{"DEL byte refused", "a\x7fb", absIdentity, "", false},
		{"4096 bytes accepted", strings.Repeat("a", 4096), absIdentity, strings.Repeat("a", 4096), true},
		{"4097 bytes refused", strings.Repeat("a", 4097), absIdentity, "", false},
		{"abs error refused", "ok", absFail, "", false},
		{"abs result still dirty refused", "ok", absDirty, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := locatorValue(tc.raw, tc.abs)
			if tc.ok {
				if err != nil || got != tc.want {
					t.Fatalf("locatorValue = %q, %v; want %q, nil", got, err, tc.want)
				}
				return
			}
			if err == nil || got != "" {
				t.Fatalf("locatorValue = %q, %v; want a refusal", got, err)
			}
			if locatorCode(err) != 2 {
				t.Fatalf("refusal code = %d, want 2 (%v)", locatorCode(err), err)
			}
		})
	}
	t.Run("absLocator resolves against the working directory and cleans", func(t *testing.T) {
		cwd := t.TempDir()
		t.Chdir(cwd)
		got, err := absLocator(filepath.Join("rel", "x"))
		if err != nil || got != filepath.Join(cwd, "rel", "x") {
			t.Fatalf("absLocator relative = %q, %v; want %q", got, err, filepath.Join(cwd, "rel", "x"))
		}
		got, err = absLocator("./a/../b")
		if err != nil || got != filepath.Join(cwd, "b") {
			t.Fatalf("absLocator cleaning = %q, %v; want %q", got, err, filepath.Join(cwd, "b"))
		}
		got, err = absLocator("/abs/../x")
		if err != nil || got != "/x" {
			t.Fatalf("absLocator absolute = %q, %v; want %q", got, err, "/x")
		}
	})
}

// TestReposRootFromHome pins reposRootFromHome: home-relative spellings are
// true, absolute and other relative roots are false, and the USERPROFILE
// prefix is case-insensitive.
func TestReposRootFromHome(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want bool
	}{
		{"empty default", "", true},
		{"tilde alone", "~", true},
		{"tilde slash prefix", "~/x", true},
		{"tilde backslash prefix", `~\x`, true},
		{"userprofile backslash", `%USERPROFILE%\x`, true},
		{"userprofile slash", "%USERPROFILE%/x", true},
		{"userprofile alone", "%USERPROFILE%", true},
		{"userprofile lowercase", `%userprofile%\x`, true},
		{"userprofile mixed case", `%UserProfile%\x`, true},
		{"absolute posix", "/abs/repos", false},
		{"absolute windows", `C:\repos`, false},
		{"relative", "relative/repos", false},
		{"tilde not a prefix", "~x", false},
		{"tilde in the middle", "a~b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reposRootFromHome(tc.raw); got != tc.want {
				t.Fatalf("reposRootFromHome(%q) = %t, want %t", tc.raw, got, tc.want)
			}
		})
	}
}

// TestLocatorMismatch pins the mismatch case: a starter env without XDG and
// a server env with its own XDG select different configuration bases, and
// overlaying the locator map onto the server env restores the starter's
// file selection and repos root.
func TestLocatorMismatch(t *testing.T) {
	overlay := func(server platform.Env, loc map[string]string) platform.Env {
		out := server.Clone()
		for key, value := range loc {
			out[key] = value
		}
		return out
	}
	selection := func(env platform.Env) string {
		return core.EffectiveConfigFile(platform.UserConfigPath("linux", env), core.LegacyUserConfigPath("linux", env))
	}
	currentFile := func(home string) string { return filepath.Join(home, ".config", "herdr-soho", "config") }
	legacyFile := func(home string) string { return filepath.Join(home, ".config", "herdr-agents", "config") }

	t.Run("config file selection matches the starter", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			files       []string
			wantCurrent bool
		}{
			{"current file only", []string{"current"}, true},
			{"legacy file only", []string{"legacy"}, false},
			{"both files current wins", []string{"current", "legacy"}, true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				home, xdg := t.TempDir(), t.TempDir()
				for _, which := range tc.files {
					if which == "current" {
						writeTeamFile(t, currentFile(home), "machine_label=starter\n")
					} else {
						writeTeamFile(t, legacyFile(home), "machine_label=starter-legacy\n")
					}
				}
				wantFile := legacyFile(home)
				if tc.wantCurrent {
					wantFile = currentFile(home)
				}
				starterEnv := platform.Env{"HOME": home}
				serverEnv := platform.Env{"XDG_CONFIG_HOME": xdg, "HOME": home}
				loc, err := supervisorLocatorEnv("linux", starterEnv, false, absLocator)
				if err != nil {
					t.Fatal(err)
				}
				starterWant := selection(starterEnv)
				if starterWant != wantFile {
					t.Fatalf("starter selection = %q, want %q", starterWant, wantFile)
				}
				if got := selection(overlay(serverEnv, loc)); got != wantFile {
					t.Fatalf("overlaid selection = %q, want %q (starter %q)", got, wantFile, starterWant)
				}
			})
		}
	})

	t.Run("LoadMachine repos root matches the starter", func(t *testing.T) {
		for _, tc := range []struct {
			name string
			body string // user configuration body; empty means no file
		}{
			{"default root", ""},
			{"home relative root", "job_repos_root=~/repos\n"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				home, xdg, serverHome := t.TempDir(), t.TempDir(), t.TempDir()
				if tc.body != "" {
					writeTeamFile(t, currentFile(home), tc.body)
				}
				starterEnv := platform.Env{"HOME": home}
				serverEnv := platform.Env{"XDG_CONFIG_HOME": xdg, "HOME": serverHome}
				loc, err := supervisorLocatorEnv("linux", starterEnv, true, absLocator)
				if err != nil {
					t.Fatal(err)
				}
				wantLocatorMap(t, loc, map[string]string{
					"XDG_CONFIG_HOME": filepath.Join(home, ".config"),
					"HOME":            home,
				})
				starter, err := LoadMachine("linux", starterEnv)
				if err != nil {
					t.Fatal(err)
				}
				overlaid, err := LoadMachine("linux", overlay(serverEnv, loc))
				if err != nil {
					t.Fatal(err)
				}
				wantRoot := filepath.Join(home, "repo.git")
				if tc.body != "" {
					wantRoot = filepath.Join(home, "repos")
				}
				if starter.ReposRoot != wantRoot {
					t.Fatalf("starter ReposRoot = %q, want %q", starter.ReposRoot, wantRoot)
				}
				if overlaid.ReposRoot != starter.ReposRoot {
					t.Fatalf("overlaid ReposRoot %q differs from starter %q", overlaid.ReposRoot, starter.ReposRoot)
				}
				// Control: without the forwarded HOME the same config expands
				// against the server's own home, so the equality above only
				// holds because reposRootHome forwarded the starter's HOME.
				control := overlay(serverEnv, map[string]string{"XDG_CONFIG_HOME": loc["XDG_CONFIG_HOME"]})
				c, err := LoadMachine("linux", control)
				if err != nil {
					t.Fatal(err)
				}
				wantControl := filepath.Join(serverHome, "repo.git")
				if tc.body != "" {
					wantControl = filepath.Join(serverHome, "repos")
				}
				if c.ReposRoot != wantControl {
					t.Fatalf("control ReposRoot = %q, want %q", c.ReposRoot, wantControl)
				}
			})
		}
	})
}

// TestLoadMachineReposRootHome pins the unexported reposRootHome flag set by
// LoadMachine: home-relative job_repos_root values (including the default)
// set it, an absolute value does not.
func TestLoadMachineReposRootHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdg := filepath.Join(root, "xdg")
	env := platform.Env{"HOME": home, "XDG_CONFIG_HOME": xdg}
	config := filepath.Join(xdg, "herdr-soho", "config")
	for _, tc := range []struct {
		name      string
		body      string
		wantLabel string
		wantRoot  string
		wantHome  bool
	}{
		{"default without the key", "machine_label=default-case\n", "default-case", filepath.Join(home, "repo.git"), true},
		{"home tilde prefix", "job_repos_root=~/x\n", "", filepath.Join(home, "x"), true},
		{"USERPROFILE backslash prefix", "job_repos_root=%USERPROFILE%\\x\n", "", filepath.Join(home, "x"), true},
		{"USERPROFILE lowercase prefix", "job_repos_root=%userprofile%\\x\n", "", filepath.Join(home, "x"), true},
		{"absolute root", "job_repos_root=/abs/repos\n", "", "/abs/repos", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			writeTeamFile(t, config, tc.body)
			got, err := LoadMachine("linux", env)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantLabel != "" && got.Label != tc.wantLabel {
				t.Fatalf("Label = %q, want %q: the user configuration file was not read", got.Label, tc.wantLabel)
			}
			if got.ReposRoot != tc.wantRoot {
				t.Fatalf("ReposRoot = %q, want %q", got.ReposRoot, tc.wantRoot)
			}
			if got.reposRootHome != tc.wantHome {
				t.Fatalf("reposRootHome = %t, want %t", got.reposRootHome, tc.wantHome)
			}
		})
	}
}
