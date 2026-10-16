package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// job_private_launch_test.go: the CLI side of the job start's private
// launch context (internal/job/locator.go, internal/job/managed.go).

const (
	testLaunchBaseVar = "_SOHO_LAUNCH_CONFIG_BASE"
	testLaunchHomeVar = "_SOHO_LAUNCH_HOME"
	managedCLIVar     = "CLI_MANAGED_HELPER"
	managedCLIArgsVar = "CLI_MANAGED_ARGS"
)

// managedCLIHelperMain runs this test binary as a parent started as
// `<binary> job supervise --id job-1` that runs a child, and as that child,
// which runs one command through Run and prints its exit code.
func managedCLIHelperMain() {
	switch os.Getenv(managedCLIVar) {
	case "parent":
		self, err := os.Executable()
		if err != nil {
			os.Exit(90)
		}
		cmd := exec.Command(self)
		cmd.Env = append(os.Environ(), managedCLIVar+"=child")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Printf("child: %v\n", err)
		}
		os.Exit(0)
	case "child":
		env := platform.Env{}
		for _, kv := range os.Environ() {
			if k, v, ok := strings.Cut(kv, "="); ok && k != "" {
				env[k] = v
			}
		}
		var out, errOut bytes.Buffer
		platform.Stdout, platform.Stderr = &out, &errOut
		code := Run(strings.Fields(env.Get(managedCLIArgsVar)), env)
		fmt.Printf("code=%d\nstdout=%q\nstderr=%q\n", code, out.String(), errOut.String())
		os.Exit(0)
	}
}

func withEnv(env platform.Env, kv ...string) platform.Env {
	out := env.Clone()
	for i := 0; i+1 < len(kv); i += 2 {
		out[kv[i]] = kv[i+1]
	}
	return out
}

