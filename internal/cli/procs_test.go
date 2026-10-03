package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// procsFixture is the command fixture with a fake herdr (worker and other
// idle), a two-agent roster, a hermetic TMPDIR and the s85 pressure
// measurements faked, so the gc output stays hermetic.
type procsFixture struct {
	env        platform.Env
	cwd        string
	state      string
	fakeDir    string
	configFile string
}

func newProcsFixture(t *testing.T) *procsFixture {
	t.Helper()
	env, cwd := commandFixture(t)
	env["HERDR_ENV"] = "1"
	env["HERDR_PANE_ID"] = "p-worker"
	env["HERDR_SOHO_REGRID"] = "off"
	env["TMPDIR"] = t.TempDir()
	state := filepath.Join(env.Get("HERDR_SOHO_DIR"), "ws")
	if err := os.MkdirAll(filepath.Join(state, "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	roster := "# header\n" + releaseRow("worker", "p-worker", "1", "implementer", false, "/tmp/work") + "\n" +
		releaseRow("other", "p-other", "1", "reviewer", false, "/tmp/work") + "\n"
	if err := os.WriteFile(filepath.Join(state, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"name":"worker","agent_status":"idle"}}}`},
		{Argv: []string{"agent", "get", "other"}, Stdout: `{"result":{"agent":{"name":"other","agent_status":"idle"}}}`},
		{Argv: []string{"pane", "report-metadata", "p-worker", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"pane", "report-metadata", "p-other", "--source", "herdr-soho", "--clear-title"}},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
		{Argv: []string{"tab", "get", "t1"}, Stdout: `{"result":{"tab":{"label":"old"}}}`},
		{Argv: []string{"tab", "rename", "t1", "herd"}},
	}
	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	env = withFakeCLI(env, fakeDir)
	// withFakeCLI narrows PATH to the fake dir; the registry helpers read
	// ps from PATH, so the system directories come back in (the fake herdr
	// still wins: its dir is first).
	if p := os.Getenv("PATH"); p != "" {
		env["PATH"] = fakeDir + string(os.PathListSeparator) + p
	}
	oldDisk, oldSwap := platform.DiskFree, platform.SwapUsage
	platform.DiskFree = func(string) (int64, int64, bool) {
		return int64(60) * 1024 * 1024 * 1024, int64(100) * 1024 * 1024 * 1024, true
	}
	platform.SwapUsage = func(platform.Env) (int64, int64, bool) {
		return int64(30) * 1024 * 1024 * 1024, int64(100) * 1024 * 1024 * 1024, true
	}
	t.Cleanup(func() { platform.DiskFree, platform.SwapUsage = oldDisk, oldSwap })
	return &procsFixture{env: env, cwd: cwd, state: state, fakeDir: fakeDir, configFile: filepath.Join(fakeDir, "herdr.json")}
}

func (f *procsFixture) run(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &errOut
	code := Run(argv, f.env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), errOut.String()
}

func (f *procsFixture) procsRows(t *testing.T) []core.ProcRow {
	t.Helper()
	rows, err := core.ReadProcs(f.state)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

func (f *procsFixture) writeProcs(t *testing.T, rows ...core.ProcRow) {
	t.Helper()
	if err := core.WriteProcs(f.state, rows); err != nil {
		t.Fatal(err)
	}
}

func (f *procsFixture) registryPath() string { return filepath.Join(f.state, "procs.tsv") }

// sleeper starts a real sleep the test itself owns (killed in the cleanup,
// also when an assertion failed) and returns its pid.
func (f *procsFixture) sleeper(t *testing.T) int {
	t.Helper()
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not available on PATH")
	}
	cmd := exec.Command(path, "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid
}

// sleeperGone is a sleeper the test killed and reaped: the pid is gone.
func (f *procsFixture) sleeperGone(t *testing.T) int {
	t.Helper()
	path, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep is not available on PATH")
	}
	cmd := exec.Command(path, "300")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait() // reap: the entry leaves the table
	return pid
}

// treeProc starts sh -c "sleep 300 & wait" (exactly one descendant) and
// returns the sh pid and the descendant pid.
func (f *procsFixture) treeProc(t *testing.T) (parent, child int) {
	t.Helper()
	if path, err := exec.LookPath("sh"); err == nil {
		cmd := exec.Command(path, "-c", "sleep 300 & wait")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		parent = cmd.Process.Pid
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, name, ok := platform.ProcInfo(parent, f.env); !ok {
				t.Fatalf("the sh %d is already gone (name %q)", parent, name)
			}
			kids, err := procDescendantsForTest(parent, f.env)
			if err == nil && len(kids) == 1 {
				return parent, kids[0]
			}
			if time.Now().After(deadline) {
				t.Fatalf("the sh %d never forked its child (last: %v, %v)", parent, kids, err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	t.Skip("sh is not available on PATH")
	return 0, 0
}

// procDescendantsForTest reads the pid/ppid snapshot the same way the
// stop helper does, so the test asserts on the same data.
func procDescendantsForTest(parent int, env platform.Env) ([]int, error) {
	r := platform.RunCli("ps", []string{"-A", "-o", "pid=,ppid="}, platform.RunOptions{Env: env, TimeoutMs: 5000})
	if r.NotFound || r.TimedOut || r.Error != "" {
		return nil, os.ErrNotExist
	}
	out := []int{}
	for _, line := range strings.Split(r.Stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		child, err1 := strconv.Atoi(fields[0])
		ppid, err2 := strconv.Atoi(fields[1])
		if err1 != nil || err2 != nil || ppid != parent {
			continue
		}
		out = append(out, child)
	}
	return out, nil
}

func procAlive(t *testing.T, env platform.Env, pid int) bool {
	t.Helper()
	_, _, ok := platform.ProcInfo(pid, env)
	return ok
}

func TestProcsAddRegistersNameAndStartTime(t *testing.T) {
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	code, out, errOut := f.run(t, "procs", "add", strconv.Itoa(pid))
	if code != 0 || out != "" || errOut != "" {
		t.Fatalf("procs add: code=%d out=%q err=%q", code, out, errOut)
	}
	rows := f.procsRows(t)
	if len(rows) != 1 {
		t.Fatalf("registry rows = %v; want the one line", rows)
	}
	row := rows[0]
	started, name, ok := platform.ProcInfo(pid, f.env)
	if !ok {
		t.Fatal("the sleeper is not readable")
	}
	if row.Pid != pid || row.Name != name || row.Started != started {
		t.Fatalf("row = %+v; want pid=%d name=%q started=%q", row, pid, name, started)
	}
	if row.Owner != "worker" || row.Pane != "p-worker" {
		t.Fatalf("owner/pane = %q/%q; the pane's roster agent owns the line", row.Owner, row.Pane)
	}
	if !regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$`).MatchString(row.Created) {
		t.Fatalf("created = %q; want ISO-8601 UTC", row.Created)
	}
	// The same pid with the same start time replaces the line, no
	// duplicate.
	if code, _, _ := f.run(t, "procs", "add", strconv.Itoa(pid)); code != 0 {
		t.Fatalf("second add: code=%d", code)
	}
	if rows := f.procsRows(t); len(rows) != 1 {
		t.Fatalf("registry rows after the second add = %v; want still one line", rows)
	}
}

