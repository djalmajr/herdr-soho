package plugin

// DJA-375 B: the Team view's two presentation forms (tree and table)
// over the CLI's JSON reads, the v switch, the frame chrome and the
// BoardState's persisted form preference. The fixtures are the brief's
// roster and 13 roles; the expected lines are the brief's examples
// (width 80 tree, width 100 table) and the width-40 cuts and wraps.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// The brief's roster fixture (workspace w6Y, one team agent and one
// outside pane, the settings line's values).
const teamViewRosterFixture = `{"workspace":"w6Y","scope":"workspace","settings":{"layout":"split","reuse_workers":"on","multi_role":"on","auto_approve":"off"},"agents":[{"name":"build","role":"implementer","previous_role":"","kind":"pi","pane":"w6Y:p2","tab":"1","state":"working","report":"pending","cwd":"/x/nau","task":"Brief — wake hook"}],"others":[{"name":"orchestrator-4","kind":"claude","pane":"w6Y:p1","tab":"1","state":"idle","workspace":"w6Y"}]}`

// The same roster without any agent (the empty-AGENTS cases).
const teamViewRosterEmptyFixture = `{"workspace":"w6Y","scope":"workspace","settings":{"layout":"split","reuse_workers":"on","multi_role":"on","auto_approve":"off"},"agents":[],"others":[]}`

// The brief's 13 roles: role, kind, model, effort, mode, lane,
// kind_from, model_from, effort_from.
var teamViewRoleFixture = [][9]string{
	{"designer", "pi", "lbvllm/qwen3.8-27b", "xhigh", "edit", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"documenter", "pi", "lbvllm/qwen3.8-27b", "xhigh", "edit", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"implementer", "pi", "lbvllm/qwen3.8-27b", "xhigh", "edit", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"inspector", "codex", "gpt-6.1-sol", "low", "read-only", "review", "lane review (project)", "lane review (project)", "lane review (project)"},
	{"job-orchestrator", "claude", "opus", "high", "edit", "", "role file", "model.claude.worker (defaults)", "role file"},
	{"planner", "claude", "fable", "high", "read-only", "", "role file", "role config (defaults)", "role file"},
	{"researcher", "pi", "lbvllm/qwen3.8-27b", "xhigh", "read-only", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"reviewer", "codex", "gpt-6.1-sol", "low", "read-only", "review", "lane review (project)", "lane review (project)", "lane review (project)"},
	{"scouter", "pi", "lbvllm/qwen3.8-27b", "xhigh", "read-only", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"security-reviewer", "codex", "gpt-6.1-sol", "low", "read-only", "review", "lane review (project)", "lane review (project)", "lane review (project)"},
	{"sub-orchestrator", "claude", "fable", "medium", "read-only", "", "role file", "model.claude.orchestrator (defaults)", "role file"},
	{"tasker", "pi", "lbvllm/qwen3.8-27b", "xhigh", "edit", "build", "lane build (project)", "lane build (project)", "lane build (project)"},
	{"ui-reviewer", "codex", "gpt-6.1-sol", "low", "read-only", "review", "lane review (project)", "lane review (project)", "lane review (project)"},
}

// teamViewRolesJSON builds the roles read's JSON for the given roles.
func teamViewRolesJSON(roles [][9]string) string {
	items := make([]map[string]string, 0, len(roles))
	for _, r := range roles {
		items = append(items, map[string]string{
			"role": r[0], "kind": r[1], "model": r[2], "effort": r[3], "mode": r[4],
			"lane": r[5], "kind_from": r[6], "model_from": r[7], "effort_from": r[8],
			"file": "/x/nau/" + r[0] + ".md",
		})
	}
	data, err := json.Marshal(struct {
		Roles []map[string]string `json:"roles"`
	}{items})
	if err != nil {
		panic(err)
	}
	return string(data)
}

var teamViewRolesFixture = teamViewRolesJSON(teamViewRoleFixture)

// teamViewNauEntry is the picker's row for the render tests: the local
// workspace whose label is "nau" (the brief's title line).
func teamViewNauEntry(t *testing.T, cwd string) PickerEntry {
	t.Helper()
	return PickerEntry{
		"ref": "local/w6Y:p2", "machine": "local",
		"workspace_id": "w6Y", "workspace_label": "nau",
		"tab_id": "w6Y:t1", "tab_label": "1", "pane_id": "p2",
		"name": "build", "kind": "pi", "status": "working",
		"cwd": cwd,
	}
}

func teamViewNauPicker(t *testing.T, cwd string) *PickerState {
	t.Helper()
	s := NewPickerState()
	s.ViewState.Enabled = true
	s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
	return s
}

