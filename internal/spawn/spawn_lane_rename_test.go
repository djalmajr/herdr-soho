package spawn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// laneReuseRow is a 14-column roster line for an idle review-lane worker on
// the codex kind; the model column is empty so a flagless spawn matches the
// session (no kind-mismatch) and the reuse path is reached.
func laneReuseRow(name string) string {
	return strings.Join([]string{name, "w0test:p0a", "codex", "reviewer", "openai", "1", "/tmp/work", "now", "", "ask", "reviewer", "review", "", ""}, "\t")
}

func renameCalls(calls []fakecli.Call) int {
	n := 0
	for _, call := range calls {
		if len(call.Argv) >= 2 && call.Argv[0] == "agent" && call.Argv[1] == "rename" {
			n++
		}
	}
	return n
}

func TestSpawnLaneReuseRenameJavaScriptCases(t *testing.T) {
	t.Run("spawn: a lane reuse with --name renames the idle worker and reports the new name", func(t *testing.T) {
		// A4.1: lanes on, reuse_workers=on, idle 'review-2' on the review lane,
		// spawn reviewer --name review -> herdr agent rename, roster renamed,
		// JSON name review, the renamed warning.
		f := newSpawnFixture(t, []fakecli.Rule{
			{Argv: []string{"agent", "get", "review-2"}, Stdout: `{"result":{"agent":{"name":"review-2","agent_status":"idle"}}}`},
			{Argv: []string{"agent", "rename", "w0test:p0a", "review"}, Stdout: `{"result":{}}`},
		})
		configureSpawnFixture(t, &f)
		f.env["HERDR_SOHO_LANES"] = "on"
		f.roster(t, laneReuseRow("review-2"))
		code, stdout, stderr, calls := runCmdSpawn(t, f, []string{"reviewer", "--name", "review"})
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		var renames []fakecli.Call
		for _, call := range calls {
			if len(call.Argv) >= 4 && call.Argv[0] == "agent" && call.Argv[1] == "rename" {
				renames = append(renames, call)
			}
		}
		if len(renames) != 1 || strings.Join(renames[0].Argv, " ") != "agent rename w0test:p0a review" {
			t.Fatalf("rename calls=%#v", renames)
		}
		if !strings.Contains(stdout, `"name": "review"`) || !strings.Contains(stdout, `"reused": true`) {
			t.Fatalf("JSON=%q", stdout)
		}
		roster, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(roster), "review\tw0test:p0a") || strings.Contains(string(roster), "review-2") {
			t.Fatalf("roster=%q", roster)
		}
		want := "herdr-soho: warning: reusing idle lane 'review' worker 'review-2' as reviewer, renamed to 'review'; its session already holds earlier briefs"
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr=%q want %q", stderr, want)
		}
	})

	t.Run("spawn: the inverse pair — idle 'review', --name review-2", func(t *testing.T) {
		// A4.1, the friction.log:407 pair: the lane worker is named review and
		// the requested name is review-2.
		f := newSpawnFixture(t, []fakecli.Rule{
			{Argv: []string{"agent", "get", "review"}, Stdout: `{"result":{"agent":{"name":"review","agent_status":"idle"}}}`},
			{Argv: []string{"agent", "rename", "w0test:p0a", "review-2"}, Stdout: `{"result":{}}`},
		})
		configureSpawnFixture(t, &f)
		f.env["HERDR_SOHO_LANES"] = "on"
		f.roster(t, laneReuseRow("review"))
		code, stdout, stderr, calls := runCmdSpawn(t, f, []string{"reviewer", "--name", "review-2"})
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if renameCalls(calls) != 1 {
			t.Fatalf("rename calls=%#v", calls)
		}
		if !strings.Contains(stdout, `"name": "review-2"`) {
			t.Fatalf("JSON=%q", stdout)
		}
		roster, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(roster), "review-2\tw0test:p0a") || strings.Contains(string(roster), "review\tw0test:p0a") {
			t.Fatalf("roster=%q", roster)
		}
		want := "herdr-soho: warning: reusing idle lane 'review' worker 'review' as reviewer, renamed to 'review-2'; its session already holds earlier briefs"
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr=%q want %q", stderr, want)
		}
	})

	t.Run("spawn: --name equal to the idle worker reuses without a rename", func(t *testing.T) {
		// A4.1: the pre-existing reuse behavior when the name already matches.
		f := newSpawnFixture(t, []fakecli.Rule{
			{Argv: []string{"agent", "get", "review-2"}, Stdout: `{"result":{"agent":{"name":"review-2","agent_status":"idle"}}}`},
		})
		configureSpawnFixture(t, &f)
		f.env["HERDR_SOHO_LANES"] = "on"
		f.roster(t, laneReuseRow("review-2"))
		code, stdout, stderr, calls := runCmdSpawn(t, f, []string{"reviewer", "--name", "review-2"})
		if code != 0 {
			t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
		}
		if renameCalls(calls) != 0 {
			t.Fatalf("unexpected rename calls: %#v", calls)
		}
		if !strings.Contains(stdout, `"name": "review-2"`) {
			t.Fatalf("JSON=%q", stdout)
		}
		want := "herdr-soho: warning: reusing idle lane 'review' worker 'review-2' as reviewer; its session already holds earlier briefs"
		if !strings.Contains(stderr, want) {
			t.Fatalf("stderr=%q want %q", stderr, want)
		}
	})

	t.Run("spawn: a failing rename dies 4 and leaves the roster untouched", func(t *testing.T) {
		// A4.1: herdr agent rename status != 0 -> exit 4, the cause in the
		// message, the roster line of the old name intact.
		f := newSpawnFixture(t, []fakecli.Rule{
			{Argv: []string{"agent", "get", "review-2"}, Stdout: `{"result":{"agent":{"name":"review-2","agent_status":"idle"}}}`},
			{Argv: []string{"agent", "rename", "w0test:p0a", "review"}, Stderr: `{"error":{"code":"agent_name_taken","message":"an agent named review already exists"}}`, Code: 1},
		})
		configureSpawnFixture(t, &f)
		f.env["HERDR_SOHO_LANES"] = "on"
		f.roster(t, laneReuseRow("review-2"))
		code, message, _ := runCmdSpawnExpectError(t, f, []string{"reviewer", "--name", "review"})
		want := "spawn: could not rename worker 'review-2' to 'review': agent_name_taken: an agent named review already exists"
		if code != 4 || message != want {
			t.Fatalf("exit=%d message=%q want %q", code, message, want)
		}
		roster, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(roster), "review-2\tw0test:p0a") || strings.Contains(string(roster), "review\tw0test:p0a") {
			t.Fatalf("roster changed after the failed rename: %q", roster)
		}
	})
}
