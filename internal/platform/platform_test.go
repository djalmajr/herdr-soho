package platform

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestMain(m *testing.M) { fakecli.RunTests(m) }

type windowsReference struct {
	Version int `json:"version"`
	Roots   map[string]struct {
		ProjectRoot      string `json:"projectRoot"`
		StateProjectRoot string `json:"stateProjectRoot"`
	} `json:"roots"`
	WindowsExe struct {
		ResolvedBase string `json:"resolvedBase"`
		Status       int    `json:"status"`
		Stdout       string `json:"stdout"`
		Stderr       string `json:"stderr"`
	} `json:"windowsExe"`
}

func readWindowsReference(t *testing.T) windowsReference {
	t.Helper()
	data, err := os.ReadFile("testdata/windows-roots.json")
	if err != nil {
		t.Skip("waiting for JS Windows reference from /tmp/hs-go/build/js-win-ref.mjs")
	}
	var reference windowsReference
	if err := json.Unmarshal(data, &reference); err != nil {
		t.Fatalf("decode Windows JS reference: %v", err)
	}
	if reference.Version != 1 {
		t.Fatalf("Windows JS reference version=%d want 1", reference.Version)
	}
	return reference
}

func assertRootReference(t *testing.T, reference *windowsReference, name string, env Env, cwd, fixtureRoot, nonWindowsWant string, state bool) {
	t.Helper()
	want := nonWindowsWant
	if reference != nil {
		roots, ok := reference.Roots[name]
		if !ok {
			t.Fatalf("Windows JS reference is missing root case %q", name)
		}
		want = roots.ProjectRoot
		if state {
			want = roots.StateProjectRoot
		}
	}
	got := ProjectRoot(env, cwd)
	if state {
		got = StateProjectRoot(env, cwd)
	}
	if reference != nil {
		var err error
		got, err = filepath.Rel(fixtureRoot, got)
		if err != nil {
			t.Fatalf("relative %s root: %v", name, err)
		}
		if got == "." {
			got = ""
		}
	}
	if got != want {
		t.Fatalf("%s got %q want %q", name, got, want)
	}
}

func TestUserConfigPath(t *testing.T) {
	t.Run("userConfigPath: XDG_CONFIG_HOME wins on every platform (decision 4)", func(t *testing.T) { // JS: "userConfigPath: XDG_CONFIG_HOME wins on every platform (decision 4)"
		for _, osName := range []string{"linux", "darwin", "win32"} {
			if got := UserConfigPath(osName, Env{"XDG_CONFIG_HOME": "/cfg/home"}); got != filepath.Join("/cfg/home", "herdr-soho", "config") {
				t.Errorf("%s: %q", osName, got)
			}
		}
	})
	t.Run("userConfigPath: win32 without XDG uses %APPDATA%\\herdr-soho\\config", func(t *testing.T) { // JS: "userConfigPath: win32 without XDG uses %APPDATA%\\herdr-soho\\config"
		if got := UserConfigPath("win32", Env{"APPDATA": `C:\Users\dev\AppData\Roaming`}); got != filepath.Join(`C:\Users\dev\AppData\Roaming`, "herdr-soho", "config") {
			t.Errorf("APPDATA: %q", got)
		}
		if got := UserConfigPath("win32", Env{"USERPROFILE": `C:\Users\dev`}); got != filepath.Join(`C:\Users\dev`, "AppData", "Roaming", "herdr-soho", "config") {
			t.Errorf("fallback: %q", got)
		}
	})
	t.Run("userConfigPath: Unix without XDG uses ~/.config/herdr-soho/config", func(t *testing.T) { // JS: "userConfigPath: Unix without XDG uses ~/.config/herdr-soho/config"
		if got := UserConfigPath("linux", Env{"HOME": "/home/dev"}); got != filepath.Join("/home/dev", ".config", "herdr-soho", "config") {
			t.Errorf("linux: %q", got)
		}
		if got := UserConfigPath("darwin", Env{"HOME": "/Users/dev"}); got != filepath.Join("/Users/dev", ".config", "herdr-soho", "config") {
			t.Errorf("darwin: %q", got)
		}
		if got := UserConfigPath("linux", Env{"XDG_CONFIG_HOME": "", "HOME": "/home/dev"}); got != filepath.Join("/home/dev", ".config", "herdr-soho", "config") {
			t.Errorf("empty XDG: %q", got)
		}
	})
}

func TestHomeDir(t *testing.T) {
	t.Run("homeDir: USERPROFILE on win32, HOME elsewhere", func(t *testing.T) { // JS: "homeDir: USERPROFILE on win32, HOME elsewhere"
		if got := HomeDir("win32", Env{"USERPROFILE": `C:\Users\dev`}); got != `C:\Users\dev` {
			t.Errorf("win32: %q", got)
		}
		if got := HomeDir("linux", Env{"HOME": "/home/dev"}); got != "/home/dev" {
			t.Errorf("linux: %q", got)
		}
	})
	t.Run("homeDir falls back to the OS user and dies clearly when unresolved", func(t *testing.T) {
		old := currentUserHome
		defer func() { currentUserHome = old }()
		currentUserHome = func() (string, error) { return "/resolved/home", nil }
		if got := HomeDir("linux", Env{}); got != "/resolved/home" {
			t.Fatalf("fallback home = %q", got)
		}
		currentUserHome = func() (string, error) { return "", errors.New("no passwd entry") }
		for _, tc := range []struct{ platform, variable string }{{"linux", "HOME"}, {"win32", "USERPROFILE"}} {
			value := capturePanic(func() { HomeDir(tc.platform, Env{}) })
			exitErr, ok := value.(*ExitError)
			want := "cannot resolve the home directory; set " + tc.variable
			if !ok || exitErr.Code != 2 || exitErr.Msg != want {
				t.Errorf("%s unresolved home panic = %#v, want %q", tc.platform, value, want)
			}
		}
	})
}

func TestSkillDirRequiresRolesDirectory(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("skill"), 0o600); err != nil {
		t.Fatal(err)
	}
	previous := currentExecutable
	currentExecutable = func() (string, error) { return filepath.Join(bin, "herdr-soho"), nil }
	defer func() { currentExecutable = previous }()
	defer func() {
		got := recover()
		exit, ok := got.(*ExitError)
		if !ok || exit.Code != 2 {
			t.Fatalf("SkillDir panic=%#v; want ExitError code 2 when roles is missing", got)
		}
	}()
	_ = SkillDir(Env{}) // Mutation captured: removing the roles/ validation accepts an incomplete skill tree.
}

func capturePanic(run func()) (value any) {
	defer func() { value = recover() }()
	run()
	return nil
}

