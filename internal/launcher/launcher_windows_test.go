//go:build windows

package launcher

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
		} else if len(os.Args) > 1 && os.Args[1] == "-e" {
			os.Exit(0)
		} else {
			fmt.Println("js")
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

func TestWindowsBatchLauncherChoosesBinaryAndPassesArguments(t *testing.T) {
	// Mutation captured: selecting the .cmd script from PATH prevents the JS fallback when no Go executable exists.
	// The test binary runs in its package directory; the source path from
	// runtime.Caller is the build host's and may not exist here.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	launcher := filepath.Join(root, "skills", "herdr-soho", "scripts", "herdr-soho.cmd")
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	systemRoot := os.Getenv("SystemRoot")
	if systemRoot == "" {
		t.Fatal("SystemRoot is not set")
	}
	args := []string{"value with spaces", `say"hello`, "percent%"}
	tests := []struct {
		name       string
		setup      string
		env        map[string]string
		want       string
		status     int
		stderr     string
		wantStderr string
		notStderr  string
	}{
		{name: "forced JS", setup: "node-and-path-binary", env: map[string]string{"HERDR_SOHO_JS": "1", "HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, notStderr: "warning"},
		{name: "override binary", setup: "node-and-go", env: map[string]string{"HERDR_SOHO_BIN": "go", "HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "invalid override", setup: "node", env: map[string]string{"HERDR_SOHO_BIN": "missing.exe", "HERDR_SOHO_HELPER": "js"}, status: 2, stderr: "HERDR_SOHO_BIN is missing or not executable"},
		{name: "non-executable override", setup: "node-and-bad-override", env: map[string]string{"HERDR_SOHO_BIN": "bad", "HERDR_SOHO_HELPER": "js"}, status: 2, stderr: "HERDR_SOHO_BIN is missing or not executable"},
		{name: "PATH binary", setup: "path-binary", env: map[string]string{"HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "launcher cmd is not a PATH binary", setup: "launcher-cmd-and-node", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23},
		{name: "no binary or runtime", setup: "system-only", env: map[string]string{}, status: 2, stderr: "install the herdr-soho binary"},
		// Install-dir lookup: a shell opened before the install keeps a PATH
		// without the binary; the .cmd must find where install.ps1 (or
		// HERDR_SOHO_INSTALL_DIR) put it. The fake LOCALAPPDATA /
		// HERDR_SOHO_INSTALL_DIR live outside binDir so `where herdr-soho.exe`
		// never sees them. Runs only on Windows (the orchestrator's round).
		{name: "install dir binary (HERDR_SOHO_INSTALL_DIR)", setup: "node", env: map[string]string{"HERDR_SOHO_INSTALL_DIR": "installdir-binary", "HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "install dir binary (LOCALAPPDATA default)", setup: "node", env: map[string]string{"LOCALAPPDATA": "localappdata-binary", "HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "custom install dir beats the default", setup: "node", env: map[string]string{"HERDR_SOHO_INSTALL_DIR": "installdir-binary", "LOCALAPPDATA": "localappdata-junk", "HERDR_SOHO_HELPER": "go"}, want: wantOutput("go-bin", args), status: 23},
		{name: "no binary: JS with the warning", setup: "node", env: map[string]string{"HERDR_SOHO_HELPER": "js"}, want: wantOutput("js", args), status: 23, wantStderr: "the herdr-soho binary was not found (PATH or the install directory)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			binDir := t.TempDir()
			if err := populateWindowsFixture(binDir, test.setup, systemRoot); err != nil {
				t.Fatal(err)
			}
			// The install-dir fixtures (see the cases above): built outside
			// binDir so the PATH lookup stays empty.
			var installDir, localDir string
			if test.env["HERDR_SOHO_INSTALL_DIR"] == "installdir-binary" {
				installDir = t.TempDir()
				if err := writeWindowsExecutable(t, installDir, "herdr-soho.exe"); err != nil {
					t.Fatal(err)
				}
			}
			if value := test.env["LOCALAPPDATA"]; value == "localappdata-binary" || value == "localappdata-junk" {
				localDir = t.TempDir()
				program := filepath.Join(localDir, "Programs", "herdr-soho")
				if err := os.MkdirAll(program, 0o700); err != nil {
					t.Fatal(err)
				}
				if value == "localappdata-binary" {
					if err := writeWindowsExecutable(t, program, "herdr-soho.exe"); err != nil {
						t.Fatal(err)
					}
				} else {
					// Not a valid PE image: if the .cmd tried the default before
					// the custom dir, CreateProcess fails and the exit is not 23.
					if err := os.WriteFile(filepath.Join(program, "herdr-soho.exe"), []byte("not a valid executable"), 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			env := []string{"PATH=" + binDir + ";" + filepath.Join(systemRoot, "System32"), "SystemRoot=" + systemRoot, "HERDR_SOHO_HELPER_EXIT=23"}
			for key, value := range test.env {
				if key == "HERDR_SOHO_BIN" && value == "go" {
					env = append(env, key+"="+filepath.Join(binDir, "herdr-soho.exe"))
				} else if key == "HERDR_SOHO_BIN" && value == "bad" {
					env = append(env, key+"="+filepath.Join(binDir, "herdr-soho.cmd"))
				} else if key == "HERDR_SOHO_BIN" && value == "missing.exe" {
					env = append(env, key+"="+filepath.Join(binDir, value))
				} else if key == "HERDR_SOHO_INSTALL_DIR" && value == "installdir-binary" {
					env = append(env, key+"="+installDir)
				} else if key == "LOCALAPPDATA" && (value == "localappdata-binary" || value == "localappdata-junk") {
					env = append(env, key+"="+localDir)
				} else {
					env = append(env, key+"="+value)
				}
			}
			wrapper := filepath.Join(t.TempDir(), "run.cmd")
			// In a batch file a lone % is dropped (%% is one %; a `call` would
			// expand it twice, so the launcher runs without one and its exit /b
			// is cmd's exit code), and the helper parses its command line by the
			// CRT rules, where \" is a literal quote inside a quoted argument
			// ("" would merge it with the next).
			quoteArg := func(arg string) string {
				return `"` + strings.ReplaceAll(strings.ReplaceAll(arg, `"`, `\"`), "%", "%%") + `"`
			}
			body := fmt.Sprintf("@echo off\r\n\"%s\" %s %s %s\r\n", launcher, quoteArg(args[0]), quoteArg(args[1]), quoteArg(args[2]))
			if err := os.WriteFile(wrapper, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(cmdExe, "/d", "/c", wrapper)
			cmd.Env = env
			output, runErr := cmd.Output()
			if got := exitCode(runErr); got != test.status {
				t.Fatalf("exit=%d want=%d output=%q stderr=%q err=%v", got, test.status, output, stderrOf(runErr), runErr)
			}
			if test.stderr != "" {
				if !strings.Contains(stderrOf(runErr), test.stderr) {
					t.Fatalf("stderr=%q want substring %q", stderrOf(runErr), test.stderr)
				}
				return
			}
			if string(output) != test.want {
				t.Fatalf("output=%q want=%q", output, test.want)
			}
			if test.wantStderr != "" {
				if !strings.Contains(stderrOf(runErr), test.wantStderr) {
					t.Fatalf("stderr=%q want substring %q", stderrOf(runErr), test.wantStderr)
				}
			}
			if test.notStderr != "" {
				if strings.Contains(stderrOf(runErr), test.notStderr) {
					t.Fatalf("stderr=%q must not contain %q", stderrOf(runErr), test.notStderr)
				}
			}
		})
	}
}

func writeWindowsExecutable(t *testing.T, dir, name string) error {
	t.Helper()
	current, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o700)
}

func populateWindowsFixture(dir, setup, systemRoot string) error {
	current, err := os.Executable()
	if err != nil {
		return err
	}
	data, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	writeExecutable := func(name string) error {
		return os.WriteFile(filepath.Join(dir, name), data, 0o700)
	}
	switch setup {
	case "path-binary":
		return writeExecutable("herdr-soho.exe")
	case "node-and-go":
		if err := writeExecutable("herdr-soho.exe"); err != nil {
			return err
		}
		return writeExecutable("node.exe")
	case "node-and-path-binary":
		if err := writeExecutable("herdr-soho.exe"); err != nil {
			return err
		}
		return writeExecutable("node.exe")
	case "node":
		return writeExecutable("node.exe")
	case "node-and-bad-override":
		if err := os.WriteFile(filepath.Join(dir, "herdr-soho.cmd"), []byte("@echo off\r\n"), 0o600); err != nil {
			return err
		}
		return writeExecutable("node.exe")
	case "launcher-cmd-and-node":
		if err := os.WriteFile(filepath.Join(dir, "herdr-soho.cmd"), []byte("@echo off\r\necho wrong\r\n"), 0o600); err != nil {
			return err
		}
		return writeExecutable("node.exe")
	case "system-only":
		return nil
	default:
		return fmt.Errorf("unknown fixture setup %q", setup)
	}
}

func TestWindowsBatchLauncherTellsTheBinaryWhereTheSkillIs(t *testing.T) {
	// Mutation captured: without the set, a binary installed outside the skill
	// tree (install.ps1) cannot find the skill.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(cwd, "..", ".."))
	launcher := filepath.Join(root, "skills", "herdr-soho", "scripts", "herdr-soho.cmd")
	cmdExe, err := exec.LookPath("cmd.exe")
	if err != nil {
		t.Fatal(err)
	}
	systemRoot := os.Getenv("SystemRoot")
	binDir := t.TempDir()
	if err := populateWindowsFixture(binDir, "path-binary", systemRoot); err != nil {
		t.Fatal(err)
	}
	env := []string{"PATH=" + binDir + ";" + filepath.Join(systemRoot, "System32"), "SystemRoot=" + systemRoot, "HERDR_SOHO_HELPER=go-skill"}
	cmd := exec.Command(cmdExe, "/d", "/c", launcher)
	cmd.Env = env
	output, err := cmd.Output()
	want := "skill=<" + filepath.Join(root, "skills", "herdr-soho") + ">\n"
	if err != nil || !strings.EqualFold(string(output), want) {
		t.Fatalf("output=%q err=%v want %q", output, err, want)
	}
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
