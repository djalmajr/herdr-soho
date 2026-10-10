package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeDocker replaces runCommand for the test duration and restores
// the original on cleanup. It records, per call, the executable name
// followed by its argv.
func withFakeDocker(t *testing.T, fn func(name string, args []string) (string, int, error)) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runCommand
	runCommand = func(ctx context.Context, name string, args []string) ([]byte, int, error) {
		calls = append(calls, append([]string{name}, args...))
		out, code, err := fn(name, args)
		return []byte(out), code, err
	}
	t.Cleanup(func() { runCommand = orig })
	return &calls
}

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runSmokeLines(t *testing.T, args []string) (int, []string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runSmoke(args, &stdout, &stderr)
	var lines []string
	for _, l := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		lines = append(lines, string(l))
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return code, lines, stderr.String()
}

func TestSmokeRunChecks(t *testing.T) {
	dir := t.TempDir()
	// Quoted ARG defaults (double and single) must be unquoted.
	df := writeFile(t, dir, "Dockerfile",
		"FROM base\nARG CLAUDE_CODE_VERSION=\"1.25.3\"\nARG PAGER='less -F'\nRUN echo $PAGER\n")
	spec := writeFile(t, dir, "spec.json",
		`{"checks":[{"name":"claude","argv":["claude","--version"],"version_arg":"CLAUDE_CODE_VERSION"},`+
			`{"name":"claude-help","argv":["claude","--help"]}]}`)
	calls := withFakeDocker(t, func(name string, args []string) (string, int, error) {
		if strings.HasSuffix(args[len(args)-1], "--version") {
			return "claude-code v1.25.3 (go 1.25.3)\n", 0, nil
		}
		return "usage: claude\n", 0, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{
		"--image", "img:dev", "--spec", spec, "--dockerfile", df, "--docker", "fake-docker",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if len(*calls) != 2 {
		t.Fatalf("calls = %v", *calls)
	}
	// Exact argv passed to the docker executable.
	want := []string{"fake-docker", "run", "--rm", "--pull", "never", "--network", "none", "img:dev", "claude", "--version"}
	if len((*calls)[0]) != len(want) {
		t.Fatalf("call 0 = %v, want %v", (*calls)[0], want)
	}
	for i := range want {
		if (*calls)[0][i] != want[i] {
			t.Fatalf("call 0 = %v, want %v", (*calls)[0], want)
		}
	}
	if len((*calls)[1]) != 10 || (*calls)[1][9] != "--help" {
		t.Fatalf("call 1 = %v", (*calls)[1])
	}
	wantLines := []string{
		`{"name":"claude","argv":["claude","--version"],"exit":0,"expected":"1.25.3","ok":true,"output":"claude-code v1.25.3 (go 1.25.3)"}`,
		`{"name":"claude-help","argv":["claude","--help"],"exit":0,"expected":"","ok":true,"output":"usage: claude"}`,
		`{"summary":true,"checks":2,"failed":0}`,
	}
	if len(lines) != len(wantLines) {
		t.Fatalf("lines = %q, want %q", lines, wantLines)
	}
	for i := range wantLines {
		if lines[i] != wantLines[i] {
			t.Fatalf("line %d = %s, want %s", i, lines[i], wantLines[i])
		}
	}
}

func TestSmokeConflictingArg(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\nARG TOOL_VERSION=1.0.0\nARG OTHER=x\nARG TOOL_VERSION=2.0.0\n")
	spec := writeFile(t, dir, "spec.json", `{"checks":[{"name":"c","argv":["c"]}]}`)
	calls := withFakeDocker(t, func(name string, args []string) (string, int, error) {
		t.Fatal("docker must not be called when the Dockerfile ARGs conflict")
		return "", 0, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if len(lines) != 0 {
		t.Fatalf("stdout = %q", lines)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v", *calls)
	}
	if !strings.Contains(stderr, "TOOL_VERSION") {
		t.Fatalf("stderr = %q, want the conflicting ARG named", stderr)
	}
}

func TestSmokeBuildArgOverride(t *testing.T) {
	dir := t.TempDir()
	// The same ARG declared twice with the SAME default is not a conflict.
	df := writeFile(t, dir, "Dockerfile", "FROM base\nARG TOOL_VERSION=1.0.0\nARG SAME=9\nARG SAME=9\n")
	spec := writeFile(t, dir, "spec.json",
		`{"checks":[{"name":"tool","argv":["tool","--version"],"version_arg":"TOOL_VERSION"}]}`)
	withFakeDocker(t, func(name string, args []string) (string, int, error) {
		return "tool 2.0.0\n", 0, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{
		"--image", "img", "--spec", spec, "--dockerfile", df, "--build-arg", "TOOL_VERSION=2.0.0",
	})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := `{"name":"tool","argv":["tool","--version"],"exit":0,"expected":"2.0.0","ok":true,"output":"tool 2.0.0"}`
	if lines[0] != want {
		t.Fatalf("line = %s, want %s", lines[0], want)
	}
	if lines[1] != `{"summary":true,"checks":1,"failed":0}` {
		t.Fatalf("summary = %s", lines[1])
	}
}

func TestSmokeMissingVersionArg(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\nARG TOOL_VERSION=1.0.0\n")
	spec := writeFile(t, dir, "spec.json",
		`{"checks":[{"name":"tool","argv":["tool","--version"],"version_arg":"NOPE"}]}`)
	calls := withFakeDocker(t, func(name string, args []string) (string, int, error) {
		t.Fatal("no check may run when a version_arg does not resolve")
		return "", 0, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
	if code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
	if len(lines) != 0 {
		t.Fatalf("stdout = %q", lines)
	}
	if len(*calls) != 0 {
		t.Fatalf("calls = %v", *calls)
	}
	if !strings.Contains(stderr, "NOPE") {
		t.Fatalf("stderr = %q, want the missing version_arg named", stderr)
	}
}

func TestSmokeVersionTokenMatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  string
		ok   bool
		code int
	}{
		{"mid-token letters", "go1.25.3 linux/arm64\n", true, 0},
		{"v prefix at end", "claude v1.25.3", true, 0},
		{"longer version 1.25.30", "version 1.25.30\n", false, 1},
		{"dot before version", "build .1.25.3 ok\n", false, 1},
		{"bad occurrence then good one", "1.25.30\n1.25.3\n", true, 0},
		{"dotted suffix", "v1.25.3.1\n", false, 1},
		{"prerelease suffix", "go version go1.25.3rc1 linux/amd64\n", false, 1},
		{"hyphen suffix", "1.25.3-rc1\n", false, 1},
		{"plus suffix", "1.25.3+build\n", false, 1},
		{"followed by space and paren", "1.25.3 (tool)\n", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			df := writeFile(t, dir, "Dockerfile", "FROM base\nARG TOOL_VERSION=1.25.3\n")
			spec := writeFile(t, dir, "spec.json",
				`{"checks":[{"name":"tool","argv":["tool","--version"],"version_arg":"TOOL_VERSION"}]}`)
			withFakeDocker(t, func(name string, args []string) (string, int, error) {
				return tc.out, 0, nil
			})
			code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
			if code != tc.code {
				t.Fatalf("exit = %d, want %d (stderr = %q)", code, tc.code, stderr)
			}
			var got bool
			if err := unmarshalField(t, lines[0], "ok", &got); err != nil {
				t.Fatal(err)
			}
			if got != tc.ok {
				t.Fatalf("ok = %v, want %v (line = %s)", got, tc.ok, lines[0])
			}
		})
	}
}

func TestSmokeCheckFails(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\n")
	spec := writeFile(t, dir, "spec.json", `{"checks":[{"name":"bad","argv":["bad"]}]}`)
	withFakeDocker(t, func(name string, args []string) (string, int, error) {
		return "boom\n", 3, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (stderr = %q)", code, stderr)
	}
	if lines[0] != `{"name":"bad","argv":["bad"],"exit":3,"expected":"","ok":false,"output":"boom"}` {
		t.Fatalf("line = %s", lines[0])
	}
	if lines[1] != `{"summary":true,"checks":1,"failed":1}` {
		t.Fatalf("summary = %s", lines[1])
	}
}

func TestSmokeStartError(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\n")
	spec := writeFile(t, dir, "spec.json", `{"checks":[{"name":"c","argv":["c"]}]}`)
	withFakeDocker(t, func(name string, args []string) (string, int, error) {
		return "", 0, errors.New("exec: docker: executable file not found")
	})
	code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if len(lines) != 0 {
		t.Fatalf("stdout = %q", lines)
	}
	if !strings.Contains(stderr, "not found") {
		t.Fatalf("stderr = %q", stderr)
	}
}

func TestSmokeOutputLineAndTruncation(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\n")
	spec := writeFile(t, dir, "spec.json", `{"checks":[{"name":"c","argv":["c"]}]}`)
	long := strings.Repeat("é", 250)
	withFakeDocker(t, func(name string, args []string) (string, int, error) {
		return "\n   \n" + long + "\nsecond\n", 0, nil
	})
	code, lines, stderr := runSmokeLines(t, []string{"--image", "img", "--spec", spec, "--dockerfile", df})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	var out string
	if err := unmarshalField(t, lines[0], "output", &out); err != nil {
		t.Fatal(err)
	}
	if len([]rune(out)) != 200 {
		t.Fatalf("output has %d runes, want 200", len([]rune(out)))
	}
	if !strings.HasPrefix(out, strings.Repeat("é", 200)) {
		t.Fatalf("output = %q", out)
	}
}

func TestSmokeUsageAndInvalidSpec(t *testing.T) {
	dir := t.TempDir()
	df := writeFile(t, dir, "Dockerfile", "FROM base\n")
	cases := []struct {
		name string
		spec string
		args []string
	}{
		{"missing image", `{"checks":[{"name":"c","argv":["c"]}]}`, []string{"--spec", "s.json", "--dockerfile", df}},
		{"missing spec", `{"checks":[{"name":"c","argv":["c"]}]}`, []string{"--image", "img", "--dockerfile", df}},
		{"unknown flag", `{"checks":[{"name":"c","argv":["c"]}]}`, []string{"--image", "img", "--spec", "s.json", "--bogus"}},
		{"bad timeout", `{"checks":[{"name":"c","argv":["c"]}]}`, []string{"--image", "img", "--spec", "s.json", "--dockerfile", df, "--timeout", "soon"}},
		{"malformed spec json", "{", []string{"--image", "img", "--spec", "s.json", "--dockerfile", df}},
		{"no checks", `{"checks":[]}`, []string{"--image", "img", "--spec", "s.json", "--dockerfile", df}},
		{"check without name", `{"checks":[{"argv":["c"]}]}`, []string{"--image", "img", "--spec", "s.json", "--dockerfile", df}},
		{"check with empty argv", `{"checks":[{"name":"c","argv":[]}]}`, []string{"--image", "img", "--spec", "s.json", "--dockerfile", df}},
		{"bad build-arg", `{"checks":[{"name":"c","argv":["c"]}]}`, []string{"--image", "img", "--spec", "s.json", "--dockerfile", df, "--build-arg", "NO_EQUALS"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spec := writeFile(t, dir, "spec.json", tc.spec)
			args := tc.args
			for i, a := range args {
				if a == "s.json" {
					args[i] = spec
				}
			}
			var stdout, stderr bytes.Buffer
			code := runSmoke(args, &stdout, &stderr)
			if code != 2 {
				t.Fatalf("exit = %d, want 2 (stderr = %q)", code, stderr)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q", stdout.String())
			}
		})
	}
	// A spec file that does not exist is an I/O error, not a usage error.
	var stdout, stderr bytes.Buffer
	code := runSmoke([]string{"--image", "img", "--spec", filepath.Join(dir, "absent.json"), "--dockerfile", df}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("exit = %d, want 4 (stderr = %q)", code, stderr)
	}
}

// unmarshalField decodes one JSON line of smoke output and extracts the
// named field into dst.
func unmarshalField(t *testing.T, line, field string, dst any) error {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		return err
	}
	raw, ok := m[field]
	if !ok {
		return fmt.Errorf("field %s not present in %s", field, line)
	}
	return json.Unmarshal(raw, dst)
}

func TestSmokeVersionWordToken(t *testing.T) {
	for out, want := range map[string]bool{"tool dev\n": true, "tool development\n": false, "device ready\n": false} {
		if got := versionMatch([]byte(out), "dev"); got != want {
			t.Errorf("versionMatch(%q, dev) = %v, want %v", out, got, want)
		}
	}
}
