package job

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// actor_confine_test.go: the workspace session pin end to end over real
// git checkouts and real configuration layers, across the state roots of
// both sides (default, relative custom, absolute custom), with the team
// the starter's user configuration selects; and the confinement of every
// write the pin makes.

const acfWorkspace = "w7"

type acfFix struct {
	root, co, wt, id string
	env              platform.Env
}

// newAcf builds a real checkout "<root>/repos/org/re po" with the job
// worktree <co>/.worktrees/job-<id>, and an environment with git on PATH,
// an isolated git configuration and the repository's skill.
func newAcf(t *testing.T) *acfFix {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := filepath.Abs("../../skills/herdr-soho")
	if err != nil {
		t.Fatal(err)
	}
	gitConf := filepath.Join(root, "gitconfig")
	if err := os.WriteFile(gitConf, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	f := &acfFix{root: root, id: "j1", env: platform.Env{
		"PATH": filepath.Dir(gitPath), "HOME": home, "USERPROFILE": home,
		"GIT_CONFIG_GLOBAL": gitConf, "GIT_CONFIG_NOSYSTEM": "1",
		"HERDR_SOHO_SKILL_DIR": skill,
	}}
	f.co = filepath.Join(root, "repos", "org", "re po")
	if err := os.MkdirAll(f.co, 0o755); err != nil {
		t.Fatal(err)
	}
	f.git(t, f.co, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(f.co, "README.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.git(t, f.co, "add", "README.md")
	f.git(t, f.co, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "commit", "-q", "-m", "init")
	f.wt = filepath.Join(f.co, ".worktrees", "job-"+f.id)
	f.git(t, f.co, "worktree", "add", "-q", "-b", "job/"+f.id, f.wt)
	return f
}

func (f *acfFix) git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = f.env.List()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, out, err)
	}
	return string(out)
}

