package fakecli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func waitFileEnvFor(t *testing.T, dir string) platform.Env {
	t.Helper()
	env := platform.Env{}
	for _, entry := range Env(os.Environ(), dir) {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			env[key] = value
		}
	}
	return env
}

// Mutation captured: answering before the wait file exists, or not waiting at
// all, breaks the gated-fake contract the picker's completion-order test uses.
func TestWaitFileRuleWaitsForTheFileThenAnswers(t *testing.T) {
	dir := t.TempDir()
	release := filepath.Join(dir, "gate")
	if _, err := Install(t, dir, "probe", []Rule{{Argv: []string{"gated"}, WaitFile: release, Stdout: "released\n"}}); err != nil {
		t.Fatal(err)
	}
	env := waitFileEnvFor(t, dir)
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = os.WriteFile(release, []byte("go"), 0o644)
	}()
	start := time.Now()
	r := platform.RunCli("probe", []string{"gated"}, platform.RunOptions{Env: env, Platform: platform.Current(), TimeoutMs: 5000})
	elapsed := time.Since(start)
	if r.Status == nil || *r.Status != 0 || r.Stdout != "released\n" {
		t.Fatalf("result after the file appeared: %+v", r)
	}
	// The answer can only come after the file exists (150 ms): a shorter
	// elapsed time means the rule answered without waiting.
	if elapsed < 150*time.Millisecond {
		t.Fatalf("answered after %s, before the file existed (released at 150 ms)", elapsed)
	}
}

// Mutation captured: exiting 0 (or silently) when the file never appears, or
// keeping the 60 s default inside the fake, breaks the timeout contract.
func TestWaitFileRuleExits124WhenTheFileNeverAppears(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "never")
	if _, err := Install(t, dir, "probe", []Rule{{Argv: []string{"gated"}, WaitFile: missing, Stdout: "never\n"}}); err != nil {
		t.Fatal(err)
	}
	env := waitFileEnvFor(t, dir)
	// The fake re-executes the test binary, so the deadline is injected via
	// the child environment, not a package variable.
	env[waitFileTimeoutEnv] = "300"
	start := time.Now()
	r := platform.RunCli("probe", []string{"gated"}, platform.RunOptions{Env: env, Platform: platform.Current(), TimeoutMs: 5000})
	elapsed := time.Since(start)
	// 124 can only come from the injected 300 ms deadline: with the 60 s
	// default the RunCli timeout (5000 ms) would kill the fake first.
	if r.Status == nil || *r.Status != 124 {
		t.Fatalf("status: %+v", r)
	}
	if !strings.Contains(r.Stderr, "fakecli: wait_file "+missing+" never appeared") {
		t.Fatalf("stderr: %q", r.Stderr)
	}
	if r.Stdout != "" {
		t.Fatalf("stdout before the timeout must be empty: %q", r.Stdout)
	}
	// It waited (it did not skip the wait), at the injected scale.
	if elapsed < 250*time.Millisecond {
		t.Fatalf("gave up after %s, before the 300 ms deadline", elapsed)
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Fatalf("the missing file must not be created: %v", err)
	}
}

// Mutation captured: a malformed or non-positive injected deadline must fall
// back to the 60 s default instead of being treated as zero (skip the wait).
func TestWaitFileTimeoutInjectionFallsBackOnBadValues(t *testing.T) {
	for _, bad := range []string{"", "abc", "0", "-5"} {
		t.Run("value="+bad, func(t *testing.T) {
			if got := waitFileTimeoutFor(bad); got != waitFileDefaultTimeout {
				t.Fatalf("timeout for %q = %s, want the default %s", bad, got, waitFileDefaultTimeout)
			}
		})
	}
	if got := waitFileTimeoutFor("250"); got != 250*time.Millisecond {
		t.Fatalf("timeout for 250 = %s, want 250 ms", got)
	}
}

func waitFileTimeoutFor(raw string) time.Duration {
	old := os.Getenv(waitFileTimeoutEnv)
	_ = os.Setenv(waitFileTimeoutEnv, raw)
	defer func() {
		if old == "" {
			_ = os.Unsetenv(waitFileTimeoutEnv)
		} else {
			_ = os.Setenv(waitFileTimeoutEnv, old)
		}
	}()
	return waitFileTimeout()
}
