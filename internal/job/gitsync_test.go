package job

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// gitsync_test.go exercises the git sync primitives against a local bare
// remote, the real git, and a fake gh: a push that never forces, at most one
// draft pull request with lookup-before-create, the wip checkpoint commit,
// and the clean and in-sync facts behind limpeza.removivel. It reuses the
// prepare fixture (bare remote, hermetic envs, fake gh) and stages the job
// worktree the way preparation does.

const (
	gitSyncBranch = "job/" + prepID
	gitSyncRepo   = prepOrg + "/" + prepRepo
	gitSyncPRURL  = "https://github.com/example-org/example-repo/pull/7"
)

func ghListArgv() []string {
	return []string{"pr", "list", "--repo", gitSyncRepo, "--head", gitSyncBranch, "--state", "all", "--json", "number,url,isDraft", "--limit", "1"}
}

func ghCreateArgv(title, body string) []string {
	return []string{"pr", "create", "--repo", gitSyncRepo, "--draft", "--base", "main", "--head", gitSyncBranch, "--title", title, "--body", body}
}

// jobSyncFixture wires the prepare fixture to one GitSync on the job
// worktree: a checkout cloned from the bare remote and a worktree on
// job/<id> from origin/main.
type jobSyncFixture struct {
	base *prepareFixture
	dir  string
	sync GitSync
}

func newJobSyncFixture(t *testing.T, ghRules []fakecli.Rule) *jobSyncFixture {
	t.Helper()
	f := newPrepareFixture(t, ghRules)
	checkout := f.cloneCheckout()
	dir := filepath.Join(checkout, ".worktrees", "job-"+prepID)
	if out, err := f.git(checkout, "worktree", "add", "-b", gitSyncBranch, dir, "refs/remotes/origin/main"); err != nil {
		t.Fatalf("git worktree add: %s: %v", out, err)
	}
	return &jobSyncFixture{
		base: f,
		dir:  dir,
		sync: GitSync{Env: f.env, Dir: dir, Branch: gitSyncBranch, Base: "main", Repo: gitSyncRepo},
	}
}

// commitIn makes one empty commit in the worktree through the fixture git
// (test setup only, never through GitSync) and returns its sha.
func (j *jobSyncFixture) commitIn(t *testing.T, msg string) string {
	t.Helper()
	if out, err := j.base.git(j.dir, "commit", "-q", "--allow-empty", "-m", msg); err != nil {
		t.Fatalf("setup commit %q: %s: %v", msg, out, err)
	}
	return j.head(t)
}

func (j *jobSyncFixture) head(t *testing.T) string {
	t.Helper()
	out, err := j.base.git(j.dir, "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse HEAD: %s: %v", out, err)
	}
	return strings.TrimSpace(out)
}

