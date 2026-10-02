package stats

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

type taskHistoryFixture struct {
	t       *testing.T
	root    string
	sd      string
	briefs  string
	reports string
}

func newTaskHistoryFixture(t *testing.T, rosterAgents ...string) *taskHistoryFixture {
	t.Helper()
	root := t.TempDir()
	f := &taskHistoryFixture{
		t:       t,
		root:    root,
		sd:      filepath.Join(root, "state", "ws"),
		briefs:  filepath.Join(root, "state", "ws", "briefs"),
		reports: filepath.Join(root, "state", "ws", "reports"),
	}
	for _, dir := range []string{f.briefs, f.reports} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	roster := "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\n"
	for _, a := range rosterAgents {
		roster += a + "\tws-p1\tcodex\timplementer\topenai\t0\t\n"
	}
	if err := os.WriteFile(filepath.Join(f.sd, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *taskHistoryFixture) env() platform.Env {
	return platform.Env{
		"HERDR_ENV":          "1",
		"HERDR_SOHO_DIR":     filepath.Dir(f.sd),
		"HERDR_WORKSPACE_ID": "ws",
		"HOME":               f.root,
		"TMPDIR":             filepath.Join(f.root, "tmp"),
		"PATH":               filepath.Join(f.root, "bin"),
	}
}

func (f *taskHistoryFixture) brief(agent, stamp, content string) string {
	f.t.Helper()
	p := filepath.Join(f.briefs, agent+"-"+stamp+".md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *taskHistoryFixture) report(agent, stamp, content string) string {
	f.t.Helper()
	p := filepath.Join(f.reports, agent+"-"+stamp+".md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *taskHistoryFixture) sidecar(agent, stamp string, extra map[string]any) string {
	f.t.Helper()
	obj := jsonjs.O("version", 1, "kind", "codex", "model", "gpt-5", "effort", "high", "submission", "accepted")
	for k, v := range extra {
		obj.Set(k, v)
	}
	p := filepath.Join(f.briefs, agent+"-"+stamp+".dispatch.json")
	if err := os.WriteFile(p, []byte(jsonjs.Stringify(obj)+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *taskHistoryFixture) pointer(agent, taskReport, current string, history ...string) {
	f.t.Helper()
	content := jsonjs.Stringify(jsonjs.O("version", 1, "task_report", taskReport, "current", current, "history", history))
	if err := os.WriteFile(filepath.Join(f.sd, "task-report-"+agent+".json"), []byte(content+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *taskHistoryFixture) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	old := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	t.Cleanup(func() { platform.Stdout = old })
	code := CmdStats(args, CommandContext{Config: &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}, Env: f.env(), Cwd: f.root, FrictionLog: ""})
	return out.String(), code
}

type taskHistoryDoc struct {
	Roles map[string]struct {
		Tasks      int `json:"tasks"`
		Briefs     int `json:"briefs"`
		Amendments int `json:"amendments"`
		NoReport   struct {
			Pending int `json:"pending"`
			Lost    int `json:"lost"`
		} `json:"no_report"`
	} `json:"roles"`
	LostBriefs map[string][]string `json:"lost_briefs"`
}

func (f *taskHistoryFixture) parse(t *testing.T, out string) taskHistoryDoc {
	t.Helper()
	var doc taskHistoryDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stats output %q is not JSON: %v", out, err)
	}
	return doc
}

func sha256HexField(t *testing.T, content string) string {
	t.Helper()
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func lostContains(lost []string, want string) bool {
	for _, p := range lost {
		if filepath.Clean(p) == filepath.Clean(want) {
			return true
		}
	}
	return false
}

// d20State lays out the D20 state for agent build-5: task A (a brief plus an
// amendment, reported only on the amendment's path) and, after it, a fresh
// dispatch B that repoints the task report pointer. sidecarsNew selects the
// sidecars with the s72 fields (task_report + brief_sha256).
func d20State(t *testing.T, f *taskHistoryFixture, sidecarsNew bool) (a0Brief string) {
	t.Helper()
	a0 := "You are running as the `implementer` role\n\n# Brief A\nwork\n"
	a1 := "# Amendment to your current brief\n\ndo the rest\n"
	b0 := "You are running as the `implementer` role\n\n# Brief B\nwork\n"
	a0Brief = f.brief("build-5", "20260930T100000", a0)
	f.brief("build-5", "20260930T110000", a1)
	f.brief("build-5", "20260930T120000", b0)
	f.report("build-5", "20260930T110000", "report covering the brief and the amendment\n")
	// The session field is a pre-s72 field: task A's members share one worker
	// session and the fresh dispatch B runs in another, so B is a task and
	// not a reuse of A's brief.
	aStable := filepath.Join(f.reports, "build-5-20260930T100000.current.md")
	bStable := filepath.Join(f.reports, "build-5-20260930T120000.current.md")
	if sidecarsNew {
		f.sidecar("build-5", "20260930T100000", map[string]any{"task_report": aStable, "brief_sha256": sha256HexField(t, a0), "session": "20260930T100001"})
		f.sidecar("build-5", "20260930T110000", map[string]any{"task_report": aStable, "brief_sha256": sha256HexField(t, a1), "session": "20260930T100001"})
		f.sidecar("build-5", "20260930T120000", map[string]any{"task_report": bStable, "brief_sha256": sha256HexField(t, b0), "session": "20260930T120001"})
	} else {
		f.sidecar("build-5", "20260930T100000", map[string]any{"session": "20260930T100001"})
		f.sidecar("build-5", "20260930T110000", map[string]any{"session": "20260930T100001"})
		f.sidecar("build-5", "20260930T120000", map[string]any{"session": "20260930T120001"})
	}
	f.pointer("build-5", bStable, filepath.Join(f.reports, "build-5-20260930T120000.md"))
	return a0Brief
}

// D20: stats closes a brief by the amendment that reported, using only the
// task report pointer; a new (non-amendment) dispatch repoints it and the
// old task's brief comes back as lost. The sidecar's task_report keeps the
// brief tied to its task (the same for the original brief and its
// amendments): a member with a non-empty report closes every earlier member
// of the same group, and that closure adds to the pointer closure, which
// stays for the old sidecars.
func TestStatsTaskHistoryD20(t *testing.T) {
	t.Run("stats d20: a new dispatch does not lose the earlier task's brief", func(t *testing.T) {
		f := newTaskHistoryFixture(t, "build-5")
		d20State(t, f, true)
		out, code := f.run(t, "--json")
		if code != 0 {
			t.Fatalf("stats exit=%d out=%s", code, out)
		}
		doc := f.parse(t, out)
		if len(doc.LostBriefs) != 0 {
			t.Fatalf("lost_briefs=%v, want empty (the amendment's report closes the brief it amends): %s", doc.LostBriefs, out)
		}
		g, ok := doc.Roles["implementer"]
		if !ok || g.Tasks != 2 || g.Amendments != 1 || g.NoReport.Pending != 1 || g.NoReport.Lost != 0 {
			t.Fatalf("implementer group=%+v, want tasks=2 amendments=1 no_report=1/0: %s", g, out)
		}
	})
	t.Run("stats d20 control: the old sidecars without the fields keep today's result", func(t *testing.T) {
		// The same state with the sidecars as written before s72: the pointer
		// now belongs to task B, so A's brief is lost again (today's behavior).
		f := newTaskHistoryFixture(t, "build-5")
		a0Brief := d20State(t, f, false)
		out, code := f.run(t, "--json")
		if code != 0 {
			t.Fatalf("stats exit=%d out=%s", code, out)
		}
		doc := f.parse(t, out)
		g, ok := doc.Roles["implementer"]
		if !ok || g.Tasks != 2 || g.Amendments != 1 || g.NoReport.Pending != 1 || g.NoReport.Lost != 1 {
			t.Fatalf("implementer group=%+v, want tasks=2 amendments=1 no_report=1/1: %s", g, out)
		}
		lost := doc.LostBriefs["implementer"]
		if len(lost) != 1 || !lostContains(lost, a0Brief) {
			t.Fatalf("lost_briefs=%v, want only %q", lost, a0Brief)
		}
	})
	t.Run("stats d20: the closure only closes members of the same task_report group", func(t *testing.T) {
		// Task A's brief never got a report and task B's did: B's report must
		// not close A's brief (a different task_report group). A group
		// closure that ignored the task_report would close it.
		f := newTaskHistoryFixture(t, "build-5")
		a0 := "You are running as the `implementer` role\n\n# Brief A\nwork\n"
		b0 := "You are running as the `implementer` role\n\n# Brief B\nwork\n"
		a0Brief := f.brief("build-5", "20260930T100000", a0)
		f.brief("build-5", "20260930T120000", b0)
		f.report("build-5", "20260930T120000", "report of B\n")
		aStable := filepath.Join(f.reports, "build-5-20260930T100000.current.md")
		bStable := filepath.Join(f.reports, "build-5-20260930T120000.current.md")
		f.sidecar("build-5", "20260930T100000", map[string]any{"task_report": aStable, "brief_sha256": sha256HexField(t, a0), "session": "20260930T100001"})
		f.sidecar("build-5", "20260930T120000", map[string]any{"task_report": bStable, "brief_sha256": sha256HexField(t, b0), "session": "20260930T120001"})
		f.pointer("build-5", bStable, filepath.Join(f.reports, "build-5-20260930T120000.md"))
		out, code := f.run(t, "--json")
		if code != 0 {
			t.Fatalf("stats exit=%d out=%s", code, out)
		}
		doc := f.parse(t, out)
		g, ok := doc.Roles["implementer"]
		if !ok || g.Tasks != 2 || g.NoReport.Pending != 0 || g.NoReport.Lost != 1 {
			t.Fatalf("implementer group=%+v, want tasks=2 no_report=0/1: %s", g, out)
		}
		lost := doc.LostBriefs["implementer"]
		if len(lost) != 1 || !lostContains(lost, a0Brief) {
			t.Fatalf("lost_briefs=%v, want only %q (a report of another task does not close it)", lost, a0Brief)
		}
	})
}
