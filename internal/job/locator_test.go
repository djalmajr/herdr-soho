package job

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
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

// nativeRoot is the root of the test's volume as this OS writes it: "/" off
// Windows, `D:\` (the working directory's drive) on Windows, so the literals
// built from it are absolute and clean under filepath.IsAbs and filepath.Abs
// on every platform.
func nativeRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.VolumeName(cwd) + string(filepath.Separator)
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
// launchConfigBaseVar always, set to the base directory of the starter's
// own selection, plus launchHomeVar only when reposRootHome is true, and
// never a generic selector (XDG_CONFIG_HOME, HOME, USERPROFILE, APPDATA).
func TestSupervisorLocator(t *testing.T) {
	t.Run("POSIX default without XDG", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{"HOME": home}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: filepath.Join(home, ".config")})
	})

	t.Run("POSIX home forwarded only when reposRootHome", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{"HOME": home}
		got, err := supervisorLocatorEnv("linux", env, true, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{
			launchConfigBaseVar: filepath.Join(home, ".config"),
			launchHomeVar:       home,
		})
	})

	t.Run("XDG absolute", func(t *testing.T) {
		home, xdg := t.TempDir(), t.TempDir()
		env := platform.Env{"HOME": home, "XDG_CONFIG_HOME": xdg}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: xdg})
	})

	t.Run("XDG relative normalized by absLocator", func(t *testing.T) {
		cwd := t.TempDir()
		t.Chdir(cwd)
		env := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": filepath.Join("rel cfg", "home")}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: filepath.Join(cwd, "rel cfg", "home")})
	})

	t.Run("spaces preserved except cleaning", func(t *testing.T) {
		root, sep := nativeRoot(t), string(filepath.Separator)
		xdg := root + "cfg base" + sep + "with spaces" + sep + "." + sep + "extra"
		want := root + "cfg base" + sep + "with spaces" + sep + "extra"
		env := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": xdg}
		got, err := supervisorLocatorEnv("linux", env, false, absLocator)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: want})
	})

	t.Run("win32 XDG set", func(t *testing.T) {
		env := platform.Env{"XDG_CONFIG_HOME": `C:\xdg home`}
		got, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: `C:\xdg home`})
	})

	t.Run("win32 APPDATA only pins the base to the APPDATA value", func(t *testing.T) {
		env := platform.Env{"APPDATA": `C:\app data`, "HOME": t.TempDir()}
		got, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: `C:\app data`})
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
		wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: want})
		// Joined the way platform.UserConfigPath joins on the test host:
		// the drive-letter segment keeps its backslash, the rest uses the
		// host separator.
		wantExplicit := `C:\users\me` + string(filepath.Separator) + "AppData" + string(filepath.Separator) + "Roaming"
		if got[launchConfigBaseVar] != wantExplicit {
			t.Fatalf("launchConfigBaseVar = %q, want %q", got[launchConfigBaseVar], wantExplicit)
		}
	})

	t.Run("win32 USERPROFILE forwarded only when reposRootHome", func(t *testing.T) {
		env := platform.Env{"USERPROFILE": `C:\users\me`}
		base := filepath.Dir(filepath.Dir(platform.UserConfigPath("win32", env)))
		wantBase, err := winAbs(base)
		if err != nil {
			t.Fatal(err)
		}
		got, err := supervisorLocatorEnv("win32", env, true, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, got, map[string]string{
			launchConfigBaseVar: wantBase,
			launchHomeVar:       `C:\users\me`,
		})
		gotOff, err := supervisorLocatorEnv("win32", env, false, winAbs)
		if err != nil {
			t.Fatal(err)
		}
		wantLocatorMap(t, gotOff, map[string]string{launchConfigBaseVar: wantBase})
	})

	t.Run("allowlist holds only the documented keys", func(t *testing.T) {
		home := t.TempDir()
		env := platform.Env{
			"HOME":                  home,
			"GH_TOKEN":              "gh-secret",
			"AWS_SECRET_ACCESS_KEY": "aws-secret",
			"SOHO_DIR":              "/state/soho",
			"SOHO_STATE_DIR":        "/state/soho2",
			"APPDATA":               `C:\appdata`,
			"PATH":                  "/usr/bin:/bin",
			"SSH_AUTH_SOCK":         "/run/user/1000/ssh-agent.sock",
		}
		for _, reposRootHome := range []bool{false, true} {
			got, err := supervisorLocatorEnv("linux", env, reposRootHome, absLocator)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{launchConfigBaseVar: filepath.Join(home, ".config")}
			if reposRootHome {
				want[launchHomeVar] = home
			}
			wantLocatorMap(t, got, want)
			for _, key := range []string{"XDG_CONFIG_HOME", "HOME", "USERPROFILE", "APPDATA"} {
				if _, ok := got[key]; ok {
					t.Fatalf("locator map carries the generic selector %q: %v", key, got)
				}
			}
			for _, secret := range []string{
				env["GH_TOKEN"], env["AWS_SECRET_ACCESS_KEY"], env["SOHO_DIR"],
				env["SOHO_STATE_DIR"], env["APPDATA"], env["PATH"], env["SSH_AUTH_SOCK"],
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
		root := nativeRoot(t)
		long4096 := root + strings.Repeat("a", maxLocatorBytes-len(root))
		long4097 := long4096 + "a"
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
					wantLocatorMap(t, got, map[string]string{launchConfigBaseVar: long4096})
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

// parityDirs is one set of real temp dirs for a TestLocatorSelectionParity
// subtest.
type parityDirs struct {
	home       string
	conf       string
	serverHome string
	serverXDG  string
	spaceConf  string
}

// TestLocatorSelectionParity pins that the private locators select, on a
// different server environment, exactly what the starter selected: the
// selection read back from the locator map is present and non-zero, it
// reproduces the starter's effective user configuration file, and for the
// repos-root cases LoadMachineFor with it resolves the same repos root as
// the starter's LoadMachine; clearing the home from the selection expands
// a home-relative root from the server's own home.
func TestLocatorSelectionParity(t *testing.T) {
	const osName = "linux"
	starterEffective := func(env platform.Env) string {
		return platform.UserConfigPath(osName, env)
	}
	for _, tc := range []struct {
		name       string
		starter    func(d parityDirs) platform.Env
		server     func(d parityDirs) platform.Env
		files      []string                  // "current" and/or "legacy", planted at the starter's base
		body       string                    // the planted current file's body; "" plants nothing
		reposHome  bool                      // reposRootHome the starter passes
		wantRoot   func(d parityDirs) string // the starter's ReposRoot; nil skips the machine checks
		wantCtl    func(d parityDirs) string // the root with the home cleared from the selection; nil skips
		wantNoHome bool                      // loc must not carry launchHomeVar
	}{
		{
			name:    "current file only",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server:  func(d parityDirs) platform.Env { return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.home} },
			files:   []string{"current"},
		},
		{
			name:    "former user file only: the soho path is selected",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server:  func(d parityDirs) platform.Env { return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.home} },
			files:   []string{"legacy"},
		},
		{
			name:    "both files: the soho path is selected",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server:  func(d parityDirs) platform.Env { return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.home} },
			files:   []string{"current", "legacy"},
		},
		{
			name:    "server env with its own XDG_CONFIG_HOME",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			files: []string{"current"},
		},
		{
			name:    "server env without XDG and a different HOME",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server:  func(d parityDirs) platform.Env { return platform.Env{"HOME": d.serverHome} },
			files:   []string{"current"},
		},
		{
			name:    "starter XDG_CONFIG_HOME set and server without",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home, "XDG_CONFIG_HOME": d.conf} },
			server:  func(d parityDirs) platform.Env { return platform.Env{"HOME": d.serverHome} },
			files:   []string{"current"},
		},
		{
			name:    "starter XDG_CONFIG_HOME set to the empty string",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home, "XDG_CONFIG_HOME": ""} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			files: []string{"current"},
		},
		{
			name:    "base with spaces",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home, "XDG_CONFIG_HOME": d.spaceConf} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			files: []string{"current"},
		},
		{
			name:    "default repos root",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			reposHome: true,
			wantRoot:  func(d parityDirs) string { return filepath.Join(d.home, "repo.git") },
			wantCtl:   func(d parityDirs) string { return filepath.Join(d.serverHome, "repo.git") },
		},
		{
			name:    "home relative repos root",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			files:     []string{"current"},
			body:      "job_repos_root=~/repos\n",
			reposHome: true,
			wantRoot:  func(d parityDirs) string { return filepath.Join(d.home, "repos") },
			wantCtl:   func(d parityDirs) string { return filepath.Join(d.serverHome, "repos") },
		},
		{
			name:    "absolute repos root",
			starter: func(d parityDirs) platform.Env { return platform.Env{"HOME": d.home} },
			server: func(d parityDirs) platform.Env {
				return platform.Env{"XDG_CONFIG_HOME": d.serverXDG, "HOME": d.serverHome}
			},
			files:      []string{"current"},
			body:       "job_repos_root=/abs/repos\n",
			wantRoot:   func(parityDirs) string { return "/abs/repos" },
			wantNoHome: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			d := parityDirs{
				home:       filepath.Join(root, "home"),
				conf:       filepath.Join(root, "conf"),
				serverHome: filepath.Join(root, "server-home"),
				serverXDG:  filepath.Join(root, "server-xdg"),
				spaceConf:  filepath.Join(root, "conf with spaces"),
			}
			for _, dir := range []string{d.home, d.conf, d.serverHome, d.serverXDG, d.spaceConf} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			starterEnv := tc.starter(d)
			serverEnv := tc.server(d)
			base := filepath.Dir(filepath.Dir(platform.UserConfigPath(osName, starterEnv)))
			for _, which := range tc.files {
				body := tc.body
				if body == "" {
					body = "machine_label=starter\n"
					if which == "legacy" {
						body = "machine_label=starter-legacy\n"
					}
				}
				if which == "current" {
					writeTeamFile(t, filepath.Join(base, "soho", "config"), body)
				} else {
					writeTeamFile(t, filepath.Join(base, "herdr-soho", "config"), body)
				}
			}
			loc, err := supervisorLocatorEnv(osName, starterEnv, tc.reposHome, absLocator)
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantNoHome {
				if _, ok := loc[launchHomeVar]; ok {
					t.Fatalf("an absolute root forwards the home: %v", loc)
				}
			}
			got := ReadLaunchLocators(platform.Env(loc))
			if !got.Present || got.Selection.IsZero() {
				t.Fatalf("ReadLaunchLocators = %+v, want present with a non-zero selection", got)
			}
			wantFile := starterEffective(starterEnv)
			if gotFile := core.EffectiveUserConfigFile(osName, serverEnv, got.Selection); gotFile != wantFile {
				t.Fatalf("server selection = %q, want the starter's %q", gotFile, wantFile)
			}
			if tc.wantRoot != nil {
				starter, err := LoadMachine(osName, starterEnv)
				if err != nil {
					t.Fatal(err)
				}
				if starter.ReposRoot != tc.wantRoot(d) {
					t.Fatalf("starter ReposRoot = %q, want %q", starter.ReposRoot, tc.wantRoot(d))
				}
				gotM, err := LoadMachineFor(osName, serverEnv, got.Selection)
				if err != nil {
					t.Fatal(err)
				}
				if gotM.ReposRoot != starter.ReposRoot {
					t.Fatalf("LoadMachineFor ReposRoot %q differs from the starter's %q", gotM.ReposRoot, starter.ReposRoot)
				}
				if tc.wantCtl != nil {
					noHome := got.Selection
					noHome.Home = ""
					ctl, err := LoadMachineFor(osName, serverEnv, noHome)
					if err != nil {
						t.Fatal(err)
					}
					if ctl.ReposRoot != tc.wantCtl(d) {
						t.Fatalf("control ReposRoot = %q, want the server home's %q", ctl.ReposRoot, tc.wantCtl(d))
					}
				}
			}
		})
	}

	// Windows precedence: path computations only (no file existence),
	// comparing the core.UserConfigFiles results against the starter's own
	// paths.
	t.Run("win32 precedence", func(t *testing.T) {
		for _, tc := range []struct {
			name      string
			starter   platform.Env
			server    platform.Env
			reposHome bool
		}{
			{"starter APPDATA only vs server XDG_CONFIG_HOME",
				platform.Env{"APPDATA": `C:\app data`},
				platform.Env{"XDG_CONFIG_HOME": `C:\xdg home`},
				false},
			{"starter USERPROFILE only (AppData Roaming fallback) vs server APPDATA",
				platform.Env{"USERPROFILE": `C:\users\me`},
				platform.Env{"APPDATA": `C:\app data`},
				false},
			{"starter XDG_CONFIG_HOME vs server APPDATA",
				platform.Env{"XDG_CONFIG_HOME": `C:\xdg home`},
				platform.Env{"APPDATA": `C:\app data`},
				false},
			{"launchHomeVar equals the starter USERPROFILE when reposRootHome",
				platform.Env{"USERPROFILE": `C:\users\me`},
				platform.Env{"APPDATA": `C:\app data`},
				true},
		} {
			t.Run(tc.name, func(t *testing.T) {
				loc, err := supervisorLocatorEnv("win32", tc.starter, tc.reposHome, winAbs)
				if err != nil {
					t.Fatal(err)
				}
				base := filepath.Dir(filepath.Dir(platform.UserConfigPath("win32", tc.starter)))
				want, err := winAbs(base)
				if err != nil {
					t.Fatal(err)
				}
				wantLoc := map[string]string{launchConfigBaseVar: want}
				if tc.reposHome {
					wantLoc[launchHomeVar] = tc.starter.Get("USERPROFILE")
				}
				wantLocatorMap(t, loc, wantLoc)
				got := ReadLaunchLocators(platform.Env(loc))
				if !got.Present {
					t.Fatalf("ReadLaunchLocators = %+v, want present", got)
				}
				sel := got.Selection
				if sel.IsZero() {
					// off-Windows the host's IsAbs cannot validate a
					// drive-letter base, so the selection is read from the
					// pinned locator values instead
					sel = core.UserSelection{Base: loc[launchConfigBaseVar]}
					if h := loc[launchHomeVar]; h != "" {
						sel.Home = h
					}
				}
				if sel.IsZero() {
					t.Fatalf("selection = %+v, want non-zero", sel)
				}
				wantCurrent := platform.UserConfigPath("win32", tc.starter)
				gotCurrent := core.UserConfigFiles("win32", tc.server, sel)
				if gotCurrent != wantCurrent {
					t.Fatalf("server selection = %q, want the starter's %q", gotCurrent, wantCurrent)
				}
			})
		}
	})
}

