// gitsync.go keeps the job worktree in sync with the bare remote and with at
// most one draft pull request: it reads the head, the remote head, and the
// commits against the base, pushes new commits without ever forcing, opens a
// draft pull request once (looking up an existing one first), commits
// leftovers as a wip checkpoint, and reports the clean and in-sync facts the
// report derives limpeza.removivel from. It never removes a worktree, a
// branch, or a pull request. Every subprocess goes through runStep with a
// bounded timeout, and its errors are fixed strings that name the step —
// subprocess output, URLs, and environment values never appear in them.
package job

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

const (
	defaultLocalSyncTimeout = 60 * time.Second  // local git commands
	defaultNetSyncTimeout   = 300 * time.Second // ls-remote and push
	defaultGhSyncTimeout    = 60 * time.Second  // gh pr list and create
)

// GitSync is one job's git and draft-pull-request state. Dir is the job
// worktree, Branch is job/<id>, Base the base branch name, and Repo is
// <org>/<repo>. Zero timeouts select the defaults: LocalTimeout 60 s,
// NetTimeout 300 s, GhTimeout 60 s.
type GitSync struct {
	Env          platform.Env
	Dir          string
	Branch       string
	Base         string
	Repo         string
	LocalTimeout time.Duration
	NetTimeout   time.Duration
	GhTimeout    time.Duration
}

// Commit is one commit of the job branch ahead of the base, oldest first.
// Push is left empty here; the caller fills it in.
type Commit struct {
	SHA    string `json:"sha"`
	Titulo string `json:"titulo"`
	Push   string `json:"push"`
}

// PushResult reports one Push. Pushed means this call pushed the local head
// to the remote job branch; Rejected means the remote refused a
// fast-forward and the caller turns it into a blocked event; Head is the
// local head. An up-to-date remote leaves both flags false.
type PushResult struct {
	Pushed   bool
	Rejected bool
	Head     string
}

// gitSyncError is the fixed step error: code 19, a message that names the
// step and nothing else.
func gitSyncError(step string) error {
	return &ExitError{Code: ExitFailed, Msg: "job: git sync failed: " + step}
}

func (g *GitSync) localTimeout() time.Duration {
	if g.LocalTimeout <= 0 {
		return defaultLocalSyncTimeout
	}
	return g.LocalTimeout
}

func (g *GitSync) netTimeout() time.Duration {
	if g.NetTimeout <= 0 {
		return defaultNetSyncTimeout
	}
	return g.NetTimeout
}

func (g *GitSync) ghTimeout() time.Duration {
	if g.GhTimeout <= 0 {
		return defaultGhSyncTimeout
	}
	return g.GhTimeout
}

// gitEnv is the subprocess environment for git steps: GIT_TERMINAL_PROMPT=0
// so a push that would ask for credentials fails instead of hanging.
func (g *GitSync) gitEnv() platform.Env {
	env := g.Env.Clone()
	env["GIT_TERMINAL_PROMPT"] = "0"
	return env
}

// ghEnv is the subprocess environment for gh steps: GH_PROMPT_DISABLED=1.
func (g *GitSync) ghEnv() platform.Env {
	env := g.Env.Clone()
	env["GH_PROMPT_DISABLED"] = "1"
	return env
}

// Head is the worktree's HEAD sha.
func (g *GitSync) Head() (string, error) {
	out, err := runStep("git", []string{"-C", g.Dir, "rev-parse", "HEAD"}, g.localTimeout(), g.gitEnv())
	if err != nil {
		return "", gitSyncError("head")
	}
	return strings.TrimSpace(out), nil
}

// RemoteHead is the sha of the remote job branch; "" (no error) when the
// remote branch does not exist yet.
func (g *GitSync) RemoteHead() (string, error) {
	out, err := runStep("git", []string{"-C", g.Dir, "ls-remote", "origin", "refs/heads/" + g.Branch}, g.netTimeout(), g.gitEnv())
	if err != nil {
		return "", gitSyncError("remote head")
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "", nil
	}
	if len(fields) != 2 {
		return "", gitSyncError("remote head")
	}
	return fields[0], nil
}

// Commits lists the job branch's commits ahead of the base, oldest first,
// with the Push field left empty for the caller. Zero commits is an empty
// non-nil slice.
func (g *GitSync) Commits() ([]Commit, error) {
	commits := []Commit{}
	out, err := runStep("git", []string{"-C", g.Dir, "log", "--reverse", "--format=%H%x09%s", "refs/remotes/origin/" + g.Base + "..HEAD"}, g.localTimeout(), g.gitEnv())
	if err != nil {
		return nil, gitSyncError("commits")
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		sha, titulo, ok := strings.Cut(line, "\t")
		if !ok || sha == "" {
			return nil, gitSyncError("commits")
		}
		commits = append(commits, Commit{SHA: sha, Titulo: titulo})
	}
	return commits, nil
}

// Facts snapshots the worktree for the report: Clean when
// `git status --porcelain` prints nothing, Head from Head(), HeadRemoto from
// RemoteHead(). Resumo stays empty; the report builder derives removivel and
// sincronizado from these fields.
func (g *GitSync) Facts() (ReportFacts, error) {
	out, err := runStep("git", []string{"-C", g.Dir, "status", "--porcelain"}, g.localTimeout(), g.gitEnv())
	if err != nil {
		return ReportFacts{}, gitSyncError("status")
	}
	head, err := g.Head()
	if err != nil {
		return ReportFacts{}, err
	}
	remote, err := g.RemoteHead()
	if err != nil {
		return ReportFacts{}, err
	}
	return ReportFacts{Clean: strings.TrimSpace(out) == "", Head: head, HeadRemoto: remote}, nil
}