// teamViewJSONRules is the fake CLI's rules for the view's two JSON
// reads: a failed read answers exit 1 with its cause on stderr.
func teamViewJSONRules(t *testing.T, rosterStdout string, rosterFail bool, rolesStdout string, rolesFail bool) []fakecli.Rule {
	t.Helper()
	roster := fakecli.Rule{Argv: []string{"roster", "--json"}}
	if rosterFail {
		roster.Stderr, roster.Code = "roster boom", 1
	} else {
		roster.Stdout = rosterStdout
	}
	roles := fakecli.Rule{Argv: []string{"roles", "--json"}}
	if rolesFail {
		roles.Stderr, roles.Code = "roles boom", 1
	} else {
		roles.Stdout = rolesStdout
	}
	return []fakecli.Rule{roster, roles}
}

// teamViewPickerLoaded opens the picker's team view on the nau row and
// applies the finished load from the given fake outputs.
func teamViewPickerLoaded(t *testing.T, rosterStdout string, rosterFail bool, rolesStdout string, rolesFail bool) *PickerState {
	t.Helper()
	cwd := t.TempDir()
	env, cli, _ := pickerTeamFixture(t, teamViewJSONRules(t, rosterStdout, rosterFail, rolesStdout, rolesFail))
	s := teamViewNauPicker(t, cwd)
	if action := s.ApplyKey("i"); action != "" {
		t.Fatalf("i on the list action=%q want empty", action)
	}
	pickerTeamLoadAndApply(t, s, env, cli)
	return s
}

// teamViewBoardLoaded opens the board's team view on the nau row and
// applies the finished load from the given fake outputs.
func teamViewBoardLoaded(t *testing.T, rosterStdout string, rosterFail bool, rolesStdout string, rolesFail bool) *BoardState {
	t.Helper()
	cwd := t.TempDir()
	env, cli, _ := pickerTeamFixture(t, teamViewJSONRules(t, rosterStdout, rosterFail, rolesStdout, rolesFail))
	s := NewBoardState()
	s.ViewState.Enabled = true
	s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
	if action := s.ApplyKey("i"); action != "team" {
		t.Fatalf("i on the list action=%q want team", action)
	}
	boardTeamLoadAndApply(t, s, env, cli)
	return s
}

var teamViewSGRRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// teamViewPlainLines strips the SGR styles of the view's unscrolled
// content at avail display columns: the frame's window lines without
// the pad, the way a terminal prints them.
func teamViewPlainLines(t *testing.T, v *pickerTeamView, avail int) []string {
	t.Helper()
	lines := pickerTeamContent(v, avail)
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = teamViewSGRRe.ReplaceAllString(line, "")
	}
	return out
}

func teamViewContentWant(t *testing.T, s *PickerState, avail int, want []string) {
	t.Helper()
	got := teamViewPlainLines(t, s.TeamView, avail)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("content lines=%#v\nwant=%#v", got, want)
	}
}

func frameLines(frame string) []string {
	return strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
}

// The brief's examples, unstyled: the width-80 tree (avail 79) and the
// width-100 table (avail 99, the long FROM lines cut by teamCut, which
// cuts without ellipsis).
var teamViewTree79Want = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS (2)",
	"  build           implementer  pi      w6Y:p2  working  Brief — wake hook",
	"  orchestrator-4  -            claude  w6Y:p1  idle     not in the team",
	"",
	"LANE build  pi · lbvllm/qwen3.8-27b · xhigh",
	"  edit       designer, documenter, implementer, tasker",
	"  read-only  researcher, scouter",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer, security-reviewer, ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · high",
	"  planner           claude · fable · high  the orchestrator",
	"  sub-orchestrator  claude · fable · medium",
}

var teamViewTable99Want = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS",
	"NAME            ROLE         KIND    PANE    STATE    REPORT   TASK",
	"build           implementer  pi      w6Y:p2  working  pending  Brief — wake hook",
	"orchestrator-4  -            claude  w6Y:p1  idle     -        not in the team",
	"",
	"ROLES",
	"ROLE               KIND    MODEL               EFFORT  MODE       FROM",
	"designer           pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"documenter         pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"implementer        pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"inspector          codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"job-orchestrator   claude  opus                high    edit       kind: role file; model: model.cla",
	"planner            claude  fable               high    read-only  kind: role file; model: role conf",
	"researcher         pi      lbvllm/qwen3.8-27b  xhigh   read-only  lane build (project)",
	"reviewer           codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"scouter            pi      lbvllm/qwen3.8-27b  xhigh   read-only  lane build (project)",
	"security-reviewer  codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"sub-orchestrator   claude  fable               medium  read-only  kind: role file; model: model.cla",
	"tasker             pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"ui-reviewer        codex   gpt-6.1-sol         low     read-only  lane review (project)",
}

// The job-orchestrator row before the cut (139 display columns): the
// cut line must be its true prefix (teamCut cuts without ellipsis).
const teamViewJobOrchestratorRowFull = "job-orchestrator   claude  opus                high    edit       kind: role file; model: model.claude.worker (defaults); effort: role file"

