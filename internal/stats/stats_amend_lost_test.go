package stats

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

type amendLostFixture struct {
	t       *testing.T
	root    string
	sd      string
	briefs  string
	reports string
}

func newAmendLostFixture(t *testing.T) *amendLostFixture {
	t.Helper()
	root := t.TempDir()
	f := &amendLostFixture{
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
	if err := os.WriteFile(filepath.Join(f.sd, "agents.tsv"), []byte("# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *amendLostFixture) env() platform.Env {
	return platform.Env{
		"HERDR_ENV":          "1",
		"HERDR_SOHO_DIR":     filepath.Dir(f.sd),
		"HERDR_WORKSPACE_ID": "ws",
		"HOME":               f.root,
		"TMPDIR":             filepath.Join(f.root, "tmp"),
		"PATH":               filepath.Join(f.root, "bin"),
	}
}

func (f *amendLostFixture) brief(agent, stamp, content string) string {
	f.t.Helper()
	p := filepath.Join(f.briefs, agent+"-"+stamp+".md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *amendLostFixture) report(agent, stamp, content string) string {
	f.t.Helper()
	p := filepath.Join(f.reports, agent+"-"+stamp+".md")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *amendLostFixture) pointer(agent, taskReport, current string, history ...string) {
	f.t.Helper()
	content := jsonjs.Stringify(jsonjs.O("version", 1, "task_report", taskReport, "current", current, "history", history))
	if err := os.WriteFile(filepath.Join(f.sd, "task-report-"+agent+".json"), []byte(content+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

func (f *amendLostFixture) run(t *testing.T, args ...string) (string, int) {
	t.Helper()
	old := platform.Stdout
	var out bytes.Buffer
	platform.Stdout = &out
	t.Cleanup(func() { platform.Stdout = old })
	code := CmdStats(args, CommandContext{Config: &core.Config{Entries: map[string]core.ConfigEntry{}, Order: []string{}}, Env: f.env(), Cwd: f.root, FrictionLog: ""})
	return out.String(), code
}

type amendLostDoc struct {
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

func (f *amendLostFixture) parse(t *testing.T, out string) amendLostDoc {
	t.Helper()
	var doc amendLostDoc
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("stats output %q is not JSON: %v", out, err)
	}
	return doc
}

func containsLost(lost []string, want string) bool {
	for _, p := range lost {
		if filepath.Clean(p) == filepath.Clean(want) {
			return true
		}
	}
	return false
}

// D13 (skedly): an amendment's report closes the brief it amends. The task
// report pointer (task-report-<agent>.json) keeps the task's report paths in
// dispatch order (history plus current); a member whose non-empty report
// exists closes every earlier member of the same task, so the amended brief
// is neither lost nor pending when the worker reported on the amendment's
// path.
func TestStatsAmendLost(t *testing.T) {
	t.Run("stats_amend_lost_brief_closed_by_amendment_report", func(t *testing.T) {
		// The rc.11 case: the brief build-5-20261001T213737 received two
		// amendments; the worker wrote one report, on the last amendment's
		// path, covering the brief and both amendments. The brief must not
		// be lost.
		f := newAmendLostFixture(t)
		f.brief("build-5", "20261001T213737", "You are running as the `implementer` role\n\n# Brief\nwork\n")
		f.brief("build-5", "20261001T215000", "# Amendment to your current brief\n")
		f.brief("build-5", "20261001T223343", "# Amendment to your current brief\n")
		f.report("build-5", "20261001T223343", "report covering the brief and the amendments\n")
		// the task's stable copy holds the same content (dispatch --amend syncs it)
		if err := os.WriteFile(filepath.Join(f.reports, "build-5-20261001T213737.current.md"), []byte("report covering the brief and the amendments\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		f.pointer("build-5",
			filepath.Join(f.reports, "build-5-20261001T213737.current.md"),
			filepath.Join(f.reports, "build-5-20261001T223343.md"),
			filepath.Join(f.reports, "build-5-20261001T213737.md"),
			filepath.Join(f.reports, "build-5-20261001T215000.md"))
		out, code := f.run(t, "--json")
		if code != 0 {
			t.Fatalf("stats exit=%d out=%s", code, out)
		}
		doc := f.parse(t, out)
		if len(doc.LostBriefs) != 0 {
			t.Fatalf("lost_briefs=%v, want empty (the amendment's report closes the brief it amends): %s", doc.LostBriefs, out)
		}
		g, ok := doc.Roles["implementer"]
		if !ok || g.Tasks != 1 || g.Briefs != 1 || g.Amendments != 2 || g.NoReport.Pending != 0 || g.NoReport.Lost != 0 {
			t.Fatalf("implementer group=%+v, want tasks=1 amendments=2 no_report=0/0: %s", g, out)
		}
	})
	t.Run("stats_amend_lost_control_brief_without_report_still_lost", func(t *testing.T) {
		// Controls: a brief really without a report keeps showing. `gone`
		// never reported and never got an amendment; `stuck` got an
		// amendment whose report never arrived (the pointer alone, without a
		// non-empty later report, must not close the brief).
		f := newAmendLostFixture(t)
		goneBrief := f.brief("gone", "20261001T090000", "You are running as the `implementer` role\n\n# Brief\nwork\n")
		stuckBrief := f.brief("stuck", "20261001T100000", "You are running as the `implementer` role\n\n# Brief\nwork\n")
		f.brief("stuck", "20261001T103000", "# Amendment to your current brief\n")
		f.pointer("stuck",
			filepath.Join(f.reports, "stuck-20261001T100000.current.md"),
			filepath.Join(f.reports, "stuck-20261001T103000.md"),
			filepath.Join(f.reports, "stuck-20261001T100000.md"))
		out, code := f.run(t, "--json")
		if code != 0 {
			t.Fatalf("stats exit=%d out=%s", code, out)
		}
		doc := f.parse(t, out)
		g, ok := doc.Roles["implementer"]
		if !ok || g.Tasks != 2 || g.Amendments != 1 || g.NoReport.Lost != 2 {
			t.Fatalf("implementer group=%+v, want tasks=2 lost=2: %s", g, out)
		}
		lost := doc.LostBriefs["implementer"]
		if len(lost) != 2 || !containsLost(lost, goneBrief) || !containsLost(lost, stuckBrief) {
			t.Fatalf("lost_briefs=%v, want both %q and %q", lost, goneBrief, stuckBrief)
		}
	})
}
