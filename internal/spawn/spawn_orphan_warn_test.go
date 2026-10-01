package spawn

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// laneOrphanRules is freshSpawnRules with a live-agent list that holds an
// idle orphan on the lane's base name (build-2), a working one (build-3),
// and a workspace pane list that holds both of their panes.
func laneOrphanRules() []fakecli.Rule {
	rules := append([]fakecli.Rule{}, freshSpawnRules()...)
	for i := range rules {
		if len(rules[i].Argv) >= 2 && rules[i].Argv[0] == "agent" && rules[i].Argv[1] == "list" && !rules[i].ArgvPrefix {
			rules[i].Stdout = `{"result":{"agents":[{"name":"build-2","pane_id":"ws:p2","agent_status":"idle"},{"name":"build-3","pane_id":"ws:p3","agent_status":"working"}]}}`
		}
		if rules[i].ArgvPrefix && len(rules[i].Argv) >= 2 && rules[i].Argv[0] == "pane" && rules[i].Argv[1] == "list" {
			rules[i].Stdout = `{"result":{"panes":[{"pane_id":"ws:p2"},{"pane_id":"ws:p3"}]}}`
		}
	}
	return rules
}

func TestSpawnWarnsAboutIdleLaneOrphansBeforeOpeningThePane(t *testing.T) {
	// Mutation captured: re-adopting the orphan (or silently skipping the
	// warning) loses the operator's released idle worker again.
	f := newSpawnFixture(t, laneOrphanRules())
	configureSpawnFixture(t, &f)
	f.env["HERDR_PANE_ID"] = "p1"
	f.env["HERDR_SOHO_LANES"] = "on"
	f.ctx.Entries["lane_build_roles"] = core.ConfigEntry{Value: "worker", Source: "project"}
	f.ctx.Entries["lane_build_kind"] = core.ConfigEntry{Value: "grok", Source: "project"}
	f.ctx.Order = []string{"lane_build_roles", "lane_build_kind"}
	f.roster(t)
	code, stdout, stderr, calls := runCmdSpawn(t, f, []string{"worker"})
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.Contains(stdout, `"name": "build"`) || !strings.Contains(stdout, `"created_pane": true`) {
		t.Fatalf("the spawn did not proceed to a new pane:\n%s", stdout)
	}
	want := "herdr-soho: spawn: 'build-2' is idle outside the roster (ws:p2); close it with: herdr-soho release build-2 --close\n"
	if !strings.Contains(stderr, want) {
		t.Fatalf("missing the idle orphan warning:\n%s", stderr)
	}
	// The working orphan is not idle: no warning names it.
	if strings.Contains(stderr, "build-3") {
		t.Fatalf("the working orphan was warned as idle:\n%s", stderr)
	}
	// The orphan check runs before the pane is opened: its workspace pane
	// list read is logged before the pane split.
	split, list := -1, -1
	for i, call := range calls {
		if len(call.Argv) >= 2 && call.Argv[0] == "pane" && call.Argv[1] == "split" {
			if split < 0 {
				split = i
			}
		}
		if strings.Join(call.Argv, "\x00") == strings.Join([]string{"pane", "list", "--workspace", "ws"}, "\x00") {
			list = i
		}
	}
	if split < 0 || list < 0 || list > split {
		t.Fatalf("the orphan check did not run before the pane split (list=%d split=%d): %#v", list, split, calls)
	}
	// The orphan is not re-adopted: the roster holds only the new worker.
	rows := core.RosterRows(filepath.Join(f.state, "ws"))
	if len(rows) != 1 || !strings.HasPrefix(rows[0], "build\t") {
		t.Fatalf("the orphan was adopted into the roster: %#v", rows)
	}
}

func TestSpawnHasNoOrphanWarningWithoutIdleLaneOrphans(t *testing.T) {
	// Control: with no idle orphan on the lane's base name the spawn stderr
	// carries no orphan warning (the freshSpawnRules live list is empty).
	f := newSpawnFixture(t, freshSpawnRules())
	configureSpawnFixture(t, &f)
	f.env["HERDR_SOHO_LANES"] = "on"
	f.ctx.Entries["lane_build_roles"] = core.ConfigEntry{Value: "worker", Source: "project"}
	f.ctx.Entries["lane_build_kind"] = core.ConfigEntry{Value: "grok", Source: "project"}
	f.ctx.Order = []string{"lane_build_roles", "lane_build_kind"}
	f.roster(t)
	code, stdout, stderr, _ := runCmdSpawn(t, f, []string{"worker"})
	if code != 0 || !strings.Contains(stdout, `"created_pane": true`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stderr, "is idle outside the roster") {
		t.Fatalf("an orphan warning without an orphan:\n%s", stderr)
	}
}

func TestSpawnLanesOffDoesNotWarnAboutRoleNamedOrphans(t *testing.T) {
	// The warning is for lane workers only: with lanes=off a role-named idle
	// orphan outside the roster does not stop or name the spawn.
	rules := laneOrphanRules()
	for i := range rules {
		if len(rules[i].Argv) >= 2 && rules[i].Argv[0] == "agent" && rules[i].Argv[1] == "list" && !rules[i].ArgvPrefix {
			rules[i].Stdout = `{"result":{"agents":[{"name":"worker-2","pane_id":"ws:p2","agent_status":"idle"}]}}`
		}
	}
	f := newSpawnFixture(t, rules)
	configureSpawnFixture(t, &f)
	f.roster(t)
	code, stdout, stderr, _ := runCmdSpawn(t, f, []string{"worker"})
	if code != 0 || !strings.Contains(stdout, `"created_pane": true`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stderr, "is idle outside the roster") {
		t.Fatalf("lanes=off spawn warned about a role-named orphan:\n%s", stderr)
	}
}