// remoteRef is the bare remote's refs/heads/job/<id>, or "" when absent.
func (j *jobSyncFixture) remoteRef(t *testing.T) string {
	t.Helper()
	out, err := j.base.git(j.base.bare, "rev-parse", "--verify", "--quiet", "refs/heads/"+gitSyncBranch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// bareReflog is the bare remote's reflog of the job ref, "" when the ref is
// absent.
func (j *jobSyncFixture) bareReflog(t *testing.T) string {
	t.Helper()
	out, err := j.base.git(j.base.bare, "reflog", "show", "refs/heads/"+gitSyncBranch)
	if err != nil {
		return ""
	}
	return out
}

// assertSyncPostconditions is criterion 8: after a test the worktree dir and
// the local job branch still exist, and the fake gh log contains no pr
// ready, merge, close, or edit call.
func assertSyncPostconditions(t *testing.T, j *jobSyncFixture) {
	t.Helper()
	f := j.base
	if _, err := os.Stat(j.dir); err != nil {
		t.Fatalf("the worktree dir is gone: %v", err)
	}
	if out, err := f.git(j.dir, "rev-parse", "--verify", "refs/heads/"+gitSyncBranch); err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("the local job branch is gone: %q %v", out, err)
	}
	for _, c := range f.ghCalls() {
		if len(c.Argv) >= 2 && c.Argv[0] == "pr" {
			switch c.Argv[1] {
			case "ready", "merge", "close", "edit":
				t.Fatalf("the fake gh log has a forbidden pr call: %v", c.Argv)
			}
		}
	}
}

func TestGitSyncPush(t *testing.T) {
	t.Run("a new commit is pushed and an up-to-date branch runs no push", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		// The bare reflog records every ref update: the sentinel the brief
		// asks for on the up-to-date push.
		if out, err := j.base.git(j.base.bare, "config", "core.logAllRefUpdates", "true"); err != nil {
			t.Fatalf("git config on the bare: %s: %v", out, err)
		}
		sha := j.commitIn(t, "first")
		res, err := j.sync.Push()
		if err != nil || !res.Pushed || res.Rejected || res.Head != sha {
			t.Fatalf("first Push = %+v err=%v, want pushed", res, err)
		}
		if got := j.remoteRef(t); got != sha {
			t.Fatalf("remote ref = %q, want the local head %q", got, sha)
		}
		remoteBefore, reflogBefore := j.remoteRef(t), j.bareReflog(t)
		if reflogBefore == "" {
			t.Fatal("the bare reflog did not record the push")
		}
		res, err = j.sync.Push()
		if err != nil || res.Pushed || res.Rejected || res.Head != sha {
			t.Fatalf("second Push = %+v err=%v, want up-to-date", res, err)
		}
		if got := j.remoteRef(t); got != remoteBefore {
			t.Fatalf("the remote ref moved on an up-to-date push: %q, want %q", got, remoteBefore)
		}
		if got := j.bareReflog(t); got != reflogBefore {
			t.Fatalf("the bare reflog changed on an up-to-date push:\nbefore %q\nafter  %q", reflogBefore, got)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("a rejected push stays rejected and never forces", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		// Another clone advances the remote job branch past the local head,
		// so the local push is a non-fast-forward.
		other := filepath.Join(j.base.root, "other")
		if out, err := j.base.git(j.base.root, "clone", "-q", j.base.bare, other); err != nil {
			t.Fatalf("clone the second clone: %s: %v", out, err)
		}
		for _, args := range [][]string{
			{"checkout", "-q", "-b", gitSyncBranch, "refs/remotes/origin/main"},
			{"commit", "-q", "--allow-empty", "-m", "other"},
			{"push", "-q", "origin", gitSyncBranch},
		} {
			if out, err := j.base.git(other, args...); err != nil {
				t.Fatalf("setup git %v in the second clone: %s: %v", args, out, err)
			}
		}
		remoteSHA := j.remoteRef(t)
		localSHA := j.commitIn(t, "local")
		if remoteSHA == "" || remoteSHA == localSHA {
			t.Fatalf("setup refs remote=%q local=%q, want distinct", remoteSHA, localSHA)
		}
		for n := 1; n <= 2; n++ {
			res, err := j.sync.Push()
			if err != nil || !res.Rejected || res.Pushed || res.Head != localSHA {
				t.Fatalf("push %d = %+v err=%v, want rejected without error", n, res, err)
			}
			if got := j.remoteRef(t); got != remoteSHA {
				t.Fatalf("push %d moved the remote ref to %q, want %q (a force push)", n, got, remoteSHA)
			}
		}
		// The push argv is the fixed constant in gitsync.go; the behavioral
		// proof is that two rejected pushes never moved the remote ref, and
		// no gh ran in a push-only flow.
		if calls := j.base.ghCalls(); len(calls) != 0 {
			t.Fatalf("gh ran for a push-only flow: %v", calls)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("a missing git is a push error, not a rejection", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		j.commitIn(t, "first")
		env := j.base.env.Clone()
		env["PATH"] = t.TempDir() // no git resolvable on the PATH
		j.sync.Env = env
		res, err := j.sync.Push()
		wantExit(t, err, ExitFailed, "job: git sync failed: push")
		if res.Pushed || res.Rejected || res.Head != "" {
			t.Fatalf("Push result = %+v, want zero", res)
		}
		if _, err := j.sync.Head(); !isCode(err, ExitFailed) {
			t.Fatalf("Head err = %v, want code 19", err)
		}
		assertSyncPostconditions(t, j)
	})
}

func TestGitSyncDraftPR(t *testing.T) {
	t.Run("the first push opens exactly one draft PR and the retry finds it", func(t *testing.T) {
		title, body := "Open the change", "Body of the draft."
		j := newJobSyncFixture(t, []fakecli.Rule{
			{Argv: ghListArgv(), Call: 1, Stdout: "[]\n"},
			{Argv: ghCreateArgv(title, body), Stdout: "Open a draft pull request:\n" + gitSyncPRURL + "\n\n"},
			{Argv: ghListArgv(), Call: 2, Stdout: `[{"number":7,"url":"` + gitSyncPRURL + `","isDraft":true}]` + "\n"},
		})
		if _, err := j.sync.Push(); err != nil {
			t.Fatalf("Push: %v", err)
		}
		pr, created, err := j.sync.EnsureDraftPR(title, body)
		if err != nil || !created {
			t.Fatalf("first EnsureDraftPR = %+v created=%v err=%v, want created", pr, created, err)
		}
		if pr.Numero != 7 || pr.URL != gitSyncPRURL || pr.Estado != "draft" {
			t.Fatalf("PR = %+v", pr)
		}
		pr2, created2, err := j.sync.EnsureDraftPR(title, body)
		if err != nil || created2 || pr2 != pr {
			t.Fatalf("second EnsureDraftPR = %+v created=%v err=%v, want the same PR without create", pr2, created2, err)
		}
		calls := j.base.ghCalls()
		if len(calls) != 3 {
			t.Fatalf("gh calls = %d (%v), want 3", len(calls), calls)
		}
		if !sameArgv(calls[0].Argv, ghListArgv()) || !sameArgv(calls[1].Argv, ghCreateArgv(title, body)) || !sameArgv(calls[2].Argv, ghListArgv()) {
			t.Fatalf("gh argv = %v", calls)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("a crash between push and create still opens at most one PR", func(t *testing.T) {
		// The remote branch exists (the push completed) but no PR exists
		// (the create did not run); the retry then sees the PR.
		title, body := "Crashed once", "Retry body."
		j := newJobSyncFixture(t, []fakecli.Rule{
			{Argv: ghListArgv(), Call: 1, Stdout: "[]\n"},
			{Argv: ghCreateArgv(title, body), Stdout: gitSyncPRURL + "\n"},
			{Argv: ghListArgv(), Call: 2, Stdout: `[{"number":7,"url":"` + gitSyncPRURL + `","isDraft":true}]` + "\n"},
		})
		sha := j.commitIn(t, "work")
		res, err := j.sync.Push()
		if err != nil || !res.Pushed || j.remoteRef(t) != sha {
			t.Fatalf("crash-state Push = %+v err=%v, want pushed", res, err)
		}
		pr, created, err := j.sync.EnsureDraftPR(title, body)
		if err != nil || !created {
			t.Fatalf("EnsureDraftPR after the crash = %+v created=%v err=%v", pr, created, err)
		}
		again, createdAgain, err := j.sync.EnsureDraftPR(title, body)
		if err != nil || createdAgain || again != pr {
			t.Fatalf("retry EnsureDraftPR = %+v created=%v err=%v, want the same PR", again, createdAgain, err)
		}
		creates := 0
		for _, c := range j.base.ghCalls() {
			if len(c.Argv) >= 2 && c.Argv[0] == "pr" && c.Argv[1] == "create" {
				creates++
			}
		}
		if creates != 1 {
			t.Fatalf("pr create calls = %d, want exactly 1 overall", creates)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("a non-draft existing PR is reported open without creating", func(t *testing.T) {
		j := newJobSyncFixture(t, []fakecli.Rule{
			{Argv: ghListArgv(), Stdout: `[{"number":7,"url":"` + gitSyncPRURL + `","isDraft":false}]` + "\n"},
		})
		pr, created, err := j.sync.EnsureDraftPR("t", "b")
		if err != nil || created || pr.Numero != 7 || pr.URL != gitSyncPRURL || pr.Estado != "open" {
			t.Fatalf("PR = %+v created=%v err=%v, want open without create", pr, created, err)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("a malformed pr list refuses the whole response without creating", func(t *testing.T) {
		two := `[{"number":6,"url":"https://github.com/example-org/example-repo/pull/6","isDraft":true},{"number":7,"url":"` + gitSyncPRURL + `","isDraft":true}]`
		for _, list := range []string{"not json\n", "null\n", two} {
			j := newJobSyncFixture(t, []fakecli.Rule{
				{Argv: ghListArgv(), Stdout: list},
				{Argv: ghCreateArgv("t", "b"), Stdout: gitSyncPRURL + "\n"},
			})
			pr, created, err := j.sync.EnsureDraftPR("t", "b")
			wantExit(t, err, ExitFailed, "job: git sync failed: pr")
			if created || pr.Numero != 0 || pr.URL != "" {
				t.Fatalf("malformed list %q: pr=%+v created=%v, want refused", list, pr, created)
			}
			for _, c := range j.base.ghCalls() {
				if len(c.Argv) >= 2 && c.Argv[0] == "pr" && c.Argv[1] == "create" {
					t.Fatalf("a create ran after a malformed list: %v", c.Argv)
				}
			}
			assertSyncPostconditions(t, j)
		}
	})

	t.Run("a create without a parseable pull number is a pr error", func(t *testing.T) {
		for name, out := range map[string]string{
			"no /pull/":  "done\n",
			"not an int": "https://github.com/example-org/example-repo/pull/notanumber\n",
			"empty":      "",
		} {
			t.Run(name, func(t *testing.T) {
				j := newJobSyncFixture(t, []fakecli.Rule{
					{Argv: ghListArgv(), Stdout: "[]\n"},
					{Argv: ghCreateArgv("t", "b"), Stdout: out},
				})
				pr, created, err := j.sync.EnsureDraftPR("t", "b")
				wantExit(t, err, ExitFailed, "job: git sync failed: pr")
				if created || pr.Numero != 0 {
					t.Fatalf("create parse: pr=%+v created=%v, want none", pr, created)
				}
				assertSyncPostconditions(t, j)
			})
		}
	})

	t.Run("a timed out gh is a bounded pr error", func(t *testing.T) {
		j := newJobSyncFixture(t, []fakecli.Rule{
			{Argv: ghListArgv(), Delay: 5000},
			{Argv: ghCreateArgv("t", "b"), Delay: 5000},
		})
		j.sync.GhTimeout = 200 * time.Millisecond
		start := time.Now()
		pr, created, err := j.sync.EnsureDraftPR("t", "b")
		elapsed := time.Since(start)
		wantExit(t, err, ExitFailed, "job: git sync failed: pr")
		if elapsed > 10*time.Second {
			t.Fatalf("EnsureDraftPR took %s, want under 10s", elapsed)
		}
		if created || pr.Numero != 0 {
			t.Fatalf("timed out PR = %+v created=%v, want none", pr, created)
		}
		assertSyncPostconditions(t, j)
	})
}

func TestGitSyncCommitsAndFacts(t *testing.T) {
	t.Run("zero commits is empty, facts are clean and unsynced, no PR", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		commits, err := j.sync.Commits()
		if err != nil || len(commits) != 0 || commits == nil {
			t.Fatalf("Commits = %v err=%v, want empty non-nil", commits, err)
		}
		if raw, err := json.Marshal(commits); err != nil || string(raw) != "[]" {
			t.Fatalf("empty commits marshal = %s err=%v, want []", raw, err)
		}
		if got, err := j.sync.RemoteHead(); err != nil || got != "" {
			t.Fatalf("RemoteHead = %q err=%v, want empty without error", got, err)
		}
		facts, err := j.sync.Facts()
		if err != nil {
			t.Fatalf("Facts: %v", err)
		}
		if !facts.Clean || facts.Head != j.head(t) || facts.HeadRemoto != "" || facts.Resumo != "" {
			t.Fatalf("Facts = %+v, want clean, head at the base sha, no remote head", facts)
		}
		// No GitSync function opens a pull request on its own.
		for _, c := range j.base.ghCalls() {
			if len(c.Argv) >= 2 && c.Argv[0] == "pr" && c.Argv[1] == "create" {
				t.Fatalf("a pr create ran without EnsureDraftPR: %v", c.Argv)
			}
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("commits are oldest first with titles and an empty push field", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		first := j.commitIn(t, "first")
		second := j.commitIn(t, "second")
		commits, err := j.sync.Commits()
		if err != nil || len(commits) != 2 {
			t.Fatalf("Commits = %v err=%v", commits, err)
		}
		if commits[0].SHA != first || commits[0].Titulo != "first" || commits[0].Push != "" {
			t.Fatalf("first commit = %+v", commits[0])
		}
		if commits[1].SHA != second || commits[1].Titulo != "second" || commits[1].Push != "" {
			t.Fatalf("second commit = %+v", commits[1])
		}
		assertSyncPostconditions(t, j)
	})
}

func TestGitSyncFactsReport(t *testing.T) {
	t.Run("removivel is only clean and in sync, through WriteReport", func(t *testing.T) {
		s, _ := startedJob(t)
		j := newJobSyncFixture(t, nil)
		sha := j.commitIn(t, "work")

		// A clean tree with an unpushed commit is not removable.
		facts, err := j.sync.Facts()
		if err != nil {
			t.Fatalf("Facts: %v", err)
		}
		if !facts.Clean || facts.Head != sha || facts.HeadRemoto != "" {
			t.Fatalf("pre-push Facts = %+v, want clean, head %s, no remote head", facts, sha)
		}
		rep, err := s.WriteReport("job-1", facts)
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if rep.Limpeza.Removivel || rep.Sincronizado {
			t.Fatalf("unpushed report removivel=%v sync=%v, want false/false", rep.Limpeza.Removivel, rep.Sincronizado)
		}

		// After the push of the clean tree it is removable.
		res, err := j.sync.Push()
		if err != nil || !res.Pushed {
			t.Fatalf("Push = %+v err=%v", res, err)
		}
		facts, err = j.sync.Facts()
		if err != nil {
			t.Fatalf("Facts after push: %v", err)
		}
		if !facts.Clean || facts.Head != sha || facts.HeadRemoto != sha {
			t.Fatalf("post-push Facts = %+v, want clean and in sync", facts)
		}
		rep, err = s.WriteReport("job-1", facts)
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if !rep.Limpeza.Removivel || !rep.Sincronizado {
			t.Fatalf("in-sync report removivel=%v sync=%v, want true/true", rep.Limpeza.Removivel, rep.Sincronizado)
		}

		// A dirty tree with the same head is not removable.
		if err := os.WriteFile(filepath.Join(j.dir, "dirty.txt"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		facts, err = j.sync.Facts()
		if err != nil || facts.Clean || facts.HeadRemoto != sha {
			t.Fatalf("dirty Facts = %+v err=%v, want Clean=false", facts, err)
		}
		rep, err = s.WriteReport("job-1", facts)
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if rep.Limpeza.Removivel {
			t.Fatal("a dirty tree is removable")
		}

		// A clean tree with an unpushed commit is not removable again.
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--no-verify", "-m", "more"}} {
			if out, err := j.base.git(j.dir, args...); err != nil {
				t.Fatalf("setup git %v: %s: %v", args, out, err)
			}
		}
		facts, err = j.sync.Facts()
		if err != nil || !facts.Clean || facts.Head == sha || facts.HeadRemoto != sha {
			t.Fatalf("unpushed-commit Facts = %+v err=%v, want clean with a new head", facts, err)
		}
		rep, err = s.WriteReport("job-1", facts)
		if err != nil || rep.Limpeza.Removivel || rep.Sincronizado {
			t.Fatalf("unpushed report = %+v err=%v, want removivel and sync false", rep, err)
		}
		assertSyncPostconditions(t, j)
	})
}

func TestGitSyncCheckpoint(t *testing.T) {
	t.Run("a checkpoint commits tracked and untracked files, not ignored ones", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		// Setup: a tracked file and a gitignore in the worktree.
		for _, item := range []struct{ name, content string }{
			{"README", "v1\n"},
			{".gitignore", "ignored.txt\n"},
		} {
			if err := os.WriteFile(filepath.Join(j.dir, item.name), []byte(item.content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		for _, args := range [][]string{{"add", "-A"}, {"commit", "-q", "--no-verify", "-m", "setup"}} {
			if out, err := j.base.git(j.dir, args...); err != nil {
				t.Fatalf("setup git %v: %s: %v", args, out, err)
			}
		}
		base := j.head(t)
		// Dirty the tracked file, add an untracked non-ignored file, and an
		// ignored one that must stay out of the commit.
		if err := os.WriteFile(filepath.Join(j.dir, "README"), []byte("v2\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, item := range []struct{ name, content string }{
			{"new.txt", "new\n"},
			{"ignored.txt", "ignored\n"},
		} {
			if err := os.WriteFile(filepath.Join(j.dir, item.name), []byte(item.content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		ok, err := j.sync.Checkpoint()
		if err != nil || !ok {
			t.Fatalf("Checkpoint = %v err=%v, want committed", ok, err)
		}
		head := j.head(t)
		if head == base {
			t.Fatal("Checkpoint did not move HEAD")
		}
		subject, _ := j.base.git(j.dir, "log", "-1", "--format=%s")
		if strings.TrimSpace(subject) != "wip(job): checkpoint" {
			t.Fatalf("checkpoint subject = %q, want wip(job): checkpoint", strings.TrimSpace(subject))
		}
		names, _ := j.base.git(j.dir, "show", "--name-only", "--pretty=format:", "HEAD")
		var files []string
		for _, line := range strings.Split(names, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				files = append(files, line)
			}
		}
		if len(files) != 2 || files[0] != "README" || files[1] != "new.txt" {
			t.Fatalf("checkpoint files = %v, want [README new.txt]", files)
		}
		if _, err := os.Stat(filepath.Join(j.dir, "ignored.txt")); err != nil {
			t.Fatalf("the ignored file was removed: %v", err)
		}
		status, _ := j.base.git(j.dir, "status", "--porcelain")
		if strings.TrimSpace(status) != "" {
			t.Fatalf("post-checkpoint status = %q, want clean (the ignored file stays ignored)", status)
		}
		// A second checkpoint commits nothing and keeps HEAD.
		ok, err = j.sync.Checkpoint()
		if err != nil || ok {
			t.Fatalf("second Checkpoint = %v err=%v, want nothing", ok, err)
		}
		if got := j.head(t); got != head {
			t.Fatalf("the second Checkpoint moved HEAD to %q", got)
		}
		// [crash] the wip commit exists and the push did not run: the next
		// Push pushes it.
		res, err := j.sync.Push()
		if err != nil || !res.Pushed {
			t.Fatalf("Push after the checkpoint = %+v err=%v", res, err)
		}
		if got := j.remoteRef(t); got != head {
			t.Fatalf("remote ref = %q, want the checkpoint %q", got, head)
		}
		assertSyncPostconditions(t, j)
	})

	t.Run("nothing to checkpoint commits nothing", func(t *testing.T) {
		j := newJobSyncFixture(t, nil)
		base := j.head(t)
		ok, err := j.sync.Checkpoint()
		if err != nil || ok {
			t.Fatalf("Checkpoint on a clean tree = %v err=%v, want nothing", ok, err)
		}
		if got := j.head(t); got != base {
			t.Fatalf("Checkpoint moved HEAD on a clean tree to %q", got)
		}
		assertSyncPostconditions(t, j)
	})
}