// TestJobSupervisePrivateLaunchContext: absent, the supervisor is located
// as before; present but unusable, it stops with exit 2 before any lookup,
// even for a job the environment's own configuration would find; usable,
// only the selection's stores are searched (no fallback to the
// environment's), and without its launch binding it does not run.
func TestJobSupervisePrivateLaunchContext(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	jobDir := filepath.Join(co, ".soho", "jobs", "job-1")
	body := fmt.Sprintf(`{"schema":1,"id":"job-1","status":"running","brief_sha256":"x","decisions_acked_seq":0,"motivo":null,"dir":%q}`+"\n", co)
	if err := os.WriteFile(filepath.Join(jobDir, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	herdr := withEnv(f.env, "HERDR_ENV", "1", "HERDR_WORKSPACE_ID", "w9", "HERDR_PANE_ID", "w9:p1")
	base := filepath.Clean(filepath.Join(f.root, "xdg"))

	t.Run("unusable values stop before any lookup", func(t *testing.T) {
		for _, bad := range []platform.Env{
			withEnv(herdr, testLaunchBaseVar, "relative/base"),
			withEnv(herdr, testLaunchBaseVar, ""),
			withEnv(herdr, testLaunchBaseVar, base+string(filepath.Separator)+".."+string(filepath.Separator)+"xdg"),
			withEnv(herdr, testLaunchHomeVar, f.home),
			withEnv(herdr, testLaunchBaseVar, base, testLaunchHomeVar, "relative/home"),
		} {
			code, out, errOut := f.runEnv(t, bad, "supervise", "--id", "job-1")
			if code != 2 || out != "" || errOut != "soho: job supervise: the private launch context is unusable\n" {
				t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
			}
		}
		if _, err := os.Lstat(filepath.Join(jobDir, "friction.log")); !os.IsNotExist(err) {
			t.Fatalf("a refused supervisor wrote the friction log: %v", err)
		}
	})
	t.Run("a usable selection without the job is not_found, never another store", func(t *testing.T) {
		other := filepath.Join(f.root, "other-base")
		if err := os.MkdirAll(filepath.Join(other, "soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		empty := filepath.Join(f.root, "empty-repos")
		if err := os.MkdirAll(empty, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(other, "soho", "config"), []byte("job_repos_root="+empty+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		code, out, errOut := f.runEnv(t, withEnv(herdr, testLaunchBaseVar, other), "supervise", "--id", "job-1")
		if code != 3 || out != `{"status":"not_found"}`+"\n" || errOut != "" {
			t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
		}
	})
	t.Run("a usable selection without a launch binding does not run", func(t *testing.T) {
		code, out, errOut := f.runEnv(t, withEnv(herdr, testLaunchBaseVar, base), "supervise", "--id", "job-1")
		if code != 4 || !strings.Contains(out, `"status":"running"`) || errOut != "soho: warning: job: the supervisor is not bound to its launch: the job has no launch record\n" {
			t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
		}
		friction, err := os.ReadFile(filepath.Join(jobDir, "friction.log"))
		if err != nil || !strings.Contains(string(friction), "job: the supervisor is not bound to its launch: the job has no launch record") {
			t.Fatalf("friction %q (%v)", friction, err)
		}
		if _, err := os.Lstat(filepath.Join(jobDir, "launch.json")); !os.IsNotExist(err) {
			t.Fatalf("an unbound supervisor wrote a launch record: %v", err)
		}
	})
	t.Run("a launcher that is another file is refused before binding", func(t *testing.T) {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		other := filepath.Join(t.TempDir(), filepath.Base(self))
		if err := os.WriteFile(other, data, 0o755); err != nil {
			t.Fatal(err)
		}
		code, _, errOut := f.runEnv(t, withEnv(herdr, testLaunchBaseVar, base, "SOHO_BIN", other), "supervise", "--id", "job-1")
		if code != 4 || errOut != "soho: warning: job: the supervisor is not bound to its launch: the team command executable is not this supervisor's\n" {
			t.Fatalf("code=%d err=%q", code, errOut)
		}
		friction, err := os.ReadFile(filepath.Join(jobDir, "friction.log"))
		if err != nil || !strings.Contains(string(friction), "job: the supervisor is not bound to its launch: the team command executable is not this supervisor's") {
			t.Fatalf("friction %q (%v)", friction, err)
		}
	})
}

// TestManagedCommandInheritedNamesIgnored: a command whose parent is not a
// job supervisor (this test process) runs exactly as without the private
// names, whatever they hold.
func TestManagedCommandInheritedNamesIgnored(t *testing.T) {
	env, repo := commandFixture(t)
	other := filepath.Join(t.TempDir(), "other-base")
	if err := os.MkdirAll(filepath.Join(other, "soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "soho", "config"), []byte("max_workers=9\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runIn := func(env platform.Env, args ...string) (int, string, string) {
		t.Helper()
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(repo); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(old) }()
		var out, errOut bytes.Buffer
		oldOut, oldErr := platform.Stdout, platform.Stderr
		platform.Stdout, platform.Stderr = &out, &errOut
		defer func() { platform.Stdout, platform.Stderr = oldOut, oldErr }()
		code := Run(args, env)
		return code, out.String(), errOut.String()
	}
	baseCode, baseOut, baseErr := runIn(env, "config")
	for _, names := range []platform.Env{
		withEnv(env, testLaunchBaseVar, other),
		withEnv(env, testLaunchBaseVar, other, testLaunchHomeVar, other),
		withEnv(env, testLaunchBaseVar, "relative"),
		withEnv(env, testLaunchHomeVar, other),
	} {
		code, out, errOut := runIn(names, "config")
		if code != baseCode || out != baseOut || errOut != baseErr {
			t.Fatalf("with inherited names: code=%d out=%q err=%q; without: code=%d out=%q err=%q", code, out, errOut, baseCode, baseOut, baseErr)
		}
	}
	if strings.Contains(baseOut, other) {
		t.Fatalf("the baseline names the inherited base: %q", baseOut)
	}
}

// TestManagedCommandRefusedBeforeSideEffects: a managed command whose
// parent is a job supervisor of this executable, without a binding that
// holds, stops with exit 2 before it reads or writes anything of its own:
// the state directory it would create is never created.
func TestManagedCommandRefusedBeforeSideEffects(t *testing.T) {
	if _, _, err := platform.ProcExec(os.Getpid()); err != nil {
		t.Fatalf("this platform cannot read a process's executable and arguments (%v): the managed refusal cannot be exercised here", err)
	}
	env, repo := commandFixture(t)
	state := filepath.Join(t.TempDir(), "never-created")
	base := filepath.Join(t.TempDir(), "base")
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		args  string
		names []string
	}{
		{"status", []string{testLaunchBaseVar, base}},
		{"spawn build", []string{testLaunchBaseVar, base}},
		{"session set lane.job.panes 1", []string{testLaunchBaseVar, base}},
		{"config", []string{testLaunchBaseVar, "relative"}},
		{"roster", []string{testLaunchHomeVar, base}},
	} {
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		run := withEnv(env, "HERDR_ENV", "1", "HERDR_PANE_ID", "ws:p1", "SOHO_DIR", state, managedCLIVar, "parent", managedCLIArgsVar, c.args)
		for i := 0; i+1 < len(c.names); i += 2 {
			run[c.names[i]] = c.names[i+1]
		}
		cmd := exec.Command(self, "job", "supervise", "--id", "job-1")
		cmd.Dir = repo
		cmd.Env = run.List()
		done := make(chan struct{})
		var out []byte
		go func() { out, _ = cmd.CombinedOutput(); close(done) }()
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			_ = cmd.Process.Kill()
			t.Fatal("the managed parent did not finish within 60s")
		}
		command := strings.Fields(c.args)[0]
		want := fmt.Sprintf("code=2\nstdout=\"\"\nstderr=%q\n", "soho: "+command+": the job supervisor's launch binding does not hold\n")
		if string(out) != want {
			t.Fatalf("%s: output %q, want %q", c.args, out, want)
		}
		if _, err := os.Lstat(state); !os.IsNotExist(err) {
			t.Fatalf("%s: the state directory was created: %v", c.args, err)
		}
	}
}
