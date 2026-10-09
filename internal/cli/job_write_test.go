package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/job"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

const jobStartBrief = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","base":"main","modo":"worktree","maquina":"machine-a","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/cli"}],"equipe":{"lane.review.effort":"high"}}`

func (f *jobFix) setMachine(t *testing.T) {
	t.Helper()
	userFile := platform.UserConfigPath(platform.Current(), f.env)
	if err := os.MkdirAll(filepath.Dir(userFile), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "job_repos_root=" + f.repos + "\njob_orgs=example-org\nmachine_label=machine-a\n"
	if err := os.WriteFile(userFile, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *jobFix) setTeam(t *testing.T) {
	t.Helper()
	dir := filepath.Dir(platform.UserConfigPath(platform.Current(), f.env))
	if err := os.MkdirAll(filepath.Join(dir, "teams"), 0o700); err != nil {
		t.Fatal(err)
	}
	body := "panes=3\nlane.build.roles=implementer\nlane.review.roles=reviewer\n"
	if err := os.WriteFile(filepath.Join(dir, "teams", "default.conf"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *jobFix) briefFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "brief.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runStdin runs a command with the given stdin body.
func (f *jobFix) runStdin(t *testing.T, stdin string, args ...string) (int, string, string) {
	t.Helper()
	old := jobStdin
	jobStdin = strings.NewReader(stdin)
	t.Cleanup(func() { jobStdin = old })
	return f.run(t, args...)
}

func TestJobAckMonotonicAndFlagLimit(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	store := f.store(t, co)
	if _, err := store.Note("job-1", job.EventIn{Tipo: "decision", Resumo: "route it", Escopo: "global"}); err != nil {
		t.Fatal(err)
	}

	// The decision at seq 2 is acknowledged.
	code, out, errOut := f.run(t, "ack", "--id", "job-1", "--upto", "2")
	if code != 0 || errOut != "" || out != `{"id":"job-1","decisions_acked_seq":2,"decisions_pendentes":[]}`+"\n" {
		t.Fatalf("ack 2 code=%d out=%q err=%q", code, out, errOut)
	}
	// A lower or equal value is a no-op with exit 0.
	code, out, _ = f.run(t, "ack", "--id", "job-1", "--upto", "2")
	if code != 0 || out != `{"id":"job-1","decisions_acked_seq":2,"decisions_pendentes":[]}`+"\n" {
		t.Fatalf("noop ack code=%d out=%q", code, out)
	}
	if _, err := store.Note("job-1", job.EventIn{Tipo: "decision", Resumo: "second", Escopo: "projeto"}); err != nil {
		t.Fatal(err)
	}
	// A lower value leaves the pending decision listed (the ack itself
	// appended a decision_acked event, so this decision is at seq 4).
	code, out, _ = f.run(t, "ack", "--id", "job-1", "--upto", "1")
	if code != 0 || out != `{"id":"job-1","decisions_acked_seq":2,"decisions_pendentes":[4]}`+"\n" {
		t.Fatalf("lower ack code=%d out=%q", code, out)
	}
	code, out, _ = f.run(t, "ack", "--id", "job-1", "--upto", "4")
	if code != 0 || out != `{"id":"job-1","decisions_acked_seq":4,"decisions_pendentes":[]}`+"\n" {
		t.Fatalf("ack 4 code=%d out=%q", code, out)
	}

	for _, args := range [][]string{
		{"ack", "--id", "job-1", "--upto", "0"},
		{"ack", "--id", "job-1", "--upto", "-4"},
		{"ack", "--id", "job-1", "--upto", "abc"},
		{"ack", "--id", "job-1"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}
	code, out, _ = f.run(t, "ack", "--id", "nope", "--upto", "1")
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("unknown id code=%d out=%q", code, out)
	}
}

func TestJobClosePendingForceAndRepeat(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	store := f.store(t, co)
	jobDir := filepath.Join(co, ".herdr-soho", "jobs", "job-1")
	f.setStatus(t, co, "job-1", "done")
	if _, err := store.Note("job-1", job.EventIn{Tipo: "decision", Resumo: "route it", Escopo: "global"}); err != nil {
		t.Fatal(err)
	}

	// Pending decisions without --force: exit 24, the seqs listed on stdout.
	code, out, errOut := f.run(t, "close", "--id", "job-1")
	if code != 24 || errOut != "" || out != `{"id":"job-1","status":"done","decisions_pendentes":[2]}`+"\n" {
		t.Fatalf("pending code=%d out=%q err=%q", code, out, errOut)
	}

	// --force closes and records the cleanup event.
	code, out, errOut = f.run(t, "close", "--id", "job-1", "--force")
	if code != 0 || errOut != "" || out != `{"id":"job-1","status":"closed","decisions_pendentes":[2]}`+"\n" {
		t.Fatalf("force code=%d out=%q err=%q", code, out, errOut)
	}
	log, err := os.ReadFile(filepath.Join(jobDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(log), `"motivo":"force_sem_ack"`) {
		t.Fatalf("no force_sem_ack cleanup event:\n%s", log)
	}

	// A repeated close exits 0 with the same line.
	code, out, _ = f.run(t, "close", "--id", "job-1")
	if code != 0 || out != `{"id":"job-1","status":"closed","decisions_pendentes":[2]}`+"\n" {
		t.Fatalf("repeat code=%d out=%q", code, out)
	}

	// An acknowledged close leaves no pending decisions.
	brief2 := strings.Replace(jobTestBrief, `"job-1"`, `"job-2"`, 1)
	if _, err := store.Start("job-2", []byte(brief2)); err != nil {
		t.Fatal(err)
	}
	f.setStatus(t, co, "job-2", "done")
	if _, err := store.Note("job-2", job.EventIn{Tipo: "decision", Resumo: "route it", Escopo: "global"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Ack("job-2", 2); err != nil {
		t.Fatal(err)
	}
	code, out, _ = f.run(t, "close", "--id", "job-2")
	if code != 0 || out != `{"id":"job-2","status":"closed","decisions_pendentes":[]}`+"\n" {
		t.Fatalf("acked close code=%d out=%q", code, out)
	}

	// A non-terminal job is refused with the package's exit 2.
	brief3 := strings.Replace(jobTestBrief, `"job-1"`, `"job-3"`, 1)
	if _, err := store.Start("job-3", []byte(brief3)); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "close", "--id", "job-3")
	if code != 2 || out != "" || !strings.Contains(errOut, "not allowed from accepted") {
		t.Fatalf("non-terminal code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, _ = f.run(t, "close", "--id", "nope")
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("unknown id code=%d out=%q", code, out)
	}
}

func TestJobNoteTypesRefsAndLimits(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)

	code, out, errOut := f.run(t, "note", "--id", "job-1", "--tipo", "note", "hello")
	if code != 0 || errOut != "" {
		t.Fatalf("note code=%d err=%q", code, errOut)
	}
	var ev map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &ev); err != nil {
		t.Fatalf("event line %q: %v", out, err)
	}
	if ev["seq"] != float64(2) || ev["tipo"] != "note" || ev["resumo"] != "hello" || !reflect.DeepEqual(ev["refs"], map[string]any{}) {
		t.Fatalf("event = %#v", ev)
	}

	code, out, _ = f.run(t, "note", "--id", "job-1", "--tipo", "decision", "--escopo", "global", "--motivo", "why", "--refs", "pr=12,sha=abc", "route")
	if code != 0 {
		t.Fatalf("decision code=%d out=%q", code, out)
	}
	ev = map[string]any{}
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &ev); err != nil {
		t.Fatal(err)
	}
	if ev["seq"] != float64(3) || ev["tipo"] != "decision" || ev["escopo"] != "global" || ev["motivo"] != "why" {
		t.Fatalf("decision event = %#v", ev)
	}
	refs, _ := ev["refs"].(map[string]any)
	if refs["pr"] != "12" || refs["sha"] != "abc" {
		t.Fatalf("refs = %#v", ev["refs"])
	}

	for _, tipo := range []string{"question", "checkpoint"} {
		code, out, _ = f.run(t, "note", "--id", "job-1", "--tipo", tipo, "one")
		if code != 0 {
			t.Fatalf("%s code=%d out=%q", tipo, code, out)
		}
	}

	for _, args := range [][]string{
		{"note", "--id", "job-1", "--tipo", "bogus", "x"},
		{"note", "--id", "job-1", "x"},
		{"note", "--id", "job-1", "--tipo", "note"},
		{"note", "--id", "job-1", "--tipo", "note", "a", "b"},
		{"note", "--id", "job-1", "--tipo", "note", "x", "--refs", "noequals"},
		{"note", "--id", "job-1", "--tipo", "note", "x", "--refs", "=v"},
		{"note", "--id", "job-1", "--tipo", "note", "x", "--refs", "unknownkey=1"},
		{"note", "--id", "job-1", "--tipo", "note", "--escopo", "global", "x"},
	} {
		code, out, errOut := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, errOut)
		}
	}

	// A stdin summary within the 16 KiB cap is accepted, above it is exit 2.
	code, _, errOut = f.runStdin(t, strings.Repeat("a", 16<<10), "note", "--id", "job-1", "--tipo", "note", "-")
	if code != 0 || errOut != "" {
		t.Fatalf("stdin at cap code=%d err=%q", code, errOut)
	}
	code, out, _ = f.runStdin(t, strings.Repeat("a", 16<<10+1), "note", "--id", "job-1", "--tipo", "note", "-")
	if code != 2 || out != "" {
		t.Fatalf("stdin over cap code=%d out=%q", code, out)
	}
	code, out, _ = f.run(t, "note", "--id", "nope", "--tipo", "note", "x")
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("unknown id code=%d out=%q", code, out)
	}
}

func TestJobStartDryRun(t *testing.T) {
	f := newJobFix(t)
	f.setMachine(t)
	f.setTeam(t)
	brief := f.briefFile(t, jobStartBrief)

	code, out, errOut := f.run(t, "start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", brief, "--dry-run")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &got); err != nil {
		t.Fatalf("dry run %q: %v", out, err)
	}
	if got["status"] != "dry_run" || got["id"] != "job-1" || got["repo"] != "example-org/example-repo" {
		t.Fatalf("dry run = %#v", got)
	}
	if got["base"] != "main" || got["modo"] != "worktree" || got["timeout_min"] != float64(120) {
		t.Fatalf("base/modo/timeout = %#v", got)
	}
	if sha, _ := got["brief_sha256"].(string); len(sha) != 64 {
		t.Fatalf("brief_sha256 = %#v", got["brief_sha256"])
	}
	equipe, _ := got["equipe"].(map[string]any)
	if equipe["fonte"] != "teams/default.conf" || !reflect.DeepEqual(equipe["override_brief"], []any{"lane.review.effort"}) {
		t.Fatalf("equipe = %#v", got["equipe"])
	}

	// Flags override the brief, and a reordered-key brief hashes identically
	// (the hash is over the canonical form, not the raw bytes).
	reordered := `{"id":"job-1","repo":"example-org/example-repo","objetivo":"Ship the change.","schema":1,"base":"main","modo":"worktree","maquina":"machine-a","aceite":[{"criterio":"tests pass","prova":"go test ./internal/cli"}],"origem":{"tipo":"card","ref":"CARD-1"},"equipe":{"lane.review.effort":"high"}}`
	code, out, _ = f.run(t, "start", "--id", "job-1", "--repo", "example-org/example-repo", "--base", "feature/x", "--timeout", "90", "--brief", f.briefFile(t, reordered), "--dry-run")
	if code != 0 {
		t.Fatalf("flags code=%d out=%q", code, out)
	}
	got = map[string]any{}
	_ = json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &got)
	if got["base"] != "feature/x" || got["modo"] != "worktree" || got["timeout_min"] != float64(90) {
		t.Fatalf("flags = %#v", got)
	}
	var first map[string]any
	code, out, _ = f.run(t, "start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", brief, "--dry-run")
	if code != 0 {
		t.Fatalf("first rerun code=%d out=%q", code, out)
	}
	first = map[string]any{}
	_ = json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &first)
	if first["brief_sha256"] != got["brief_sha256"] {
		t.Fatalf("reordered brief hash differs: %v vs %v", first["brief_sha256"], got["brief_sha256"])
	}

	noBase := `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/cli"}]}`
	code, out, _ = f.run(t, "start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, noBase), "--dry-run")
	if code != 0 {
		t.Fatalf("no base code=%d out=%q", code, out)
	}
	if !strings.Contains(out, `"base":null,`) || !strings.Contains(out, `"modo":"worktree"`) {
		t.Fatalf("no base out=%q", out)
	}

	// Dry run writes no job state and creates no state root.
	if _, err := os.Stat(filepath.Join(f.repos, "example-org", "example-repo", ".herdr-soho")); !os.IsNotExist(err) {
		t.Fatalf("dry run created a state root: %v", err)
	}
}

