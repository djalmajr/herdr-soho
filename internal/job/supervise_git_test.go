package job

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// supervise_git_test.go exercises the supervisor's git sync and the final
// release against a local bare remote, real git, fake gh, the fake team and
// the fake Herdr: the commit / push / pr_opened events on the 30 s tick
// cadence, the rejected push that blocks and the later push that unblocks,
// and the terminal path that checkpoints, pushes, publishes the complete
// report, releases, cleans up and closes the workspace last. The crash,
// retry and clock matrix cases drive restarted supervisors over the same
// store.

const (
	supGitID     = prepID
	supGitBranch = gitSyncBranch
	supGitPRBody = "Job: " + supGitID + "\nOrigin: CARD-1"
	supGitPRJSON = `[{"number":7,"url":"` + gitSyncPRURL + `","isDraft":true}]`
)

var supGitBrief = []byte(`{"schema":1,"id":"` + supGitID + `","origem":{"tipo":"card","ref":"CARD-1"},"repo":"` + gitSyncRepo + `","base":"main","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`)

var supGitGhRules = []fakecli.Rule{
	{Argv: ghListArgv(), Stdout: "[]\n"},
	{Argv: ghCreateArgv("feat: first", supGitPRBody), Stdout: "Creating pull request\n" + gitSyncPRURL + "\n"},
}

// crashPRRecord is the sentinel panic of the crash between the push and the
// pr_opened record.
var crashPRRecord = errors.New("crash between push and PR record")

// crashBeforeWorkspaceClose is the sentinel panic of the crash after the
// cleanup event and before the workspace close.
var crashBeforeWorkspaceClose = errors.New("crash before the workspace close")

// supGitFixture wires the job store and a supervisor over the git sync
// fixture: the real job worktree on job/<id>, the fake team, the fake
// Herdr and the shared fake clock.
type supGitFixture struct {
	job      *jobSyncFixture
	store    *Store
	jobDir   string
	clock    *fakeClock
	team     *fakeTeam
	herdr    *fakeHerdr
	friction *[]string
	sup      *Supervisor
}