// The width-40 forms (avail 39): in the tree the role lists wrap with
// the continuation at the first role's column and every other line is
// cut; in the table no line wraps (every line is cut).
var teamViewTree39Want = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi",
	"",
	"AGENTS (2)",
	"  build           implementer  pi      ",
	"  orchestrator-4  -            claude  ",
	"",
	"LANE build  pi · lbvllm/qwen3.8-27b · x",
	"  edit       designer, documenter",
	"             implementer, tasker",
	"  read-only  researcher, scouter",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer",
	"             security-reviewer",
	"             ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · hig",
	"  planner           claude · fable · hi",
	"  sub-orchestrator  claude · fable · me",
}

var teamViewTable39Want = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi",
	"",
	"AGENTS",
	"NAME            ROLE         KIND    PA",
	"build           implementer  pi      w6",
	"orchestrator-4  -            claude  w6",
	"",
	"ROLES",
	"ROLE               KIND    MODEL       ",
	"designer           pi      lbvllm/qwen3",
	"documenter         pi      lbvllm/qwen3",
	"implementer        pi      lbvllm/qwen3",
	"inspector          codex   gpt-6.1-sol ",
	"job-orchestrator   claude  opus        ",
	"planner            claude  fable       ",
	"researcher         pi      lbvllm/qwen3",
	"reviewer           codex   gpt-6.1-sol ",
	"scouter            pi      lbvllm/qwen3",
	"security-reviewer  codex   gpt-6.1-sol ",
	"sub-orchestrator   claude  fable       ",
	"tasker             pi      lbvllm/qwen3",
	"ui-reviewer        codex   gpt-6.1-sol ",
}

// A lane build role with another effort: the lane stops being uniform,
// its header loses the chain and every role gets one OUTSIDE-LANES
// format line plus its mode.
var teamViewMixedLane79Want = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS (2)",
	"  build           implementer  pi      w6Y:p2  working  Brief — wake hook",
	"  orchestrator-4  -            claude  w6Y:p1  idle     not in the team",
	"",
	"LANE build",
	"  designer     pi · lbvllm/qwen3.8-27b · xhigh  edit",
	"  documenter   pi · lbvllm/qwen3.8-27b · xhigh  edit",
	"  implementer  pi · lbvllm/qwen3.8-27b · high  edit",
	"  researcher   pi · lbvllm/qwen3.8-27b · xhigh  read-only",
	"  scouter      pi · lbvllm/qwen3.8-27b · xhigh  read-only",
	"  tasker       pi · lbvllm/qwen3.8-27b · xhigh  edit",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer, security-reviewer, ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · high",
	"  planner           claude · fable · high  the orchestrator",
	"  sub-orchestrator  claude · fable · medium",
}

// The empty-AGENTS cases and the failure blocks (the failed call's
// block is its one cause line; the other block keeps its content).
var teamViewEmptyAgentsTreeWant = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS (0)",
	"  no agent in this workspace",
	"",
	"LANE build  pi · lbvllm/qwen3.8-27b · xhigh",
	"  edit       designer, documenter, implementer, tasker",
	"  read-only  researcher, scouter",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer, security-reviewer, ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · high",
	"  planner           claude · fable · high  the orchestrator",
	"  sub-orchestrator  claude · fable · medium",
}

var teamViewEmptyAgentsTableWant = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS",
	"NAME  ROLE  KIND  PANE  STATE  REPORT  TASK",
	"no agent in this workspace",
	"",
	"ROLES",
	"ROLE               KIND    MODEL               EFFORT  MODE       FROM",
	"designer           pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"documenter         pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"implementer        pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"inspector          codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"job-orchestrator   claude  opus                high    edit       kind: role file; model: model.cla",
	"planner            claude  fable               high    read-only  kind: role file; model: role conf",
	"researcher         pi      lbvllm/qwen3.8-27b  xhigh   read-only  lane build (project)",
	"reviewer           codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"scouter            pi      lbvllm/qwen3.8-27b  xhigh   read-only  lane build (project)",
	"security-reviewer  codex   gpt-6.1-sol         low     read-only  lane review (project)",
	"sub-orchestrator   claude  fable               medium  read-only  kind: role file; model: model.cla",
	"tasker             pi      lbvllm/qwen3.8-27b  xhigh   edit       lane build (project)",
	"ui-reviewer        codex   gpt-6.1-sol         low     read-only  lane review (project)",
}

var teamViewRosterInvalidTreeWant = []string{
	"TEAM  nau · local",
	"",
	"roster: invalid output",
	"",
	"LANE build  pi · lbvllm/qwen3.8-27b · xhigh",
	"  edit       designer, documenter, implementer, tasker",
	"  read-only  researcher, scouter",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer, security-reviewer, ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · high",
	"  planner           claude · fable · high  the orchestrator",
	"  sub-orchestrator  claude · fable · medium",
}

