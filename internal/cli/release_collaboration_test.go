package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/collaboration"
	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// releaseCollaborationFixture builds a copiesFixture whose roster holds the
// two collaboration participants (plus, when withWorker, a plain third
// agent) and seeds a collaboration assignment under the state dir. run
// executes the release command through the real CLI entry (Run).
type releaseCollaborationFixture struct {
	f   *copiesFixture
	sd  string
	env platform.Env
	a   collaboration.Assignment
}

type agentGetOverride struct {
	stdout string
	stderr string
	code   int
}

// releaseCollabAuthorJSON is the live answer of `herdr agent get author`:
// the registration the identity check verifies (name, pane, kind,
// workspace, session, cwd) plus the agent_status the traditional release
// probe reads when the collaboration no longer blocks it.
const releaseCollabAuthorJSON = `{` +
	`"result":{"agent":{"name":"author","pane_id":"p-author","agent":"claude",` +
	`"agent_status":"working","workspace_id":"ws","cwd":"__REPO__",` +
	`"agent_session":{"kind":"pi","value":"s-1"}}}}`

func newReleaseCollaborationFixture(t *testing.T, withWorker bool, authorGet agentGetOverride) *releaseCollaborationFixture {
	t.Helper()
	f := newCopiesFixture(t, nil)
	repoJSON, err := json.Marshal(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	// The fake answers carry the fixture's repository dir as cwd; the script
	// is re-installed once that dir is known. The first install's binary is
	// a symlink to the test executable, so it is replaced, not rebuilt.
	rules := []fakecli.Rule{
		{Argv: []string{"agent", "get", "author"}, Stdout: strings.ReplaceAll(authorGet.stdout, `"__REPO__"`, string(repoJSON)), Stderr: authorGet.stderr, Code: authorGet.code},
		{Argv: []string{"agent", "get", "reviewer"}, Stdout: strings.ReplaceAll(`{"result":{"agent":{"name":"reviewer","pane_id":"p-reviewer","agent":"grok","workspace_id":"ws","cwd":"__REPO__","agent_session":{"kind":"pi","value":"s-2"}}}}`, `"__REPO__"`, string(repoJSON))},
		{Argv: []string{"agent", "get", "worker"}, Stdout: `{"result":{"agent":{"agent_status":"working"}}}`},
		{Argv: []string{"pane", "list", "--workspace", "ws"}, Stdout: `{"result":{"panes":[]}}`},
	}
	fakeDir := f.env["HERDR_SOHO_FAKECLI_CONFIG"]
	for _, entry := range []string{"herdr", "herdr.exe"} {
		if err := os.Remove(filepath.Join(fakeDir, entry)); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}
	if _, err := fakecli.Install(t, fakeDir, "herdr", rules); err != nil {
		t.Fatal(err)
	}
	sd := f.stateDir(t)
	rows := []string{
		fmtReleaseCollabRow("author", "p-author", "claude", "implementer", "anthropic", f.repo, "2026-10-04T12:00:00Z", "claude-sonnet-4-5"),
		fmtReleaseCollabRow("reviewer", "p-reviewer", "grok", "reviewer", "xai", f.repo, "2026-10-04T12:00:05Z", "grok-4"),
	}
	if withWorker {
		rows = append(rows, fmtReleaseCollabRow("worker", "p-worker", "claude", "implementer", "anthropic", f.repo, "2026-10-04T12:00:10Z", "claude-sonnet-4-5"))
	}
	f.writeRoster(t, rows...)
	return &releaseCollaborationFixture{f: f, sd: sd, env: f.env}
}

func fmtReleaseCollabRow(name, pane, kind, role, family, cwd, started, model string) string {
	return name + "\t" + pane + "\t" + kind + "\t" + role + "\t" + family + "\t\t" + cwd + "\t" + started + "\t" + model + "\t\t\t"
}

// seed registers a valid active collaboration (phase preparing) whose
// participants verify against the roster and the fake herdr.
func (rc *releaseCollaborationFixture) seed(t *testing.T) collaboration.Assignment {
	t.Helper()
	store := collaboration.Store{StateDir: rc.sd}
	created, err := store.Create(collaboration.Assignment{
		Author: collaboration.Participant{
			Name: "author", Pane: "p-author", Role: "implementer", Kind: "claude",
			Family: "anthropic", Started: "2026-10-04T12:00:00Z", Model: "claude-sonnet-4-5",
			Session: collaboration.Hash([]byte("pi\x00s-1")), Cwd: rc.f.repo,
		},
		Reviewer: collaboration.Participant{
			Name: "reviewer", Pane: "p-reviewer", Role: "reviewer", Kind: "grok",
			Family: "xai", Started: "2026-10-04T12:00:05Z", Model: "grok-4",
			Session: collaboration.Hash([]byte("pi\x00s-2")), Cwd: rc.f.repo,
		},
		Workspace: "ws",
		Root:      rc.f.repo,
		BriefHash: "0",
		Paths:     []string{rc.f.repo},
		MaxRounds: 3,
	})
	if err != nil {
		t.Fatalf("cannot seed the assignment: %v", err)
	}
	rc.a = created
	return created
}

// finish moves the seeded assignment to the finished phase.
func (rc *releaseCollaborationFixture) finish(t *testing.T) {
	t.Helper()
	store := collaboration.Store{StateDir: rc.sd}
	a, err := store.Update(rc.a.ID, rc.a.Generation, func(a *collaboration.Assignment) error {
		a.Phase = collaboration.Finished
		a.Verdict = "accept"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rc.a = a
}

// corruptAssignment overwrites the record file.
func (rc *releaseCollaborationFixture) corruptAssignment(t *testing.T, transform func(raw []byte) []byte) {
	t.Helper()
	path := filepath.Join(rc.sd, "collaboration", rc.a.ID, "assignment.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, transform(raw), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (rc *releaseCollaborationFixture) run(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	return rc.f.run(t, rc.env, argv...)
}

func (rc *releaseCollaborationFixture) herdrCalls(t *testing.T) []fakecli.Call {
	t.Helper()
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(rc.f.env["HERDR_SOHO_FAKECLI_CONFIG"], "herdr.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return []fakecli.Call{}
		}
		t.Fatal(err)
	}
	return calls
}

// assertNoPaneClosed proves the refusal happened before any pane close.
func assertNoPaneClosed(t *testing.T, calls []fakecli.Call) {
	t.Helper()
	for _, c := range calls {
		if len(c.Argv) >= 2 && c.Argv[0] == "pane" && c.Argv[1] == "close" {
			t.Fatalf("a pane was closed despite the active collaboration: %v", c.Argv)
		}
	}
}

// TestReleaseRefusesActiveCollaborationParticipant: a participant of an
// active assignment is refused with code 10 — the roster line and the
// assignment stay untouched, and no pane is closed.
func TestReleaseRefusesActiveCollaborationParticipant(t *testing.T) {
	rc := newReleaseCollaborationFixture(t, false, agentGetOverride{stdout: releaseCollabAuthorJSON})
	rc.seed(t)
	code, out, errOut := rc.run(t, "release", "author")
	if code != 10 {
		t.Fatalf("code=%d out=%q stderr=%q; want 10 while the assignment is active", code, out, errOut)
	}
	if !strings.Contains(errOut, "is a participant of active collaboration "+rc.a.ID) {
		t.Fatalf("stderr=%q; want the refusal to name the assignment", errOut)
	}
	if !strings.Contains(errOut, "collaborate stop "+rc.a.ID) || !strings.Contains(errOut, "collaborate finalize "+rc.a.ID+" --verdict accept|reject") {
		t.Fatalf("stderr=%q; want the explicit stop/finalize path named", errOut)
	}
	if core.RosterLine(rc.sd, "author") == "" {
		t.Fatal("the refusal must keep the roster line")
	}
	if _, err := os.Stat(filepath.Join(rc.sd, "collaboration", rc.a.ID, "assignment.json")); err != nil {
		t.Fatalf("the refusal must keep the assignment: %v", err)
	}
	assertNoPaneClosed(t, rc.herdrCalls(t))
}

// TestReleaseRefusesActiveCollaborationWithForce: --force never overrides
// the active-collaboration refusal — a release must not fake the cycle's
// outcome.
func TestReleaseRefusesActiveCollaborationWithForce(t *testing.T) {
	rc := newReleaseCollaborationFixture(t, false, agentGetOverride{stdout: releaseCollabAuthorJSON})
	rc.seed(t)
	code, out, errOut := rc.run(t, "release", "author", "--force")
	if code != 10 {
		t.Fatalf("code=%d out=%q stderr=%q; --force must not release an active participant", code, out, errOut)
	}
	if !strings.Contains(errOut, "is a participant of active collaboration "+rc.a.ID) {
		t.Fatalf("stderr=%q; want the active-collaboration refusal", errOut)
	}
	if core.RosterLine(rc.sd, "author") == "" {
		t.Fatal("--force must not remove the roster line of an active participant")
	}
	assertNoPaneClosed(t, rc.herdrCalls(t))
}

// TestReleaseRefusesActiveCollaborationWithClose: --close is refused before
// touching the pane.
func TestReleaseRefusesActiveCollaborationWithClose(t *testing.T) {
	rc := newReleaseCollaborationFixture(t, false, agentGetOverride{stdout: releaseCollabAuthorJSON})
	rc.seed(t)
	code, out, errOut := rc.run(t, "release", "author", "--close")
	if code != 10 {
		t.Fatalf("code=%d out=%q stderr=%q; want 10 before closing anything", code, out, errOut)
	}
	assertNoPaneClosed(t, rc.herdrCalls(t))
	if core.RosterLine(rc.sd, "author") == "" {
		t.Fatal("--close must not remove the roster line of an active participant")
	}
}

// TestReleaseCollaborationUnreadable: a registration that cannot be read, or
// whose participants cannot be verified, is code 4 with an inspect-first
// message — the release never proceeds.
func TestReleaseCollaborationUnreadable(t *testing.T) {
	validAuthor := agentGetOverride{stdout: releaseCollabAuthorJSON}
	t.Run("corrupt record", func(t *testing.T) {
		rc := newReleaseCollaborationFixture(t, false, validAuthor)
		rc.seed(t)
		rc.corruptAssignment(t, func(raw []byte) []byte { return append([]byte("{"), raw...) })
		code, out, errOut := rc.run(t, "release", "author")
		if code != 4 {
			t.Fatalf("code=%d out=%q stderr=%q; want 4", code, out, errOut)
		}
		if !strings.Contains(errOut, "the collaboration state of pane p-author is unreadable") {
			t.Fatalf("stderr=%q; want the unreadable-registration message", errOut)
		}
		if core.RosterLine(rc.sd, "author") == "" {
			t.Fatal("the release must not proceed on an unreadable registration")
		}
	})
	t.Run("participants unverifiable", func(t *testing.T) {
		rc := newReleaseCollaborationFixture(t, false, agentGetOverride{code: 1, stderr: `{"error":{"code":"agent_not_found","message":"agent target author not found"},"id":"cli:agent:get"`})
		rc.seed(t)
		code, out, errOut := rc.run(t, "release", "author")
		if code != 4 {
			t.Fatalf("code=%d out=%q stderr=%q; want 4", code, out, errOut)
		}
		if !strings.Contains(errOut, "cannot be verified") {
			t.Fatalf("stderr=%q; want the verification-failure message", errOut)
		}
		if core.RosterLine(rc.sd, "author") == "" {
			t.Fatal("the release must not proceed when the identity cannot be verified")
		}
	})
}

// TestReleaseProceedsWhenCollaborationFinished: a finished assignment (and a
// project with no collaboration at all) is transparent to release — the
// traditional behavior stands.
func TestReleaseProceedsWhenCollaborationFinished(t *testing.T) {
	validAuthor := agentGetOverride{stdout: releaseCollabAuthorJSON}
	t.Run("finished assignment", func(t *testing.T) {
		rc := newReleaseCollaborationFixture(t, false, validAuthor)
		rc.seed(t)
		rc.finish(t)
		code, out, errOut := rc.run(t, "release", "author")
		if code != 0 {
			t.Fatalf("code=%d out=%q stderr=%q; a finished assignment must not block the release", code, out, errOut)
		}
		if !strings.Contains(out, "released author") {
			t.Fatalf("out=%q; want the traditional release line", out)
		}
	})
	t.Run("no collaboration", func(t *testing.T) {
		rc := newReleaseCollaborationFixture(t, false, validAuthor)
		code, out, errOut := rc.run(t, "release", "author")
		if code != 0 {
			t.Fatalf("code=%d out=%q stderr=%q; without collaboration the release stands", code, out, errOut)
		}
		if !strings.Contains(out, "released author") {
			t.Fatalf("out=%q; want the traditional release line", out)
		}
	})
}

// TestReleaseCollaborationProceedsForNonParticipant: the refusal is scoped to the
// assignment's participants; a roster agent on another pane releases as
// usual while the collaboration stays active.
func TestReleaseCollaborationProceedsForNonParticipant(t *testing.T) {
	validAuthor := agentGetOverride{stdout: releaseCollabAuthorJSON}
	rc := newReleaseCollaborationFixture(t, true, validAuthor)
	rc.seed(t)
	code, out, errOut := rc.run(t, "release", "worker")
	if code != 0 {
		t.Fatalf("code=%d out=%q stderr=%q; a non-participant releases as usual", code, out, errOut)
	}
	if !strings.Contains(out, "released worker") {
		t.Fatalf("out=%q; want the traditional release line", out)
	}
	if _, err := os.Stat(filepath.Join(rc.sd, "collaboration", rc.a.ID, "assignment.json")); err != nil {
		t.Fatalf("releasing a non-participant must not touch the collaboration: %v", err)
	}
}
