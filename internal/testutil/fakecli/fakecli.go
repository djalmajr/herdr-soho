// Package fakecli provides re-executed Go test binaries as deterministic CLI
// fakes. Call Install from a test and Main first in package TestMain.
package fakecli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const configEnv = "HERDR_SOHO_FAKECLI_CONFIG"

type Rule struct {
	Argv        []string `json:"argv"`
	ArgvPrefix  bool     `json:"argv_prefix,omitempty"`
	AnyArgs     bool     `json:"any_args,omitempty"`
	Call        int      `json:"call,omitempty"`
	Stdout      string   `json:"stdout,omitempty"`
	StdoutBytes []byte   `json:"stdout_bytes,omitempty"`
	Stderr      string   `json:"stderr,omitempty"`
	StderrBytes []byte   `json:"stderr_bytes,omitempty"`
	Code        int      `json:"code,omitempty"`
	Delay       int      `json:"delay_ms,omitempty"`
	WaitFile    string   `json:"wait_file,omitempty"`
	Signal      string   `json:"signal,omitempty"`
}

type Call struct {
	Argv  []string          `json:"argv"`
	Stdin string            `json:"stdin,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
}

type script struct {
	Log          string   `json:"log"`
	Rules        []Rule   `json:"rules"`
	CaptureStdin bool     `json:"capture_stdin,omitempty"`
	CaptureEnv   []string `json:"capture_env,omitempty"`
}

type InstallOptions struct {
	CaptureStdin bool
	CaptureEnv   []string
}

type EnvOptions struct {
	IncludeBasePath bool
	SystemPath      string
}

var preparedBinary string
var prepareOnce sync.Once
var prepareErr error

// waitFileTimeoutEnv carries a shorter cap than the 60 s default into the
// re-executed fake, so tests do not wait for the timeout. The variable would
// not cross the re-execution, hence the environment.
const waitFileTimeoutEnv = "FAKECLI_WAIT_FILE_TIMEOUT_MS"

const waitFileDefaultTimeout = 60 * time.Second

// waitFileTimeout returns the WaitFile cap: the injected value in
// milliseconds when valid, else the 60 s default.
func waitFileTimeout() time.Duration {
	if raw := os.Getenv(waitFileTimeoutEnv); raw != "" {
		if ms, err := strconv.Atoi(raw); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return waitFileDefaultTimeout
}

// waitForFile polls every 20 ms until the file exists or the timeout passes.
func waitForFile(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("wait_file %s never appeared", path)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// RunTests handles fake CLI re-execution, runs the package tests, and cleans up
// a lazily prepared Windows binary after the package finishes.
func RunTests(m *testing.M) {
	Main()
	code := m.Run()
	if preparedBinary != "" {
		_ = os.RemoveAll(filepath.Dir(preparedBinary))
	}
	os.Exit(code)
}

func prepareBinary() error {
	prepareOnce.Do(func() {
		bin, err := os.Executable()
		if err != nil {
			prepareErr = err
			return
		}
		dir, err := os.MkdirTemp("", "herdr-fakecli-")
		if err != nil {
			prepareErr = err
			return
		}
		target := filepath.Join(dir, "fakecli.exe")
		data, err := os.ReadFile(bin)
		if err != nil {
			_ = os.RemoveAll(dir)
			prepareErr = err
			return
		}
		if err = os.WriteFile(target, data, 0o700); err != nil {
			_ = os.RemoveAll(dir)
			prepareErr = err
			return
		}
		preparedBinary = target
	})
	return prepareErr
}

// Install writes a JSON script and places a re-executable entry for the test
// binary in dir. Tests should prepend dir to PATH. On Windows the entry is a
// hard link to the package's prepared executable; on Unix it is a symlink
// where supported. Windows preparation happens lazily when the package uses
// Install, so each test package does not need its own setup code.
func Install(t testing.TB, dir, name string, rules []Rule) (string, error) {
	return InstallWithOptions(t, dir, name, rules, InstallOptions{})
}

func InstallWithOptions(t testing.TB, dir, name string, rules []Rule, options InstallOptions) (string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if err := prepareBinary(); err != nil {
			return "", fmt.Errorf("fakecli: prepare test binary: %w", err)
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	log := filepath.Join(dir, name+".calls.jsonl")
	config := filepath.Join(dir, name+".json")
	b, err := json.Marshal(script{Log: log, Rules: rules, CaptureStdin: options.CaptureStdin, CaptureEnv: options.CaptureEnv})
	if err != nil {
		return "", err
	}
	if err = os.WriteFile(config, b, 0o600); err != nil {
		return "", err
	}
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	target := filepath.Join(dir, name+ext)
	if _, statErr := os.Lstat(target); statErr == nil {
		return "", fmt.Errorf("fakecli: %s already exists", target)
	}
	if runtime.GOOS == "windows" {
		err = os.Link(preparedBinary, target)
	} else {
		err = os.Symlink(bin, target)
		if err != nil {
			data, readErr := os.ReadFile(bin)
			if readErr != nil {
				return "", readErr
			}
			err = os.WriteFile(target, data, 0o755)
		}
	}
	if err != nil {
		return "", err
	}
	return target, nil
}

// Env returns a copy of base with the fake enabled and its directory first on PATH.
func Env(base []string, dir string, options ...EnvOptions) []string {
	option := EnvOptions{}
	if len(options) > 0 {
		option = options[0]
	}
	out := make([]string, 0, len(base)+3)
	inherited := sameEnvironment(base, os.Environ())
	for _, v := range base {
		key, _, ok := strings.Cut(v, "=")
		if ok {
			upperKey := strings.ToUpper(key)
			if upperKey == "PATH" || strings.EqualFold(key, configEnv) || upperKey == "HERDR_SOCKET_PATH" || (inherited && strings.HasPrefix(upperKey, "HERDR_")) {
				continue
			}
		}
		if !ok || key != "" {
			out = append(out, v)
		}
	}
	path := dir
	if option.IncludeBasePath {
		path += string(os.PathListSeparator) + pathValue(base)
	}
	if option.SystemPath != "" {
		path += string(os.PathListSeparator) + option.SystemPath
	}
	out = append(out, "PATH="+path, configEnv+"="+dir, "HERDR_SOCKET_PATH="+filepath.Join(dir, "herdr.sock"))
	return out
}

func sameEnvironment(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func pathValue(env []string) string {
	for _, v := range env {
		if strings.HasPrefix(strings.ToUpper(v), "PATH=") {
			return v[len("PATH="):]
		}
	}
	return ""
}

// Main assumes the fake CLI role when Install's config environment variable is set.
// Call at the start of TestMain; it exits the re-executed test process after dispatch.
func Main() {
	configPath := os.Getenv(configEnv)
	if configPath == "" {
		return
	}
	if info, statErr := os.Stat(configPath); statErr == nil && info.IsDir() {
		name := configName(filepath.Base(os.Args[0]))
		configPath = filepath.Join(configPath, name+".json")
	}
	b, err := os.ReadFile(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(126)
	}
	var cfg script
	if err = json.Unmarshal(b, &cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(126)
	}
	argv := append([]string(nil), os.Args[1:]...)
	call := Call{Argv: argv}
	if cfg.CaptureStdin {
		stdin, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			fmt.Fprintln(os.Stderr, "fakecli: read stdin:", readErr)
			os.Exit(126)
		}
		call.Stdin = string(stdin)
	}
	if len(cfg.CaptureEnv) > 0 {
		call.Env = make(map[string]string, len(cfg.CaptureEnv))
		for _, key := range cfg.CaptureEnv {
			if value, ok := os.LookupEnv(key); ok {
				call.Env[key] = value
			}
		}
	}
	unlock, err := lock(cfg.Log + ".lock")
	if err != nil {
		fmt.Fprintln(os.Stderr, "fakecli: lock call log:", err)
		os.Exit(126)
	}
	line, _ := json.Marshal(call)
	f, err := os.OpenFile(cfg.Log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(append(line, '\n'))
		closeErr := f.Close()
		if err == nil {
			err = closeErr
		}
	}
	if err != nil {
		unlock()
		fmt.Fprintln(os.Stderr, err)
		os.Exit(126)
	}
	callNumber := 0
	if calls, readErr := ReadCalls(cfg.Log); readErr == nil {
		for _, c := range calls {
			if same(argv, c.Argv) {
				callNumber++
			}
		}
	}
	unlock()
	for _, rule := range cfg.Rules {
		if !matches(argv, rule) || (rule.Call != 0 && rule.Call != callNumber) {
			continue
		}
		if rule.WaitFile != "" {
			if err := waitForFile(rule.WaitFile, waitFileTimeout()); err != nil {
				fmt.Fprintf(os.Stderr, "fakecli: %v\n", err)
				os.Exit(124)
			}
		}
		if rule.Delay > 0 {
			time.Sleep(time.Duration(rule.Delay) * time.Millisecond)
		}
		_, _ = os.Stdout.Write(append([]byte(rule.Stdout), rule.StdoutBytes...))
		_, _ = os.Stderr.Write(append([]byte(rule.Stderr), rule.StderrBytes...))
		if rule.Signal != "" {
			terminate(rule.Signal, rule.Code)
		}
		os.Exit(rule.Code)
	}
	fmt.Fprintf(os.Stderr, "fakecli: no rule for %q\n", argv)
	os.Exit(127)
}

func configName(name string) string {
	if strings.EqualFold(filepath.Ext(name), ".exe") {
		return name[:len(name)-len(filepath.Ext(name))]
	}
	return name
}

func matches(argv []string, rule Rule) bool {
	if rule.AnyArgs {
		return true
	}
	if rule.ArgvPrefix {
		return len(argv) >= len(rule.Argv) && same(argv[:len(rule.Argv)], rule.Argv)
	}
	return same(argv, rule.Argv)
}

// ReadCalls decodes the ordered JSONL call log.
func ReadCalls(file string) ([]Call, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var calls []Call
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var c Call
		if err = json.Unmarshal([]byte(line), &c); err != nil {
			return nil, err
		}
		calls = append(calls, c)
	}
	return calls, nil
}

// ReadCallsForConfig locates and reads the log created from Install's config file.
func ReadCallsForConfig(configFile string) ([]Call, error) {
	b, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var cfg script
	if err = json.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	return ReadCalls(cfg.Log)
}

func same(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
