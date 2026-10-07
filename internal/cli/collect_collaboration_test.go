package cli

// D7: a registered agent inside an active collaboration is collected as the
// cycle's state — never as the stale or initial report, which is an artifact
// only. The tests run the real CLI entry with the collaboration fixture
// (collaborate_test.go): roster + policy + fake herdr, in-process Run.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
)

// collectSD resolves the fixture's workspace state dir, the same way the
// collaboration fixture does.
func collectSD(t *testing.T, f *copiesFixture) string {
	t.Helper()
	ctx := core.LoadConfig(f.env, f.repo)
	return core.StateDir(&ctx, f.env, f.repo)
}

func collectJSONField(key, value string) string {
	encoded, _ := json.Marshal(value)
	return `"` + key + `":` + string(encoded)
}

// writeCollectStaleReport records a complete report from the previous task
// the traditional collect would print as the result.
func writeCollectStaleReport(t *testing.T, f *copiesFixture, agent, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(core.LastReportPath(collectSD(t, f), agent), []byte(path+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// snapshotDir maps every file under root to its sha256, for the no-trace
// assertions.
func snapshotDir(t *testing.T, root string) map[string]string {
	t.Helper()
	sums := map[string]string{}
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		sum := sha256.Sum256(b)
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return nil
		}
		sums[filepath.ToSlash(rel)] = fmt.Sprintf("%x", sum)
		return nil
	})
	return sums
}

