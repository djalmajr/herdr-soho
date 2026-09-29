//go:build windows

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTM11WindowsTreeKillCases(t *testing.T) {
	for _, tc := range []struct {
		title      string
		mode       string
		input      string
		merge      bool
		files      bool
		grandchild bool
	}{
		{title: `runCli win32 (simulated): mergeOutput mode — finishes before the timeout and times out`, mode: "mergeOutput", merge: true},
		{title: `runCli win32 (simulated): outputFiles mode — finishes before the timeout and times out`, mode: "outputFiles", files: true},
		{title: `runCli win32 (simulated): default mode with input — finishes before the timeout and times out`, mode: "default", input: "stdin-bytes\n"},
		{title: `runCli win32 (simulated): default mode — a grandchild holding the pipes cannot hold the call past the timeout`, mode: "parent-exits", grandchild: true},
	} {
		t.Run(`JS: "`+tc.title+`"`, func(t *testing.T) {
			dir, wrapper, env := tm11WindowsFake(t)
			if tc.grandchild {
				env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent-exits"
				// The helper records its PIDs here; the grandchild outlives the
				// call by design, so the cleanup stops it.
				pidFile := filepath.Join(dir, "grandchild-pids.txt")
				env["HERDR_SOHO_TREEKILL_PID_FILE"] = pidFile
				t.Cleanup(func() { cleanupRecordedProcesses(pidFile) })
				started := time.Now()
				got := RunCli(wrapper, nil, RunOptions{Env: env, Platform: "win32", Cwd: dir, TimeoutMs: 5000})
				if time.Since(started) >= 3*time.Second || got.Status == nil || *got.Status != 0 || got.TimedOut || got.Error != "" {
					t.Fatalf("grandchild-held pipes result=%#v elapsed=%s", got, time.Since(started))
				}
				return
			}
			env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "complete"
			ok := RunCli(wrapper, nil, RunOptions{Env: env, Platform: "win32", Cwd: dir, Input: tc.input, MergeOutput: tc.merge, OutputFiles: tc.files, TimeoutMs: 15000})
			if ok.Status == nil || *ok.Status != 0 || ok.TimedOut || ok.Error != "" || !strings.Contains(ok.Stdout, "tree-output") || !strings.Contains(ok.Stdout, tc.input) {
				t.Fatalf("completion result=%#v", ok)
			}
			env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent"
			pidFile := filepath.Join(dir, "pids.txt")
			env["HERDR_SOHO_TREEKILL_PID_FILE"] = pidFile
			t.Cleanup(func() { cleanupRecordedProcesses(pidFile) })
			got := RunCli(wrapper, nil, RunOptions{Env: env, Platform: "win32", Cwd: dir, Input: tc.input, MergeOutput: tc.merge, OutputFiles: tc.files, TimeoutMs: 1000})
			if got.Status != nil || got.Signal != "SIGTERM" || !got.TimedOut || got.Error != "ETIMEDOUT" || !strings.Contains(got.Stdout, "tree-output") {
				t.Fatalf("timeout result=%#v", got)
			}
			if tc.merge && got.Stderr != "" || tc.files && !strings.Contains(got.Stderr, "tree-error") || !tc.merge && !tc.files && !strings.Contains(got.Stderr, "tree-error") {
				t.Fatalf("streams stdout=%q stderr=%q", got.Stdout, got.Stderr)
			}
			assertRecordedProcessesGone(t, pidFile)
		})
	}
	t.Run(`JS: "runCli (Windows only): a .cmd that hangs past the timeout returns the timeout shape and no process of the tree survives"`, func(t *testing.T) {
		dir, wrapper, env := tm11WindowsFake(t)
		pidFile := filepath.Join(dir, "windows-only-pids.txt")
		env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent"
		env["HERDR_SOHO_TREEKILL_PID_FILE"] = pidFile
		t.Cleanup(func() { cleanupRecordedProcesses(pidFile) })
		got := RunCli(wrapper, []string{"a"}, RunOptions{Env: env, Platform: "win32", Cwd: dir, TimeoutMs: 1000})
		if got.Status != nil || got.Signal != "SIGTERM" || !got.TimedOut || got.Error != "ETIMEDOUT" || !strings.Contains(got.Stdout, "tree-output") {
			t.Fatalf("Windows .cmd timeout result=%#v", got)
		}
		assertRecordedProcessesGone(t, pidFile)
	})
}

func tm11WindowsFake(t *testing.T) (string, string, Env) {
	t.Helper()
	dir := t.TempDir()
	baseExe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(baseExe)
	if err != nil {
		t.Fatal(err)
	}
	fakeCli := filepath.Join(dir, "fakecli.exe")
	if err := os.WriteFile(fakeCli, data, 0o700); err != nil {
		t.Fatal(err)
	}
	warmTreeKillFake(t, fakeCli)
	wrapper := filepath.Join(dir, "wrapper.cmd")
	if err := os.WriteFile(wrapper, []byte("@echo off\r\n@\"%~dp0fakecli.exe\" -test.run=TestTreeKillFakeCliProcess\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := EnvFromOS()
	setTestPath(env, dir+";"+testPath(env))
	env["PATHEXT"] = ".EXE;.CMD;.BAT;.COM"
	env["TMPDIR"] = dir
	if env.Get("COMSPEC") == "" {
		env["COMSPEC"] = `C:\Windows\System32\cmd.exe`
	}
	return dir, wrapper, env
}
