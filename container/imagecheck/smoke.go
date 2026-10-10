package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// runCommand is the external entry point to the docker CLI: it starts name
// with args under ctx and returns the combined stdout+stderr, the process
// exit code and an error reserved for a failure to start or a timeout. A
// non-zero exit code of the process is a failed check, not an error. The
// tests replace this variable and restore it with t.Cleanup.
var runCommand = func(ctx context.Context, name string, args []string) (output []byte, exitCode int, err error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, 0, fmt.Errorf("timed out: %v", ctx.Err())
	}
	if runErr == nil {
		return buf.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return buf.Bytes(), exitErr.ExitCode(), nil
	}
	return nil, 0, runErr
}

// smokeCheck is one tool check of the spec file.
type smokeCheck struct {
	Name       string   `json:"name"`
	Argv       []string `json:"argv"`
	VersionArg string   `json:"version_arg"`
}

// smokeSpec is the JSON inventory of tool checks; name and a non-empty
// argv are required per check, version_arg is optional.
type smokeSpec struct {
	Checks []smokeCheck `json:"checks"`
}

// checkLine is one JSON result line, in the fixed field order
// name,argv,exit,expected,ok,output.
type checkLine struct {
	Name     string   `json:"name"`
	Argv     []string `json:"argv"`
	Exit     int      `json:"exit"`
	Expected string   `json:"expected"`
	OK       bool     `json:"ok"`
	Output   string   `json:"output"`
}

// summaryLine is the final JSON line after all checks ran.
type summaryLine struct {
	Summary bool `json:"summary"`
	Checks  int  `json:"checks"`
	Failed  int  `json:"failed"`
}

// argLine matches a Dockerfile ARG with an inline default value; the value
// may be wrapped in matching double or single quotes.
var argLine = regexp.MustCompile(`^\s*ARG\s+([A-Za-z_][A-Za-z0-9_]*)=(.*?)\s*$`)

// runSmoke runs every tool check of the spec JSON inside the image with
// `docker run --rm --pull never --network none` and verifies the tool
// version against the Dockerfile ARG defaults overridden by --build-arg.
// It prints one JSON line per check plus a summary line. Exit codes: 0 all
// checks ok, 1 at least one check failed, 2 usage or invalid spec/ARGs, 4
// I/O or docker start/timeout failure.
func runSmoke(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("smoke", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var image, specPath, dockerfile string
	var buildArgs stringList
	var dockerExe string
	var timeout time.Duration
	fs.StringVar(&image, "image", "", "image reference to smoke-test (required)")
	fs.StringVar(&specPath, "spec", "", "path to the check spec JSON (required)")
	fs.StringVar(&dockerfile, "dockerfile", "Dockerfile", "Dockerfile whose ARG defaults are read")
	fs.Var(&buildArgs, "build-arg", "NAME=VALUE overriding a Dockerfile ARG default (repeatable)")
	fs.StringVar(&dockerExe, "docker", "docker", "docker executable")
	fs.DurationVar(&timeout, "timeout", 60*time.Second, "per-check timeout")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if image == "" || specPath == "" {
		fmt.Fprintln(stderr, "imagecheck smoke: --image and --spec are required")
		fs.Usage()
		return 2
	}
	data, err := os.ReadFile(specPath)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	var spec smokeSpec
	if err := json.Unmarshal(data, &spec); err != nil {
		fmt.Fprintf(stderr, "imagecheck: spec: %v\n", err)
		return 2
	}
	if len(spec.Checks) == 0 {
		fmt.Fprintln(stderr, "imagecheck: spec: at least one check is required")
		return 2
	}
	for i, c := range spec.Checks {
		if c.Name == "" || len(c.Argv) == 0 {
			fmt.Fprintf(stderr, "imagecheck: spec: check %d: name and a non-empty argv are required\n", i+1)
			return 2
		}
	}
	dfData, err := os.ReadFile(dockerfile)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	merged, err := dockerArgValues(dfData)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 2
	}
	for _, ba := range buildArgs {
		name, value, ok := splitBuildArg(ba)
		if !ok {
			fmt.Fprintf(stderr, "imagecheck: invalid --build-arg %q (want NAME=VALUE)\n", ba)
			return 2
		}
		merged[name] = value
	}
	// Every version_arg must resolve before any check runs.
	for _, c := range spec.Checks {
		if c.VersionArg != "" {
			if _, ok := merged[c.VersionArg]; !ok {
				fmt.Fprintf(stderr, "imagecheck: check %s: version_arg %s is not a Dockerfile ARG nor a --build-arg\n", c.Name, c.VersionArg)
				return 2
			}
		}
	}
	failed := 0
	for _, c := range spec.Checks {
		expected := ""
		if c.VersionArg != "" {
			expected = merged[c.VersionArg]
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		argv := append([]string{"run", "--rm", "--pull", "never", "--network", "none", image}, c.Argv...)
		out, exitCode, err := runCommand(ctx, dockerExe, argv)
		cancel()
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %s: %v\n", c.Name, err)
			return 4
		}
		ok := exitCode == 0 && versionMatch(out, expected)
		if !ok {
			failed++
		}
		line, err := json.Marshal(checkLine{
			Name:     c.Name,
			Argv:     c.Argv,
			Exit:     exitCode,
			Expected: expected,
			OK:       ok,
			Output:   firstOutputLine(out),
		})
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %s: %v\n", c.Name, err)
			return 4
		}
		fmt.Fprintln(stdout, string(line))
	}
	summary, err := json.Marshal(summaryLine{Summary: true, Checks: len(spec.Checks), Failed: failed})
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	fmt.Fprintln(stdout, string(summary))
	if failed > 0 {
		return 1
	}
	return 0
}

