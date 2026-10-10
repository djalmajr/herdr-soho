//go:build windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestRunCliWindowsTreeKill(t *testing.T) {
	dir := t.TempDir()
	helper := treeKillHelperExe(t)
	treeKillFixtureWrapper(t, dir, helper)
	pidFile := filepath.Join(dir, "pids.txt")
	env := EnvFromOS()
	setTestPath(env, dir+";"+testPath(env))
	env["PATHEXT"] = ".EXE;.CMD;.BAT;.COM"
	env["TMPDIR"] = dir
	env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent"
	env["HERDR_SOHO_TREEKILL_PID_FILE"] = pidFile
	if env.Get("COMSPEC") == "" {
		env["COMSPEC"] = `C:\Windows\System32\cmd.exe`
	}
	treeKillChainReady(t, env, dir)
	started := time.Now()
	result := RunCli("wrapper", nil, RunOptions{Platform: "win32", Env: env, TimeoutMs: 1000})
	elapsed := time.Since(started)
	var pids []uint32
	if data, readErr := os.ReadFile(pidFile); readErr == nil {
		for _, item := range strings.Fields(string(data)) {
			pid, parseErr := strconv.ParseUint(item, 10, 32)
			if parseErr != nil {
				t.Fatalf("invalid child pid %q: %v", item, parseErr)
			}
			pids = append(pids, uint32(pid))
		}
	}
	defer func() {
		for _, pid := range pids {
			terminateOwnedProcess(pid)
		}
	}()
	if elapsed >= 10*time.Second {
		t.Fatalf("timed out tree run took %v, want under 10 seconds", elapsed)
	}
	if result.NotFound || result.Status != nil || result.Signal != "SIGTERM" || !result.TimedOut || result.Error != "ETIMEDOUT" {
		t.Fatalf("timeout result = %#v", result)
	}
	if !strings.Contains(result.Stdout, "tree-output") {
		t.Fatalf("captured output = %q; stderr=%q status=%s %s; child env=%s", result.Stdout, result.Stderr, treeKillStatus(result), treeKillStarted(pidFile), treeKillChildEnv(env))
	}
	if len(pids) != 2 {
		t.Fatalf("recorded process tree pids = %v, want parent and child", pids)
	}
	assertRecordedProcessesGone(t, pidFile)

	for _, tc := range []struct {
		name    string
		options RunOptions
		wantOut string
		wantErr string
	}{
		{"merged streams", RunOptions{MergeOutput: true, TimeoutMs: 10000}, "tree-output", ""},
		{"separate streams", RunOptions{OutputFiles: true, TimeoutMs: 10000}, "tree-output", "tree-error"},
		{"stdin", RunOptions{Input: "stdin-payload", TimeoutMs: 10000}, "stdin-payload", "tree-error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runEnv := env.Clone()
			runEnv["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "complete"
			got := RunCli("wrapper", nil, RunOptions{Env: runEnv, Platform: "win32", Cwd: dir, Input: tc.options.Input, MergeOutput: tc.options.MergeOutput, OutputFiles: tc.options.OutputFiles, TimeoutMs: tc.options.TimeoutMs})
			if got.Status == nil || *got.Status != 0 || got.Signal != "" || got.TimedOut || got.Error != "" {
				t.Fatalf("completion result = %#v", got)
			}
			if !strings.Contains(got.Stdout, tc.wantOut) || !strings.Contains(got.Stderr, tc.wantErr) {
				t.Fatalf("streams = stdout %q, stderr %q", got.Stdout, got.Stderr)
			}
		})
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".herdr-soho-out-") {
			t.Errorf("temporary output file remained: %s", entry.Name())
		}
	}
	missingTempEnv := env.Clone()
	missingTemp := filepath.Join(dir, "missing-temp")
	missingTempEnv["TMPDIR"] = missingTemp
	created := RunCli("wrapper", nil, RunOptions{Env: missingTempEnv, Platform: "win32", MergeOutput: true, TimeoutMs: 1000})
	if created.Error == "ENOENT" || strings.Contains(created.Stderr, "cannot write temporary files") {
		t.Fatalf("a missing TMPDIR is created, as mkdirSync recursive does in the JS: %#v", created)
	}
	if info, statErr := os.Stat(missingTemp); statErr != nil || !info.IsDir() {
		t.Fatalf("missing TMPDIR was not created: %v", statErr)
	}
	t.Run("tree-kill uses the created temp directory and reports a file at TMPDIR", func(t *testing.T) { // Mutation captured: creating output temp files directly in TMPDIR returns a silent ENOTDIR instead of the shared temp-file error.
		fileTemp := filepath.Join(dir, "tmp-is-a-file")
		if err := os.WriteFile(fileTemp, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		badTempEnv := env.Clone()
		badTempEnv["TMPDIR"] = fileTemp
		got := RunCli("wrapper", nil, RunOptions{Env: badTempEnv, Platform: "win32", MergeOutput: true, TimeoutMs: 1000})
		if got.Status != nil || got.Error != "EEXIST" || got.Stdout != "" || got.Stderr != "herdr-soho: cannot write temporary files: EEXIST\n" {
			t.Fatalf("file TMPDIR result = %#v", got)
		}
	})

	for _, tc := range []struct {
		name    string
		options RunOptions
		wantOut string
		wantErr string
	}{
		{"default streams", RunOptions{TimeoutMs: 1000}, "tree-output", "tree-error"},
		{"merged streams", RunOptions{MergeOutput: true, TimeoutMs: 1000}, "tree-output", ""},
		{"separate streams", RunOptions{OutputFiles: true, TimeoutMs: 1000}, "tree-output", "tree-error"},
		{"stdin", RunOptions{Input: "stdin-payload", TimeoutMs: 1000}, "stdin-payload", "tree-error"},
	} {
		t.Run("timeout "+tc.name, func(t *testing.T) {
			runEnv := env.Clone()
			runEnv["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent"
			runEnv["HERDR_SOHO_TREEKILL_PID_FILE"] = filepath.Join(dir, "pids-"+strings.ReplaceAll(tc.name, " ", "-")+".txt")
			t.Cleanup(func() { cleanupRecordedProcesses(runEnv.Get("HERDR_SOHO_TREEKILL_PID_FILE")) })
			got := RunCli("wrapper", nil, RunOptions{Env: runEnv, Platform: "win32", Cwd: dir, Input: tc.options.Input, MergeOutput: tc.options.MergeOutput, OutputFiles: tc.options.OutputFiles, TimeoutMs: tc.options.TimeoutMs})
			if got.Status != nil || got.Signal != "SIGTERM" || !got.TimedOut || got.Error != "ETIMEDOUT" {
				t.Fatalf("timeout result = %#v", got)
			}
			if !strings.Contains(got.Stdout, tc.wantOut) || (tc.wantErr != "" && !strings.Contains(got.Stderr, tc.wantErr)) || (tc.wantErr == "" && got.Stderr != "") {
				t.Fatalf("streams = stdout %q, stderr %q", got.Stdout, got.Stderr)
			}
			assertRecordedProcessesGone(t, runEnv.Get("HERDR_SOHO_TREEKILL_PID_FILE"))
		})
	}

	t.Run("root exit closes the job and releases descendant-held pipes", func(t *testing.T) {
		runEnv := env.Clone()
		pidPath := filepath.Join(dir, "pids-natural.txt")
		runEnv["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent-exits"
		runEnv["HERDR_SOHO_TREEKILL_PID_FILE"] = pidPath
		t.Cleanup(func() { cleanupRecordedProcesses(pidPath) })
		started := time.Now()
		got := RunCli("wrapper", nil, RunOptions{Env: runEnv, Platform: "win32", Cwd: dir, TimeoutMs: 5000})
		if elapsed := time.Since(started); elapsed >= 3*time.Second {
			t.Fatalf("descendant-held pipes delayed return: %v", elapsed)
		}
		if got.Status == nil || *got.Status != 0 || got.Signal != "" || got.TimedOut || got.Error != "" {
			t.Fatalf("natural completion = %#v", got)
		}
		assertRecordedProcessesGone(t, pidPath)
	})
}

func TestRunCliWindowsTreeKillDeduplicatesEnvironmentCase(t *testing.T) {
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "environment.cmd")
	if err := os.WriteFile(wrapper, []byte("@echo off\r\necho %PATH%\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := EnvFromOS()
	pathValue := dir + ";path-marker-upper"
	titlePathValue := dir + ";path-marker-title"
	env["PATH"] = pathValue
	env["Path"] = titlePathValue
	if env.Get("COMSPEC") == "" {
		env["COMSPEC"] = `C:\Windows\System32\cmd.exe`
	}
	result := RunCli(wrapper, nil, RunOptions{Platform: "win32", Env: env, TimeoutMs: 15000})
	if result.Status == nil || *result.Status != 0 || result.TimedOut || result.Error != "" || result.Stdout != titlePathValue+"\r\n" {
		t.Fatalf("timeout .cmd with Path/PATH environment = %#v", result)
	}
	if strings.Contains(result.Stdout, "path-marker-upper") || strings.Count(result.Stdout, "path-marker-title") != 1 {
		t.Fatalf("PATH did not contain exactly the value selected for Path: %q", result.Stdout)
	}
}

func TestWindowsResumeThreadFailureUsesDWORDWidth(t *testing.T) { // Mutation captured: comparing ResumeThread's DWORD failure to ^uintptr(0) misses it on 64-bit Windows.
	if !windowsResumeThreadFailed(uintptr(^uint32(0))) {
		t.Fatal("ResumeThread DWORD -1 was not recognized as failure")
	}
	if windowsResumeThreadFailed(1) {
		t.Fatal("ResumeThread previous suspend count 1 was treated as failure")
	}
}

func TestWindowsEnvironmentBlockPreservesWTF8AndRejectsNUL(t *testing.T) { // Mutation captured: converting WTF-8 through []rune replaces an unpaired UTF-16 surrogate.
	wtf8 := string([]byte{'V', '=', 0xed, 0xa0, 0x80})
	block, err := windowsEnvironmentBlock([]string{wtf8})
	if err != nil {
		t.Fatal(err)
	}
	want := []uint16{'V', '=', 0xd800, 0, 0}
	if len(block) != len(want) {
		t.Fatalf("WTF-8 block length = %d, want %d: %v", len(block), len(want), block)
	}
	for i := range want {
		if block[i] != want[i] {
			t.Fatalf("WTF-8 block = %v, want %v", block, want)
		}
	}
	if _, err := windowsEnvironmentBlock([]string{"V=one\x00two"}); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("NUL entry error = %v, want EINVAL", err)
	}
}

func TestWindowsEnvironmentBlockEmptyHasTwoNULs(t *testing.T) { // Mutation captured: returning a one-unit empty block omits CreateProcessW's second NUL.
	block, err := windowsEnvironmentBlock(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(block) != 2 || block[0] != 0 || block[1] != 0 {
		t.Fatalf("empty environment block = %v, want [0 0]", block)
	}
}

func TestJobObjectLimitInformationLayout(t *testing.T) {
	basicSize := unsafe.Sizeof(jobObjectBasicLimitInformation{})
	extendedSize := unsafe.Sizeof(jobObjectExtendedLimitInformation{})
	wantBasic, wantExtended := uintptr(64), uintptr(144)
	if runtime.GOARCH == "386" || runtime.GOARCH == "arm" {
		wantBasic, wantExtended = 48, 112
	}
	t.Logf("GOARCH=%s basicSize=%d extendedSize=%d", runtime.GOARCH, basicSize, extendedSize)
	if basicSize != wantBasic || extendedSize != wantExtended {
		t.Fatalf("job object structures = basic %d extended %d, want basic %d extended %d", basicSize, extendedSize, wantBasic, wantExtended)
	}
}

func TestRunCliWindowsTaskkillFallbackKillsGrandchild(t *testing.T) { // Mutation captured: bypassing taskkill leaves the recorded grandchild active after timeout.
	dir := t.TempDir()
	helper := treeKillHelperExe(t)
	treeKillFixtureWrapper(t, dir, helper)
	pidFile := filepath.Join(dir, "fallback-pids.txt")
	env := EnvFromOS()
	setTestPath(env, dir+";"+testPath(env))
	env["PATHEXT"] = ".EXE;.CMD;.BAT;.COM"
	env["TMPDIR"] = dir
	env["HERDR_SOHO_TREEKILL_FAKE_MODE"] = "parent"
	env["HERDR_SOHO_TREEKILL_PID_FILE"] = pidFile
	env[treeKillForceFallbackEnv] = "1"
	if env.Get("COMSPEC") == "" {
		env["COMSPEC"] = `C:\Windows\System32\cmd.exe`
	}
	t.Cleanup(func() { cleanupRecordedProcesses(pidFile) })
	treeKillChainReady(t, env, dir)
	result := RunCli("wrapper", nil, RunOptions{Platform: "win32", Env: env, Cwd: dir, TimeoutMs: 1000})
	if !result.TimedOut || result.Status != nil || result.Signal != "SIGTERM" || result.Error != "ETIMEDOUT" {
		t.Fatalf("fallback timeout result = %#v; stderr=%q status=%s child env=%s", result, result.Stderr, treeKillStatus(result), treeKillChildEnv(env))
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatalf("fallback pid file: %v; stderr=%q status=%s %s; child env=%s", err, result.Stderr, treeKillStatus(result), treeKillStarted(pidFile), treeKillChildEnv(env))
	}
	assertRecordedProcessesGone(t, pidFile)
}

// treeKillChildEnv renders the few environment values that diagnose a
// failed tree-kill fixture without dumping the whole process environment:
// the helper mode, its pid file, PATHEXT, and whether the fixture dir is
// first on PATH.
func treeKillChildEnv(env Env) string {
	pidFile := env.Get("HERDR_SOHO_TREEKILL_PID_FILE")
	pathFirst := false
	if dir := filepath.Dir(pidFile); dir != "" && dir != "." {
		pathFirst = strings.HasPrefix(env.Get("PATH"), dir+";")
	}
	return fmt.Sprintf("mode=%s pidFile=%s PATHEXT=%s pathFirst=%v",
		env.Get("HERDR_SOHO_TREEKILL_FAKE_MODE"), filepath.Base(pidFile), env.Get("PATHEXT"), pathFirst)
}

func treeKillStatus(result RunResult) string {
	if result.Status == nil {
		return "<nil>"
	}
	return strconv.Itoa(*result.Status)
}

func systemDirectory(env Env) string {
	windowsDir := env.Get("WINDIR")
	if windowsDir == "" {
		windowsDir = `C:\Windows`
	}
	return filepath.Join(windowsDir, "System32")
}

func assertRecordedProcessesGone(t *testing.T, pidFile string) {
	t.Helper()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatalf("read recorded pids: %v", err)
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		t.Fatalf("recorded pids = %q", data)
	}
	for _, field := range fields {
		pid, parseErr := strconv.ParseUint(field, 10, 32)
		if parseErr != nil {
			t.Fatalf("invalid recorded pid %q: %v", field, parseErr)
		}
		deadline := time.Now().Add(2 * time.Second)
		for {
			active, checkErr := processIsActive(uint32(pid))
			if checkErr != nil {
				t.Errorf("pid %s check error=%v", field, checkErr)
				break
			}
			if !active {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("pid %s is still active", field)
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	}
}

func cleanupRecordedProcesses(pidFile string) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return
	}
	for _, field := range strings.Fields(string(data)) {
		pid, parseErr := strconv.ParseUint(field, 10, 32)
		if parseErr == nil {
			terminateOwnedProcess(uint32(pid))
		}
	}
}

func processIsActive(pid uint32) (bool, error) {
	process, err := syscall.OpenProcess(0x1000, false, pid)
	if err != nil {
		if errors.Is(err, syscall.Errno(87)) {
			return false, nil
		}
		return false, err
	}
	defer syscall.CloseHandle(process)
	var code uint32
	if err := syscall.GetExitCodeProcess(process, &code); err != nil {
		return false, err
	}
	return code == 259, nil
}

func terminateOwnedProcess(pid uint32) {
	process, err := syscall.OpenProcess(0x0001, false, pid)
	if err != nil {
		return
	}
	defer syscall.CloseHandle(process)
	_ = syscall.TerminateProcess(process, 1)
	_, _ = syscall.WaitForSingleObject(process, uint32(2*time.Second/time.Millisecond))
}

// treeKillStarted reports whether the helper reached its start marker;
// used only in failure diagnostics.
func treeKillStarted(pidFile string) string {
	matches, _ := filepath.Glob(pidFile + ".*.started")
	if len(matches) == 0 {
		return "helper never started"
	}
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, filepath.Base(m))
	}
	return "helper started: " + strings.Join(names, ", ")
}