// TestReadLaunchLocators pins the ReadLaunchLocators usability rule: a
// usable pair is an absolute, clean, control-free base of at most
// maxLocatorBytes plus an optional home of the same shape; anything else
// (a home without a base, an empty, relative, unclean or over-long value,
// or a usable base with an unusable home) is ignored as a whole.
func TestReadLaunchLocators(t *testing.T) {
	root, sep := nativeRoot(t), string(filepath.Separator)
	cfg, home := root+"cfg", root+"home"
	unclean := root + "a" + sep + ".." + sep + "b"
	long4096 := root + strings.Repeat("a", maxLocatorBytes-len(root))
	long4097 := long4096 + "a"
	for _, tc := range []struct {
		name    string
		env     platform.Env
		wantP   bool
		wantSel core.UserSelection
	}{
		{"absent", platform.Env{}, false, core.UserSelection{}},
		{"base only valid", platform.Env{launchConfigBaseVar: cfg}, true, core.UserSelection{Base: cfg}},
		{"base and home valid", platform.Env{launchConfigBaseVar: cfg, launchHomeVar: home}, true, core.UserSelection{Base: cfg, Home: home}},
		{"home without base", platform.Env{launchHomeVar: home}, true, core.UserSelection{}},
		{"empty base", platform.Env{launchConfigBaseVar: ""}, true, core.UserSelection{}},
		{"relative base", platform.Env{launchConfigBaseVar: "cfg"}, true, core.UserSelection{}},
		{"unclean base", platform.Env{launchConfigBaseVar: unclean}, true, core.UserSelection{}},
		{"base with a control byte", platform.Env{launchConfigBaseVar: root + "bad\x01cfg"}, true, core.UserSelection{}},
		{"4097-byte base", platform.Env{launchConfigBaseVar: long4097}, true, core.UserSelection{}},
		{"4096-byte base accepted at the boundary", platform.Env{launchConfigBaseVar: long4096}, true, core.UserSelection{Base: long4096}},
		{"valid base with an unusable home", platform.Env{launchConfigBaseVar: cfg, launchHomeVar: "relative"}, true, core.UserSelection{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ReadLaunchLocators(tc.env)
			if got.Present != tc.wantP {
				t.Fatalf("Present = %t, want %t", got.Present, tc.wantP)
			}
			if !reflect.DeepEqual(got.Selection, tc.wantSel) {
				t.Fatalf("Selection = %+v, want %+v", got.Selection, tc.wantSel)
			}
		})
	}
}