// stringList accumulates the values of a repeatable flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, " ") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// splitBuildArg splits one --build-arg NAME=VALUE; the value may itself
// contain "=".
func splitBuildArg(v string) (name, value string, ok bool) {
	i := strings.Index(v, "=")
	if i <= 0 {
		return "", "", false
	}
	if !argNamePattern.MatchString(v[:i]) {
		return "", "", false
	}
	return v[:i], v[i+1:], true
}

// argNamePattern is a valid ARG (and therefore build-arg) name.
var argNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// dockerArgValues collects the default value of every ARG NAME=value line
// of a Dockerfile. Matching quotes around the value are stripped. An ARG
// declared more than once with different default values is an error
// naming the ARG.
func dockerArgValues(b []byte) (map[string]string, error) {
	merged := map[string]string{}
	for _, line := range strings.Split(string(b), "\n") {
		m := argLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, value := m[1], unquoteArg(m[2])
		if prev, ok := merged[name]; ok && prev != value {
			return nil, fmt.Errorf("Dockerfile: ARG %s declared more than once with different default values", name)
		}
		merged[name] = value
	}
	return merged, nil
}

// unquoteArg strips one pair of matching double or single quotes around
// an ARG default value.
func unquoteArg(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// versionMatch reports whether expected occurs in out as a version token:
// the character before the match is neither a digit nor a dot, and the
// character after it is none of a letter, digit, dot, hyphen or plus, so
// 1.25.3 matches "go1.25.3 linux/arm64" and "v1.25.3" but not "1.25.30",
// "1.25.3.1", "1.25.3rc1" or "1.25.3-rc1", and "dev" does not match
// "development". An empty expected value matches.
func versionMatch(out []byte, expected string) bool {
	if expected == "" {
		return true
	}
	s := string(out)
	rest := s
	for {
		i := strings.Index(rest, expected)
		if i < 0 {
			return false
		}
		start := len(s) - len(rest) + i
		beforeOK := start == 0 || (!isDigit(s[start-1]) && s[start-1] != '.')
		after := start + len(expected)
		afterOK := after == len(s) || !continuesToken(s[after])
		if beforeOK && afterOK {
			return true
		}
		rest = rest[i+len(expected):]
	}
}

// continuesToken reports whether b would extend a version token: an
// ASCII letter or digit, a dot, a hyphen or a plus.
func continuesToken(b byte) bool {
	return isDigit(b) || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b == '.' || b == '-' || b == '+'
}

// isDigit reports whether b is an ASCII digit.
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// firstOutputLine is the first non-empty line of out, with a trailing
// carriage return dropped, truncated to at most 200 runes.
func firstOutputLine(out []byte) string {
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		r := []rune(line)
		if len(r) > 200 {
			r = r[:200]
		}
		return string(r)
	}
	return ""
}