// userBase writes <root>/<name>/herdr-soho/config and returns the base.
func (f *acfFix) userBase(t *testing.T, name, body string) string {
	t.Helper()
	base := filepath.Join(f.root, name)
	if err := os.MkdirAll(filepath.Join(base, "herdr-soho"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(base, "herdr-soho", "config"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return base
}

// job records the job (worktree mode unless dir is the checkout) in the
// store at storeRoot and writes the start's session file.
func (f *acfFix) job(t *testing.T, storeRoot, dir, session string) *Store {
	t.Helper()
	store := Open(storeRoot)
	if _, err := store.Start(f.id, []byte(strings.Replace(supTestBrief, `"job-1"`, `"`+f.id+`"`, 1))); err != nil {
		t.Fatal(err)
	}
	modo := "worktree"
	if dir == f.co {
		modo = "workspace"
	}
	if _, err := store.Record(f.id, func(st *State) {
		st.Checkout, st.Dir, st.Modo, st.WorkspaceID = f.co, dir, modo, acfWorkspace
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storeRoot, acfWorkspace), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storeRoot, acfWorkspace, "session.conf"), []byte(session), 0o600); err != nil {
		t.Fatal(err)
	}
	return store
}

func acfSupervisor(store *Store, id string, actorEnv platform.Env, sel core.UserSelection, friction *[]string) *Supervisor {
	return &Supervisor{Store: store, ID: id, PrivateLaunch: true, actorEnv: actorEnv, launchSel: sel,
		Friction: func(m string) { *friction = append(*friction, m) }}
}

// TestActorConfineStateRootMatrix: for every pair of state roots (the
// starter's and the agents' own: default, relative custom, absolute
// custom), with the team file naming only the lane's roles and the
// starter's user configuration choosing the implementer's kind, model,
// effort and max_workers while the agents' own configuration chooses
// others, after the pin the agents resolve the job's state root, the
// starter's team, their own approvals (never copied), and the job by its
// own checkout; a workspace pin inside the checkout is excluded from git.
func TestActorConfineStateRootMatrix(t *testing.T) {
	type side struct{ name, line, rel, abs string }
	sides := []side{
		{name: "default"},
		{name: "relative", rel: "%s state"},
		{name: "absolute", abs: "%s abs"},
	}
	for _, starter := range sides {
		for _, agent := range sides {
			t.Run("starter "+starter.name+", agents "+agent.name, func(t *testing.T) {
				f := newAcf(t)
				root := func(s side, who string) (string, string) {
					switch {
					case s.rel != "":
						rel := strings.Replace(s.rel, "%s", who, 1)
						return filepath.Join(f.co, rel), "state_dir=" + rel + "\n"
					case s.abs != "":
						abs := filepath.Join(f.root, strings.Replace(s.abs, "%s", who, 1))
						return abs, "state_dir=" + abs + "\n"
					}
					return filepath.Join(f.co, ".herdr-soho"), ""
				}
				storeRoot, starterLine := root(starter, "starter")
				agentRoot, agentLine := root(agent, "agent")
				base := f.userBase(t, "starter-xdg", "role.implementer.kind=pi\nrole.implementer.model=model-a\nrole.implementer.effort=low\nmax_workers=5\n"+starterLine)
				agentXDG := f.userBase(t, "agent-xdg", "role.implementer.kind=claude\nrole.implementer.model=model-b\nrole.implementer.effort=high\nmax_workers=1\napprovals=full\n"+agentLine)
				store := f.job(t, storeRoot, f.wt, "lane.build.roles=implementer\n")
				actorEnv := f.env.Clone()
				actorEnv["XDG_CONFIG_HOME"] = agentXDG
				sel := core.UserSelection{Base: base}
				var friction []string
				sup := acfSupervisor(store, f.id, actorEnv, sel, &friction)
				if err := sup.pinActorSession(); err != nil {
					t.Fatalf("pin: %v (friction %v)", err, friction)
				}
				agentEnv := actorEnv.Clone()
				agentEnv["HERDR_WORKSPACE_ID"] = acfWorkspace
				ctx := core.LoadConfig(agentEnv, f.wt)
				if got := core.StateRootPath(&ctx, agentEnv, f.wt); !sameEntryPath(got, storeRoot) {
					t.Fatalf("agents' state root = %s, want the job's %s", got, storeRoot)
				}
				rs := core.ResolveRoleSettings("implementer", &ctx, agentEnv, f.wt, core.RoleFlags{})
				if rs.Lane != "build" || rs.Kind != "pi" || rs.ModelSpec != "model-a" || rs.Effort != "low" {
					t.Fatalf("agents' implementer = lane %q kind %q model %q effort %q, want build/pi/model-a/low", rs.Lane, rs.Kind, rs.ModelSpec, rs.Effort)
				}
				if rs.Approvals != "full" {
					t.Fatalf("agents' approvals = %q (%s), want their own full: approvals are never copied", rs.Approvals, rs.ApprovalsFrom)
				}
				if mw := core.Cfg(&ctx, "max_workers", "", agentEnv); mw != "5" {
					t.Fatalf("agents' max_workers = %q, want the starter's 5", mw)
				}
				// The supervisor's own view is unchanged by the snapshot.
				sctx := core.LoadConfigFor(agentEnv, f.wt, sel)
				srs := core.ResolveRoleSettings("implementer", &sctx, agentEnv, f.wt, core.RoleFlags{})
				if srs.Kind != "pi" || srs.ModelSpec != "model-a" || srs.Effort != "low" || srs.Approvals == "full" {
					t.Fatalf("starter's implementer after the pin = %+v", srs)
				}
				if got := OwnCheckoutStore(agentEnv, filepath.Join(f.wt), f.id); got == nil || !sameEntryPath(got.Root, storeRoot) {
					t.Fatalf("own-checkout anchor from the worktree = %v, want %s", got, storeRoot)
				}
				pin := filepath.Join(agentRoot, acfWorkspace, "session.conf")
				_, pinErr := os.Lstat(pin)
				if sameEntryPath(agentRoot, storeRoot) {
					if starter.name == agent.name && starter.name == "relative" {
						t.Fatal("fixture: distinct relative names expected")
					}
				} else if pinErr != nil {
					t.Fatalf("workspace pin %s: %v", pin, pinErr)
				}
				if agent.rel != "" && pinErr == nil {
					rel, _ := filepath.Rel(f.co, filepath.Join(agentRoot, acfWorkspace))
					line := "/" + filepath.ToSlash(rel) + "/"
					exclude, err := os.ReadFile(filepath.Join(f.co, ".git", "info", "exclude"))
					if err != nil || !excludeHas(exclude, line) {
						t.Fatalf("exclude lacks %q: %q %v", line, exclude, err)
					}
					if status := f.git(t, f.co, "status", "--porcelain", "--untracked-files=all"); strings.Contains(status, filepath.ToSlash(rel)) {
						t.Fatalf("git status shows the workspace pin: %q", status)
					}
				}
			})
		}
	}
}

// TestActorConfineWorkspaceMode: the same pin for a workspace-mode job
// (the job directory is the checkout).
func TestActorConfineWorkspaceMode(t *testing.T) {
	f := newAcf(t)
	storeRoot := filepath.Join(f.root, "starter abs")
	base := f.userBase(t, "starter-xdg", "role.implementer.model=model-a\nstate_dir="+storeRoot+"\n")
	agentXDG := f.userBase(t, "agent-xdg", "role.implementer.model=model-b\n")
	store := f.job(t, storeRoot, f.co, "lane.build.roles=implementer\nlane.build.kind=pi\n")
	actorEnv := f.env.Clone()
	actorEnv["XDG_CONFIG_HOME"] = agentXDG
	var friction []string
	if err := acfSupervisor(store, f.id, actorEnv, core.UserSelection{Base: base}, &friction).pinActorSession(); err != nil {
		t.Fatalf("pin: %v (%v)", err, friction)
	}
	agentEnv := actorEnv.Clone()
	agentEnv["HERDR_WORKSPACE_ID"] = acfWorkspace
	ctx := core.LoadConfig(agentEnv, f.co)
	rs := core.ResolveRoleSettings("implementer", &ctx, agentEnv, f.co, core.RoleFlags{})
	// lane.build.kind is the session's; the starter's user-layer role model
	// was below it and discarded, so the resolved model is the pi default
	// (none) on both sides: the snapshot adds no role model.
	sctx := core.LoadConfigFor(agentEnv, f.co, core.UserSelection{Base: base})
	srs := core.ResolveRoleSettings("implementer", &sctx, agentEnv, f.co, core.RoleFlags{})
	if rs.Kind != "pi" || rs.ModelSpec != srs.ModelSpec || rs.Effort != srs.Effort {
		t.Fatalf("agents %+v, starter %+v", rs, srs)
	}
	if got := OwnCheckoutStore(agentEnv, filepath.Join(f.co, "README.md", ".."), f.id); got == nil || !sameEntryPath(got.Root, storeRoot) {
		t.Fatalf("anchor = %v", got)
	}
}

// TestActorConfineRefusesWithoutWriting: a link at a level the pin writes
// (the state root, the workspace directory, the session file) is refused
// and the link's target is left as it was; a missing level under an
// existing alias ancestor is created at the alias's real location.
func TestActorConfineRefusesWithoutWriting(t *testing.T) {
	newSup := func(t *testing.T) (*Supervisor, string, *[]string) {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		var friction []string
		return &Supervisor{Store: Open(filepath.Join(root, "store")), Friction: func(m string) { friction = append(friction, m) }}, root, &friction
	}
	t.Run("state root is a link", func(t *testing.T) {
		sup, root, friction := newSup(t)
		target := filepath.Join(root, "target")
		mustMkdir(t, target)
		mustSymlink(t, target, filepath.Join(root, "linked"))
		if sup.writeSessionFile(filepath.Join(root, "linked"), acfWorkspace, "a=1\n", false) {
			t.Fatal("written through a linked state root")
		}
		assertEmptyDir(t, target)
		if len(*friction) != 1 {
			t.Fatalf("friction = %v", *friction)
		}
	})
	t.Run("workspace directory is a link", func(t *testing.T) {
		sup, root, _ := newSup(t)
		target := filepath.Join(root, "target")
		mustMkdir(t, target)
		mustMkdir(t, filepath.Join(root, "state"))
		mustSymlink(t, target, filepath.Join(root, "state", acfWorkspace))
		if sup.writeSessionFile(filepath.Join(root, "state"), acfWorkspace, "a=1\n", false) {
			t.Fatal("written through a linked workspace directory")
		}
		assertEmptyDir(t, target)
	})
	t.Run("session file is a link", func(t *testing.T) {
		sup, root, _ := newSup(t)
		target := filepath.Join(root, "target.conf")
		if err := os.WriteFile(target, []byte("keep=1\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, filepath.Join(root, "state", acfWorkspace))
		mustSymlink(t, target, filepath.Join(root, "state", acfWorkspace, "session.conf"))
		if sup.writeSessionFile(filepath.Join(root, "state"), acfWorkspace, "a=1\n", false) {
			t.Fatal("written over a linked session file")
		}
		if got, _ := os.ReadFile(target); string(got) != "keep=1\n" {
			t.Fatalf("link target = %q", got)
		}
		if info, err := os.Lstat(filepath.Join(root, "state", acfWorkspace, "session.conf")); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("the link was replaced: %v %v", info, err)
		}
	})
	t.Run("missing levels under an alias ancestor", func(t *testing.T) {
		sup, root, _ := newSup(t)
		real := filepath.Join(root, "real")
		mustMkdir(t, real)
		mustSymlink(t, real, filepath.Join(root, "alias"))
		if !sup.writeSessionFile(filepath.Join(root, "alias", "new", "state"), acfWorkspace, "a=1\n", false) {
			t.Fatal("refused under an alias ancestor")
		}
		got, err := os.ReadFile(filepath.Join(real, "new", "state", acfWorkspace, "session.conf"))
		if err != nil || string(got) != "a=1\n" {
			t.Fatalf("session at the alias's real location = %q %v", got, err)
		}
		for _, level := range []string{filepath.Join(real, "new"), filepath.Join(real, "new", "state"), filepath.Join(real, "new", "state", acfWorkspace)} {
			if info, err := os.Lstat(level); err != nil || info.Mode().Type() != os.ModeDir || info.Mode().Perm() != 0o700 {
				t.Fatalf("created level %s: %v %v", level, info, err)
			}
		}
	})
	t.Run("an existing level that is a file", func(t *testing.T) {
		sup, root, _ := newSup(t)
		if err := os.WriteFile(filepath.Join(root, "state"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if sup.writeSessionFile(filepath.Join(root, "state"), acfWorkspace, "a=1\n", false) {
			t.Fatal("written below a file")
		}
	})
	t.Run("the workspace pin must read back the store root", func(t *testing.T) {
		sup, root, _ := newSup(t)
		if sup.writeSessionFile(filepath.Join(root, "state"), acfWorkspace, "state_dir=/elsewhere\n", true) {
			t.Fatal("a pin naming another root was accepted")
		}
		if !sup.writeSessionFile(filepath.Join(root, "state2"), acfWorkspace, pinnedSessionBody("a=1\n", sup.Store.Root), true) {
			t.Fatal("a pin naming the store root was refused")
		}
	})
}

// TestActorConfineExclude: the checkout's local exclude file gains the
// pin line once, through a regular file at its own path; a link at info or
// exclude is refused and its target is left as it was.
func TestActorConfineExclude(t *testing.T) {
	newCheckout := func(t *testing.T) string {
		root, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		mustMkdir(t, filepath.Join(root, ".git"))
		return root
	}
	t.Run("missing info and file are created, then the line is not repeated", func(t *testing.T) {
		co := newCheckout(t)
		for i := 0; i < 2; i++ {
			if !appendExclude(co, "/agent state/w7/") {
				t.Fatalf("append %d refused", i)
			}
		}
		got, _ := os.ReadFile(filepath.Join(co, ".git", "info", "exclude"))
		if string(got) != "/agent state/w7/\n" {
			t.Fatalf("exclude = %q", got)
		}
	})
	t.Run("an existing file without a final newline", func(t *testing.T) {
		co := newCheckout(t)
		mustMkdir(t, filepath.Join(co, ".git", "info"))
		if err := os.WriteFile(filepath.Join(co, ".git", "info", "exclude"), []byte("/.herdr-soho/"), 0o644); err != nil {
			t.Fatal(err)
		}
		if !appendExclude(co, "/x/w7/") {
			t.Fatal("refused")
		}
		got, _ := os.ReadFile(filepath.Join(co, ".git", "info", "exclude"))
		if string(got) != "/.herdr-soho/\n/x/w7/\n" {
			t.Fatalf("exclude = %q", got)
		}
	})
	t.Run("exclude is a link", func(t *testing.T) {
		co := newCheckout(t)
		mustMkdir(t, filepath.Join(co, ".git", "info"))
		target := filepath.Join(co, "outside")
		if err := os.WriteFile(target, []byte("keep\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		mustSymlink(t, target, filepath.Join(co, ".git", "info", "exclude"))
		if appendExclude(co, "/x/w7/") {
			t.Fatal("appended through a link")
		}
		if got, _ := os.ReadFile(target); string(got) != "keep\n" {
			t.Fatalf("target = %q", got)
		}
	})
	t.Run("info is a link", func(t *testing.T) {
		co := newCheckout(t)
		target := filepath.Join(co, "outside")
		mustMkdir(t, target)
		mustSymlink(t, target, filepath.Join(co, ".git", "info"))
		if appendExclude(co, "/x/w7/") {
			t.Fatal("appended through a linked info")
		}
		assertEmptyDir(t, target)
	})
	t.Run("oversized exclude", func(t *testing.T) {
		co := newCheckout(t)
		mustMkdir(t, filepath.Join(co, ".git", "info"))
		big := strings.Repeat("#\n", maxExcludeBytes/2+1)
		if err := os.WriteFile(filepath.Join(co, ".git", "info", "exclude"), []byte(big), 0o644); err != nil {
			t.Fatal(err)
		}
		if appendExclude(co, "/x/w7/") {
			t.Fatal("appended to an oversized exclude")
		}
	})
}

// TestActorConfineCaseVariant: a case variant of an existing path is the
// same entry exactly when the file system is case-insensitive (probed on
// the test's own directory), so identity, not path strings, decides; on a
// case-sensitive file system the variant is another (missing) path.
func TestActorConfineCaseVariant(t *testing.T) {
	f := newAcf(t)
	storeRoot := filepath.Join(f.co, ".herdr-soho")
	f.job(t, storeRoot, f.wt, "lane.build.roles=implementer\n")
	variant := filepath.Join(filepath.Dir(f.co), strings.ToUpper(filepath.Base(f.co)))
	_, statErr := os.Stat(variant)
	insensitive := statErr == nil
	if got := sameEntryPath(variant, f.co); got != insensitive {
		t.Fatalf("sameEntryPath(case variant) = %v, file system case-insensitive = %v", got, insensitive)
	}
	wtVariant := filepath.Join(variant, ".worktrees", "job-"+f.id)
	got := OwnCheckoutStore(f.env, wtVariant, f.id)
	if insensitive && (got == nil || !sameEntryPath(got.Root, storeRoot)) {
		t.Fatalf("anchor from the case variant cwd = %v, want the job's store", got)
	}
	if !insensitive && got != nil {
		t.Fatalf("anchor from a missing case variant = %v", got.Root)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
}

func assertEmptyDir(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("%s changed: %v %v", dir, entries, err)
	}
}