func TestJobStartValidationRefusals(t *testing.T) {
	f := newJobFix(t)
	f.setMachine(t)
	f.setTeam(t)

	otherOrg := `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"other-org/example-repo","objetivo":"Ship.","aceite":[{"criterio":"tests","prova":"go test ./x"}]}`
	otherMachine := strings.Replace(jobStartBrief, `"machine-a"`, `"machine-b"`, 1)
	otherRepo := strings.Replace(jobStartBrief, `"repo":"example-org/example-repo"`, `"repo":"example-org/other-repo"`, 1)
	otherID := strings.Replace(jobStartBrief, `"id":"job-1"`, `"id":"job-9"`, 1)
	unknownKey := strings.Replace(jobStartBrief, `"equipe":{"lane.review.effort":"high"}`, `"equipe":{"bogus":"x"}`, 1)

	cases := []struct {
		name  string
		args  []string
		need  string
		stand string
	}{
		{name: "missing brief", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--dry-run"}},
		{name: "bad id", args: []string{"start", "--id", "bad id", "--repo", "example-org/example-repo", "--brief", "-", "--dry-run"}},
		{name: "bad repo", args: []string{"start", "--id", "job-1", "--repo", "noslash", "--brief", "-", "--dry-run"}},
		{name: "bad base", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--base", "a..b", "--brief", "-", "--dry-run"}},
		{name: "bad mode", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--mode", "bogus", "--brief", "-", "--dry-run"}},
		{name: "timeout 0", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--timeout", "0", "--brief", "-", "--dry-run"}},
		{name: "timeout 1441", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--timeout", "1441", "--brief", "-", "--dry-run"}},
		{name: "org out of scope", args: []string{"start", "--id", "job-1", "--repo", "other-org/example-repo", "--brief", f.briefFile(t, otherOrg), "--dry-run"}, need: "out of scope"},
		{name: "maquina mismatch", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, otherMachine), "--dry-run"}, need: "machine_label"},
		{name: "brief repo mismatch", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, otherRepo), "--dry-run"}},
		{name: "brief id mismatch", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, otherID), "--dry-run"}},
		{name: "unknown equipe key", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, unknownKey), "--dry-run"}, need: "unknown key"},
		{name: "non-dry", args: []string{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", f.briefFile(t, jobStartBrief)}, need: "only --dry-run is available in this build"},
	}
	for _, tc := range cases {
		code, out, errOut := f.run(t, tc.args...)
		if code != 2 || out != "" {
			t.Fatalf("%s: code=%d out=%q err=%q", tc.name, code, out, errOut)
		}
		if tc.need != "" && !strings.Contains(errOut, tc.need) {
			t.Fatalf("%s: err=%q", tc.name, errOut)
		}
	}

	// A stdin brief above the 256 KiB cap is exit 2.
	capCode, capOut, _ := f.runStdin(t, strings.Repeat("a", 256<<10+1), "start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", "-", "--dry-run")
	if capCode != 2 || capOut != "" {
		t.Fatalf("stdin over cap code=%d out=%q", capCode, capOut)
	}

	// No team configuration is exit 2.
	f2 := newJobFix(t)
	f2.setMachine(t)
	brief2 := f2.briefFile(t, jobStartBrief)
	noTeamCode, noTeamOut, noTeamErr := f2.run(t, "start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", brief2, "--dry-run")
	if noTeamCode != 2 || noTeamOut != "" || !strings.Contains(noTeamErr, "no team configuration") {
		t.Fatalf("no team code=%d out=%q err=%q", noTeamCode, noTeamOut, noTeamErr)
	}
}

