//go:build !windows

package launcher

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("HERDR_SOHO_HELPER"); mode != "" {
		if mode == "go-skill" {
			fmt.Printf("skill=<%s>\n", os.Getenv("HERDR_SOHO_SKILL_DIR"))
			os.Exit(0)
		}
		if mode == "go" {
			fmt.Println("go-bin")
			for _, arg := range os.Args[1:] {
				fmt.Printf("arg=<%s>\n", arg)
			}
		} else {
			fmt.Println("js")
			if len(os.Args) > 1 && os.Args[1] == "-e" {
				fmt.Print("20")
				os.Exit(0)
			}
			for _, arg := range os.Args[2:] {
				fmt.Printf("arg=<%s>\n", arg)
			}
		}
		code := 0
		if raw := os.Getenv("HERDR_SOHO_HELPER_EXIT"); raw != "" {
			fmt.Sscanf(raw, "%d", &code)
		}
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func TestPOSIXLauncherChoosesBinaryAndPreservesArguments(t *testing.T) {
	// Mutation captured: reversing JS/BIN precedence stops HERDR_SOHO_JS from selecting JavaScript.
	fixture := newLauncherFixture(t)
	goBin := fixture.copyTestBinary(t, filepath.Join(fixture.binDir, "herdr-soho"))
	fixture.writeNode(t)
	nonExecutable := filepath.Join(fixture.binDir, "not-executable")
	if err := os.WriteFile(nonExecutable, []byte("not a binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"value with spaces", `say"hello`, "percent%"}

	tests := []struct {
		name         string
		env          map[string]string
		want         string
		status       int
		stderr       string
		wantStderr   string
		notStderr    string
		ownLauncher  bool
		shebang      bool
		noRuntime    bool
		noPathBinary bool
		homeDir      string
		installDir   string
	}{
		{name: "force JS overrides PATH binary", env: map[string]string{"HERDR_SOHO_JS": "1", "HERDR_SOHO_HELPER": "go"}, want: wantOutput("js", args), status: 23, notStderr: "warning"},
		{name: "explicit binary", env: map[string]string{"HERDR_SOHO_BIN": goBin, "HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "invalid override fails without fallback", env: map[string]string{"HERDR_SOHO_BIN": filepath.Join(fixture.root, "missing"), "HERDR_SOHO_HELPER": "js"}, status: 2, stderr: "HERDR_SOHO_BIN is missing or not an executable binary"},
		{name: "non-executable override fails without fallback", env: map[string]string{"HERDR_SOHO_BIN": nonExecutable, "HERDR_SOHO_HELPER": "js"}, status: 2, stderr: "HERDR_SOHO_BIN is missing or not an executable binary"},
		{name: "PATH binary", env: map[string]string{"HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "own launcher path falls back", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, ownLauncher: true},
		{name: "shebang script falls back", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, shebang: true},
		{name: "no binary or runtime", env: map[string]string{}, status: 2, stderr: "install the herdr-soho binary", noRuntime: true},
		// Install-dir lookup: a shell opened before the install keeps a PATH
		// without the binary; the shim must find where install.sh /
		// install.ps1 put it. PATH has node (the JS fallback stays reachable)
		// and no herdr-soho; HOME / HERDR_SOHO_INSTALL_DIR / LOCALAPPDATA are
		// the fake fixture values below (never the host's).
		{name: "HOME install dir binary, PATH without it", env: map[string]string{"HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23, noPathBinary: true, homeDir: "binary"},
		{name: "custom install dir beats HOME", env: map[string]string{"HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23, noPathBinary: true, homeDir: "junk", installDir: "binary"},
		{name: "install dir holding the shim falls back to JS with the warning", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, noPathBinary: true, homeDir: "shim", wantStderr: "the herdr-soho binary was not found (PATH or the install directory)"},
		{name: "no binary: JS with the warning", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, noPathBinary: true, wantStderr: "the herdr-soho binary was not found (PATH or the install directory)"},
		{name: "forced JS without any binary: no warning", env: map[string]string{"HERDR_SOHO_JS": "1", "HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, noPathBinary: true, notStderr: "warning"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pathDir := fixture.binDir
			if test.noRuntime {
				pathDir = t.TempDir()
				fixture.addSystemTools(t, pathDir)
			}
			if test.noPathBinary {
				pathDir = t.TempDir()
				fixture.addSystemTools(t, pathDir)
				if _, err := fixture.writeNodeIn(t, pathDir); err != nil {
					t.Fatal(err)
				}
			}
			if test.ownLauncher {
				pathDir = t.TempDir()
				launcher, err := os.ReadFile(fixture.launcher)
				if err != nil {
					t.Fatal(err)
				}
				launcher = []byte(strings.TrimPrefix(string(launcher), "#!/bin/sh\n"))
				copyPath := filepath.Join(pathDir, "herdr-soho")
				if err := os.WriteFile(copyPath, launcher, 0o700); err != nil {
					t.Fatal(err)
				}
				fixture.addSystemTools(t, pathDir)
				if _, err := fixture.writeNodeIn(t, pathDir); err != nil {
					t.Fatal(err)
				}
				test.env["HERDR_SOHO_LAUNCH_PATH"] = copyPath
			}
			if test.shebang {
				pathDir = t.TempDir()
				fixture.addSystemTools(t, pathDir)
				if _, err := fixture.writeNodeIn(t, pathDir); err != nil {
					t.Fatal(err)
				}
				fake := filepath.Join(pathDir, "herdr-soho")
				if err := os.WriteFile(fake, []byte("#!/bin/sh\nprintf 'wrong\\n'\n"), 0o700); err != nil {
					t.Fatal(err)
				}
			}

			// Fake install directories, built before the env is assembled so
			// the shim's $HOME / $HERDR_SOHO_INSTALL_DIR lookups are hermetic.
			if test.homeDir != "" {
				home := t.TempDir()
				bin := filepath.Join(home, ".local", "bin", "herdr-soho")
				if err := os.MkdirAll(filepath.Dir(bin), 0o700); err != nil {
					t.Fatal(err)
				}
				switch test.homeDir {
				case "binary":
					fixture.copyTestBinary(t, bin)
				case "shim":
					data, err := os.ReadFile(fixture.launcher)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(bin, data, 0o700); err != nil {
						t.Fatal(err)
					}
				case "junk":
					// Passes is_binary (executable, no shebang) but the kernel
					// refuses to exec it: it discriminates the candidate order
					// (HOME tried before the custom dir would die here, 126).
					if err := os.WriteFile(bin, []byte("not a valid executable"), 0o700); err != nil {
						t.Fatal(err)
					}
				default:
					t.Fatalf("unknown homeDir %q", test.homeDir)
				}
				test.env["HOME"] = home
			}
			if test.installDir != "" {
				dir := t.TempDir()
				fixture.copyTestBinary(t, filepath.Join(dir, "herdr-soho"))
				test.env["HERDR_SOHO_INSTALL_DIR"] = dir
			}

			launcher := fixture.launcher
			if path, ok := test.env["HERDR_SOHO_LAUNCH_PATH"]; ok {
				launcher = path
			}
			// Bounds the self-exec loop a broken own-path check would cause; generous so a loaded host does not fail it.
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, fixture.shell, append([]string{launcher}, args...)...)
			cmd.Env = fixture.env(pathDir, test.env)
			output, err := cmd.Output()
			status := exitCode(err)
			if status != test.status {
				t.Fatalf("exit=%d want=%d output=%q err=%v", status, test.status, output, err)
			}
			if test.stderr != "" {
				if exitErr, ok := err.(*exec.ExitError); !ok || !strings.Contains(string(exitErr.Stderr), test.stderr) {
					t.Fatalf("stderr=%q want substring %q", stderrOf(err), test.stderr)
				}
				return
			}
			if string(output) != test.want {
				t.Fatalf("output=%q want=%q", output, test.want)
			}
			if test.wantStderr != "" {
				exitErr, ok := err.(*exec.ExitError)
				if !ok || !strings.Contains(string(exitErr.Stderr), test.wantStderr) {
					t.Fatalf("stderr=%q want substring %q", stderrOf(err), test.wantStderr)
				}
			}
			if test.notStderr != "" {
				if strings.Contains(stderrOf(err), test.notStderr) {
					t.Fatalf("stderr=%q must not contain %q", stderrOf(err), test.notStderr)
				}
			}
		})
	}
}

type launcherFixture struct {
	binDir   string
	launcher string
	root     string
	shell    string
}

func TestPOSIXLauncherTellsTheBinaryWhereTheSkillIs(t *testing.T) {
	// Mutation captured: without the export, a binary installed outside the
	// skill tree (install.sh puts it in ~/.local/bin) cannot find the skill.
	fixture := newLauncherFixture(t)
	fixture.copyTestBinary(t, filepath.Join(fixture.binDir, "herdr-soho"))
	skill, err := filepath.EvalSymlinks(filepath.Join(fixture.root, "skills", "herdr-soho"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "unset: the launcher's skill root", env: map[string]string{"HERDR_SOHO_HELPER": "go-skill"}, want: skill},
		{name: "set: kept as given", env: map[string]string{"HERDR_SOHO_HELPER": "go-skill", "HERDR_SOHO_SKILL_DIR": "/elsewhere/skill"}, want: "/elsewhere/skill"},
	} {
		t.Run(test.name, func(t *testing.T) {
			cmd := exec.Command(fixture.shell, fixture.launcher)
			cmd.Env = fixture.env(fixture.binDir, test.env)
			output, err := cmd.Output()
			if err != nil || string(output) != "skill=<"+test.want+">\n" {
				t.Fatalf("output=%q err=%v want skill=<%s>", output, err, test.want)
			}
		})
	}
}

func newLauncherFixture(t *testing.T) launcherFixture {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	binDir := t.TempDir()
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	fixture := launcherFixture{root: root, binDir: binDir, launcher: filepath.Join(root, "skills/herdr-soho/scripts/herdr-soho"), shell: shell}
	fixture.addSystemTools(t, binDir)
	return fixture
}

func (f launcherFixture) addSystemTools(t *testing.T, dir string) {
	t.Helper()
	for _, name := range []string{"dd", "dirname", "readlink", "basename"} {
		path, err := exec.LookPath(name)
		if err != nil {
			t.Fatalf("find %s: %v", name, err)
		}
		if err := os.Symlink(path, filepath.Join(dir, name)); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
	}
}

func (f launcherFixture) copyTestBinary(t *testing.T, path string) string {
	t.Helper()
	current, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(current)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f launcherFixture) writeNode(t *testing.T) string {
	t.Helper()
	path, err := f.writeNodeIn(t, f.binDir)
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func (f launcherFixture) writeNodeIn(t *testing.T, dir string) (string, error) {
	t.Helper()
	path := filepath.Join(dir, "node")
	content := "#!/bin/sh\nif [ \"${1:-}\" = -e ]; then printf '20'; exit 0; fi\nprintf 'js\\n'\nshift\nfor arg do printf 'arg=<%s>\\n' \"$arg\"; done\nexit \"${HERDR_SOHO_HELPER_EXIT:-0}\"\n"
	if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
		return "", err
	}
	return path, nil
}

func (f launcherFixture) env(pathDir string, overrides map[string]string) []string {
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key := strings.SplitN(entry, "=", 2)[0]
		if key == "PATH" || key == "HERDR_SOHO_JS" || key == "HERDR_SOHO_BIN" || key == "HERDR_SOHO_HELPER" || key == "HERDR_SOHO_HELPER_EXIT" || key == "HERDR_SOHO_LAUNCH_PATH" || key == "HERDR_SOHO_SKILL_DIR" || key == "HOME" || key == "HERDR_SOHO_INSTALL_DIR" || key == "LOCALAPPDATA" {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "PATH="+pathDir)
	env = append(env, "HERDR_SOHO_HELPER_EXIT=23")
	for key, value := range overrides {
		if key != "HERDR_SOHO_LAUNCH_PATH" {
			env = append(env, key+"="+value)
		}
	}
	return env
}

func wantOutput(marker string, args []string) string {
	var builder strings.Builder
	fmt.Fprintln(&builder, marker)
	for _, arg := range args {
		fmt.Fprintf(&builder, "arg=<%s>\n", arg)
	}
	return builder.String()
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode()
	}
	return -1
}

func stderrOf(err error) string {
	if exitErr, ok := err.(*exec.ExitError); ok {
		return string(exitErr.Stderr)
	}
	return fmt.Sprint(err)
}