func newSupGitFixture(t *testing.T, ghRules []fakecli.Rule) *supGitFixture {
	t.Helper()
	job := newJobSyncFixture(t, ghRules)
	root := t.TempDir()
	clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
	store := Open(root)
	store.Clock = clock
	if _, err := store.Start(supGitID, supGitBrief); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(supGitID, StatusPreparing); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Record(supGitID, func(st *State) {
		st.Dir = job.dir
		st.Branch = supGitBranch
		st.Base = "main"
		st.StartedAt = formatTS(clock.Now())
		st.Orchestrator = "orch-1"
		st.WorkspaceID = "ws-1"
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Transition(supGitID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Append(supGitID, EventIn{
		Tipo:   "worker_spawned",
		Resumo: "job orchestrator spawned",
		Refs:   map[string]string{"agente": "orch-1", "papel": "job-orchestrator"},
	}); err != nil {
		t.Fatal(err)
	}
	team := &fakeTeam{spawnName: "orch-1"}
	herdr := &fakeHerdr{}
	var friction []string
	sup := &Supervisor{
		Store:    store,
		ID:       supGitID,
		Ops:      team,
		Clock:    clock,
		Git:      &GitSync{Env: job.base.env, Dir: job.dir, Branch: supGitBranch, Base: "main", Repo: gitSyncRepo},
		Herdr:    herdr,
		Friction: func(message string) { friction = append(friction, message) },
	}
	return &supGitFixture{
		job: job, store: store, jobDir: filepath.Join(root, "jobs", supGitID),
		clock: clock, team: team, herdr: herdr, friction: &friction, sup: sup,
	}
}

// sup is a restarted supervisor over the fixture store: fresh in-memory
// sync bookkeeping, the same team, Herdr and friction sink.
func (f *supGitFixture) supRestarted(t *testing.T) *Supervisor {
	t.Helper()
	return &Supervisor{
		Store:    f.store,
		ID:       supGitID,
		Ops:      f.team,
		Clock:    f.clock,
		Git:      f.sup.Git,
		Herdr:    f.herdr,
		Friction: f.sup.Friction,
	}
}

// tickN drives the watch loop for n ticks with the poll sleep between them,
// the way Run does between orchestrator reads; every scripted observation
// is working.
func (f *supGitFixture) tickN(t *testing.T, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		f.team.statusScript = append(f.team.statusScript, fakeStatus{State: "working"})
		code, done, err := f.sup.tick()
		if err != nil || done {
			t.Fatalf("tick %d: code=%d done=%v err=%v", i+1, code, done, err)
		}
		f.sup.clockSleep(f.sup.poll())
	}
}

func (f *supGitFixture) events(t *testing.T) []Event {
	t.Helper()
	events, err := readEvents(f.jobDir)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func (f *supGitFixture) tipes(t *testing.T) []string {
	t.Helper()
	return eventTipes(f.events(t))
}

func (f *supGitFixture) state(t *testing.T) State {
	t.Helper()
	snap, err := f.store.Snapshot(supGitID)
	if err != nil {
		t.Fatal(err)
	}
	return snap.State
}

func countTipes(events []Event, tipo string) int {
	n := 0
	for _, event := range events {
		if event.Tipo == tipo {
			n++
		}
	}
	return n
}

func (f *supGitFixture) report(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(f.jobDir, "report.json"))
	if err != nil {
		t.Fatalf("read report.json: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("report.json is not a JSON object: %v", err)
	}
	return doc
}

func reportSlice(t *testing.T, doc map[string]any, key string) []map[string]any {
	t.Helper()
	raw, ok := doc[key].([]any)
	if !ok {
		t.Fatalf("report.json %s = %v, want a list", key, doc[key])
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		object, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("report.json %s item = %v, want an object", key, item)
		}
		out = append(out, object)
	}
	return out
}

func TestSuperviseGit(t *testing.T) {
	t.Run("a new commit is announced and pushed and opens the draft pull request", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		sha := f.job.commitIn(t, "feat: first")
		f.tickN(t, 6) // the 30 s cadence at the 5 s default poll

		want := []string{"accepted", "worker_spawned", "commit", "push", "pr_opened"}
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		events := f.events(t)
		commit := events[2]
		if commit.Resumo != "feat: first" || commit.Refs["sha"] != sha {
			t.Fatalf("commit event = %+v, want the sha and the title resumo", commit)
		}
		push := events[3]
		if push.Resumo != "pushed" || push.Refs["sha"] != sha {
			t.Fatalf("push event = %+v, want pushed with the head sha", push)
		}
		pr := events[4]
		if pr.Resumo != "draft pull request opened" || pr.Refs["pr"] != "7" {
			t.Fatalf("pr_opened event = %+v", pr)
		}
		if got := f.job.remoteRef(t); got != sha {
			t.Fatalf("remote ref = %q, want the local head %q", got, sha)
		}
		// Exactly one draft pull request: the lookup before the create.
		calls := f.job.base.ghCalls()
		if len(calls) != 2 {
			t.Fatalf("gh calls = %d (%v), want the list and the create", len(calls), calls)
		}
		if !sameArgv(calls[0].Argv, ghListArgv()) || !sameArgv(calls[1].Argv, ghCreateArgv("feat: first", supGitPRBody)) {
			t.Fatalf("gh argv = %v", calls)
		}
		if st := f.state(t); st.Status != StatusRunning {
			t.Fatalf("state = %+v, want running", st)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a failed commit listing does not hide the commits from the next sync", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		base := f.sup.Git.Base
		f.sup.Git.Base = "no-such-base"
		if _, _, err := f.sup.syncGitOnce(); err != nil {
			t.Fatalf("sync with a broken base: %v", err)
		}
		if n := countTipes(f.events(t), "commit"); n != 0 {
			t.Fatalf("commit events = %d, want none while the listing fails", n)
		}
		f.sup.Git.Base = base
		if _, _, err := f.sup.syncGitOnce(); err != nil {
			t.Fatalf("sync: %v", err)
		}
		if n := countTipes(f.events(t), "commit"); n != 1 {
			t.Fatalf("commit events = %d, want the commit announced once the listing works", n)
		}
		if n := countTipes(f.events(t), "pr_opened"); n != 1 {
			t.Fatalf("pr_opened events = %d, want the draft pull request", n)
		}
	})

	t.Run("a second commit pushes without another pull request", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		f.tickN(t, 6)
		sha2 := f.job.commitIn(t, "feat: second")
		f.tickN(t, 6)

		want := []string{"accepted", "worker_spawned", "commit", "push", "pr_opened", "commit", "push"}
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		if n := countTipes(f.events(t), "pr_opened"); n != 1 {
			t.Fatalf("pr_opened events = %d, want 1", n)
		}
		if got := f.job.remoteRef(t); got != sha2 {
			t.Fatalf("remote ref = %q, want the second head %q", got, sha2)
		}
		// The pull request is known from the event: no new gh call.
		calls := f.job.base.ghCalls()
		if len(calls) != 2 {
			t.Fatalf("gh calls = %d (%v), want the original two", len(calls), calls)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("pushNow syncs on the next tick", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		f.tickN(t, 1)
		// One tick is not the cadence: nothing is announced yet.
		if got := f.tipes(t); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned"}) {
			t.Fatalf("event types after one tick = %v, want no sync", got)
		}
		sha2 := f.job.commitIn(t, "feat: second")
		f.sup.pushNow = true
		f.tickN(t, 1)

		// The sync ran on the pushNow tick: both commits are announced,
		// the head is pushed and the draft pull request is opened.
		want := []string{"accepted", "worker_spawned", "commit", "commit", "push", "pr_opened"}
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		if got := f.job.remoteRef(t); got != sha2 {
			t.Fatalf("remote ref = %q, want the second head %q", got, sha2)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a rejected push blocks the job and the later push unblocks it", func(t *testing.T) {
		ghRules := []fakecli.Rule{
			{Argv: ghListArgv(), Stdout: "[]\n"},
			{Argv: ghCreateArgv("other", supGitPRBody), Stdout: "Creating pull request\n" + gitSyncPRURL + "\n"},
		}
		f := newSupGitFixture(t, ghRules)
		// Another clone advances the remote job branch past the local
		// head, so the local push is a non-fast-forward.
		other := filepath.Join(f.job.base.root, "other")
		for _, args := range [][]string{
			{"clone", "-q", f.job.base.bare, other},
		} {
			if out, err := f.job.base.git(f.job.base.root, args...); err != nil {
				t.Fatalf("setup git %v: %s: %v", args, out, err)
			}
		}
		for _, args := range [][]string{
			{"checkout", "-q", "-b", supGitBranch, "refs/remotes/origin/main"},
			{"commit", "-q", "--allow-empty", "-m", "other"},
			{"push", "-q", "origin", supGitBranch},
		} {
			if out, err := f.job.base.git(other, args...); err != nil {
				t.Fatalf("setup git %v in the second clone: %s: %v", args, out, err)
			}
		}
		remoteSHA := f.job.remoteRef(t)
		f.job.commitIn(t, "local")
		if remoteSHA == "" {
			t.Fatal("the second clone did not advance the remote job branch")
		}

		// The first sync: the push is rejected and the running job is
		// blocked for push_rejected; the remote ref never moves (no force).
		f.tickN(t, 6)
		if got := f.job.remoteRef(t); got != remoteSHA {
			t.Fatalf("remote ref = %q, want the second clone's head %q (a force push)", got, remoteSHA)
		}
		st := f.state(t)
		if st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != "push_rejected" {
			t.Fatalf("state = %+v, want blocked for push_rejected", st)
		}
		want := []string{"accepted", "worker_spawned", "commit", "push", "blocked"}
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		push := f.events(t)[3]
		if push.Refs["motivo"] != "push_rejected" {
			t.Fatalf("rejected push event = %+v, want refs.motivo push_rejected", push)
		}

		// The next sync retries normally: rejected again, one more push
		// event, no second blocked event, still no force.
		f.tickN(t, 6)
		if got := f.job.remoteRef(t); got != remoteSHA {
			t.Fatalf("remote ref = %q, want %q (the retry must not force)", got, remoteSHA)
		}
		if n := countTipes(f.events(t), "blocked"); n != 1 {
			t.Fatalf("blocked events = %d, want 1", n)
		}
		if n := countTipes(f.events(t), "push"); n != 2 {
			t.Fatalf("push events = %d, want 2", n)
		}

		// The orchestrator integrates the remote head and commits, the
		// way the job would; the next sync pushes fast-forward and lifts
		// the block this path set.
		for _, args := range [][]string{
			{"fetch", "-q", "origin"},
			{"merge", "-q", "--no-edit", "refs/remotes/origin/" + supGitBranch},
		} {
			if out, err := f.job.base.git(f.job.dir, args...); err != nil {
				t.Fatalf("setup git %v in the job worktree: %s: %v", args, out, err)
			}
		}
		mergeSHA := f.job.head(t)
		f.tickN(t, 6)
		if got := f.job.remoteRef(t); got != mergeSHA {
			t.Fatalf("remote ref = %q, want the merged head %q", got, mergeSHA)
		}
		if st := f.state(t); st.Status != StatusRunning || st.Motivo != nil {
			t.Fatalf("state = %+v, want running with the motivo cleared", st)
		}
		// The merge brought the other clone's commit into the branch: it
		// and the merge itself are announced (the local one was
		// already), the push lands, the block lifts, and the pull request
		// opens with the first (oldest) commit's title.
		want = append(want, "push", "commit", "commit", "push", "unblocked", "pr_opened")
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a successful push does not lift a block with another motivo", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		sha := f.job.commitIn(t, "feat: first")
		motivo := "other"
		if _, err := f.store.Transition(supGitID, StatusBlocked); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Record(supGitID, func(st *State) { st.Motivo = &motivo }); err != nil {
			t.Fatal(err)
		}
		f.tickN(t, 6)
		if got := f.job.remoteRef(t); got != sha {
			t.Fatalf("remote ref = %q, want the pushed head %q", got, sha)
		}
		// The push succeeded, but the block with the other motivo stays
		// blocked, with its motivo, and no unblocked event lands.
		if st := f.state(t); st.Status != StatusBlocked || st.Motivo == nil || *st.Motivo != "other" {
			t.Fatalf("state = %+v, want still blocked with the other motivo", st)
		}
		if n := countTipes(f.events(t), "unblocked"); n != 0 {
			t.Fatalf("unblocked events = %d, want none", n)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("the 30 s cadence counts ticks, not wall clock, with a backwards clock", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.clock.rewind = true // every sleep moves the wall clock backwards
		nowBefore := f.clock.Now()
		sha := f.job.commitIn(t, "feat: first")

		// Five ticks: the wall clock goes backwards five times, and the
		// cadence still has one tick to go.
		f.tickN(t, 5)
		if got := f.tipes(t); !reflect.DeepEqual(got, []string{"accepted", "worker_spawned"}) {
			t.Fatalf("event types after five ticks = %v, want no sync", got)
		}
		if got := f.job.remoteRef(t); got != "" {
			t.Fatalf("remote ref = %q, want nothing pushed before the sixth tick", got)
		}
		f.tickN(t, 1)

		if got := f.job.remoteRef(t); got != sha {
			t.Fatalf("remote ref = %q, want the head %q pushed on the sixth tick", got, sha)
		}
		want := []string{"accepted", "worker_spawned", "commit", "push", "pr_opened"}
		if got := f.tipes(t); !reflect.DeepEqual(got, want) {
			t.Fatalf("event types = %v, want %v", got, want)
		}
		if f.clock.sleeps != 6 {
			t.Fatalf("sleeps = %d, want 6 (the cadence is 6 ticks at the 5 s poll)", f.clock.sleeps)
		}
		if !f.clock.Now().Before(nowBefore) {
			t.Fatal("the rewind clock did not move backwards: the test proves nothing")
		}
		assertSyncPostconditions(t, f.job)
	})
}

// releaseOps records each release-family call in the shared order log so
// the test proves the workspace close is the last call.
type releaseOps struct {
	inner teamOps
	log   *[]string
}

func (o releaseOps) EnsureJobLane() error               { return o.inner.EnsureJobLane() }
func (o releaseOps) SpawnOrchestrator() (string, error) { return o.inner.SpawnOrchestrator() }
func (o releaseOps) Dispatch(name, briefPath string, amend bool) error {
	return o.inner.Dispatch(name, briefPath, amend)
}
func (o releaseOps) Status(name string) (string, string, error) { return o.inner.Status(name) }
func (o releaseOps) Send(name, path string) error               { return o.inner.Send(name, path) }
func (o releaseOps) Release(name string) error {
	*o.log = append(*o.log, "release:"+name)
	return o.inner.Release(name)
}
func (o releaseOps) ReleaseTeam() error {
	*o.log = append(*o.log, "releaseTeam")
	return o.inner.ReleaseTeam()
}
func (o releaseOps) GC() error {
	*o.log = append(*o.log, "gc")
	return o.inner.GC()
}
func (o releaseOps) Roster() ([]rosterRow, error) {
	*o.log = append(*o.log, "roster")
	return o.inner.Roster()
}

// closingHerdr records the workspace close in the shared order log and
// counts the cleanup events in the job log at the moment of the close:
// the close is last, so the cleanup event must already be there.
type closingHerdr struct {
	inner          herdrWorkspaces
	log            *[]string
	jobDir         string
	cleanupAtClose int
}

func (h closingHerdr) Create(cwd, label string, env map[string]string) (string, string, error) {
	return h.inner.Create(cwd, label, env)
}
func (h *closingHerdr) Close(id string) error {
	if h.jobDir != "" {
		if events, err := readEvents(h.jobDir); err == nil {
			for _, event := range events {
				if event.Tipo == "cleanup" {
					h.cleanupAtClose++
				}
			}
		}
	}
	*h.log = append(*h.log, "herdrClose:"+id)
	return h.inner.Close(id)
}
func (h closingHerdr) ServerReachable() bool           { return h.inner.ServerReachable() }
func (h closingHerdr) List() ([]herdrWorkspace, error) { return h.inner.List() }

// crashingHerdr panics once in Close: a crash after the cleanup event and
// before the workspace close.
type crashingHerdr struct {
	inner herdrWorkspaces
	once  bool
}

func (h crashingHerdr) Create(cwd, label string, env map[string]string) (string, string, error) {
	return h.inner.Create(cwd, label, env)
}
func (h crashingHerdr) Close(id string) error {
	if h.once {
		h.once = false
		panic(crashBeforeWorkspaceClose)
	}
	return h.inner.Close(id)
}
func (h crashingHerdr) ServerReachable() bool           { return h.inner.ServerReachable() }
func (h crashingHerdr) List() ([]herdrWorkspace, error) { return h.inner.List() }

func TestSuperviseRelease(t *testing.T) {
	t.Run("done with commits: the complete report and the release order", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		sha := f.job.commitIn(t, "feat: first")
		var order []string
		f.sup.Ops = releaseOps{inner: f.team, log: &order}
		closeHerdr := &closingHerdr{inner: f.herdr, log: &order, jobDir: f.jobDir}
		f.sup.Herdr = closeHerdr

		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d %v %v, want done with exit 0", exit, done, err)
		}

		rep := f.report(t)
		if rep["status"] != "done" || rep["motivo"] != nil {
			t.Fatalf("report = status %v motivo %v, want done without a motivo", rep["status"], rep["motivo"])
		}
		if rep["head"] != sha || rep["head_remoto"] != sha || rep["sincronizado"] != true {
			t.Fatalf("report head = %v/%v sincronizado %v, want the pushed head", rep["head"], rep["head_remoto"], rep["sincronizado"])
		}
		commits := reportSlice(t, rep, "commits")
		if len(commits) != 1 || commits[0]["sha"] != sha || commits[0]["titulo"] != "feat: first" || commits[0]["push"] != "ok" {
			t.Fatalf("report commits = %v, want the pushed commit", commits)
		}
		pr, ok := rep["pr"].(map[string]any)
		if !ok || pr["numero"] != float64(7) || pr["url"] != gitSyncPRURL || pr["estado"] != "draft" {
			t.Fatalf("report pr = %v, want the draft pull request", rep["pr"])
		}
		custo, ok := rep["custo"].(map[string]any)
		if !ok {
			t.Fatalf("report custo = %v, want an object", rep["custo"])
		}
		if _, err := time.Parse(time.RFC3339, custo["inicio"].(string)); err != nil {
			t.Fatalf("custo.inicio %q is not RFC 3339: %v", custo["inicio"], err)
		}
		fim, ok := custo["fim"].(string)
		if !ok || strings.HasSuffix(fim, "Z") {
			t.Fatalf("custo.fim = %v, want an explicit offset", custo["fim"])
		}
		if custo["duracao_s"] != float64(0) || custo["workers"] != float64(1) {
			t.Fatalf("custo = %v, want zero duration and one worker", custo)
		}
		logs, ok := rep["logs"].(map[string]any)
		if !ok || logs["report_md"] != filepath.Join(f.jobDir, "report.md") ||
			logs["state_dir"] != f.jobDir || logs["friction"] != filepath.Join(f.jobDir, "friction.log") {
			t.Fatalf("report logs = %v, want the job log paths", rep["logs"])
		}
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["workspace"] != "closed" || limpeza["worktree"] != "kept" || limpeza["removivel"] != true {
			t.Fatalf("report limpeza = %v, want the closed workspace and the removable clean tree", rep["limpeza"])
		}

		// The release calls happen once each and the workspace close is
		// the last call.
		want := []string{"release:orch-1", "releaseTeam", "gc", "herdrClose:ws-1"}
		if !reflect.DeepEqual(order, want) {
			t.Fatalf("release order = %v, want %v", order, want)
		}
		if f.team.releaseCalls != 1 || f.team.releaseTeamCalls != 1 || f.team.gcCalls != 1 {
			t.Fatalf("release=%d releaseTeam=%d gc=%d, want one each", f.team.releaseCalls, f.team.releaseTeamCalls, f.team.gcCalls)
		}
		if closeHerdr.cleanupAtClose != 1 {
			t.Fatalf("cleanup events at close time = %d, want 1: the close is last", closeHerdr.cleanupAtClose)
		}
		events := f.events(t)
		if n := countTipes(events, "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want 1", n)
		}
		cleanup := events[len(events)-1]
		if cleanup.Tipo != "cleanup" || cleanup.Resumo != "processes released; worktree kept" {
			t.Fatalf("cleanup event = %+v", cleanup)
		}
		if len(*f.friction) != 0 {
			t.Fatalf("friction = %v, want none", *f.friction)
		}
		// Nothing is removed: the worktree dir and the local branch stay,
		// and the fake gh log has no pr ready, merge, close, or edit call.
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a dirty tree is not removable", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		if err := os.WriteFile(filepath.Join(f.job.dir, "leftover.txt"), []byte("wip\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		rep := f.report(t)
		// The done outcome runs no checkpoint: the tree stays dirty, and
		// the in-sync dirty tree is not removable.
		if rep["sincronizado"] != true {
			t.Fatalf("report sincronizado = %v, want true (the push landed)", rep["sincronizado"])
		}
		if limpeza := rep["limpeza"].(map[string]any); limpeza["removivel"] != false {
			t.Fatalf("report limpeza = %v, want not removable for a dirty tree", limpeza)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a workspace-mode job runs no git at all", func(t *testing.T) {
		// A recording git on the PATH proves no git subprocess runs.
		fakeGitDir := t.TempDir()
		if _, err := fakecli.Install(t, fakeGitDir, "git", []fakecli.Rule{{AnyArgs: true, Code: 7}}); err != nil {
			t.Fatalf("install the recording git: %v", err)
		}
		root := t.TempDir()
		clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.FixedZone("-03:00", -3*3600))}
		store := Open(root)
		store.Clock = clock
		brief := []byte(strings.Replace(supTestBrief, "job-1", "job-9", 1))
		if _, err := store.Start("job-9", brief); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Transition("job-9", StatusPreparing); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Record("job-9", func(st *State) {
			st.Dir = t.TempDir()
			st.Modo = "workspace"
			st.Orchestrator = "orch-1"
			st.WorkspaceID = "ws-1"
			st.StartedAt = formatTS(clock.Now())
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Transition("job-9", StatusRunning); err != nil {
			t.Fatal(err)
		}
		herdr := &fakeHerdr{}
		var friction []string
		sup := &Supervisor{
			Store: store, ID: "job-9", Ops: &fakeTeam{}, Clock: clock,
			Herdr: herdr, Friction: func(message string) { friction = append(friction, message) },
		}
		if _, err := store.Transition("job-9", StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := sup.finishOutcome(StatusDone, "", 0)
		if err != nil || !done || exit != 0 {
			t.Fatalf("finishOutcome = %d %v %v", exit, done, err)
		}
		raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-9", "report.json"))
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("report.json: %v", err)
		}
		// No commits: no pull request and the motivo says so.
		if doc["pr"] != nil || doc["motivo"] != "sem commits" {
			t.Fatalf("report pr = %v motivo = %v, want null and sem commits", doc["pr"], doc["motivo"])
		}
		if doc["sincronizado"] != false {
			t.Fatalf("report sincronizado = %v, want false (no head)", doc["sincronizado"])
		}
		if commits := doc["commits"].([]any); len(commits) != 0 {
			t.Fatalf("report commits = %v, want none", doc["commits"])
		}
		// The workspace is still released and closed last.
		if len(herdr.CloseIDs) != 1 || herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1", herdr.CloseIDs)
		}
		if n := countTipes(readAllEvents(t, root, "job-9"), "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want 1", n)
		}
		// The recording git never ran.
		calls, err := fakecli.ReadCalls(filepath.Join(fakeGitDir, "git.calls.jsonl"))
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("read the recording git log: %v", err)
		}
		if len(calls) != 0 {
			t.Fatalf("git ran for a workspace-mode job: %v", calls)
		}
		if len(friction) != 0 {
			t.Fatalf("friction = %v, want none", friction)
		}
	})

	t.Run("a canceled job with an uncommitted file pushes one checkpoint commit", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		if err := os.WriteFile(filepath.Join(f.job.dir, "leftover.txt"), []byte("wip\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		exit, done, err := f.sup.finishOutcome(StatusCanceled, "", ExitCanceled)
		if err != nil || !done || exit != ExitCanceled {
			t.Fatalf("finishOutcome = %d %v %v, want canceled with exit 21", exit, done, err)
		}
		// Exactly one wip checkpoint commit, pushed.
		out, err := f.job.base.git(f.job.dir, "log", "--reverse", "--format=%s", "refs/remotes/origin/main..HEAD")
		if err != nil {
			t.Fatalf("git log: %s: %v", out, err)
		}
		titles := strings.Split(strings.TrimRight(out, "\n"), "\n")
		checkpoints := 0
		for _, title := range titles {
			if title == "wip(job): checkpoint" {
				checkpoints++
			}
		}
		if checkpoints != 1 {
			t.Fatalf("checkpoint commits = %d (titles %v), want exactly one", checkpoints, titles)
		}
		if got := f.job.remoteRef(t); got != f.job.head(t) || got == "" {
			t.Fatalf("remote ref = %q, want the pushed checkpoint head", got)
		}
		rep := f.report(t)
		if rep["status"] != "canceled" {
			t.Fatalf("report status = %v, want canceled", rep["status"])
		}
		commits := reportSlice(t, rep, "commits")
		if len(commits) != 2 || commits[0]["push"] != "ok" || commits[1]["push"] != "ok" {
			t.Fatalf("report commits = %v, want the two pushed commits", commits)
		}
		if commits[1]["titulo"] != "wip(job): checkpoint" {
			t.Fatalf("report commit 2 = %v, want the checkpoint", commits[1])
		}
		// The final push runs while the job is finishing: its push and
		// pr_opened events precede the terminal event, so they wake the
		// dispatcher before the job ends.
		if got := eventTipes(f.events(t)); !reflect.DeepEqual(got[len(got)-5:], []string{"commit", "push", "pr_opened", "terminal", "cleanup"}) {
			t.Fatalf("event types = %v, want the final push and pr_opened before terminal", got)
		}

		// The retry converges: one more finalize adds no events, no
		// second checkpoint commit, and the remote head does not move.
		before := len(f.events(t))
		f.sup.finalize(StatusCanceled)
		events := f.events(t)
		if len(events) != before {
			t.Fatalf("the retry added events: %v", eventTipes(events[before:]))
		}
		out, err = f.job.base.git(f.job.dir, "log", "--reverse", "--format=%s", "refs/remotes/origin/main..HEAD")
		if err != nil {
			t.Fatalf("git log: %s: %v", out, err)
		}
		checkpoints = 0
		for _, title := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if title == "wip(job): checkpoint" {
				checkpoints++
			}
		}
		if checkpoints != 1 {
			t.Fatalf("checkpoint commits after the retry = %d, want 1", checkpoints)
		}
		if got := f.job.remoteRef(t); got != f.job.head(t) {
			t.Fatalf("remote ref moved on the retry: %q", got)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a crash between push and PR opens at most one pull request", func(t *testing.T) {
		// Process one: the push lands, the create runs, and the process
		// dies between the create and the pr_opened record.
		ghRules := []fakecli.Rule{
			{Argv: ghListArgv(), Call: 1, Stdout: "[]\n"},
			{Argv: ghCreateArgv("feat: first", supGitPRBody), Stdout: gitSyncPRURL + "\n"},
			{Argv: ghListArgv(), Call: 2, Stdout: supGitPRJSON + "\n"},
		}
		f := newSupGitFixture(t, ghRules)
		sha := f.job.commitIn(t, "feat: first")
		f.tickN(t, 5) // the sync is the sixth tick

		oldAppend := appendEventFn
		appendEventFn = func(dir string, now time.Time, in EventIn) (Event, error) {
			if in.Tipo == "pr_opened" {
				panic(crashPRRecord)
			}
			return oldAppend(dir, now, in)
		}
		defer func() { appendEventFn = oldAppend }()
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("the pr_opened record did not crash: the test proves nothing")
					return
				}
				if !errors.Is(r.(error), crashPRRecord) {
					panic(r)
				}
			}()
			f.tickN(t, 1)
		}()
		// The process is dead: the crash window closed and the record hook
		// comes off before the restarted supervisor runs.
		appendEventFn = oldAppend
		// The push completed and the pull request was created, but the
		// record did not land.
		if got := f.job.remoteRef(t); got != sha {
			t.Fatalf("remote ref = %q, want the pushed head %q", got, sha)
		}
		if n := countTipes(f.events(t), "pr_opened"); n != 0 {
			t.Fatalf("pr_opened events = %d, want none before the crash", n)
		}

		// Process two: the restarted supervisor over the same store; the
		// sixth tick syncs, the lookup finds the pull request and records
		// it once.
		sup2 := f.supRestarted(t)
		for i := 0; i < 6; i++ {
			f.team.statusScript = append(f.team.statusScript, fakeStatus{State: "working"})
			code, done, err := sup2.tick()
			if err != nil || done {
				t.Fatalf("retry tick %d: code=%d done=%v err=%v", i+1, code, done, err)
			}
			sup2.clockSleep(sup2.poll())
		}
		events := f.events(t)
		if n := countTipes(events, "pr_opened"); n != 1 {
			t.Fatalf("pr_opened events = %d, want exactly one after the retry", n)
		}
		pr := events[len(events)-1]
		if pr.Tipo != "pr_opened" || pr.Resumo != "draft pull request opened" || pr.Refs["pr"] != "7" {
			t.Fatalf("pr_opened event = %+v", pr)
		}
		// No commit event replay (the sha was announced) and no push
		// event (the remote is up to date): the retry only records the
		// pull request.
		if n := countTipes(events, "commit"); n != 1 {
			t.Fatalf("commit events = %d, want 1 (no replay)", n)
		}
		if n := countTipes(events, "push"); n != 1 {
			t.Fatalf("push events = %d, want 1 (the remote is up to date)", n)
		}
		// At most one pull request: one create across both processes.
		calls := f.job.base.ghCalls()
		creates := 0
		for _, call := range calls {
			if len(call.Argv) >= 2 && call.Argv[1] == "create" {
				creates++
			}
		}
		if creates != 1 {
			t.Fatalf("gh pr create calls = %d (%v), want exactly one", creates, calls)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a crash after cleanup and before the close: the retry converges", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		f.sup.Herdr = crashingHerdr{inner: f.herdr, once: true}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		func() {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("the close did not crash: the test proves nothing")
					return
				}
				if !errors.Is(r.(error), crashBeforeWorkspaceClose) {
					panic(r)
				}
			}()
			if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
				t.Fatalf("finishOutcome: %v", err)
			}
		}()
		// The cleanup landed and the workspace did not close.
		if n := countTipes(f.events(t), "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want 1 after the crash", n)
		}
		if len(f.herdr.CloseIDs) != 0 {
			t.Fatalf("herdr close = %v, want none before the crash", f.herdr.CloseIDs)
		}
		if st := f.state(t); st.Status != StatusDone {
			t.Fatalf("state = %+v, want done", st)
		}

		// The retry is a new supervisor process: Run sees the terminal job
		// and finalizes it; the log is checked first, so no second cleanup,
		// and the workspace closes once.
		sup2 := f.supRestarted(t)
		if exit, err := sup2.Run(); err != nil || exit != 0 {
			t.Fatalf("restarted Run = %d %v, want 0", exit, err)
		}
		if n := countTipes(f.events(t), "cleanup"); n != 1 {
			t.Fatalf("cleanup events = %d, want still 1 after the retry", n)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want ws-1 exactly once", f.herdr.CloseIDs)
		}
		rep := f.report(t)
		if limpeza := rep["limpeza"].(map[string]any); limpeza["workspace"] != "closed" {
			t.Fatalf("report limpeza = %v, want the closed workspace", limpeza)
		}
		// A further restart after the close changes nothing: no second close.
		if exit, err := f.supRestarted(t).Run(); err != nil || exit != 0 {
			t.Fatalf("second restarted Run = %d %v, want 0", exit, err)
		}
		if len(f.herdr.CloseIDs) != 1 {
			t.Fatalf("herdr close = %v, want still exactly one", f.herdr.CloseIDs)
		}
		assertSyncPostconditions(t, f.job)
	})

	t.Run("a failed close leaves the workspace open in the report", func(t *testing.T) {
		f := newSupGitFixture(t, supGitGhRules)
		f.job.commitIn(t, "feat: first")
		f.herdr.CloseErr = &ExitError{Code: ExitHerdr, Msg: "job: herdr workspace close failed"}
		if _, err := f.store.Transition(supGitID, StatusFinishing); err != nil {
			t.Fatal(err)
		}
		if _, _, err := f.sup.finishOutcome(StatusDone, "", 0); err != nil {
			t.Fatal(err)
		}
		rep := f.report(t)
		limpeza, ok := rep["limpeza"].(map[string]any)
		if !ok || limpeza["workspace"] != "open" {
			t.Fatalf("report limpeza = %v, want the open workspace", rep["limpeza"])
		}
		found := false
		for _, line := range *f.friction {
			if strings.Contains(line, "job: herdr workspace close failed") {
				found = true
			}
		}
		if !found {
			t.Fatalf("friction = %v, want the close failure", *f.friction)
		}
		if len(f.herdr.CloseIDs) != 1 || f.herdr.CloseIDs[0] != "ws-1" {
			t.Fatalf("herdr close = %v, want the one attempt", f.herdr.CloseIDs)
		}
		assertSyncPostconditions(t, f.job)
	})
}