var teamViewRosterFailureTreeWant = []string{
	"TEAM  nau · local",
	"",
	"roster: roster failed (exit 1): roster boom",
	"",
	"LANE build  pi · lbvllm/qwen3.8-27b · xhigh",
	"  edit       designer, documenter, implementer, tasker",
	"  read-only  researcher, scouter",
	"",
	"LANE review  codex · gpt-6.1-sol · low",
	"  read-only  inspector, reviewer, security-reviewer, ui-reviewer",
	"",
	"OUTSIDE LANES",
	"  job-orchestrator  claude · opus · high",
	"  planner           claude · fable · high  the orchestrator",
	"  sub-orchestrator  claude · fable · medium",
}

var teamViewRolesInvalidTreeWant = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS (2)",
	"  build           implementer  pi      w6Y:p2  working  Brief — wake hook",
	"  orchestrator-4  -            claude  w6Y:p1  idle     not in the team",
	"",
	"roles: invalid output",
}

var teamViewRolesFailureTreeWant = []string{
	"TEAM  nau · local",
	"layout split · reuse workers on · multi-role on · auto-approve off",
	"",
	"AGENTS (2)",
	"  build           implementer  pi      w6Y:p2  working  Brief — wake hook",
	"  orchestrator-4  -            claude  w6Y:p1  idle     not in the team",
	"",
	"roles: roles failed (exit 1): roles boom",
}

func TestTeamViewTreeMatchesTheBriefExampleAtWidth80(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	teamViewContentWant(t, s, 79, teamViewTree79Want)
}

func TestTeamViewTableMatchesTheBriefExampleAtWidth100(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	s.ApplyKey("v") // the brief's table example
	teamViewContentWant(t, s, 99, teamViewTable99Want)
	// The ROLES table is complete (all 13 roles) and the long FROM
	// lines are cut at the width by the teamCut (a true prefix, no
	// ellipsis, no wrap).
	lines := teamViewPlainLines(t, s.TeamView, 99)
	if got := lines[len(lines)-1]; !strings.Contains(got, "ui-reviewer") {
		t.Fatalf("the ROLES table lost its last role: %q", got)
	}
	job := ""
	for _, line := range lines {
		if strings.HasPrefix(line, "job-orchestrator") {
			job = line
			break
		}
	}
	if job == "" {
		t.Fatalf("the ROLES table lost the job-orchestrator row:\n%#v", lines)
	}
	if displayWidth(job) != 99 || !strings.HasPrefix(teamViewJobOrchestratorRowFull, job) || strings.Contains(job, "…") {
		t.Fatalf("job-orchestrator row=%q width=%d want the 99-column cut of %q", job, displayWidth(job), teamViewJobOrchestratorRowFull)
	}
}

func TestTeamViewWidth40TreeWrapsRoleListsAndCutsTheRest(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	teamViewContentWant(t, s, 39, teamViewTree39Want)
}

func TestTeamViewWidth40TableNeverWraps(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	s.ApplyKey("v")
	got := teamViewPlainLines(t, s.TeamView, 39)
	if !reflect.DeepEqual(got, teamViewTable39Want) {
		t.Fatalf("content lines=%#v\nwant=%#v", got, teamViewTable39Want)
	}
	for i, line := range got {
		if w := displayWidth(line); w > 39 {
			t.Fatalf("line %d is %d columns (wrapped or over the width): %q", i, w, line)
		}
	}
}

func TestTeamViewLaneWithDifferentRolesShowsOneLinePerRole(t *testing.T) {
	mixed := make([][9]string, len(teamViewRoleFixture))
	copy(mixed, teamViewRoleFixture)
	for i, r := range mixed {
		if r[0] == "implementer" {
			mixed[i][3] = "high" // a different effort breaks the lane's uniformity
		}
	}
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesJSON(mixed), false)
	teamViewContentWant(t, s, 79, teamViewMixedLane79Want)
}

func TestTeamViewEmptyAgentsTreeShowsThePlaceholder(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterEmptyFixture, false, teamViewRolesFixture, false)
	teamViewContentWant(t, s, 79, teamViewEmptyAgentsTreeWant)
}

func TestTeamViewEmptyAgentsTableShowsTheHeaderAndMessage(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterEmptyFixture, false, teamViewRolesFixture, false)
	s.ApplyKey("v")
	teamViewContentWant(t, s, 99, teamViewEmptyAgentsTableWant)
}

func TestTeamViewNoOutsideRolesDropsTheOutsideSection(t *testing.T) {
	// Every role gets a lane: the OUTSIDE LANES section is gone.
	laned := make([][9]string, len(teamViewRoleFixture))
	copy(laned, teamViewRoleFixture)
	for i, r := range laned {
		if r[5] == "" {
			laned[i][5] = "ops"
		}
	}
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesJSON(laned), false)
	lines := teamViewPlainLines(t, s.TeamView, 79)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, "OUTSIDE LANES") {
		t.Fatalf("the OUTSIDE LANES section survived with no outside role:\n%s", joined)
	}
	if !strings.Contains(joined, "LANE ops") {
		t.Fatalf("the laned roles are missing the LANE ops section:\n%s", joined)
	}
}