func TestReadTextFile(t *testing.T) {
	t.Run("readTextFile: CRLF lines are normalized to LF (decision 7)", func(t *testing.T) { // JS: "readTextFile: CRLF lines are normalized to LF (decision 7)"
		file := filepath.Join(t.TempDir(), "a.txt")
		_ = os.WriteFile(file, []byte("a=1\r\nb=2 # c\r\n"), 0o600)
		if got, err := ReadTextFile(file); err != nil || got != "a=1\nb=2 # c\n" {
			t.Fatalf("CRLF: %q %v", got, err)
		}
		_ = os.WriteFile(file, []byte("x=1\ny=2\n"), 0o600)
		if got, err := ReadTextFile(file); err != nil || got != "x=1\ny=2\n" {
			t.Fatalf("LF: %q %v", got, err)
		}
	})
	t.Run("ReadTextFile matches Node UTF-8 replacement and newline normalization", func(t *testing.T) {
		var fixture struct {
			Version int `json:"version"`
			Cases   []struct {
				Hex      string `json:"hex"`
				Expected string `json:"expected"`
			} `json:"cases"`
		}
		data, err := os.ReadFile("testdata/readtext.json")
		if err != nil {
			t.Fatal(err)
		}
		if err = json.Unmarshal(data, &fixture); err != nil {
			t.Fatal(err)
		}
		if fixture.Version != 1 || len(fixture.Cases) < 2003 {
			t.Fatalf("fixture version=%d cases=%d", fixture.Version, len(fixture.Cases))
		}
		for index, testCase := range fixture.Cases {
			raw, err := hex.DecodeString(testCase.Hex)
			if err != nil {
				t.Fatalf("case %d hex: %v", index, err)
			}
			file := filepath.Join(t.TempDir(), "bytes.txt")
			if err = os.WriteFile(file, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := ReadTextFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if got != testCase.Expected {
				t.Fatalf("case %d got %q want %q (input %s)", index, got, testCase.Expected, hex.EncodeToString(raw))
			}
		}
	})
}

func TestFindExecutable(t *testing.T) {
	// Mutation captured: dropping the Unix execute-bit check admits a plain file.
	t.Run("findExecutable: finds node on PATH, returns null for unknown names", func(t *testing.T) { // JS: "findExecutable: finds node on PATH, returns null for unknown names"
		if runtime.GOOS == "windows" {
			t.Skip("Unix execute-bit lookup is not available on Windows")
		}
		bin := t.TempDir()
		node := filepath.Join(bin, "node")
		_ = os.WriteFile(node, []byte("#!/bin/sh\n"), 0o700)
		if got, ok := FindExecutable("node", Env{"PATH": bin}, "linux"); !ok || got != node {
			t.Fatalf("found: %q %v", got, ok)
		}
		if _, ok := FindExecutable("definitely-not-a-real-command-xyz", Env{"PATH": bin}, "linux"); ok {
			t.Fatal("unexpected executable")
		}
		plain := filepath.Join(bin, "plain")
		if err := os.WriteFile(plain, []byte("not executable"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got, ok := FindExecutable("plain", Env{"PATH": bin}, "linux"); ok || got != "" {
			t.Fatalf("non-executable file resolved as %q", got)
		}
	})
	// Mutation captured: retaining empty PATHEXT entries resolves a Windows file without an extension.
	t.Run("findExecutable: simulated win32 honors PATHEXT and discards empty extensions", func(t *testing.T) { // JS: "findExecutable: simulated win32 honors PATHEXT (.CMD); darwin does not"
		bin := t.TempDir()
		bare := filepath.Join(bin, "npm")
		shim := filepath.Join(bin, "tool.CMD")
		if err := os.WriteFile(bare, []byte("script"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(shim, []byte("batch"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, pathext := range []string{".EXE;.CMD", ".EXE;.CMD;", ";.CMD", ".CMD;;.EXE"} {
			got, ok := FindExecutable("npm", Env{"PATH": bin, "PATHEXT": pathext}, "win32")
			if ok || got != "" {
				t.Fatalf("PATHEXT=%q resolved extensionless npm as %q", pathext, got)
			}
		}
		if got, ok := FindExecutable("tool", Env{"PATH": bin, "PATHEXT": ".CMD"}, "win32"); !ok || got != shim {
			t.Fatalf("PATHEXT lookup = %q, %v", got, ok)
		}
		lowerExe := filepath.Join(bin, "lower.exe")
		if err := os.WriteFile(lowerExe, []byte("binary"), 0o600); err != nil {
			t.Fatal(err)
		}
		caseVariant := filepath.Join(bin, "lower.EXE")
		wantLower := lowerExe
		if _, err := os.Stat(caseVariant); err == nil {
			wantLower = caseVariant
		}
		if got, ok := FindExecutable("lower", Env{"PATH": bin, "PATHEXT": ".EXE"}, "win32"); !ok || got != wantLower {
			t.Fatalf("case-insensitive PATHEXT lookup = %q, %v", got, ok)
		}
		if got, ok := FindExecutable("lower.EXE", Env{"PATH": bin, "PATHEXT": ".exe"}, "win32"); !ok || got != wantLower {
			t.Fatalf("explicit mixed-case extension lookup = %q, %v", got, ok)
		}
		joined := filepath.Join(t.TempDir(), "later")
		if err := os.MkdirAll(joined, 0o700); err != nil {
			t.Fatal(err)
		}
		later := filepath.Join(joined, "other.CMD")
		if err := os.WriteFile(later, []byte("batch"), 0o600); err != nil {
			t.Fatal(err)
		}
		pathValue := bin + string(os.PathListSeparator) + joined
		if got, ok := FindExecutable("other", Env{"PATH": pathValue, "PATHEXT": ".CMD"}, "win32"); !ok || got != later {
			t.Fatalf("host PATH split = %q, %v", got, ok)
		}
		if _, ok := FindExecutable("tool", Env{"PATH": bin, "PATHEXT": ".CMD"}, "linux"); ok {
			t.Fatal("linux lookup accepted a non-executable extension shim")
		}
	})
	// Mutation captured: requiring ReadDir before Stat misses an exact Win32 path in an unlistable directory.
	t.Run("simulated win32 stats an exact candidate before listing its directory", func(t *testing.T) {
		bin := t.TempDir()
		file := filepath.Join(bin, "grok.CMD")
		if err := os.WriteFile(file, []byte("batch"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(bin, 0o111); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(bin, 0o700) })
		if got, ok := FindExecutable("grok", Env{"PATH": bin, "PATHEXT": ".CMD"}, "win32"); !ok || got != file {
			t.Fatalf("unlistable exact candidate = %q, %v; want %q", got, ok, file)
		}
	})
}

func TestCmdInvocation(t *testing.T) {
	// Mutation captured: omitting the second escaping pass breaks npm .cmd shims.
	t.Run("models.test.mjs: win32 cmdInvocation escapes batch arguments", func(t *testing.T) { // JS: "runCli: a win32 .cmd/.bat target runs through cmd.exe with every argument escaped; notFound and timeout"
		inv := CmdInvocation(`C:\tools\grok.CMD`, []string{"models", "has space", "x& echo INJECTED", "50%", `a"b`}, Env{"COMSPEC": `C:\Windows\system32\cmd.exe`})
		if inv.Command != `C:\Windows\system32\cmd.exe` || !inv.WindowsVerbatimArguments || len(inv.Args) != 4 {
			t.Fatalf("invocation = %#v", inv)
		}
		line := inv.Args[3]
		for _, part := range []string{`^"models^"`, `^"has^ space^"`, `^"x^&^ echo^ INJECTED^"`, `^"50^%^"`, `^"a\^"b^"`} {
			if !strings.Contains(line, part) {
				t.Errorf("missing %q in %q", part, line)
			}
		}
		if strings.Contains(line, "& echo") {
			t.Fatalf("unescaped command metacharacter in %q", line)
		}
		shim := CmdInvocation(`C:\p\node_modules\.bin\tool.cmd`, []string{"a&b"}, Env{})
		if !strings.Contains(shim.Args[3], `^^^"a^^^&b^^^"`) {
			t.Fatalf("npm shim escaping = %q", shim.Args[3])
		}
	})
}

func TestWindowsPlatformMatchesJSFixtures(t *testing.T) {
	var fixture struct {
		Version int `json:"version"`
		Find    []struct {
			Name     string  `json:"name"`
			PathSpec string  `json:"pathSpec"`
			PATHEXT  string  `json:"pathext"`
			Basename *string `json:"basename"`
		} `json:"find"`
		Invocations []struct {
			Resolved string              `json:"resolved"`
			Args     []string            `json:"args"`
			Env      map[string]string   `json:"env"`
			Expected CmdInvocationResult `json:"expected"`
		} `json:"invocations"`
		Resolve []struct {
			Base     string `json:"base"`
			Value    string `json:"value"`
			Expected string `json:"expected"`
		} `json:"resolve"`
	}
	data, err := os.ReadFile("testdata/windows.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Find) != 5 || len(fixture.Invocations) != 2 {
		t.Fatalf("fixture version=%d find=%d invocations=%d", fixture.Version, len(fixture.Find), len(fixture.Invocations))
	}
	root := t.TempDir()
	first, second := filepath.Join(root, "first"), filepath.Join(root, "second")
	if err := os.MkdirAll(first, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(second, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(first, "npm"), []byte("extensionless"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(second, "tool.CMD"), []byte("batch"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range fixture.Find {
		t.Run("findExecutable "+tc.PATHEXT, func(t *testing.T) {
			pathValue := first
			if tc.PathSpec == "second" {
				pathValue = second
			}
			got, ok := FindExecutable(tc.Name, Env{"PATH": pathValue, "PATHEXT": tc.PATHEXT}, "win32")
			var basename *string
			if ok {
				value := filepath.Base(got)
				basename = &value
			}
			if (basename == nil) != (tc.Basename == nil) || (basename != nil && *basename != *tc.Basename) {
				t.Fatalf("basename=%v JS=%v", basename, tc.Basename)
			}
		})
	}
	for _, tc := range fixture.Invocations {
		t.Run("cmdInvocation "+tc.Resolved, func(t *testing.T) {
			got := CmdInvocation(tc.Resolved, tc.Args, Env(tc.Env))
			if got.Command != tc.Expected.Command || strings.Join(got.Args, "\x00") != strings.Join(tc.Expected.Args, "\x00") || got.WindowsVerbatimArguments != tc.Expected.WindowsVerbatimArguments {
				t.Fatalf("Go=%#v JS=%#v", got, tc.Expected)
			}
		})
	}
	if runtime.GOOS == "windows" {
		for _, tc := range fixture.Resolve {
			t.Run("resolveFrom "+tc.Value, func(t *testing.T) {
				if got := resolveFrom(tc.Base, tc.Value); got != tc.Expected {
					t.Fatalf("Go=%q JS=%q", got, tc.Expected)
				}
			})
		}
	}
}

func TestRunCli(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fixture commands are unavailable on Windows")
	}
	bin := t.TempDir()
	program := filepath.Join(bin, "fixture")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf 'out-line\\n'\nprintf 'err-line\\n' >&2\nexit 5\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	env := Env{"PATH": bin, "TMPDIR": t.TempDir()}
	for _, tc := range []struct {
		name string
		mode RunOptions
		out  string
		err  string
	}{
		{"default", RunOptions{}, "out-line\n", "err-line\n"},
		{"merge output", RunOptions{MergeOutput: true}, "out-line\nerr-line\n", ""},
		{"output files", RunOptions{OutputFiles: true}, "out-line\n", "err-line\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.mode.Env = env
			r := RunCli("fixture", nil, tc.mode)
			if r.Status == nil || *r.Status != 5 || r.Stdout != tc.out || r.Stderr != tc.err || r.Signal != "" || r.TimedOut {
				t.Fatalf("result = %#v", r)
			}
		})
	}
	t.Run("stdin is passed through and exit codes are preserved", func(t *testing.T) {
		if err := os.WriteFile(program, []byte("#!/bin/sh\n/bin/cat\nprintf 'err' >&2\nexit 7\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		r := RunCli("fixture", nil, RunOptions{Env: env, Input: "stdin body"})
		if r.Status == nil || *r.Status != 7 || r.Stdout != "stdin body" || r.Stderr != "err" {
			t.Fatalf("result = %#v", r)
		}
	})
	t.Run("RunOptions.Cwd controls the child working directory", func(t *testing.T) { // Mutation captured: removing cmd.Dir ignores the caller's working directory.
		cwd := t.TempDir()
		canonicalCwd, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(program, []byte("#!/bin/sh\npwd\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		r := RunCli("fixture", nil, RunOptions{Env: env, Cwd: cwd})
		if r.Status == nil || *r.Status != 0 || strings.TrimSpace(r.Stdout) != canonicalCwd {
			t.Fatalf("cwd result=%#v want=%q", r, canonicalCwd)
		}
	})
	t.Run("temporary output files use Env.TMPDIR and are removed", func(t *testing.T) { // Mutations captured: ignoring Env.TMPDIR or leaving output files behind changes observable filesystem state.
		customTemp := t.TempDir()
		if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf 'out\\n'\nprintf 'err\\n' >&2\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		missingTemp := filepath.Join(t.TempDir(), "missing")
		created := RunCli("fixture", nil, RunOptions{Env: Env{"PATH": bin, "TMPDIR": missingTemp}, OutputFiles: true})
		if created.Status == nil || *created.Status != 0 || created.Error != "" {
			t.Fatalf("a missing Env.TMPDIR is created (mkdirSync recursive in the JS): %#v", created)
		}
		if entries, err := os.ReadDir(missingTemp); err != nil || len(entries) != 0 {
			t.Fatalf("the created Env.TMPDIR must be used and left empty: %v, %v", entries, err)
		}
		for _, options := range []RunOptions{{MergeOutput: true}, {OutputFiles: true}} {
			options.Env = Env{"PATH": bin, "TMPDIR": customTemp}
			r := RunCli("fixture", nil, options)
			entries, err := os.ReadDir(customTemp)
			if err != nil || len(entries) != 0 {
				t.Fatalf("temporary output files remain: %v, %v", entries, err)
			}
			wantOut, wantErr := "out\n", "err\n"
			if options.MergeOutput {
				wantOut, wantErr = "out\nerr\n", ""
			}
			if r.Status == nil || *r.Status != 0 || r.Stdout != wantOut || r.Stderr != wantErr {
				t.Fatalf("stream result=%#v", r)
			}
		}
	})
	t.Run("a signaled child reports Node signal shape", func(t *testing.T) {
		if err := os.WriteFile(program, []byte("#!/bin/sh\nkill -KILL $$\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		r := RunCli("fixture", nil, RunOptions{Env: env})
		if r.Status != nil || r.Signal != "SIGKILL" || !r.TimedOut || r.Error != "" {
			t.Fatalf("result = %#v", r)
		}
	})
	t.Run("spawn errors map errno codes", func(t *testing.T) {
		bad := filepath.Join(bin, "bad-shebang")
		if err := os.WriteFile(bad, []byte("#!/definitely/missing/interpreter\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		r := RunCli("bad-shebang", nil, RunOptions{Env: env})
		if r.Error != "ENOENT" {
			t.Fatalf("error = %q (%#v)", r.Error, r)
		}
		if got := errorCode(&os.PathError{Op: "exec", Path: "x", Err: syscall.ENOEXEC}); got != "ENOEXEC" {
			t.Fatalf("ENOEXEC mapped as %q", got)
		}
	})
	t.Run("timeout wait delay stays short when a grandchild holds pipes", func(t *testing.T) {
		if err := os.WriteFile(program, []byte("#!/bin/sh\n/bin/sleep 5 &\n/bin/sleep 5\n"), 0o700); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		r := RunCli("fixture", nil, RunOptions{Env: env, TimeoutMs: 100})
		elapsed := time.Since(start)
		if !r.TimedOut || r.Status != nil || r.Signal == "" || elapsed > time.Second {
			t.Fatalf("result=%#v elapsed=%v", r, elapsed)
		}
	})
}

func TestRunCliWindowsTreeKillSelection(t *testing.T) {
	for _, tc := range []struct {
		name      string
		platform  string
		resolved  string
		timeoutMs int
		want      bool
	}{
		{"Windows command script with timeout", "win32", "fake.CMD", 1, true},
		{"Windows batch script with timeout", "win32", "fake.BaT", 1, true},
		{"Windows executable with timeout", "win32", "fake.exe", 1, false},
		{"Windows command script without timeout", "win32", "fake.cmd", 0, false},
		{"POSIX command script with timeout", "darwin", "fake.cmd", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldUseWindowsCmdTreeKill(tc.platform, tc.resolved, tc.timeoutMs); got != tc.want {
				t.Fatalf("shouldUseWindowsCmdTreeKill(%q, %q, %d) = %v, want %v", tc.platform, tc.resolved, tc.timeoutMs, got, tc.want)
			}
		})
	}
}

func TestRunCliWindowsTreeKillAssignsBeforeResume(t *testing.T) {
	var steps []string
	assigned, err := assignThenResume(func() error {
		steps = append(steps, "job")
		return nil
	}, func() error {
		steps = append(steps, "resume")
		return nil
	})
	if err != nil || !assigned || strings.Join(steps, ",") != "job,resume" {
		t.Fatalf("assignment sequence = %v, assigned=%v, err=%v", steps, assigned, err)
	}

	steps = nil
	assigned, err = assignThenResume(func() error {
		steps = append(steps, "job")
		return syscall.EINVAL
	}, func() error {
		steps = append(steps, "resume")
		return nil
	})
	if err != nil || assigned || strings.Join(steps, ",") != "job,resume" {
		t.Fatalf("fallback sequence = %v, assigned=%v, err=%v", steps, assigned, err)
	}
}

func TestForceWindowsTreeKillFallback(t *testing.T) { // Mutation captured: ignoring the test-only flag leaves the Job Object path selected.
	if forceWindowsTreeKillFallback(Env{}) {
		t.Fatal("normal environment selected the fallback")
	}
	if !forceWindowsTreeKillFallback(Env{treeKillForceFallbackEnv: "1"}) {
		t.Fatal("test environment did not select the fallback")
	}
	if forceWindowsTreeKillFallback(Env{treeKillForceFallbackEnv: "true"}) {
		t.Fatal("non-exact test value selected the fallback")
	}
}

func TestRunCliWindowsTreeKillTimeoutShape(t *testing.T) {
	status := 3
	result := RunResult{Resolved: "fixture.cmd", Status: &status, Stdout: "partial output", Stderr: "partial error"}
	setRunTimeoutResult(&result)
	if result.Status != nil || result.Signal != "SIGTERM" || !result.TimedOut || result.Error != "ETIMEDOUT" || result.Stdout != "partial output" || result.Stderr != "partial error" {
		t.Fatalf("timeout result = %#v", result)
	}
}

func TestRunTreeKillTestKiller(t *testing.T) {
	tempDir := t.TempDir()
	killerDir := filepath.Join(tempDir, "bin")
	if err := os.MkdirAll(killerDir, 0o700); err != nil {
		t.Fatal(err)
	}
	currentExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	killer := filepath.Join(killerDir, "killer.exe")
	data, err := os.ReadFile(currentExe)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(killer, data, 0o700); err != nil {
		t.Fatal(err)
	}
	var gotPath string
	var gotArgs []string
	run := func(path string, args []string) {
		gotPath = path
		gotArgs = args
	}
	if !runTreeKillTestKiller(Env{"HERDR_SOHO_TREEKILL_TEST_KILLER": killer}, tempDir, 1234, run) {
		t.Fatal("temporary test killer was not accepted")
	}
	resolvedKiller, err := filepath.EvalSymlinks(killer)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != resolvedKiller || len(gotArgs) != 1 || gotArgs[0] != "1234" {
		t.Fatalf("killer invocation = %q %#v", gotPath, gotArgs)
	}
	for name, value := range map[string]string{
		"relative":     filepath.Join("bin", filepath.Base(killer)),
		"outside temp": os.Args[0],
	} {
		t.Run(name, func(t *testing.T) {
			called := false
			if runTreeKillTestKiller(Env{"HERDR_SOHO_TREEKILL_TEST_KILLER": value}, tempDir, 1234, func(string, []string) { called = true }) || called {
				t.Fatalf("unsafe test killer %q was accepted", value)
			}
		})
	}
}

func TestRunCliWaitDelay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX fixture process")
	}
	bin := t.TempDir()
	program := filepath.Join(bin, "fixture")
	pidFile := filepath.Join(t.TempDir(), "grandchild.pid")
	const grandchildDelay = 12 * time.Second
	if err := os.WriteFile(program, []byte("#!/bin/sh\n/bin/sleep 12 &\necho $! > \"$PID_FILE\"\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	r := RunCli("fixture", nil, RunOptions{Env: Env{"PATH": bin, "PID_FILE": pidFile}})
	elapsed := time.Since(start)
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read grandchild pid: %v", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		t.Fatalf("parse grandchild pid %q: %v", data, err)
	}
	grandchild, err := os.FindProcess(pid)
	if err != nil {
		t.Fatalf("find grandchild %d: %v", pid, err)
	}
	if err := grandchild.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("stop grandchild %d: %v", pid, err)
	}
	if r.Status == nil || *r.Status != 0 || r.Error != "" || elapsed >= grandchildDelay {
		t.Fatalf("result=%#v elapsed=%v", r, elapsed)
	}
}

func TestRunExecutableUsesExactPathInsteadOfPATH(t *testing.T) {
	targetDir, pathDir := t.TempDir(), t.TempDir()
	target, err := fakecli.Install(t, targetDir, "fixture", []fakecli.Rule{{Argv: []string{"identity"}, Stdout: "target\n"}})
	if err != nil {
		t.Fatal(err)
	}
	decoy, err := fakecli.Install(t, pathDir, "fixture", []fakecli.Rule{{Argv: []string{"identity"}, Stdout: "decoy\n"}})
	if err != nil {
		t.Fatal(err)
	}
	env := EnvFromOS()
	env["PATH"] = pathDir
	env["HERDR_SOHO_FAKECLI_CONFIG"] = filepath.Join(targetDir, "fixture.json")

	got := RunExecutable(target, []string{"identity"}, RunOptions{Env: env})
	if got.NotFound || got.Resolved != target || got.Status == nil || *got.Status != 0 || got.Stdout != "target\n" {
		t.Fatalf("exact executable result=%#v target=%q decoy=%q", got, target, decoy)
	}
	pathResult := RunCli("fixture", []string{"identity"}, RunOptions{Env: env})
	if !strings.EqualFold(pathResult.Resolved, decoy) {
		t.Fatalf("PATH lookup resolved %q, want decoy %q", pathResult.Resolved, decoy)
	}
}

func TestRunCliWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires native Windows process and cmd.exe behavior")
	}
	reference := readWindowsReference(t)
	dir := t.TempDir()
	exe, err := fakecli.Install(t, dir, "fakecli", []fakecli.Rule{
		{Argv: []string{"space here", `quote"inside`, "50%", "caret^", "bang!", "café"}, Stdout: "exe-out\n", Stderr: "exe-err\n", Code: 7},
		{Argv: []string{"space here", `quote"inside`, "50%", "caret", "bang!", "café"}, Stdout: "exe-out\n", Stderr: "exe-err\n", Code: 7},
		{Argv: []string{"stdin", "large"}, Stdout: strings.Repeat("x", 128*1024), Code: 0},
		{Argv: []string{"timeout"}, Delay: 5000, Code: 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "fakecli.json")
	baseEnv := EnvFromOS()
	setTestPath(baseEnv, dir+";"+testPath(baseEnv))
	baseEnv["PATHEXT"] = ".EXE;.CMD;.BAT;.COM"
	baseEnv["HERDR_SOHO_FAKECLI_CONFIG"] = config
	baseEnv["TMPDIR"] = dir
	if baseEnv.Get("COMSPEC") == "" {
		baseEnv["COMSPEC"] = `C:\Windows\System32\cmd.exe`
	}
	args := []string{"space here", `quote"inside`, "50%", "caret^", "bang!", "café"}
	got := RunCli("fakecli", args, RunOptions{Platform: "win32", Env: baseEnv})
	if got.Status == nil || *got.Status != reference.WindowsExe.Status || got.Stdout != reference.WindowsExe.Stdout || got.Stderr != reference.WindowsExe.Stderr || !strings.EqualFold(filepath.Base(got.Resolved), reference.WindowsExe.ResolvedBase) {
		t.Fatalf(".exe result = %#v", got)
	}
	calls, err := fakecli.ReadCalls(filepath.Join(dir, "fakecli.calls.jsonl"))
	if err != nil || len(calls) != 1 || strings.Join(calls[0].Argv, "\x00") != strings.Join(args, "\x00") {
		t.Fatalf(".exe argv = %#v, %v", calls, err)
	}

	shim := filepath.Join(dir, "fixture.cmd")
	if err := os.WriteFile(shim, []byte("@echo off\r\n\"%FAKECLI%\" %*\r\nexit /b %errorlevel%\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	shimArgs := []string{"space here", `quote"inside`, "50%", "caret^", "bang!", "café"}
	shimEnv := baseEnv.Clone()
	shimEnv["PATHEXT"] = ".CMD;.EXE;.BAT;.COM"
	shimEnv["FAKECLI"] = exe
	got = RunCli("fixture", shimArgs, RunOptions{Platform: "win32", Env: shimEnv})
	if got.Status == nil || *got.Status != 7 || got.Stdout != "exe-out\n" || got.Stderr != "exe-err\n" || !strings.EqualFold(got.Resolved, shim) {
		t.Fatalf(".cmd result = %#v", got)
	}
	calls, err = fakecli.ReadCalls(filepath.Join(dir, "fakecli.calls.jsonl"))
	wantShimArgs := append([]string(nil), shimArgs...)
	wantShimArgs[3] = "caret"
	wantShimArgs[len(wantShimArgs)-1] = "café"
	if err != nil || len(calls) != 2 || strings.Join(calls[1].Argv, "\x00") != strings.Join(wantShimArgs, "\x00") {
		t.Fatalf(".cmd argv = %#v, %v", calls, err)
	}
	stdinCmd := filepath.Join(dir, "stdin.cmd")
	if err := os.WriteFile(stdinCmd, []byte("@echo off\r\nmore\r\nexit /b 9\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdin := RunCli("stdin", nil, RunOptions{Platform: "win32", Env: baseEnv, Input: "stdin payload\r\n"})
	if stdin.Status == nil || *stdin.Status != 9 || stdin.Stdout != "stdin payload\r\n\r\n" {
		t.Fatalf(".cmd stdin = %#v; child env PATH=%q Path=%q PATHEXT=%q COMSPEC=%q ComSpec=%q SystemRoot=%q SystemDrive=%q", stdin, baseEnv["PATH"], baseEnv["Path"], baseEnv["PATHEXT"], baseEnv["COMSPEC"], baseEnv["ComSpec"], baseEnv["SystemRoot"], baseEnv["SystemDrive"])
	}

	large := RunCli("fakecli", []string{"stdin", "large"}, RunOptions{Platform: "win32", Env: baseEnv, Input: "stdin payload"})
	if large.Status == nil || *large.Status != 0 || large.Stdout != strings.Repeat("x", 128*1024) {
		t.Fatalf("stdin/large output = status %v stdout bytes %d", large.Status, len(large.Stdout))
	}
	timeout := RunCli("fakecli", []string{"timeout"}, RunOptions{Platform: "win32", Env: baseEnv, TimeoutMs: 100})
	if !timeout.TimedOut || timeout.Status != nil {
		t.Fatalf("timeout = %#v", timeout)
	}
}

func testPath(env Env) string {
	for key, value := range env {
		if strings.EqualFold(key, "PATH") {
			return value
		}
	}
	return ""
}

func setTestPath(env Env, value string) {
	key := ""
	var existingKeys []string
	for existing := range env {
		if strings.EqualFold(existing, "PATH") {
			existingKeys = append(existingKeys, existing)
			if key == "" || existing == "PATH" {
				key = existing
			}
		}
	}
	for _, existing := range existingKeys {
		delete(env, existing)
	}
	if key == "" {
		key = "PATH"
	}
	env[key] = value
}

func skipUnavailableSymlink(t *testing.T, err error) {
	t.Helper()
	if runtime.GOOS == "windows" && strings.Contains(err.Error(), "A required privilege is not held by the client") {
		t.Skip("Windows symlink privilege unavailable: " + err.Error())
	}
	t.Fatal(err)
}

// Mutation captured: losing the named result drops streams read by temp-file defers.
func TestRunCliMatchesJSFixtures(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process fixtures are covered by TestRunCliWindows on Windows")
	}
	var fixture struct {
		Version int `json:"version"`
		Cases   []struct {
			Name      string   `json:"name"`
			Command   string   `json:"command"`
			Args      []string `json:"args"`
			Mode      string   `json:"mode"`
			TimeoutMs int      `json:"timeoutMs"`
			Script    string   `json:"script"`
			Result    struct {
				NotFound bool    `json:"notFound"`
				Status   *int    `json:"status"`
				Signal   *string `json:"signal"`
				Stdout   string  `json:"stdout"`
				Stderr   string  `json:"stderr"`
				TimedOut bool    `json:"timedOut"`
				Error    *string `json:"error"`
			} `json:"result"`
		} `json:"cases"`
	}
	data, err := os.ReadFile("testdata/runcli.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Version != 1 || len(fixture.Cases) < 7 {
		t.Fatalf("fixture version=%d cases=%d", fixture.Version, len(fixture.Cases))
	}
	for _, tc := range fixture.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			bin, tmp := t.TempDir(), t.TempDir()
			if tc.Mode != "missing" && tc.Command == "" {
				if err := os.WriteFile(filepath.Join(bin, tc.Name), []byte(tc.Script), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			options := RunOptions{Env: Env{"PATH": bin + string(os.PathListSeparator) + os.Getenv("PATH"), "TMPDIR": tmp}}
			switch tc.Mode {
			case "mergeOutput":
				options.MergeOutput = true
			case "outputFiles":
				options.OutputFiles = true
			}
			options.TimeoutMs = tc.TimeoutMs
			if tc.Mode == "timeout" && options.TimeoutMs == 0 {
				options.TimeoutMs = 100
			}
			command := tc.Command
			if command == "" {
				command = tc.Name
			}
			got := RunCli(command, tc.Args, options)
			if got.NotFound != tc.Result.NotFound || !equalIntPointer(got.Status, tc.Result.Status) || !equalStringPointer(got.Signal, tc.Result.Signal) || got.Stdout != tc.Result.Stdout || got.Stderr != tc.Result.Stderr || got.TimedOut != tc.Result.TimedOut || !equalStringPointer(got.Error, tc.Result.Error) {
				t.Fatalf("Go=%#v JS=%#v", got, tc.Result)
			}
		})
	}
}

func equalIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func equalStringPointer(left string, right *string) bool {
	if right == nil {
		return left == ""
	}
	return left == *right
}

func TestAtomicWrite(t *testing.T) {
	t.Run("atomic rename defaults to os.Rename", func(t *testing.T) {
		if reflect.ValueOf(renameFile).Pointer() != reflect.ValueOf(os.Rename).Pointer() {
			t.Fatal("renameFile no longer defaults to os.Rename")
		}
	})
	t.Run("atomicWrite preserves the destination and removes its temp when rename returns EIO", func(t *testing.T) { // Mutation captured: removing temp cleanup leaves an extra file after the injected rename error.
		root := t.TempDir()
		dest := filepath.Join(root, "config")
		if err := os.WriteFile(dest, []byte("original\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		realRename := renameFile
		renameFile = func(string, string) error { return errors.New("injected EIO") }
		t.Cleanup(func() { renameFile = realRename })
		if err := AtomicWrite(dest, "replacement\n"); err == nil {
			t.Fatal("expected injected rename failure")
		}
		got, err := os.ReadFile(dest)
		entries, readErr := os.ReadDir(root)
		if err != nil || string(got) != "original\n" || readErr != nil || len(entries) != 1 || entries[0].Name() != "config" {
			t.Fatalf("destination=%q entries=%v readErr=%v fileErr=%v", got, entries, readErr, err)
		}
	})
	t.Run("atomicWrite removes its temporary file when rename fails", func(t *testing.T) { // Mutation captured: omitting cleanup leaves a temp after a failed atomic replacement.
		root := t.TempDir()
		dest := filepath.Join(root, "directory")
		if err := os.Mkdir(dest, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(dest, "content"); err == nil {
			t.Fatal("expected replacing a directory to fail")
		}
		entries, err := os.ReadDir(root)
		if err != nil || len(entries) != 1 || entries[0].Name() != "directory" {
			t.Fatalf("directory entries after failed write=%v err=%v", entries, err)
		}
	})
	t.Run("atomicWrite: a new file uses mode 0600", func(t *testing.T) { // Mutation captured: creating a new file with 0644 breaks private task/config state.
		file := filepath.Join(t.TempDir(), "new-file")
		if err := AtomicWrite(file, "new\n"); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(file)
		if err != nil || (runtime.GOOS == "windows" && info.Mode().Perm()&0o200 == 0) || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("mode=%v err=%v", info, err)
		}
	})
	t.Run("atomicWrite: a symlink to an existing file writes through, keeps the link and the target mode", func(t *testing.T) { // JS: "atomicWrite: a symlink to an existing file writes through, keeps the link and the target mode"
		root := t.TempDir()
		target, link := filepath.Join(root, "CLAUDE.md"), filepath.Join(root, "AGENTS.md")
		if err := os.WriteFile(target, []byte("seed\n"), 0o640); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, link); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := AtomicWrite(link, "through\n"); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(link)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("link lost: %v %v", info, err)
		}
		got, _ := os.ReadFile(target)
		stat, _ := os.Stat(target)
		if string(got) != "through\n" || stat.Mode().Perm() != 0o640 {
			t.Fatalf("content=%q mode=%o", got, stat.Mode().Perm())
		}
	})
	t.Run("atomicWrite: a relative link to a file in another directory resolves to that file", func(t *testing.T) { // JS: "atomicWrite: a relative link to a file in another directory resolves to that file"
		root := t.TempDir()
		other := filepath.Join(root, "other")
		_ = os.Mkdir(other, 0o700)
		target := filepath.Join(other, "CLAUDE.md")
		_ = os.WriteFile(target, []byte("seed\n"), 0o600)
		link := filepath.Join(root, "AGENTS.md")
		if err := os.Symlink(filepath.Join("other", "CLAUDE.md"), link); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := AtomicWrite(link, "relative\n"); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(target)
		if string(got) != "relative\n" {
			t.Fatalf("%q", got)
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 2 {
			t.Fatalf("temp left: %v", entries)
		}
	})
	t.Run("atomicWrite: a broken chain creates the file at its missing end, links stay links", func(t *testing.T) { // JS: "atomicWrite: a broken chain creates the file at its missing end, links stay links"
		root := t.TempDir()
		a, b, missing := filepath.Join(root, "a.md"), filepath.Join(root, "b.md"), filepath.Join(root, "missing.txt")
		c := filepath.Join(root, "c.md")
		if err := os.Symlink("b.md", a); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := os.Symlink("c.md", b); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := os.Symlink("./missing.txt", c); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := AtomicWrite(a, "created\n"); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(missing)
		info, _ := os.Lstat(a)
		infoB, _ := os.Lstat(b)
		infoC, _ := os.Lstat(c)
		if string(got) != "created\n" || info.Mode()&os.ModeSymlink == 0 || infoB.Mode()&os.ModeSymlink == 0 || infoC.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("content=%q link=%v", got, info)
		}
	})
	t.Run("atomicWrite: a symlink cycle throws the realpath ELOOP error, leaves the links and no temp behind", func(t *testing.T) { // JS: "atomicWrite: a symlink cycle throws the realpath ELOOP error, leaves the links and no temp behind"
		root := t.TempDir()
		a, b := filepath.Join(root, "a.md"), filepath.Join(root, "b.md")
		if err := os.Symlink("b.md", a); err != nil {
			skipUnavailableSymlink(t, err)
		}
		if err := os.Symlink("a.md", b); err != nil {
			skipUnavailableSymlink(t, err)
		}
		err := AtomicWrite(a, "x\n")
		if err == nil || !strings.Contains(err.Error(), "ELOOP") || !errors.Is(err, syscall.ELOOP) {
			t.Fatalf("got %v", err)
		}
		entries, _ := os.ReadDir(root)
		if len(entries) != 2 {
			t.Fatalf("temp left: %v", entries)
		}
	})
}

func TestStateProjectRoot(t *testing.T) {
	var reference *windowsReference
	if runtime.GOOS == "windows" {
		loaded := readWindowsReference(t)
		reference = &loaded
	}
	t.Run("state root preserves the main checkout and non-git path behavior", func(t *testing.T) { // JS: "state root preserves the main checkout and non-git path behavior"
		root := t.TempDir()
		repo := filepath.Join(root, "repo")
		outside := filepath.Join(root, "outside")
		_ = os.Mkdir(repo, 0o700)
		_ = os.Mkdir(outside, 0o700)
		gitInit(t, repo)
		realRepo, _ := filepath.EvalSymlinks(repo)
		env := EnvFromOS()
		assertRootReference(t, reference, "repo", env, repo, root, realRepo, false)
		assertRootReference(t, reference, "repo", env, repo, root, realRepo, true)
		realOutside, _ := filepath.EvalSymlinks(outside)
		assertRootReference(t, reference, "outside", env, realOutside, root, realOutside, false)
		assertRootReference(t, reference, "outside", env, realOutside, root, realOutside, true)
	})
	t.Run("a main checkout inside a .git path does not become the linked worktree state root", func(t *testing.T) { // Mutation captured: removing the .git segment guard selects metadata inside .git.
		root := t.TempDir()
		repo := filepath.Join(root, ".git", "cont", "proj")
		if err := os.MkdirAll(repo, 0o700); err != nil {
			t.Fatal(err)
		}
		gitInit(t, repo)
		worker := filepath.Join(root, "worker")
		gitRun(t, repo, "worktree", "add", "-q", "-b", "worker", worker)
		resetRootCacheForTests()
		got := StateProjectRoot(EnvFromOS(), worker)
		canonicalWorker, err := filepath.EvalSymlinks(worker)
		if err != nil {
			t.Fatal(err)
		}
		if got != canonicalWorker {
			t.Fatalf("state root=%q want linked worktree %q", got, canonicalWorker)
		}
	})
	t.Run("a separate git dir containing its listed worktree is not used as the state root", func(t *testing.T) { // Mutation captured: ignoring isWithin selects the common git directory as a checkout.
		root := t.TempDir()
		common := filepath.Join(root, "container")
		repo := filepath.Join(common, "repo")
		if err := os.MkdirAll(repo, 0o700); err != nil {
			t.Fatal(err)
		}
		gitRun(t, root, "init", "-q", "--separate-git-dir="+common, repo)
		gitRun(t, repo, "config", "user.email", "test@example.invalid")
		gitRun(t, repo, "config", "user.name", "Test")
		if err := os.WriteFile(filepath.Join(repo, "tracked"), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		gitRun(t, repo, "add", "tracked")
		gitRun(t, repo, "commit", "-qm", "fixture")
		worker := filepath.Join(root, "worker")
		gitRun(t, repo, "worktree", "add", "-q", "-b", "worker", worker)
		resetRootCacheForTests()
		canonicalWorker, err := filepath.EvalSymlinks(worker)
		if err != nil {
			t.Fatal(err)
		}
		if got := StateProjectRoot(EnvFromOS(), worker); got != canonicalWorker {
			t.Fatalf("state root=%q want worker %q", got, canonicalWorker)
		}
	})
	t.Run("roster, status, and wait in a linked worktree read the main checkout state", func(t *testing.T) { // JS: "roster, status, and wait in a linked worktree read the main checkout state"
		root := t.TempDir()
		repo := filepath.Join(root, "repo")
		_ = os.Mkdir(repo, 0o700)
		gitInit(t, repo)
		worker := filepath.Join(repo, ".worktrees", "worker")
		_ = os.MkdirAll(filepath.Dir(worker), 0o700)
		gitRun(t, repo, "worktree", "add", "-q", "-b", "worker", worker)
		realRepo, _ := filepath.EvalSymlinks(repo)
		realWorker, _ := filepath.EvalSymlinks(worker)
		env := EnvFromOS()
		assertRootReference(t, reference, "linked-worker", env, worker, repo, realWorker, false)
		assertRootReference(t, reference, "linked-worker", env, worker, repo, realRepo, true)
	})
	t.Run("a linked worktree of a bare repository keeps state in that worktree", func(t *testing.T) { // JS: "a linked worktree of a bare repository keeps state in that worktree"
		root := t.TempDir()
		source := filepath.Join(root, "source")
		_ = os.Mkdir(source, 0o700)
		gitInit(t, source)
		bare := filepath.Join(root, "origin.git")
		gitRun(t, root, "clone", "--bare", "-q", source, bare)
		worker := filepath.Join(root, "worker")
		gitRun(t, root, "--git-dir", bare, "worktree", "add", "-q", "-b", "worker", worker, "HEAD")
		realWorker, _ := filepath.EvalSymlinks(worker)
		env := EnvFromOS()
		assertRootReference(t, reference, "bare-linked-worker", env, worker, root, realWorker, false)
		assertRootReference(t, reference, "bare-linked-worker", env, worker, root, realWorker, true)
	})
	t.Run("linked and ordinary submodule checkouts use the submodule checkout as state root", func(t *testing.T) { // JS: "linked and ordinary submodule checkouts use the submodule checkout as state root"
		root := t.TempDir()
		super, src := filepath.Join(root, "super"), filepath.Join(root, "src")
		_ = os.Mkdir(super, 0o700)
		_ = os.Mkdir(src, 0o700)
		gitInit(t, super)
		gitInit(t, src)
		gitRun(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "submodule")
		gitRun(t, super, "add", ".gitmodules", "submodule")
		gitRun(t, super, "commit", "-qm", "submodule")
		sub := filepath.Join(super, "submodule")
		realSub, _ := filepath.EvalSymlinks(sub)
		env := EnvFromOS()
		assertRootReference(t, reference, "submodule", env, sub, root, realSub, false)
		assertRootReference(t, reference, "submodule", env, sub, root, realSub, true)
		linked := filepath.Join(root, "sub-linked")
		gitRun(t, sub, "worktree", "add", "-q", "-b", "linked", linked, "HEAD")
		realLinked, _ := filepath.EvalSymlinks(linked)
		assertRootReference(t, reference, "linked-submodule", env, linked, root, realLinked, false)
		assertRootReference(t, reference, "linked-submodule", env, linked, root, realSub, true)
	})
	t.Run("core.worktree under the superproject .git falls back to project root", func(t *testing.T) { // JS: "core.worktree that resolves up to the superproject .git falls back to projectRoot"
		// Mutation captured: accepting a .git path segment stores state inside Git metadata.
		for index, target := range []string{"../..", filepath.Join("..", "..", "..", "..", "repo", ".git")} {
			root := t.TempDir()
			super, src := filepath.Join(root, "repo"), filepath.Join(root, "src")
			_ = os.Mkdir(super, 0o700)
			_ = os.Mkdir(src, 0o700)
			gitInit(t, super)
			gitInit(t, src)
			gitRun(t, super, "-c", "protocol.file.allow=always", "submodule", "add", "-q", src, "submodule")
			gitRun(t, super, "add", ".gitmodules", "submodule")
			gitRun(t, super, "commit", "-qm", "submodule")
			sub := filepath.Join(super, "submodule")
			worker := filepath.Join(root, "sub-worktree")
			gitRun(t, sub, "worktree", "add", "-q", "-b", "linked", worker, "HEAD")
			common, status := runGit(EnvFromOS(), worker, "rev-parse", "--git-common-dir")
			if status != 0 {
				t.Fatal("cannot resolve fixture common directory")
			}
			common = resolveFrom(worker, strings.TrimSpace(common))
			configured := target
			if filepath.IsAbs(target) || strings.Contains(target, "repo") {
				configured = filepath.Join(super, ".git")
			}
			gitRun(t, worker, "--git-dir", common, "config", "core.worktree", configured)
			resetRootCacheForTests()
			realWorker, _ := filepath.EvalSymlinks(worker)
			env := EnvFromOS()
			key := "core-worktree-dotdot"
			if index == 1 {
				key = "core-worktree-git"
			}
			assertRootReference(t, reference, key, env, worker, root, realWorker, false)
			assertRootReference(t, reference, key, env, worker, root, realWorker, true)
			got := StateProjectRoot(env, worker)
			if hasGitPathSegment(got) {
				t.Fatalf("target=%q state root=%q contains .git path segment", configured, got)
			}
		}
	})
	t.Run("Win32 metadata guard recognizes .git path segments", func(t *testing.T) { // JS: "state-root metadata guard recognizes .git path segments on Win32"
		if !hasGitPathSegment(`C:\repo\.git\worktrees\worker`) || hasGitPathSegment(`C:\repo\.gitmodules\worktrees\worker`) {
			t.Fatal("Win32 .git path-segment recognition mismatch")
		}
	})
}

func TestProjectRootCache(t *testing.T) {
	// Mutation captured: bypassing the cache repeats git calls for the same cwd and env.
	resetRootCacheForTests()
	old := gitRunner
	defer func() { gitRunner = old; resetRootCacheForTests() }()
	calls := 0
	gitRunner = func(_ Env, cwd string, args ...string) (string, int) {
		calls++
		if len(args) == 1 && args[0] == "--show-toplevel" {
			return cwd + "\n", 0
		}
		return "", 1
	}
	cwd := filepath.Join(t.TempDir(), "project")
	if first, second := ProjectRoot(Env{"GIT_DIR": "one"}, cwd), ProjectRoot(Env{"GIT_DIR": "one"}, cwd); first != second || calls != 1 {
		t.Fatalf("same key results=%q/%q calls=%d", first, second, calls)
	}
	ProjectRoot(Env{"GIT_DIR": "two"}, cwd)
	if calls != 2 {
		t.Fatalf("different GIT_DIR should use a distinct key, calls=%d", calls)
	}
	resetRootCacheForTests()
	calls = 0
	repoRoot := filepath.Join(t.TempDir(), "repo")
	commonDir := filepath.Join(repoRoot, ".git")
	gitRunner = func(_ Env, _ string, args ...string) (string, int) {
		calls++
		switch strings.Join(args, " ") {
		case "rev-parse --git-dir":
			return filepath.Join(commonDir, "worktrees", "worker") + "\n", 0
		case "rev-parse --git-common-dir":
			return commonDir + "\n", 0
		default:
			return "", 1
		}
	}
	stateCwd := filepath.Join(t.TempDir(), "worker")
	first := StateProjectRoot(Env{"GIT_WORK_TREE": "worker"}, stateCwd)
	second := StateProjectRoot(Env{"GIT_WORK_TREE": "worker"}, stateCwd)
	wantRoot := repoRoot
	if first != wantRoot || second != first || calls != 2 {
		t.Fatalf("state root=%q/%q calls=%d", first, second, calls)
	}
}

func resetRootCacheForTests() {
	rootCache.Lock()
	rootCache.values = make(map[string]string)
	rootCache.Unlock()
}

func gitRun(t *testing.T, cwd string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = cwd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
}
func gitInit(t *testing.T, root string) {
	t.Helper()
	gitRun(t, root, "init", "-q")
	gitRun(t, root, "config", "user.email", "test@example.invalid")
	gitRun(t, root, "config", "user.name", "Test")
	_ = os.WriteFile(filepath.Join(root, "tracked"), []byte("fixture\n"), 0o600)
	gitRun(t, root, "add", "tracked")
	gitRun(t, root, "commit", "-qm", "fixture")
}

func TestRunCliTempDirMatchesJSTempDirFor(t *testing.T) { // JS: "send reports a temporary-file error when TMPDIR is a file"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("a file on the TMPDIR path is EEXIST on stderr, and nothing runs", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		env := EnvFromOS()
		env["TMPDIR"] = file
		r := RunExecutable(exe, []string{"-test.run=^$"}, RunOptions{Env: env, TimeoutMs: 30000, MergeOutput: true})
		if r.Status != nil || r.Error != "EEXIST" || r.Stdout != "" || r.Stderr != "herdr-soho: cannot write temporary files: EEXIST\n" {
			t.Fatalf("result = %#v", r)
		}
	})
	t.Run("a missing TMPDIR is created, as mkdirSync recursive does", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "a", "b")
		env := EnvFromOS()
		env["TMPDIR"] = dir
		r := RunExecutable(exe, []string{"-test.run=^$"}, RunOptions{Env: env, TimeoutMs: 30000, OutputFiles: true})
		if r.Status == nil || *r.Status != 0 || r.Error != "" {
			t.Fatalf("result = %#v", r)
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("TMPDIR not created: %v", err)
		}
	})
}