func TestProcsAddRefusals(t *testing.T) {
	f := newProcsFixture(t)
	cases := []struct {
		name  string
		arg   string
		cause string
	}{
		{"pid 1", "1", "pid 1 is the system init process"},
		{"this process", "self", "that is this herdr-soho process"},
		{"the invoking process", "parent", "that is the process that invoked herdr-soho"},
		{"a dead pid", "dead", "no process with that pid is running"},
		{"not a number", "abc", "not a positive integer pid"},
		{"a negative pid", "-5", "not a positive integer pid"},
		{"zero", "0", "not a positive integer pid"},
	}
	dead := f.sleeperGone(t)
	self := strconv.Itoa(os.Getpid())
	parent := strconv.Itoa(os.Getppid())
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arg := tc.arg
			switch arg {
			case "self":
				arg = self
			case "parent":
				arg = parent
			case "dead":
				arg = strconv.Itoa(dead)
			}
			code, _, errOut := f.run(t, "procs", "add", arg)
			if code != 2 {
				t.Fatalf("code=%d; want 2", code)
			}
			if !strings.Contains(errOut, "procs add: refusing "+arg+": "+tc.cause) {
				t.Fatalf("stderr = %q; want the refusal naming %q", errOut, tc.cause)
			}
			if _, err := os.Stat(f.registryPath()); !os.IsNotExist(err) {
				t.Fatalf("the registry exists after a refusal: %v", err)
			}
		})
	}
}