func TestCollectShowsTheCollaborationBeforeTheStaleReport(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	a := startCollaboration(t, f, brief)
	stale := filepath.Join(f.root, "stale.md")
	writeCollectStaleReport(t, f, "author", stale, "STALE BODY NEVER PRINTED\n")
	code, out, errOut := f.run(t, f.env, "collect", "author")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{
		`"status":"collaborating"`,
		`"assignment":"` + a.ID + `"`,
		`"participants":["author","reviewer"]`,
		`"phase":"preparing"`,
		`"delivery":"none"`,
		collectJSONField("report", stale),
		collectJSONField("evidence", stale),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	// The stale report is an artifact, never the completion: its body is not
	// printed and the terminal fallback (the fake agent read) never runs.
	if strings.Contains(out, "STALE BODY NEVER PRINTED") || strings.Contains(out, "Worker is working") {
		t.Fatalf("stale report or terminal output in %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationEvidenceFollowsTheLastEvent(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	a := startCollaboration(t, f, brief)
	a2 := cliEvent(t, f, a, "ws:p1", "ready", "Ready body\n")
	stale := filepath.Join(f.root, "stale2.md")
	writeCollectStaleReport(t, f, "author", stale, "STALE2 BODY NEVER PRINTED\n")
	code, out, errOut := f.run(t, f.env, "collect", "author")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{
		`"status":"collaborating"`,
		`"phase":"reviewing"`,
		`"delivery":"submitted"`,
		`"revision":"` + a2.Revision + `"`,
		// The last evidence is the last event's report, not the stale one.
		collectJSONField("evidence", filepath.Join(f.root, "ready.md")),
		collectJSONField("report", stale),
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	if strings.Contains(out, "STALE2 BODY NEVER PRINTED") || strings.Contains(out, "Ready body") {
		t.Fatalf("report bodies in %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationWithoutEvidenceExitsZero(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	startCollaboration(t, f, brief)
	code, out, errOut := f.run(t, f.env, "collect", "author")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{`"status":"collaborating"`, `"report":"none"`, `"evidence":"none"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	// No report: no terminal fallback either.
	if strings.Contains(out, "Worker is working") {
		t.Fatalf("terminal output in %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationPolicyOffShowsTheCauseAndKeepsMetadata(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	a := startCollaboration(t, f, brief)
	ctx := core.LoadConfig(f.env, f.repo)
	if err := os.WriteFile(core.SessionConfPath(&ctx, f.env, f.repo), []byte("worker_messages=off\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run(t, f.env, "collect", "author")
	if code != 4 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{
		`"status":"collaboration-disabled"`,
		`"cause":"worker_messages is off"`,
		// The valid assignment's metadata is kept.
		`"assignment":"` + a.ID + `"`,
		`"phase":"preparing"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationIdentityFailureShowsTheCause(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	startCollaboration(t, f, brief)
	// The reviewer leaves the live roster: the registration no longer
	// verifies.
	sd := collectSD(t, f)
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n" +
		"author\tws:p1\tcodex\timplementer\topenai\t0\t" + f.repo + "\tauthor-start\tgpt-5.4\task\timplementer\tbuild\n"
	if err := os.WriteFile(filepath.Join(sd, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := f.run(t, f.env, "collect", "author")
	if code != 4 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{`"status":"collaboration-unavailable"`, "reviewer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationVerifyChecksEvidenceWithoutFinalizing(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	a := startCollaboration(t, f, brief)
	owned := filepath.Join(f.repo, "src", "app.go")
	if err := os.MkdirAll(filepath.Dir(owned), 0o700); err != nil {
		t.Fatal(err)
	}
	body := []byte("package app\n\nvar X = 1\n")
	if err := os.WriteFile(owned, body, 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	sd := collectSD(t, f)
	// The ready event's report carries a verifiable sha line for the owned
	// file; it is the last evidence the --verify checks.
	cliEvent(t, f, a, "ws:p1", "ready", fmt.Sprintf("%x  src/app.go\n", sum))
	before := snapshotDir(t, filepath.Join(sd, "collaboration"))
	code, out, errOut := f.run(t, f.env, "collect", "author", "--verify")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{
		`"status":"collaborating"`,
		collectJSONField("evidence", filepath.Join(f.root, "ready.md")),
		"ok " + owned,
		"verified 1: ok 1, changed 0, missing 0",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	// Verification does not finalize: the assignment record is untouched.
	if after := snapshotDir(t, filepath.Join(sd, "collaboration")); len(before) != len(after) || before[a.ID+"/assignment.json"] != after[a.ID+"/assignment.json"] {
		t.Fatalf("the collaboration record changed: %v vs %v", before, after)
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationVerifyWithoutShaLinesReturns16(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	a := startCollaboration(t, f, brief)
	cliEvent(t, f, a, "ws:p1", "ready", "no hash lines here\n")
	code, out, errOut := f.run(t, f.env, "collect", "author", "--verify")
	if code != 16 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if !strings.Contains(out, `"status":"collaborating"`) || !strings.Contains(out, "nothing verified") {
		t.Fatalf("out %q", out)
	}
}

func TestCollectCollaborationVerifyRefusesMissingOrChangedRoundEvidence(t *testing.T) {
	for _, change := range []string{"missing", "changed"} {
		t.Run(change, func(t *testing.T) {
			f, brief := collaborationFixture(t, false)
			a := cliEvent(t, f, startCollaboration(t, f, brief), "ws:p1", "ready", "Ready\n")
			path := a.Events[len(a.Events)-1].Report
			want := 16
			if change == "missing" {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				want = 4
			} else if err := os.WriteFile(path, []byte("Changed after publication\n"), 0600); err != nil {
				t.Fatal(err)
			}
			code, out, stderr := f.run(t, f.env, "collect", "author", "--verify")
			if code != want || !strings.Contains(out, `"status":"collaborating"`) || stderr == "" {
				t.Fatalf("verification hid %s evidence: code=%d out=%s err=%s", change, code, out, stderr)
			}
		})
	}
}

func TestCollectCollaborationVerifyWithoutEvidenceExitsZero(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	startCollaboration(t, f, brief)
	code, out, errOut := f.run(t, f.env, "collect", "author", "--verify")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, want := range []string{`"status":"collaborating"`, `"evidence":"none"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("out %q missing %s", out, want)
		}
	}
	if strings.Contains(out, "verified") {
		t.Fatalf("nothing was verified in %q", out)
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func TestCollectCollaborationNowriteLeavesNoTrace(t *testing.T) {
	f, brief := collaborationFixture(t, false)
	startCollaboration(t, f, brief)
	stale := filepath.Join(f.root, "stale.md")
	writeCollectStaleReport(t, f, "author", stale, "STALE BODY NEVER PRINTED\n")
	// A task-report pointer the traditional collect would let SyncTaskReport
	// rewrite or remove.
	sd := collectSD(t, f)
	pointer := filepath.Join(sd, "task-report-author.json")
	if err := os.WriteFile(pointer, []byte(`{"version":1,"task_report":"/tmp/task.md","current":"/tmp/task.md","history":[]}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, root := range []string{sd, f.repo} {
		for k, v := range snapshotDir(t, root) {
			before[rootName(root)+"#"+k] = v
		}
	}
	env := f.env.Clone()
	env["HERDR_SOHO_NOWRITE"] = "1"
	code, out, errOut := f.run(t, env, "collect", "author")
	if code != 0 || !strings.Contains(out, `"status":"collaborating"`) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	after := map[string]string{}
	for _, root := range []string{sd, f.repo} {
		for k, v := range snapshotDir(t, root) {
			after[rootName(root)+"#"+k] = v
		}
	}
	if len(before) != len(after) {
		t.Fatalf("snapshot sizes differ: %d before, %d after", len(before), len(after))
	}
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("file %s changed under NOWRITE", k)
		}
	}
	if errOut != "" {
		t.Fatalf("stderr %q", errOut)
	}
}

func rootName(root string) string {
	return filepath.Base(filepath.Dir(root)) + "/" + filepath.Base(root)
}