// TestLaunchLocatorEnvCopies pins the env copy helpers: WithoutLaunchLocators
// strips only the two private names and leaves its input untouched, and
// withLaunchLocators adds nothing for the zero selection (removing stale
// names), exactly both names for a selection with base and home.
func TestLaunchLocatorEnvCopies(t *testing.T) {
	t.Run("WithoutLaunchLocators strips only the private names", func(t *testing.T) {
		in := platform.Env{
			launchConfigBaseVar: "/cfg",
			launchHomeVar:       "/home",
			"HOME":              "/home",
			"XDG_CONFIG_HOME":   "/xdg",
			"GH_TOKEN":          "secret",
		}
		inBefore := in.Clone()
		got := WithoutLaunchLocators(in)
		for _, key := range []string{launchConfigBaseVar, launchHomeVar} {
			if _, ok := got[key]; ok {
				t.Fatalf("WithoutLaunchLocators kept %q: %v", key, got)
			}
		}
		for key, value := range in {
			if key == launchConfigBaseVar || key == launchHomeVar {
				continue
			}
			if got[key] != value {
				t.Fatalf("WithoutLaunchLocators lost or changed %q: %v", key, got)
			}
		}
		if !reflect.DeepEqual(in, inBefore) {
			t.Fatalf("WithoutLaunchLocators changed its input: %v", in)
		}
	})
	t.Run("withLaunchLocators with a zero selection adds nothing and removes stale names", func(t *testing.T) {
		in := platform.Env{launchConfigBaseVar: "/stale", launchHomeVar: "/stale", "HOME": "/home"}
		inBefore := in.Clone()
		got := withLaunchLocators(in, core.UserSelection{})
		for _, key := range []string{launchConfigBaseVar, launchHomeVar} {
			if _, ok := got[key]; ok {
				t.Fatalf("withLaunchLocators kept the stale %q: %v", key, got)
			}
		}
		if got["HOME"] != "/home" {
			t.Fatalf("withLaunchLocators lost HOME: %v", got)
		}
		if !reflect.DeepEqual(in, inBefore) {
			t.Fatalf("withLaunchLocators changed its input: %v", in)
		}
	})
	t.Run("withLaunchLocators with base and home adds exactly both", func(t *testing.T) {
		in := platform.Env{"HOME": "/home", launchHomeVar: "/stale"}
		got := withLaunchLocators(in, core.UserSelection{Base: "/cfg", Home: "/home2"})
		want := platform.Env{"HOME": "/home", launchConfigBaseVar: "/cfg", launchHomeVar: "/home2"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("withLaunchLocators = %v, want %v", got, want)
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
		root, sep := nativeRoot(t), string(filepath.Separator)
		got, err = absLocator(root + "abs" + sep + ".." + sep + "x")
		if err != nil || got != root+"x" {
			t.Fatalf("absLocator absolute = %q, %v; want %q", got, err, root+"x")
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

// TestLoadMachineReposRootHome pins the unexported reposRootHome flag set by
// LoadMachine: home-relative job_repos_root values (including the default)
// set it, an absolute value does not.
func TestLoadMachineReposRootHome(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	xdg := filepath.Join(root, "xdg")
	env := platform.Env{"HOME": home, "XDG_CONFIG_HOME": xdg}
	config := filepath.Join(xdg, "soho", "config")
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