func TestSuperviseNewSupervisorGitWiring(t *testing.T) {
	t.Run("a worktree-mode job wires the git sync", func(t *testing.T) {
		store, _, _, _ := supFixture(t)
		sup, err := NewSupervisor(store, "job-1", platform.Env{}, "/opt/bin/herdr-soho", nil)
		if err != nil {
			t.Fatalf("NewSupervisor: %v", err)
		}
		snap, err := store.Snapshot("job-1")
		if err != nil {
			t.Fatal(err)
		}
		if sup.Git == nil {
			t.Fatal("a worktree-mode job has no git sync")
		}
		if sup.Git.Dir != snap.State.Dir || sup.Git.Branch != snap.State.Branch || sup.Git.Base != snap.State.Base {
			t.Fatalf("git sync = %+v, want the state worktree and branch facts", sup.Git)
		}
		// The repo comes from brief.json.
		if sup.Git.Repo != "example-org/example-repo" {
			t.Fatalf("git sync repo = %q, want the brief's repo", sup.Git.Repo)
		}
		if sup.Herdr == nil {
			t.Fatal("NewSupervisor wires no Herdr adapter")
		}
	})

	t.Run("a workspace-mode job wires no git sync", func(t *testing.T) {
		store, _, _, _ := supFixture(t)
		if _, err := store.Record("job-1", func(st *State) { st.Modo = "workspace" }); err != nil {
			t.Fatal(err)
		}
		sup, err := NewSupervisor(store, "job-1", platform.Env{}, "/opt/bin/herdr-soho", nil)
		if err != nil {
			t.Fatalf("NewSupervisor: %v", err)
		}
		if sup.Git != nil {
			t.Fatalf("a workspace-mode job has a git sync: %+v", sup.Git)
		}
		if sup.Herdr == nil {
			t.Fatal("NewSupervisor wires no Herdr adapter")
		}
	})
}

// readAllEvents reads one job's events from a state root the fixture did
// not build.
func readAllEvents(t *testing.T, root, id string) []Event {
	t.Helper()
	events, err := readEvents(filepath.Join(root, "jobs", id))
	if err != nil {
		t.Fatal(err)
	}
	return events
}
