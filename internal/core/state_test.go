package core

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestRosterAppendProcess(t *testing.T) {
	if dir := os.Getenv("HERDR_SOHO_ROSTER_CHILD"); dir != "" {
		name := os.Getenv("HERDR_SOHO_ROSTER_NAME")
		for i := 0; i < 50; i++ {
			RosterAppend(dir, []string{name + "-" + fmt.Sprint(i), "p", "grok", "implementer", "xai", "0", "/tmp", "now", "", "ask", "implementer", ""})
		}
		return
	}
}

func TestStateParity(t *testing.T) {
	t.Run("creates briefs/reports/wait and the 12-column header, idempotent", func(t *testing.T) { // JS: "stateDir: creates briefs/reports/wait and the 12-column header, idempotent"
		root := t.TempDir()
		env := platform.Env{"HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws"}
		ctx := &Config{Entries: map[string]ConfigEntry{}}
		dir := StateDir(ctx, env, root)
		if dir != filepath.Join(root, "state", "ws") {
			t.Fatalf("dir=%q", dir)
		}
		for _, child := range []string{"briefs", "reports", "wait"} {
			if st, err := os.Stat(filepath.Join(dir, child)); err != nil || !st.IsDir() {
				t.Fatalf("%s missing: %v", child, err)
			}
		}
		if got, _ := os.ReadFile(filepath.Join(dir, "agents.tsv")); string(got) != "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" {
			t.Fatalf("header=%q", got)
		}
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("# keep\n"), 0o600)
		StateDir(ctx, env, root)
		got, _ := os.ReadFile(filepath.Join(dir, "agents.tsv"))
		if string(got) != "# keep\n" {
			t.Fatalf("existing roster replaced: %q", got)
		}
	})
	t.Run("state directories and friction error logs use default creation modes", func(t *testing.T) {
		root := t.TempDir()
		controlDir := filepath.Join(root, "control-dir")
		if err := os.Mkdir(controlDir, 0o777); err != nil {
			t.Fatal(err)
		}
		dirMode, _ := os.Stat(controlDir)
		env := platform.Env{"HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws"}
		dir := StateDir(&Config{Entries: map[string]ConfigEntry{}}, env, root)
		childMode, _ := os.Stat(filepath.Join(dir, "wait"))
		if childMode.Mode().Perm() != dirMode.Mode().Perm() {
			t.Fatalf("wait mode=%o want umask-adjusted default %o", childMode.Mode().Perm(), dirMode.Mode().Perm())
		}
		controlFile := filepath.Join(root, "control-file")
		f, err := os.OpenFile(controlFile, os.O_CREATE|os.O_WRONLY, 0o666)
		if err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		frictionLog(filepath.Join(dir, "friction.log"), "clean", "error(exit 4)", "bad")
		controlInfo, _ := os.Stat(controlFile)
		logInfo, _ := os.Stat(filepath.Join(dir, "friction.log"))
		if logInfo.Mode().Perm() != controlInfo.Mode().Perm() {
			t.Fatalf("friction mode=%o want umask-adjusted default %o", logInfo.Mode().Perm(), controlInfo.Mode().Perm())
		}
	})
	t.Run("two processes append 50 complete roster rows without loss", func(t *testing.T) { // JS: "roster: two children appending 50 lines concurrently lose nothing"
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(rosterHeader), 0o600)
		cmds := []*exec.Cmd{}
		for _, name := range []string{"w1", "w2"} {
			cmd := exec.Command(os.Args[0], "-test.run=^TestRosterAppendProcess$")
			cmd.Env = append(testutil.CleanEnv(t), "HERDR_SOHO_ROSTER_CHILD="+dir, "HERDR_SOHO_ROSTER_NAME="+name)
			cmds = append(cmds, cmd)
		}
		for _, cmd := range cmds {
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
		}
		for _, cmd := range cmds {
			if err := cmd.Wait(); err != nil {
				t.Fatal(err)
			}
		}
		rows := RosterRows(dir)
		if len(rows) != 100 {
			t.Fatalf("rows=%d", len(rows))
		}
		seen := map[string]bool{}
		for _, row := range rows {
			f := strings.Split(row, "\t")
			if len(f) != 12 {
				t.Fatalf("columns=%d row=%q", len(f), row)
			}
			seen[f[0]] = true
		}
		if len(seen) != 100 {
			t.Fatalf("unique rows=%d", len(seen))
		}
		if _, err := os.Stat(filepath.Join(dir, "agents.lock")); !os.IsNotExist(err) {
			t.Fatalf("lock remains: %v", err)
		}
	})
	t.Run("append waits for a roster rewrite lock", func(t *testing.T) { // Mutation captured: removing WithRosterLock from RosterAppend lets it finish while the lock is held.
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte(rosterHeader), 0o600)
		started, done := make(chan struct{}), make(chan struct{})
		go func() {
			WithRosterLock(dir, func() {
				close(started)
				<-done
			})
		}()
		<-started
		appended := make(chan struct{})
		go func() {
			RosterAppend(dir, []string{"worker", "p", "kind"})
			close(appended)
		}()
		select {
		case <-appended:
			close(done)
			t.Fatal("append completed while another writer held the lock")
		case <-time.After(100 * time.Millisecond):
		}
		close(done)
		select {
		case <-appended:
		case <-time.After(2 * time.Second):
			t.Fatal("append did not complete after the lock was released")
		}
		if rows := RosterRows(dir); len(rows) != 1 || !strings.HasPrefix(rows[0], "worker\t") {
			t.Fatalf("rows=%q", rows)
		}
	})
	t.Run("a stale lock is dropped and taken", func(t *testing.T) { // JS: "roster lock: a stale lock (mtime 2 minutes old) is dropped and taken"
		dir := t.TempDir()
		lock := filepath.Join(dir, "agents.lock")
		_ = os.Mkdir(lock, 0o700)
		old := time.Now().Add(-2 * time.Minute)
		_ = os.Chtimes(lock, old, old)
		ran := false
		WithRosterLock(dir, func() { ran = true })
		if !ran {
			t.Fatal("lock callback did not run")
		}
		if _, err := os.Stat(lock); !os.IsNotExist(err) {
			t.Fatalf("lock remains: %v", err)
		}
	})
	t.Run("a regular file lock is preserved and rejected as friction", func(t *testing.T) { // JS: "roster lock: stale regular file is not removed"
		dir := t.TempDir()
		lock := filepath.Join(dir, "agents.lock")
		_ = os.WriteFile(lock, []byte("x"), 0o600)
		old := time.Now().Add(-2 * time.Minute)
		_ = os.Chtimes(lock, old, old)
		ran := false
		var recovered any
		func() {
			defer func() { recovered = recover() }()
			WithRosterLock(dir, func() { ran = true })
		}()
		if ran {
			t.Fatal("callback ran with a regular file lock")
		}
		if exitErr, ok := recovered.(*platform.ExitError); !ok || exitErr.Code != 4 || !exitErr.Friction || !strings.Contains(exitErr.Msg, "held for too long") {
			t.Fatalf("recovered=%#v", recovered)
		}
		if data, err := os.ReadFile(lock); err != nil || string(data) != "x" {
			t.Fatalf("lock data=%q err=%v", data, err)
		}
	})
	t.Run("a throwing callback still releases the lock", func(t *testing.T) { // JS: "roster lock: a throwing fn still releases the lock"
		dir := t.TempDir()
		func() { defer func() { _ = recover() }(); WithRosterLock(dir, func() { panic("boom") }) }()
		if _, err := os.Stat(filepath.Join(dir, "agents.lock")); !os.IsNotExist(err) {
			t.Fatalf("lock remains: %v", err)
		}
	})
	t.Run("roster rewrites preserve mode and leave no temp file", func(t *testing.T) { // JS: "roster rewrites: keep the 0640 mode of the existing file and leave no temp"
		dir := t.TempDir()
		roster := filepath.Join(dir, "agents.tsv")
		_ = os.WriteFile(roster, []byte(rosterHeader+"w\tp\tg\timpl\tx\t0\t/tmp\tnow\t\t\t\t\n"), 0o640)
		_ = os.Chmod(roster, 0o640)
		RosterSetRole(dir, "w", "reviewer")
		RosterRemove(dir, "w")
		if runtime.GOOS != "windows" {
			if st, _ := os.Stat(roster); st.Mode().Perm() != 0o640 {
				t.Fatalf("mode=%o", st.Mode().Perm())
			}
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if strings.Contains(e.Name(), ".tmp") {
				t.Fatalf("temp left: %s", e.Name())
			}
		}
	})
	t.Run("set role updates column four, adds history, and expands legacy rows to eleven columns", func(t *testing.T) { // JS: "rosterSetRole: column 4 changes, history grows, 8-column rows grow to 11"
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("w\tp\tg\timplementer\tx\t0\t/tmp\tnow\n"), 0o600)
		RosterSetRole(dir, "w", "reviewer")
		f := strings.Split(RosterRows(dir)[0], "\t")
		if len(f) != 11 || f[3] != "reviewer" || f[10] != "implementer,reviewer" {
			t.Fatalf("row=%q", f)
		}
	})
	t.Run("remove preserves the header, lookup uses the last matching row, pane replacement changes column two", func(t *testing.T) { // JS: "roster: remove keeps the header, last line wins, replace_pane swaps column 2"
		dir := t.TempDir()
		file := filepath.Join(dir, "agents.tsv")
		_ = os.WriteFile(file, []byte(rosterHeader+"w\tp1\ta\tb\tc\td\te\tf\ng\tp2\ta\tb\tc\td\te\tf\nw\tp3\ta\tb\tc\td\te\tf\n"), 0o600)
		if got := strings.Split(RosterLine(dir, "w"), "\t")[1]; got != "p3" {
			t.Fatalf("last line pane=%s", got)
		}
		RosterReplacePane(dir, "p3", "p9")
		if strings.Split(RosterLine(dir, "w"), "\t")[1] != "p9" {
			t.Fatal("pane not replaced")
		}
		RosterRemove(dir, "w")
		content, _ := os.ReadFile(file)
		if !strings.HasPrefix(string(content), "# name\t") || len(RosterRows(dir)) != 1 {
			t.Fatalf("roster=%q", content)
		}
	})
	t.Run("replace pane leaves a short row unchanged", func(t *testing.T) { // Mutation captured: dropping the short-row guard panics on the missing pane column.
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("single\n"), 0o600)
		RosterReplacePane(dir, "p1", "p2")
		if got, _ := os.ReadFile(filepath.Join(dir, "agents.tsv")); string(got) != "single\n" {
			t.Fatalf("roster=%q", got)
		}
	})
	t.Run("roster rows normalize CRLF", func(t *testing.T) { // JS: "roster rows: CRLF files are normalized on read (decision 3)"
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("# header\r\nw\tp\tg\tr\r\n"), 0o600)
		if rows := RosterRows(dir); len(rows) != 1 || rows[0] != "w\tp\tg\tr" {
			t.Fatalf("rows=%q", rows)
		}
	})
	t.Run("last report strips trailing newlines and is empty when absent", func(t *testing.T) { // JS: "lastReport: trailing newlines stripped, empty when the file is absent"
		dir := t.TempDir()
		_ = os.WriteFile(LastReportPath(dir, "w"), []byte("/report\n\n"), 0o600)
		if got := LastReport(dir, "w"); got != "/report" {
			t.Fatalf("report=%q", got)
		}
		if LastReport(dir, "absent") != "" {
			t.Fatal("absent report not empty")
		}
	})
	t.Run("friction sanitizes line separators and timestamp formats are stable", func(t *testing.T) { // JS: "friction: warn() sanitizes \\r, \\n and \\t out of the message (four TSV columns)"
		if got := FrictionSafe("a\rb\nc\td"); got != "a b c d" {
			t.Fatalf("safe=%q", got)
		}
		instant := time.Date(2026, 9, 28, 1, 2, 3, 0, time.UTC)
		if NowStamp(instant) != "20260928T010203" || NowISO(instant) != "2026-09-28T01:02:03" {
			t.Fatal("timestamp format changed")
		}
	})
	t.Run("warn writes one warning line and appends one four-column friction row", func(t *testing.T) { // JS: "friction: warn() appends a TSV line with the command name"
		file := filepath.Join(t.TempDir(), "friction.log")
		old := platform.Stderr
		var stderr strings.Builder
		platform.Stderr = &stderr
		Warn("careful", file, "roster")
		platform.Stderr = old
		if stderr.String() != "herdr-soho: warning: careful\n" {
			t.Fatalf("stderr=%q", stderr.String())
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		fields := strings.Split(strings.TrimSpace(string(data)), "\t")
		if len(fields) != 4 || fields[1] != "warning" || fields[2] != "roster" || fields[3] != "careful" {
			t.Fatalf("friction row=%q", data)
		}
	})
	t.Run("workspace id environment wins and herdr query supplies an id", func(t *testing.T) { // JS: "workspaceId: env var wins; herdr query; null id; herdr failure throws DieError"
		dir := t.TempDir()
		_, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"workspace_id":"from-herdr"}}}`}})
		if err != nil {
			t.Fatal(err)
		}
		env := fakeEnvWithCLI(t, dir)
		ctx := &Config{Entries: map[string]ConfigEntry{}}
		if got := WorkspaceID(ctx, platform.Env{"HERDR_WORKSPACE_ID": "from-env"}, t.TempDir()); got != "from-env" {
			t.Fatalf("env id=%s", got)
		}
		if got := WorkspaceID(ctx, env, t.TempDir()); got != "from-herdr" {
			t.Fatalf("herdr id=%s", got)
		}
	})
	t.Run("workspace id maps a missing or null field to the literal null", func(t *testing.T) { // JS: "workspaceId returns the jq-compatible literal null for a missing id"
		dir := t.TempDir()
		_, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: `{"result":{"pane":{"workspace_id":null}}}`}})
		if err != nil {
			t.Fatal(err)
		}
		if got := WorkspaceID(&Config{Entries: map[string]ConfigEntry{}}, fakeEnvWithCLI(t, dir), t.TempDir()); got != "null" {
			t.Fatalf("id=%q", got)
		}
	})
	t.Run("unsupported pane-current response and workspace-id types fail before creating state", func(t *testing.T) { // Mutation captured: accepting a non-object pane or object/list workspace_id creates a null-named state tree.
		ctx := &Config{Entries: map[string]ConfigEntry{}}
		for _, tc := range []struct {
			name   string
			stdout string
		}{
			{name: "non-object result", stdout: `[]`},
			{name: "non-object pane", stdout: `{"result":{"pane":[]}}`},
			{name: "array workspace id", stdout: `{"result":{"pane":{"workspace_id":[1,2]}}}`},
			{name: "object workspace id", stdout: `{"result":{"pane":{"workspace_id":{"id":"x"}}}}`},
		} {
			t.Run(tc.name, func(t *testing.T) {
				root := t.TempDir()
				fakeDir := t.TempDir()
				_, err := fakecli.Install(t, fakeDir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "current", "--current"}, Stdout: tc.stdout}})
				if err != nil {
					t.Fatal(err)
				}
				env := fakeEnvWithCLI(t, fakeDir)
				env["HERDR_SOHO_DIR"] = filepath.Join(root, "state")
				var recovered any
				func() {
					defer func() { recovered = recover() }()
					StateDir(ctx, env, root)
				}()
				exitErr, ok := recovered.(*platform.ExitError)
				if !ok || exitErr.Code != 2 || exitErr.Msg == "" {
					t.Fatalf("recovered=%#v", recovered)
				}
				if _, err := os.Stat(env.Get("HERDR_SOHO_DIR")); !os.IsNotExist(err) {
					t.Fatalf("state root created after invalid response: %v", err)
				}
			})
		}
	})
	t.Run("state directory in nowrite mode creates no tree", func(t *testing.T) { // JS: "nowrite: stateDir creates nothing; normal mode still creates the state tree and the .gitignore entry"
		root := t.TempDir()
		env := platform.Env{"HERDR_SOHO_DIR": filepath.Join(root, "state"), "HERDR_WORKSPACE_ID": "ws", "HERDR_SOHO_NOWRITE": "1"}
		dir := StateDir(&Config{Entries: map[string]ConfigEntry{}}, env, root)
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("state created in nowrite: %v", err)
		}
	})
	t.Run("brief task uses the first meaningful heading and strips Brief prefix", func(t *testing.T) { // JS: "brief_task selects the named first heading, without the Brief prefix"
		file := filepath.Join(t.TempDir(), "b.md")
		_ = os.WriteFile(file, []byte("\n# Brief — adjust task flow\n# second\n"), 0o600)
		if got := BriefTask(file); got != "adjust task flow" {
			t.Fatalf("task=%q", got)
		}
	})
	t.Run("brief task ignores contract heading and falls back to file name", func(t *testing.T) { // JS: "brief_task uses the filename when the first heading is a contract section"
		file := filepath.Join(t.TempDir(), "task-file.md")
		_ = os.WriteFile(file, []byte("# Goal\n# Real task\n"), 0o600)
		if got := BriefTask(file); got != "task-file" {
			t.Fatalf("task=%q", got)
		}
	})
	t.Run("brief task does not fold long s into ASCII contract headings", func(t *testing.T) { // Mutation captured: using Unicode case folding classifies long-scope as a contract heading.
		file := filepath.Join(t.TempDir(), "task-file.md")
		_ = os.WriteFile(file, []byte("# ſcope\n"), 0o600)
		if got := BriefTask(file); got != "ſcope" {
			t.Fatalf("task=%q", got)
		}
	})
	t.Run("task completion marks the task once and updates the roster pane title", func(t *testing.T) { // JS: "mark_task_done writes the check mark once and sets the pane title"
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "agents.tsv"), []byte("worker\tpane-1\n"), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "task-worker"), []byte("Task title\n\n"), 0o600)
		fakeDir := t.TempDir()
		_, err := fakecli.Install(t, fakeDir, "herdr", []fakecli.Rule{{Argv: []string{"pane", "report-metadata", "pane-1", "--source", "herdr-soho", "--title", "Task title ✓"}}})
		if err != nil {
			t.Fatal(err)
		}
		MarkTaskDone(dir, "worker", fakeEnvWithCLI(t, fakeDir))
		if err := MarkTaskDone(dir, "worker", fakeEnvWithCLI(t, fakeDir)); err != nil {
			t.Fatalf("repeat mark error=%v", err)
		}
		data, err := os.ReadFile(filepath.Join(dir, "task-worker"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != "Task title ✓\n" {
			t.Fatalf("task=%q", data)
		}
		calls, err := fakecli.ReadCalls(filepath.Join(fakeDir, "herdr.calls.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if len(calls) != 1 || strings.Join(calls[0].Argv, " ") != "pane report-metadata pane-1 --source herdr-soho --title Task title ✓" {
			t.Fatalf("calls=%+v", calls)
		}
		calls, _ = fakecli.ReadCalls(filepath.Join(fakeDir, "herdr.calls.jsonl"))
		if len(calls) != 1 {
			t.Fatalf("marked twice: %d calls", len(calls))
		}
	})
	t.Run("task completion returns atomic write errors", func(t *testing.T) { // Mutation captured: swallowing AtomicWrite's error returns success to the caller.
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "task-worker"), []byte("Task title\n"), 0o600)
		oldWrite := atomicWriteTask
		atomicWriteTask = func(string, string) error { return errors.New("write failed") }
		defer func() { atomicWriteTask = oldWrite }()
		if err := MarkTaskDone(dir, "worker", platform.Env{}); err == nil || err.Error() != "write failed" {
			t.Fatalf("error=%v", err)
		}
	})
}

func fakeEnvWithCLI(t *testing.T, dir string) platform.Env {
	t.Helper()
	list := fakecli.Env(testutil.CleanEnv(t), dir)
	env := platform.Env{}
	for _, item := range list {
		key, value, ok := strings.Cut(item, "=")
		if ok {
			env[key] = value
		}
	}
	delete(env, "HERDR_WORKSPACE_ID")
	return env
}