func TestTeamViewInvalidRosterJSONShowsTheCauseAndKeepsTheRoles(t *testing.T) {
	s := teamViewPickerLoaded(t, "not json", false, teamViewRolesFixture, false)
	teamViewContentWant(t, s, 79, teamViewRosterInvalidTreeWant)
}

func TestTeamViewFailedRosterCallShowsItsCauseAndKeepsTheRoles(t *testing.T) {
	s := teamViewPickerLoaded(t, "", true, teamViewRolesFixture, false)
	teamViewContentWant(t, s, 79, teamViewRosterFailureTreeWant)
}

func TestTeamViewInvalidRolesJSONShowsTheCauseAndKeepsTheAgents(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, "not json", false)
	teamViewContentWant(t, s, 79, teamViewRolesInvalidTreeWant)
}

func TestTeamViewFailedRolesCallShowsItsCauseAndKeepsTheAgents(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, "", true)
	teamViewContentWant(t, s, 79, teamViewRolesFailureTreeWant)
}

func TestTeamViewFrameShowsTheFormAndTheViewFooter(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	frameWant := func(form string, seg string) {
		frame := renderSessionPopup(pickerTeamViewOptions(s, ""), 80, 24)
		lines := frameLines(frame)
		if len(lines) != 24 {
			t.Fatalf("the frame is %d lines want 24:\n%s", len(lines), frame)
		}
		plain := make([]string, len(lines))
		for i, line := range lines {
			plain[i] = teamViewSGRRe.ReplaceAllString(line, "")
		}
		if plain[0] != " "+form {
			t.Fatalf("the header line %q want %q", plain[0], " "+form)
		}
		if strings.TrimSpace(plain[1]) != "" {
			t.Fatalf("the search row is not blank: %q", plain[1])
		}
		if plain[19] != " ↑↓ Scroll · PgUp/PgDn Page" || plain[20] != " "+seg || plain[21] != " i/Esc Back · Ctrl-C Close" {
			t.Fatalf("footer lines=%#v want the view's three segments", plain[19:22])
		}
	}
	// The open view defaults to the tree.
	frameWant("Team · Tree", "v Table")
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v with the view open action=%q want empty", action)
	}
	frameWant("Team · Table", "v Tree")
	s.ApplyKey("v") // the switch is a toggle
	frameWant("Team · Tree", "v Table")
	// A short height keeps the list's footer cut: the last segments win.
	s2 := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	frame := renderSessionPopup(pickerTeamViewOptions(s2, ""), 80, 7)
	lines := frameLines(frame)
	if len(lines) != 7 {
		t.Fatalf("the frame is %d lines want 7:\n%s", len(lines), frame)
	}
	if strings.Contains(frame, "↑↓ Scroll") {
		t.Fatalf("the short frame kept the navigation segment:\n%s", frame)
	}
	if !strings.Contains(frame, "i/Esc Back · Ctrl-C Close") {
		t.Fatalf("the short frame lost the back segment:\n%s", frame)
	}
}

func TestTeamViewVSwitchesTheFormAndResetsTheScroll(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	v := s.TeamView
	if v == nil || !v.loaded {
		t.Fatalf("view=%+v want loaded", v)
	}
	s.ApplyKey("down")
	s.ApplyKey("down")
	if v.scroll != 2 {
		t.Fatalf("scroll=%d want 2", v.scroll)
	}
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v action=%q want empty (the view stays open)", action)
	}
	if v.scroll != 0 {
		t.Fatalf("v left scroll=%d want 0", v.scroll)
	}
	if frame := renderSessionPopup(pickerTeamViewOptions(s, ""), 80, 24); !strings.Contains(frame, "Team · Table") {
		t.Fatalf("v did not switch to the table:\n%s", frame)
	}
	s.ApplyKey("v")
	if frame := renderSessionPopup(pickerTeamViewOptions(s, ""), 80, 24); !strings.Contains(frame, "Team · Tree") {
		t.Fatalf("v did not switch back to the tree:\n%s", frame)
	}
	// The view's other keys keep their behavior.
	if action := s.ApplyKey("enter"); action != "" || s.NavPending {
		t.Fatalf("enter with the view open action=%q navPending=%v want nothing", action, s.NavPending)
	}
	if action := s.ApplyKey("c"); action != "" || s.Copied != nil {
		t.Fatalf("c with the view open action=%q copied=%v want nothing", action, s.Copied)
	}
	if s.ApplyKey("esc") != "" || s.TeamView != nil {
		t.Fatalf("esc exit=%q view=%v want the list back", s.Exit, s.TeamView)
	}
	// In the list, v is the list's view switch (unchanged).
	s.ApplyKey("v")
	if s.ViewState.View != sessionViewTree {
		t.Fatalf("list v chose view %q want the tree", s.ViewState.View)
	}
}