func TestProcsAddAgentOutsideTheRoster(t *testing.T) {
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	code, _, errOut := f.run(t, "procs", "add", strconv.Itoa(pid), "--agent", "ghost")
	if code != 3 {
		t.Fatalf("code=%d; want 3", code)
	}
	if !strings.Contains(errOut, "procs add: agent 'ghost' is not in the roster") {
		t.Fatalf("stderr = %q", errOut)
	}
	if rows := f.procsRows(t); len(rows) != 0 {
		t.Fatalf("registry rows = %v; nothing is registered on a refusal", rows)
	}
	// The roster's own pane comes from the roster line.
	code, _, _ = f.run(t, "procs", "add", strconv.Itoa(pid), "--agent", "other")
	if code != 0 {
		t.Fatalf("code=%d", code)
	}
	rows := f.procsRows(t)
	if len(rows) != 1 || rows[0].Owner != "other" || rows[0].Pane != "p-other" {
		t.Fatalf("rows = %v; want the line owned by 'other' on pane p-other", rows)
	}
}

func TestProcsListStates(t *testing.T) {
	f := newProcsFixture(t)
	running := f.sleeper(t)
	if code, _, _ := f.run(t, "procs", "add", strconv.Itoa(running)); code != 0 {
		t.Fatalf("add: code=%d", code)
	}
	gone := f.sleeperGone(t)
	reused := f.sleeper(t)
	rows := f.procsRows(t)
	f.writeProcs(t, append(rows,
		core.ProcRow{Pid: gone, Started: "Sun Oct  3 00:00:00 2026", Name: "sleep", Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())},
		core.ProcRow{Pid: reused, Started: "adulterated start time", Name: "sleep", Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())},
	)...)
	code, out, errOut := f.run(t, "procs")
	if code != 0 || errOut != "" {
		t.Fatalf("procs: code=%d err=%q", code, errOut)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("output = %q; want three lines", out)
	}
	if !strings.HasPrefix(lines[0], strconv.Itoa(running)+"  sleep  worker  0m  running") {
		t.Fatalf("line 1 = %q; want the running state", lines[0])
	}
	if !strings.HasPrefix(lines[1], strconv.Itoa(gone)+"  sleep  worker  0m  gone") {
		t.Fatalf("line 2 = %q; want the gone state", lines[1])
	}
	if !strings.HasPrefix(lines[2], strconv.Itoa(reused)+"  sleep  worker  0m  reused") {
		t.Fatalf("line 3 = %q; want the reused state (the start time is not the registered one)", lines[2])
	}
}

func TestReleaseStopsTheAgentTreeAndKeepsTheOthers(t *testing.T) {
	f := newProcsFixture(t)
	parent, child := f.treeProc(t)
	other := f.sleeper(t)
	_, parentName, _ := platform.ProcInfo(parent, f.env)
	otherStarted, otherName, _ := platform.ProcInfo(other, f.env)
	parentStarted, _, _ := platform.ProcInfo(parent, f.env)
	f.writeProcs(t,
		core.ProcRow{Pid: parent, Started: parentStarted, Name: parentName, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())},
		core.ProcRow{Pid: other, Started: otherStarted, Name: otherName, Owner: "other", Pane: "p-other", Created: core.FrictionISO(platform.Now())},
	)
	code, out, _ := f.run(t, "release", "worker")
	if code != 0 {
		t.Fatalf("release: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "stopped process "+strconv.Itoa(parent)+" ("+parentName+")") {
		t.Fatalf("output = %q; want the stopped line for the agent's process", out)
	}
	if !strings.Contains(out, "released worker") {
		t.Fatalf("output = %q", out)
	}
	if procAlive(t, f.env, parent) {
		t.Fatal("the agent's process survived the release")
	}
	if procAlive(t, f.env, child) {
		t.Fatal("the descendant of the agent's process survived the release (the tree must be stopped)")
	}
	if !procAlive(t, f.env, other) {
		t.Fatal("the other agent's process was stopped; the release only touches the released agent's lines")
	}
	rows := f.procsRows(t)
	if len(rows) != 1 || rows[0].Pid != other {
		t.Fatalf("registry rows = %v; want only the other agent's line", rows)
	}
}

func TestReleaseKeepProcsKeepsTheProcesses(t *testing.T) {
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	started, name, _ := platform.ProcInfo(pid, f.env)
	f.writeProcs(t, core.ProcRow{Pid: pid, Started: started, Name: name, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())})
	code, out, _ := f.run(t, "release", "worker", "--keep-procs")
	if code != 0 {
		t.Fatalf("release: code=%d out=%q", code, out)
	}
	if strings.Contains(out, "stopped process") {
		t.Fatalf("output = %q; --keep-procs must not stop anything", out)
	}
	if !procAlive(t, f.env, pid) {
		t.Fatal("the process was stopped despite --keep-procs")
	}
	if rows := f.procsRows(t); len(rows) != 1 {
		t.Fatalf("registry rows = %v; the lines stay for gc", rows)
	}
}