// Push pushes the local head to the remote job branch. An up-to-date remote
// runs no git push at all. A refused push (for example a non-fast-forward
// after another clone advanced the branch) is Rejected with a nil error so
// the caller can turn it into a blocked event; a timeout or a missing git is
// an error code 19. It never forces — no --force, no --force-with-lease, no
// + refspec, no --no-verify — and never retries.
func (g *GitSync) Push() (PushResult, error) {
	gitEnv := g.gitEnv()
	if _, ok := platform.FindExecutable("git", gitEnv, platform.Current()); !ok {
		return PushResult{}, gitSyncError("push")
	}
	head, err := g.Head()
	if err != nil {
		return PushResult{}, err
	}
	remote, err := g.RemoteHead()
	if err != nil {
		return PushResult{}, err
	}
	if head == remote {
		return PushResult{Head: head}, nil
	}
	// RunCli directly (deadline and WaitDelay, argv only) so a timeout, a
	// missing executable or a killed process stays apart from a refused push:
	// only a non-zero exit is reported as Rejected for the blocked event.
	result := platform.RunCli("git", []string{"-C", g.Dir, "push", "--porcelain", "origin", "HEAD:refs/heads/" + g.Branch}, platform.RunOptions{Env: gitEnv, TimeoutMs: int(g.netTimeout() / time.Millisecond)})
	switch {
	case result.NotFound || result.TimedOut || result.Status == nil:
		return PushResult{Head: head}, gitSyncError("push")
	case *result.Status == 0:
		return PushResult{Pushed: true, Head: head}, nil
	default:
		return PushResult{Rejected: true, Head: head}, nil
	}
}

// Checkpoint stages the tracked and untracked non-ignored files (.gitignore
// respected) and commits them as exactly "wip(job): checkpoint" when
// anything is staged; nothing staged commits nothing. It does not push; the
// caller pushes after it.
func (g *GitSync) Checkpoint() (bool, error) {
	env := g.gitEnv()
	local := g.localTimeout()
	if _, err := runStep("git", []string{"-C", g.Dir, "add", "-A"}, local, env); err != nil {
		return false, gitSyncError("checkpoint")
	}
	// diff --cached --quiet exits 0 when nothing is staged and 1 when the
	// index differs from HEAD. runStep reports every non-zero exit as the
	// same error, so a genuine diff failure falls through to the commit,
	// which fails on its own when nothing was staged and surfaces as the
	// checkpoint error instead of committing an empty commit.
	if _, err := runStep("git", []string{"-C", g.Dir, "diff", "--cached", "--quiet"}, local, env); err == nil {
		return false, nil
	}
	if _, err := runStep("git", []string{"-C", g.Dir, "commit", "--no-verify", "-m", "wip(job): checkpoint"}, local, env); err != nil {
		return false, gitSyncError("checkpoint")
	}
	return true, nil
}

// EnsureDraftPR returns the existing pull request of the job branch when
// one exists, and opens exactly one draft pull request against the base when
// none does. The bool reports whether this call created the pull request.
// It never marks a pull request ready, merges it, closes it, edits it, or
// deletes a branch.
func (g *GitSync) EnsureDraftPR(title, body string) (PullRequest, bool, error) {
	ghEnv := g.ghEnv()
	gh := g.ghTimeout()
	list, err := runStep("gh", []string{"pr", "list", "--repo", g.Repo, "--head", g.Branch, "--state", "all", "--json", "number,url,isDraft", "--limit", "1"}, gh, ghEnv)
	if err != nil {
		return PullRequest{}, false, gitSyncError("pr")
	}
	// Only a JSON array of zero or one element is accepted; anything else
	// (garbage, null, more than one) is a malformed response and refuses the
	// whole call instead of guessing.
	var items []json.RawMessage
	if err := json.Unmarshal([]byte(list), &items); err != nil || items == nil {
		return PullRequest{}, false, gitSyncError("pr")
	}
	if len(items) == 1 {
		var item struct {
			Numero  int    `json:"number"`
			URL     string `json:"url"`
			IsDraft bool   `json:"isDraft"`
		}
		if err := json.Unmarshal(items[0], &item); err != nil {
			return PullRequest{}, false, gitSyncError("pr")
		}
		estado := "open"
		if item.IsDraft {
			estado = "draft"
		}
		return PullRequest{Numero: item.Numero, URL: item.URL, Estado: estado}, false, nil
	}
	if len(items) != 0 {
		return PullRequest{}, false, gitSyncError("pr")
	}
	create, err := runStep("gh", []string{"pr", "create", "--repo", g.Repo, "--draft", "--base", g.Base, "--head", g.Branch, "--title", title, "--body", body}, gh, ghEnv)
	if err != nil {
		return PullRequest{}, false, gitSyncError("pr")
	}
	url := lastNonEmptyLine(create)
	idx := strings.LastIndex(url, "/pull/")
	if idx < 0 {
		return PullRequest{}, false, gitSyncError("pr")
	}
	numero, err := strconv.Atoi(url[idx+len("/pull/"):])
	if err != nil {
		return PullRequest{}, false, gitSyncError("pr")
	}
	return PullRequest{Numero: numero, URL: url, Estado: "draft"}, true, nil
}

// lastNonEmptyLine is the last non-empty line of the output; gh prints
// progress lines before the pull request URL.
func lastNonEmptyLine(out string) string {
	lines := strings.Split(out, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