func TestTeamViewPickerKeepsTheFormForTheRun(t *testing.T) {
	s := teamViewPickerLoaded(t, teamViewRosterFixture, false, teamViewRolesFixture, false)
	s.ApplyKey("v") // table
	if s.ApplyKey("esc") != "" || s.TeamView != nil {
		t.Fatalf("esc exit=%q view=%v want the list back", s.Exit, s.TeamView)
	}
	s.ApplyKey("i") // re-open the same row
	if frame := renderSessionPopup(pickerTeamViewOptions(s, ""), 80, 24); !strings.Contains(frame, "Team · Table") || !strings.Contains(frame, "loading team…") {
		t.Fatalf("the re-open lost the run's table form:\n%s", frame)
	}
}

func TestTeamViewBoardPersistsTheForm(t *testing.T) {
	cfg := t.TempDir()
	cwd := t.TempDir()
	envCLI, cli, _ := pickerTeamFixture(t, teamViewJSONRules(t, teamViewRosterFixture, false, teamViewRolesFixture, false))
	state := func() *BoardState {
		s := newSessionState(platform.Env{"XDG_CONFIG_HOME": cfg}, "darwin", true)
		s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
		return s
	}
	s := state()
	if action := s.ApplyKey("i"); action != "team" {
		t.Fatalf("i action=%q want team", action)
	}
	boardTeamLoadAndApply(t, s, envCLI, cli)
	if frame := renderSessionModal(s, 80, 24, ""); !strings.Contains(frame, "Team · Tree") {
		t.Fatalf("the board opened the view without the saved (default) tree form:\n%s", frame)
	}
	path := filepath.Join(cfg, "soho", "team-view")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("the open wrote the form preference: %v", err)
	}
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v action=%q", action)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "table\n" {
		t.Fatalf("saved form %q: %v", data, err)
	}
	// Back and re-open: the saved form opens the view again.
	s.ApplyKey("esc")
	if action := s.ApplyKey("i"); action != "team" {
		t.Fatalf("re-open action=%q want team", action)
	}
	if frame := renderSessionModal(s, 80, 24, ""); !strings.Contains(frame, "Team · Table") {
		t.Fatalf("the re-open lost the saved table form:\n%s", frame)
	}
	// A new state reads the same file.
	next := state()
	if action := next.ApplyKey("i"); action != "team" {
		t.Fatalf("new open action=%q want team", action)
	}
	if frame := renderSessionModal(next, 80, 24, ""); !strings.Contains(frame, "Team · Table") {
		t.Fatalf("a new open lost the saved table form:\n%s", frame)
	}
}