func TestJobWriteBodiesAndStubLimits(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)

	big := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(big, []byte(strings.Repeat("x", 64<<10+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing.md")
	for _, args := range [][]string{
		{"amend", "--id", "job-1", big},
		{"amend", "--id", "job-1", missing},
		{"amend", "--id", "job-1"},
		{"amend", "--id", "job-1", "a", "b"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}
	// An unknown id is not_found before the refusal.
	code, out, _ := f.run(t, "amend", "--id", "nope", big)
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("amend nope code=%d out=%q", code, out)
	}
	// A send stdin above the 16 KiB cap is exit 2; a small one refuses.
	code, out, _ = f.runStdin(t, strings.Repeat("x", 16<<10+1), "send", "--id", "job-1", "-")
	if code != 2 || out != "" {
		t.Fatalf("send over cap code=%d out=%q", code, out)
	}
	code, out, _ = f.runStdin(t, "hi", "send", "--id", "job-1")
	if code != 2 || out != "" {
		t.Fatalf("send no body code=%d out=%q", code, out)
	}
	code, out, _ = f.runStdin(t, "hi", "send", "--id", "nope", "-")
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("send nope code=%d out=%q", code, out)
	}

	for _, args := range [][]string{
		{"cancel", "--id", "job-1", "--grace", "-1"},
		{"cancel", "--id", "job-1", "--grace", "3601"},
		{"cancel", "--id", "job-1", "--grace", "abc"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}
	for _, args := range [][]string{
		{"cancel", "--id", "nope"},
		{"checkpoint", "--id", "nope"},
		{"supervise", "--id", "nope"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 3 || out != `{"status":"not_found"}`+"\n" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}
}

func TestJobNowriteWritingSubcommands(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	env := platform.Env{}
	for name, value := range f.env {
		env[name] = value
	}
	env["HERDR_SOHO_NOWRITE"] = "1"

	cases := [][]string{
		{"start", "--id", "job-1", "--repo", "example-org/example-repo", "--brief", "-", "--dry-run"},
		{"ack", "--id", "job-1", "--upto", "1"},
		{"close", "--id", "job-1"},
		{"close", "--id", "job-1", "--force"},
		{"note", "--id", "job-1", "--tipo", "note", "text"},
		{"amend", "--id", "job-1", "-"},
		{"send", "--id", "job-1", "-"},
		{"cancel", "--id", "job-1"},
		{"cancel", "--id", "job-1", "--grace", "5"},
		{"checkpoint", "--id", "job-1"},
		{"supervise", "--id", "job-1"},
	}
	tree := treeSnapshot(t, f.root)
	for _, args := range cases {
		code, out, errOut := f.runEnv(t, env, args...)
		want := "herdr-soho: HERDR_SOHO_NOWRITE=1 is read-only: job " + args[0] + " writes; only job status, wait, events, collect and list run\n"
		if code != 2 || out != "" || errOut != want {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
	assertJobTreeUnchanged(t, tree, treeSnapshot(t, f.root), "NOWRITE writing subcommands changed the tree")
}

// TestJobAmendBoundedFileRead proves the file branch of the body reads
// stops at cap+1 bytes: a regular file at the cap is accepted and cap+1 is
// exit 2, and a FIFO holding more than the cap with the writer still open
// (no EOF) returns the over-cap refusal within 2 s instead of blocking.
func TestJobAmendBoundedFileRead(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)

	// A regular file at exactly the cap is accepted (the read stops within
	// the cap); one byte more is exit 2 with the limit message.
	atCap := filepath.Join(t.TempDir(), "at-cap.md")
	if err := os.WriteFile(atCap, []byte(strings.Repeat("x", jobAmendBodyLimit)), 0o600); err != nil {
		t.Fatal(err)
	}
	// Phase 2 adaptation: an accepted body now queues the amend request (exit 0,
	// one status line) instead of the phase 1 "not available yet" refusal.
	code, out, errOut := f.run(t, "amend", "--id", "job-1", atCap)
	if code != 0 || !strings.Contains(out, `"id":"job-1"`) || errOut != "" {
		t.Fatalf("at cap: code=%d out=%q err=%q", code, out, errOut)
	}
	overCap := filepath.Join(t.TempDir(), "over-cap.md")
	if err := os.WriteFile(overCap, []byte(strings.Repeat("x", jobAmendBodyLimit+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "amend", "--id", "job-1", overCap)
	if code != 2 || out != "" || !strings.Contains(errOut, "exceeds 64 KiB") {
		t.Fatalf("over cap: code=%d out=%q err=%q", code, out, errOut)
	}

	// A FIFO with more than the cap and the write end held open never yields
	// EOF: an unbounded read blocks, a bounded one must exit 2 within 2 s.
	// POSIX only.
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs are POSIX-only")
	}
	fifo := filepath.Join(t.TempDir(), "body.fifo")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mkfifo", fifo)
	cmd.WaitDelay = 2 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v: %s", err, out)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		writer, err := os.OpenFile(fifo, os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer writer.Close()
		data := make([]byte, jobAmendBodyLimit+2)
		for written := 0; written < len(data); {
			n, werr := writer.Write(data[written:])
			written += n
			if werr != nil {
				break // EPIPE: the reader stopped at the cap (bounded path)
			}
		}
		time.Sleep(3 * time.Second) // keep the write end open: no EOF arrives
	}()
	type amendResult struct {
		code        int
		out, errOut string
	}
	ch := make(chan amendResult, 1)
	go func() {
		code, out, errOut := f.run(t, "amend", "--id", "job-1", fifo)
		ch <- amendResult{code, out, errOut}
	}()
	select {
	case r := <-ch:
		if r.code != 2 || r.out != "" || !strings.Contains(r.errOut, "exceeds 64 KiB") {
			t.Fatalf("fifo: code=%d out=%q err=%q", r.code, r.out, r.errOut)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("job amend on a FIFO over the cap with the writer open did not exit within 2s")
	}
}

func TestJobCleanLeavesPendingJobAlone(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	state := filepath.Join(co, ".herdr-soho")
	store := f.store(t, co)
	if _, err := store.Note("job-1", job.EventIn{Tipo: "decision", Resumo: "route it", Escopo: "global"}); err != nil {
		t.Fatal(err)
	}
	if skip, err := store.OlderThanCleanSkips("job-1"); err != nil || !skip {
		t.Fatalf("OlderThanCleanSkips = %v, %v", skip, err)
	}

	// Age the whole state root so --older-than 0 would delete everything
	// clean prunes.
	old := time.Now().AddDate(0, 0, -30)
	if err := os.MkdirAll(filepath.Join(state, "wait"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(state, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(path, old, old)
	}); err != nil {
		t.Fatal(err)
	}
	jobDir := filepath.Join(state, "jobs", "job-1")
	before, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := os.ReadFile(filepath.Join(jobDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}

	fakeDir := t.TempDir()
	if _, err := fakecli.Install(t, fakeDir, "herdr", []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[]}}`}}); err != nil {
		t.Fatal(err)
	}
	env := platform.Env{}
	for name, value := range f.env {
		env[name] = value
	}
	env = withFakeCLI(env, fakeDir)
	env["HERDR_ENV"] = "1"
	env["HERDR_WORKSPACE_ID"] = "ws"
	env["HERDR_SOHO_DIR"] = state
	var cleanOut, cleanErr bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &cleanOut, &cleanErr
	code := Run([]string{"clean", "--older-than", "0"}, env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	if code != 0 || cleanErr.String() != "" || !strings.Contains(cleanOut.String(), "removed 0 files older than 0 days under "+state) {
		t.Fatalf("code=%d out=%q err=%q", code, cleanOut.String(), cleanErr.String())
	}
	after, err := os.ReadFile(filepath.Join(jobDir, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := os.ReadFile(filepath.Join(jobDir, "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) || !bytes.Equal(eventsBefore, eventsAfter) {
		t.Fatal("clean changed the pending job's files")
	}
	info, err := os.Stat(filepath.Join(jobDir, "state.json"))
	if err != nil || info == nil || !info.ModTime().Equal(old) {
		t.Fatalf("state.json mtime changed: %v (%v)", info, err)
	}
	if skip, err := store.OlderThanCleanSkips("job-1"); err != nil || !skip {
		t.Fatalf("after clean: OlderThanCleanSkips = %v, %v", skip, err)
	}
}