func TestReleaseNeverSignalsAReusedPid(t *testing.T) {
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	f.writeProcs(t, core.ProcRow{Pid: pid, Started: "adulterated start time", Name: "sleep", Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())})
	code, out, _ := f.run(t, "release", "worker")
	if code != 0 {
		t.Fatalf("release: code=%d out=%q", code, out)
	}
	if !strings.Contains(out, "process "+strconv.Itoa(pid)+" was reused; not stopped") {
		t.Fatalf("output = %q; want the reused note", out)
	}
	if !procAlive(t, f.env, pid) {
		t.Fatal("the reused pid was signalled; a reused pid is never touched")
	}
	if rows := f.procsRows(t); len(rows) != 0 {
		t.Fatalf("registry rows = %v; the stale line leaves the registry", rows)
	}
}

func TestGcListAndStopOrphanProcesses(t *testing.T) {
	f := newProcsFixture(t)
	owned := f.sleeper(t)
	orphan := f.sleeper(t)
	reused := f.sleeper(t)
	gone := f.sleeperGone(t)
	ownedStarted, ownedName, _ := platform.ProcInfo(owned, f.env)
	orphanStarted, orphanName, _ := platform.ProcInfo(orphan, f.env)
	reusedName := "sleep"
	f.writeProcs(t,
		core.ProcRow{Pid: owned, Started: ownedStarted, Name: ownedName, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())},
		core.ProcRow{Pid: orphan, Started: orphanStarted, Name: orphanName, Owner: "ghost", Pane: "-", Created: core.FrictionISO(platform.Now())},
		core.ProcRow{Pid: reused, Started: "adulterated start time", Name: reusedName, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())},
		core.ProcRow{Pid: gone, Started: "Sun Oct  3 00:00:00 2026", Name: "sleep", Owner: "ghost", Pane: "-", Created: core.FrictionISO(platform.Now())},
	)
	t.Run("without --yes the gc only lists the running orphans", func(t *testing.T) {
		code, out, errOut := f.run(t, "gc")
		if code != 0 || errOut != "" {
			t.Fatalf("gc: code=%d err=%q out=%q", code, errOut, out)
		}
		want := "resource pressure: none (disk 60% free, swap 30% used)\norphan processes:\n" +
			strconv.Itoa(orphan) + "  " + orphanName + "  ghost  0m\n"
		if out != want {
			t.Fatalf("output = %q; want %q", out, want)
		}
		for _, pid := range []int{owned, orphan, reused} {
			if !procAlive(t, f.env, pid) {
				t.Fatalf("the dry run stopped process %d; it must only list", pid)
			}
		}
		if rows := f.procsRows(t); len(rows) != 4 {
			t.Fatalf("registry rows = %v; the dry run changes nothing", rows)
		}
	})
	t.Run("--yes stops the orphans, drops the stale lines and keeps the owned", func(t *testing.T) {
		code, out, errOut := f.run(t, "gc", "--yes")
		if code != 0 || errOut != "" {
			t.Fatalf("gc --yes: code=%d err=%q out=%q", code, errOut, out)
		}
		if !strings.Contains(out, "stopped process "+strconv.Itoa(orphan)+" ("+orphanName+")") {
			t.Fatalf("output = %q; want the orphan stopped", out)
		}
		if !strings.Contains(out, "process "+strconv.Itoa(reused)+" was reused; not stopped") {
			t.Fatalf("output = %q; want the reused note", out)
		}
		if !strings.Contains(out, "process "+strconv.Itoa(gone)+" already gone") {
			t.Fatalf("output = %q; want the gone note", out)
		}
		if !strings.HasSuffix(out, "freed: 0 B\n") {
			t.Fatalf("output = %q; the closing line is the freed total", out)
		}
		if procAlive(t, f.env, orphan) {
			t.Fatal("the orphan process survived gc --yes")
		}
		if !procAlive(t, f.env, owned) {
			t.Fatal("the owned process was stopped; a live owner's lines are never touched")
		}
		if !procAlive(t, f.env, reused) {
			t.Fatal("the reused pid was signalled; a reused pid is never touched")
		}
		rows := f.procsRows(t)
		if len(rows) != 1 || rows[0].Pid != owned {
			t.Fatalf("registry rows = %v; want only the owned line", rows)
		}
	})
}