func TestTeamViewBoardFormReadFallsBackToTree(t *testing.T) {
	for _, raw := range []string{"xyz\n", "", "landscape", strings.Repeat("x", sessionViewPreferenceLimit+1)} {
		t.Run(fmt.Sprintf("%q", raw[:min(len(raw), 12)]), func(t *testing.T) {
			cfg := t.TempDir()
			path := filepath.Join(cfg, "soho", "team-view")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			cwd := t.TempDir()
			s := newSessionState(platform.Env{"XDG_CONFIG_HOME": cfg}, "darwin", true)
			s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
			if action := s.ApplyKey("i"); action != "team" {
				t.Fatalf("i action=%q want team", action)
			}
			if frame := renderSessionModal(s, 80, 24, ""); !strings.Contains(frame, "Team · Tree") {
				t.Fatalf("raw %q did not fall back to the tree:\n%s", raw, frame)
			}
		})
	}
	// A valid table file opens the table.
	cfg := t.TempDir()
	path := filepath.Join(cfg, "soho", "team-view")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("table\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	s := newSessionState(platform.Env{"XDG_CONFIG_HOME": cfg}, "darwin", true)
	s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
	s.ApplyKey("i")
	if frame := renderSessionModal(s, 80, 24, ""); !strings.Contains(frame, "Team · Table") {
		t.Fatalf("a saved table did not open the table:\n%s", frame)
	}
}

func TestTeamViewBoardFormSaveFailureKeepsTheSwitch(t *testing.T) {
	cfg := t.TempDir()
	// A regular file where the config directory would be: the write
	// cannot create the directory (the list's save-failure rule).
	if err := os.WriteFile(filepath.Join(cfg, "soho"), []byte("owned blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	envCLI, cli, _ := pickerTeamFixture(t, teamViewJSONRules(t, teamViewRosterFixture, false, teamViewRolesFixture, false))
	s := newSessionState(platform.Env{"XDG_CONFIG_HOME": cfg}, "darwin", true)
	s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
	if action := s.ApplyKey("i"); action != "team" {
		t.Fatalf("i action=%q want team", action)
	}
	boardTeamLoadAndApply(t, s, envCLI, cli)
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v action=%q", action)
	}
	if !s.teamViewPreferenceFailed {
		t.Fatal("the failed form save was not recorded")
	}
	if s.viewPreferenceFailed {
		t.Fatal("the team-view's failed save took the list's preference flag")
	}
	if frame := renderSessionModal(s, 80, 24, ""); !strings.Contains(frame, "Team · Table") {
		t.Fatalf("the save failure blocked the switch:\n%s", frame)
	}
	if s.ApplyKey("esc") != "" || s.TeamView != nil {
		t.Fatalf("esc exit=%q view=%v want the list back", s.Exit, s.TeamView)
	}
}

// DJA-375 R2: the review's two findings - the team-view's save failure
// must not leak the list's "could not be saved" message, and every line
// the tree and the table produce fits the width for any avail >= 1
// (the review's width-1 case included), with the frame exactly as tall
// as asked and no line past the width.

// teamViewHostileFixture is the review's hostile cells: an agent whose
// name carries an SGR sequence and whose role/task carry control
// characters, plus the review's wide role (界😀abc, mode read-only, in a
// lane) on top of the brief's fixtures.
func teamViewHostileFixture() (*pickerTeamView, *pickerTeamView) {
	roles := make([]teamViewRole, 0, len(teamViewRoleFixture)+1)
	for _, r := range teamViewRoleFixture {
		roles = append(roles, teamViewRole{
			Role: r[0], Kind: r[1], Model: r[2], Effort: r[3], Mode: r[4],
			Lane: r[5], KindFrom: r[6], ModelFrom: r[7], EffortFrom: r[8],
			File: "/x/nau/" + r[0] + ".md",
		})
	}
	roles = append(roles, teamViewRole{Role: "界😀abc", Lane: "wide", Mode: "read-only"})
	roster := &teamViewRoster{
		Workspace: "w6Y",
		Settings:  teamViewRosterSettings{Layout: "split", ReuseWorkers: "on", MultiRole: "on", AutoApprove: "off"},
		Agents: []teamViewRosterAgent{
			{Name: "build", Role: "implementer", Kind: "pi", Pane: "w6Y:p2", State: "working", Report: "pending", Task: "Brief — wake hook"},
			{Name: "界😀\x1b[31mX", Role: "r\n", Kind: "pi", Pane: "w6Y:p3", State: "working", Report: "pending", Task: "task\t"},
		},
		Others: []teamViewRosterOther{{Name: "orchestrator-4", Kind: "claude", Pane: "w6Y:p1", State: "idle", Workspace: "w6Y"}},
	}
	view := func(form string) *pickerTeamView {
		return &pickerTeamView{
			loaded:         true,
			roster:         roster,
			roles:          &teamViewRoles{Roles: roles},
			machine:        "local",
			workspaceLabel: "nau",
			form:           form,
		}
	}
	return view(teamFormTree), view(teamFormTable)
}

// TestTeamViewEveryLineFitsTheWidthFromOneTo120 is the review's width
// finding: for every avail from 1 to 120, in both forms, every line of
// pickerTeamContent has at most avail display columns (styles excluded)
// and nothing panics. It includes the review's case (width 1, mode
// read-only, role 界😀abc).
func TestTeamViewEveryLineFitsTheWidthFromOneTo120(t *testing.T) {
	for _, form := range []string{teamFormTree, teamFormTable} {
		v, _ := teamViewHostileFixture()
		v.form = form
		for w := 1; w <= 120; w++ {
			for _, line := range pickerTeamContent(v, w) {
				if got := displayWidth(StripPickerControls(line)); got > w {
					t.Fatalf("form=%s width=%d line has %d columns: %q", form, w, got, line)
				}
			}
		}
	}
}

// TestTeamViewReviewWidthOneCase pins the review's exact example: the
// mode read-only with the role 界😀abc at width 1 gives one line of at
// most 1 column (the prefix cut to the width, the roles after it, cut -
// never the 2-column overflow of the old hard-cut floor).
func TestTeamViewReviewWidthOneCase(t *testing.T) {
	v, _ := teamViewHostileFixture()
	lines := pickerTeamContent(v, 1)
	for i, line := range lines {
		if got := displayWidth(StripPickerControls(line)); got > 1 {
			t.Fatalf("width=1 line %d has %d columns: %q", i, got, line)
		}
	}
}

// TestTeamViewFrameFitsItsWidthAtTinyWidths is the review's frame
// finding: a renderPickerTeamFrame with width between 6 and 20 is
// exactly the asked height, and no line goes past the width.
func TestTeamViewFrameFitsItsWidthAtTinyWidths(t *testing.T) {
	tree, table := teamViewHostileFixture()
	for _, v := range []*pickerTeamView{tree, table} {
		s := NewPickerState()
		s.ViewState.Enabled = true
		s.TeamView = v
		o := pickerTeamViewOptions(s, "")
		for w := 6; w <= 20; w++ {
			for _, h := range []int{8, 18, 24} {
				frame := renderPickerTeamFrame(&o, w, h)
				lines := strings.Split(strings.TrimSuffix(frame, "\n"), "\n")
				if len(lines) != h {
					t.Fatalf("form=%s width=%d height=%d: the frame has %d lines:\n%s", v.form, w, h, len(lines), frame)
				}
				for i, line := range lines {
					if got := displayWidth(StripPickerControls(line)); got > w {
						t.Fatalf("form=%s width=%d height=%d: line %d has %d columns: %q", v.form, w, h, i, got, line)
					}
				}
			}
		}
	}
}

// TestTeamViewFormSaveFailureIsSilentInTheList is the review's
// silent-failure finding: with the config directory blocked, v in the
// team view and Esc leave the list without the "could not be saved"
// message (that message is the list's own preference's), the switch
// happened, and the list's preference failure still shows its message.
func TestTeamViewFormSaveFailureIsSilentInTheList(t *testing.T) {
	cfg := t.TempDir()
	// A regular file where the config directory would be: the write
	// cannot create the directory (the list's save-failure rule).
	if err := os.WriteFile(filepath.Join(cfg, "soho"), []byte("owned blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	envCLI, cli, _ := pickerTeamFixture(t, teamViewJSONRules(t, teamViewRosterFixture, false, teamViewRolesFixture, false))
	s := newSessionState(platform.Env{"XDG_CONFIG_HOME": cfg}, "darwin", true)
	s.Entries = []PickerEntry{teamViewNauEntry(t, cwd)}
	if action := s.ApplyKey("i"); action != "team" {
		t.Fatalf("i action=%q want team", action)
	}
	boardTeamLoadAndApply(t, s, envCLI, cli)
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v action=%q", action)
	}
	if got := s.TeamView.form; got != teamFormTable {
		t.Fatalf("v form=%q want table (the switch happened)", got)
	}
	if !s.teamViewPreferenceFailed {
		t.Fatal("the failed form save was not recorded in the view's own flag")
	}
	if s.viewPreferenceFailed {
		t.Fatal("the team-view's failed save took the list's preference flag")
	}
	if s.ApplyKey("esc") != "" || s.TeamView != nil {
		t.Fatalf("esc exit=%q view=%v want the list back", s.Exit, s.TeamView)
	}
	var b bytes.Buffer
	redrawSession(&b, s, 100)
	if strings.Contains(b.String(), "could not be saved") {
		t.Fatalf("the list shows the save message after the team toggle and Esc:\n%s", b.String())
	}
	// The list's own preference failure still shows its message.
	s.ViewState.View = sessionViewTree
	s.rememberSessionView()
	if !s.viewPreferenceFailed {
		t.Fatal("the list's failed save was not recorded")
	}
	b.Reset()
	redrawSession(&b, s, 100)
	if !strings.Contains(b.String(), "could not be saved") {
		t.Fatalf("the list's preference failure lost its message:\n%s", b.String())
	}
}

// TestTeamViewToggleBeforeLoadKeepsTheForm is the review's guard: v
// before the load applies still takes the finished result, and the form
// the switch chose is kept.
func TestTeamViewToggleBeforeLoadKeepsTheForm(t *testing.T) {
	s := NewPickerState()
	s.TeamView = &pickerTeamView{loadSeq: 1, entryRef: "x", form: teamFormTree}
	if action := s.ApplyKey("v"); action != "" {
		t.Fatalf("v action=%q want empty", action)
	}
	if !s.applyPickerTeamView(pickerTeamViewResult{seq: 1, entryRef: "x", roster: &teamViewRoster{}, roles: &teamViewRoles{}}) {
		t.Fatal("the finished load was not applied after the pre-load switch")
	}
	if got := s.TeamView.form; got != teamFormTable {
		t.Fatalf("form=%q want table kept after the load", got)
	}
}

// A mode prefix wider than the line only because of a wide character
// still makes one cut line: the prefix is measured before any cut (a
// cut that stops before the wide character leaves it narrower than the
// width, which used to send the roles to the wrap, one piece per line).
func TestTeamViewWideModePrefixStaysOneLine(t *testing.T) {
	v := &pickerTeamView{loaded: true, roster: &teamViewRoster{}, roles: &teamViewRoles{Roles: []teamViewRole{{Role: "abc", Lane: "wide", Mode: "界"}}}}
	got := teamViewRolesBlock(v, 3)
	want := teamCut("  界  abc", 3)
	if len(got) != 2 || got[1] != want {
		t.Fatalf("want the lane header and one cut mode line %q; got %q", want, got)
	}
}