func TestProcsLockHeld(t *testing.T) {
	t.Run("release warns and keeps the lines", func(t *testing.T) {
		f := newProcsFixture(t)
		pid := f.sleeper(t)
		started, name, _ := platform.ProcInfo(pid, f.env)
		f.writeProcs(t, core.ProcRow{Pid: pid, Started: started, Name: name, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())})
		old := core.CopiesLockTimeout
		core.CopiesLockTimeout = 300 * time.Millisecond
		t.Cleanup(func() { core.CopiesLockTimeout = old })
		lockPath := filepath.Join(f.state, "procs.tsv.lock")
		hold := make(chan struct{})
		go func() {
			file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err == nil {
				_ = file.Close()
			}
			close(hold)
			time.Sleep(1500 * time.Millisecond)
			_ = os.Remove(lockPath)
		}()
		<-hold
		time.Sleep(100 * time.Millisecond)
		code, out, errOut := f.run(t, "release", "worker")
		if code != 0 {
			t.Fatalf("release must never fail for the lock: code=%d out=%q", code, out)
		}
		if !strings.Contains(errOut, "the processes of 'worker' were not stopped") || !strings.Contains(errOut, "procs: registry is locked by another herdr-soho") {
			t.Fatalf("stderr = %q; want the lock warning", errOut)
		}
		if !procAlive(t, f.env, pid) {
			t.Fatal("the process was stopped while the lock was held")
		}
		if rows := f.procsRows(t); len(rows) != 1 {
			t.Fatalf("registry rows = %v; the lines stay", rows)
		}
		if !strings.Contains(out, "released worker") {
			t.Fatalf("output = %q; the release completes", out)
		}
	})
	t.Run("gc --yes exits 4 without stopping anything", func(t *testing.T) {
		f := newProcsFixture(t)
		pid := f.sleeper(t)
		started, name, _ := platform.ProcInfo(pid, f.env)
		f.writeProcs(t, core.ProcRow{Pid: pid, Started: started, Name: name, Owner: "ghost", Pane: "-", Created: core.FrictionISO(platform.Now())})
		old := core.CopiesLockTimeout
		core.CopiesLockTimeout = 300 * time.Millisecond
		t.Cleanup(func() { core.CopiesLockTimeout = old })
		lockPath := filepath.Join(f.state, "procs.tsv.lock")
		hold := make(chan struct{})
		go func() {
			file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err == nil {
				_ = file.Close()
			}
			close(hold)
			time.Sleep(1500 * time.Millisecond)
			_ = os.Remove(lockPath)
		}()
		<-hold
		time.Sleep(100 * time.Millisecond)
		code, out, errOut := f.run(t, "gc", "--yes")
		if code != 4 {
			t.Fatalf("gc --yes: code=%d out=%q; want 4", code, out)
		}
		if !strings.Contains(errOut, "procs: registry is locked by another herdr-soho") {
			t.Fatalf("stderr = %q; want the lock error", errOut)
		}
		if strings.Contains(out, "stopped process") {
			t.Fatalf("output = %q; nothing is stopped behind a held lock", out)
		}
		if !procAlive(t, f.env, pid) {
			t.Fatal("the process was stopped behind a held lock")
		}
		if rows := f.procsRows(t); len(rows) != 1 {
			t.Fatalf("registry rows = %v; the lines stay", rows)
		}
	})
}

func TestProcsNowrite(t *testing.T) {
	f := newProcsFixture(t)
	pid := f.sleeper(t)
	started, name, _ := platform.ProcInfo(pid, f.env)
	f.writeProcs(t, core.ProcRow{Pid: pid, Started: started, Name: name, Owner: "worker", Pane: "p-worker", Created: core.FrictionISO(platform.Now())})
	env := f.env.Clone()
	env["HERDR_SOHO_NOWRITE"] = "1"
	t.Run("the exact procs runs read-only", func(t *testing.T) {
		code, out, errOut := f.runWithEnv(t, env, "procs")
		if code != 0 || errOut != "" {
			t.Fatalf("procs under NOWRITE: code=%d err=%q", code, errOut)
		}
		if !strings.HasPrefix(out, strconv.Itoa(pid)+"  sleep  worker  0m  running") {
			t.Fatalf("output = %q", out)
		}
	})
	t.Run("procs add is refused under NOWRITE", func(t *testing.T) {
		code, _, errOut := f.runWithEnv(t, env, "procs", "add", strconv.Itoa(pid))
		if code != 2 {
			t.Fatalf("code=%d; want 2", code)
		}
		if !strings.Contains(errOut, "read-only") {
			t.Fatalf("stderr = %q", errOut)
		}
	})
}

func (f *procsFixture) runWithEnv(t *testing.T, env platform.Env, argv ...string) (int, string, string) {
	t.Helper()
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(f.cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldCwd) })
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &errOut
	code := Run(argv, env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), errOut.String()
}
