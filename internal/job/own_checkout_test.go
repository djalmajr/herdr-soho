package job

// own_checkout_test.go pins the own-checkout anchor (OwnCheckoutStore)
// against real git repositories: the query's output bounds
// (parseOwnCheckoutGit, cappedOutput), the workspace/worktree registration
// shapes, the real-directory store requirement, and the state.json identity
// checks. The found subtests are the controls: they fail if any check
// over-rejects a valid job. Every nil subtest names, in a comment, the
// production check it would catch if that check were removed.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// ownEnv builds the hermetic environment OwnCheckoutStore receives: a PATH
// that resolves only the system git, a fresh HOME and XDG_CONFIG_HOME under
// a temp dir, the isolated git configuration (empty global and system
// files), and the skill directory relative to the test working directory.
// It carries no HERDR_ENV or HERDR_WORKSPACE_ID.
func ownEnv(t *testing.T) platform.Env {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	conf := filepath.Join(root, "conf")
	global := filepath.Join(root, "gitconfig-global")
	system := filepath.Join(root, "gitconfig-system")
	for _, dir := range []string{home, conf} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{global, system} {
		if err := os.WriteFile(file, []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return platform.Env{
		"PATH":              prepareGitOnlyDir(t),
		"HOME":              home,
		"XDG_CONFIG_HOME":   conf,
		"GIT_CONFIG_GLOBAL": global,
		"GIT_CONFIG_SYSTEM": system,
		"SOHO_SKILL_DIR":    "../../skills/soho",
	}
}

// realTmp returns the real (symlink-resolved) path of a fresh temp dir. git
// reports real paths, so every expectation is built from real ones.
func realTmp(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	real, err := filepath.EvalSymlinks(tmp)
	if err != nil {
		t.Fatalf("resolve the temp dir: %v", err)
	}
	return real
}

// gitIn runs a real git with the hermetic env and the fixture identity
// (user.name/user.email via -c), so no global or system configuration is
// read or written.
func gitIn(t *testing.T, env platform.Env, dir string, args ...string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("locate the system git: %v", err)
	}
	full := append([]string{"-c", "user.name=fixture", "-c", "user.email=fixture@example.org"}, args...)
	cmd := exec.Command(gitPath, full...)
	cmd.Dir = dir
	cmd.Env = env.List()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

// initRepo makes dir a real repository with one commit.
func initRepo(t *testing.T, env platform.Env, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, env, dir, "init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, env, dir, "add", ".")
	gitIn(t, env, dir, "commit", "-q", "-m", "fixture")
}

// ownFixture is one real checkout tree the anchor is checked against:
// checkout is the main checkout (real path), top the measured top level,
// and modo the mode that shape measures.
type ownFixture struct {
	env      platform.Env
	tmp      string
	id       string
	checkout string
	top      string
	modo     string
}

// newOwnWorkspaceFixture builds a plain repository: workspace mode, where
// the git dir is the common dir <checkout>/.git.
func newOwnWorkspaceFixture(t *testing.T, id string) *ownFixture {
	t.Helper()
	env := ownEnv(t)
	tmp := realTmp(t)
	checkout := filepath.Join(tmp, "checkout")
	initRepo(t, env, checkout)
	return &ownFixture{env: env, tmp: tmp, id: id, checkout: checkout, top: checkout, modo: "workspace"}
}

// newOwnWorktreeFixture builds the main repository plus the linked job
// worktree at <checkout>/.worktrees/job-<id>: worktree mode.
func newOwnWorktreeFixture(t *testing.T, id string) *ownFixture {
	t.Helper()
	f := newOwnWorkspaceFixture(t, id)
	worktree := filepath.Join(f.checkout, ".worktrees", "job-"+id)
	gitIn(t, f.env, f.checkout, "worktree", "add", "-q", worktree, "-b", "job/"+id)
	f.top = worktree
	f.modo = "worktree"
	return f
}

// storeRoot is the default state root: <checkout>/.soho.
func (f *ownFixture) storeRoot() string {
	return filepath.Join(f.checkout, ".soho")
}

// ownStateValues is the state.json body the brief names: schema 1, the id,
// status running, brief_sha256 "x", decisions_acked_seq 0, motivo null, and
// the checkout, dir and modo the anchor verifies.
func ownStateValues(id, checkout, dir, modo string) map[string]any {
	return map[string]any{
		"schema":              1,
		"id":                  id,
		"status":              "running",
		"brief_sha256":        "x",
		"decisions_acked_seq": 0,
		"motivo":              nil,
		"checkout":            checkout,
		"dir":                 dir,
		"modo":                modo,
	}
}

// writeStateFile writes <jobDir>/state.json from values.
func writeStateFile(t *testing.T, jobDir string, values map[string]any) {
	t.Helper()
	data, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("marshal the state: %v", err)
	}
	if err := os.MkdirAll(jobDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "state.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// installState writes the fixture job's state.json under root recording dir
// as the job directory.
func (f *ownFixture) installState(t *testing.T, root, dir string) {
	t.Helper()
	writeStateFile(t, filepath.Join(root, "jobs", f.id), ownStateValues(f.id, f.checkout, dir, f.modo))
}

// installStore writes the fixture job's valid state under root.
func (f *ownFixture) installStore(t *testing.T, root string) {
	f.installState(t, root, f.top)
}

// expectStore asserts the anchor finds the store at exactly want.
func (f *ownFixture) expectStore(t *testing.T, env platform.Env, cwd, want string) {
	t.Helper()
	got := OwnCheckoutStore(env, cwd, f.id)
	if got == nil {
		t.Fatalf("OwnCheckoutStore(cwd %q, id %q) = nil, want the store at %q", cwd, f.id, want)
	}
	if got.Root != want {
		t.Fatalf("store root = %q, want %q", got.Root, want)
	}
}

// expectNil asserts the anchor finds no store.
func (f *ownFixture) expectNil(t *testing.T, env platform.Env, cwd string) {
	t.Helper()
	if got := OwnCheckoutStore(env, cwd, f.id); got != nil {
		t.Fatalf("OwnCheckoutStore(cwd %q, id %q) = the store at %q, want nil", cwd, f.id, got.Root)
	}
}

// TestOwnCheckout pins the own-checkout anchor against real repositories.
func TestOwnCheckout(t *testing.T) {
	t.Run("worktree: cwd is the worktree root", func(t *testing.T) {
		f := newOwnWorktreeFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		// Control for worktree mode: the git-dir/common-dir shape, the
		// <checkout>/.worktrees/job-<id> top, worktreeRegistered and the
		// state identity checks must all accept a valid job.
		f.expectStore(t, f.env, f.top, f.storeRoot())
	})

	t.Run("worktree: cwd is a nested subdirectory with a space", func(t *testing.T) {
		f := newOwnWorktreeFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		nested := filepath.Join(f.top, "nested dir")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		// withinOrSame must accept a cwd strictly inside the top level,
		// including a path with a space: a cwd == top-only comparison or a
		// separator-blind string prefix would reject it.
		f.expectStore(t, f.env, nested, f.storeRoot())
	})

	t.Run("workspace: cwd is the checkout", func(t *testing.T) {
		f := newOwnWorkspaceFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		// Control for workspace mode: the git dir is the common dir
		// <top>/.git and the checkout is the top level.
		f.expectStore(t, f.env, f.checkout, f.storeRoot())
	})

	t.Run("workspace: cwd is a nested directory", func(t *testing.T) {
		f := newOwnWorkspaceFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		nested := filepath.Join(f.checkout, "nested dir")
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatal(err)
		}
		// Control for withinOrSame in workspace mode: a cwd strictly inside
		// the top level anchors the job.
		f.expectStore(t, f.env, nested, f.storeRoot())
	})

	t.Run("workspace: state modo is worktree", func(t *testing.T) {
		f := newOwnWorkspaceFixture(t, "j1")
		writeStateFile(t, filepath.Join(f.storeRoot(), "jobs", f.id), ownStateValues(f.id, f.checkout, f.checkout, "worktree"))
		// st.Modo != mode: the recorded modo must equal the measured mode;
		// a worktree's state cannot anchor a workspace checkout.
		f.expectNil(t, f.env, f.checkout)
	})

	t.Run("symlinked cwd pointing at the worktree", func(t *testing.T) {
		f := newOwnWorktreeFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		link := filepath.Join(f.tmp, "worktree-link")
		if err := os.Symlink(f.top, link); err != nil {
			t.Fatal(err)
		}
		// realAbs(cwd): the anchor resolves a symlinked cwd to the real top
		// level; comparing the raw cwd against git's real top would reject
		// it.
		f.expectStore(t, f.env, link, f.storeRoot())
	})

	t.Run("symlinked store components", func(t *testing.T) {
		t.Run("state root", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			real := filepath.Join(f.tmp, "real-store")
			f.installStore(t, real)
			if err := os.Symlink(real, f.storeRoot()); err != nil {
				t.Fatal(err)
			}
			// exactDirectory(root): the store root must be a directory at
			// its own path, not a symlink to one.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("jobs", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			root := f.storeRoot()
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(f.tmp, "elsewhere")
			f.installState(t, other, f.top)
			if err := os.Symlink(filepath.Join(other, "jobs"), filepath.Join(root, "jobs")); err != nil {
				t.Fatal(err)
			}
			// exactDirectory(root/jobs): the jobs directory must be a real
			// directory, not a symlink to one.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("job directory", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			jobs := filepath.Join(f.storeRoot(), "jobs")
			if err := os.MkdirAll(jobs, 0o700); err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(f.tmp, "real-job")
			writeStateFile(t, other, ownStateValues(f.id, f.checkout, f.top, f.modo))
			if err := os.Symlink(other, filepath.Join(jobs, f.id)); err != nil {
				t.Fatal(err)
			}
			// exactDirectory(jobs/<id>): the job directory must be a real
			// directory, not a symlink to one.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("state.json", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			jobDir := filepath.Join(f.storeRoot(), "jobs", f.id)
			if err := os.MkdirAll(jobDir, 0o700); err != nil {
				t.Fatal(err)
			}
			data, err := json.Marshal(ownStateValues(f.id, f.checkout, f.top, f.modo))
			if err != nil {
				t.Fatal(err)
			}
			other := filepath.Join(f.tmp, "real-state.json")
			if err := os.WriteFile(other, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(other, filepath.Join(jobDir, "state.json")); err != nil {
				t.Fatal(err)
			}
			// readPrivateFile refuses to follow: a symlink named state.json
			// is not the regular file the path names, even when the target
			// is a valid state.
			f.expectNil(t, f.env, f.top)
		})
	})

	t.Run("state.json variants", func(t *testing.T) {
		jobDir := func(f *ownFixture) string { return filepath.Join(f.storeRoot(), "jobs", f.id) }

		t.Run("oversized", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			values := ownStateValues(f.id, f.checkout, f.top, f.modo)
			values["pad"] = strings.Repeat("a", 1<<20)
			writeStateFile(t, jobDir(f), values)
			// ownCheckoutStateMax: a valid state over 1 MiB is refused by
			// the read bound before it is parsed.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("is a directory", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			if err := os.MkdirAll(filepath.Join(f.storeRoot(), "jobs", f.id, "state.json"), 0o700); err != nil {
				t.Fatal(err)
			}
			// readPrivateFile's regular-file requirement: a directory named
			// state.json is refused.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("invalid JSON", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			dir := jobDir(f)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
			// the json.Unmarshal failure: a syntactically broken state is
			// refused.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("schema 2", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			values := ownStateValues(f.id, f.checkout, f.top, f.modo)
			values["schema"] = 2
			writeStateFile(t, jobDir(f), values)
			// st.Schema != 1: only schema 1 states anchor.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("other id", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			values := ownStateValues(f.id, f.checkout, f.top, f.modo)
			values["id"] = "j9"
			writeStateFile(t, jobDir(f), values)
			// st.ID != id: the state must name the queried id.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("other modo", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			writeStateFile(t, jobDir(f), ownStateValues(f.id, f.checkout, f.top, "workspace"))
			// st.Modo != mode: the measured mode of a linked worktree is
			// worktree; a workspace modo does not match it.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("relative checkout", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			writeStateFile(t, jobDir(f), ownStateValues(f.id, "checkout", f.top, f.modo))
			// filepath.IsAbs(st.Checkout): a relative recorded checkout is
			// refused (realAbs would also fail to resolve it).
			f.expectNil(t, f.env, f.top)
		})

		t.Run("relative dir", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			writeStateFile(t, jobDir(f), ownStateValues(f.id, f.checkout, ".worktrees/job-j1", f.modo))
			// filepath.IsAbs(st.Dir): a relative recorded dir is refused
			// (realAbs would also fail to resolve it).
			f.expectNil(t, f.env, f.top)
		})

		t.Run("checkout names another checkout", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			other := filepath.Join(f.tmp, "foreign")
			if err := os.MkdirAll(other, 0o700); err != nil {
				t.Fatal(err)
			}
			writeStateFile(t, jobDir(f), ownStateValues(f.id, other, f.top, f.modo))
			// realAbs(st.Checkout) != checkout alone: the dir and the modo
			// match, but a state copied from another checkout names that
			// checkout, not the measured one.
			f.expectNil(t, f.env, f.top)
		})

		t.Run("dir names another directory", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			writeStateFile(t, jobDir(f), ownStateValues(f.id, f.checkout, f.checkout, f.modo))
			// realAbs(st.Dir) != top alone: the checkout and the modo match,
			// but the recorded job directory is the main checkout, not the
			// measured worktree top.
			f.expectNil(t, f.env, f.top)
		})
	})

	t.Run("worktree shapes", func(t *testing.T) {
		t.Run("top is a non-job path", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			other := filepath.Join(f.checkout, ".worktrees", "other")
			gitIn(t, f.env, f.checkout, "worktree", "add", "-q", other, "-b", "other")
			f.installState(t, f.storeRoot(), other)
			// top != <checkout>/.worktrees/job-<id>: a linked worktree that
			// is not the job's own directory cannot anchor it, even with a
			// state recording that top.
			f.expectNil(t, f.env, other)
		})

		t.Run("top is outside .worktrees", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			other := filepath.Join(f.checkout, "elsewhere", "job-"+f.id)
			gitIn(t, f.env, f.checkout, "worktree", "add", "-q", other, "-b", "elsewhere")
			f.installState(t, f.storeRoot(), other)
			// top != <checkout>/.worktrees/job-<id>: a worktree named
			// job-<id> under a foreign directory does not satisfy the shape.
			f.expectNil(t, f.env, other)
		})

		t.Run("top is registered for another id", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			other := filepath.Join(f.checkout, ".worktrees", "job-j9")
			gitIn(t, f.env, f.checkout, "worktree", "add", "-q", other, "-b", "job/j9")
			f.installState(t, f.storeRoot(), other)
			// top != <checkout>/.worktrees/job-<id> for the queried id: the
			// job-j9 worktree cannot anchor job j1.
			f.expectNil(t, f.env, other)
		})

		t.Run("gitdir file points elsewhere", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			f.installStore(t, f.storeRoot())
			elsewhere := filepath.Join(f.tmp, "elsewhere-dotgit")
			if err := os.MkdirAll(elsewhere, 0o700); err != nil {
				t.Fatal(err)
			}
			gitdirFile := filepath.Join(f.checkout, ".git", "worktrees", filepath.Base(f.top), "gitdir")
			if err := os.WriteFile(gitdirFile, []byte(elsewhere+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// worktreeRegistered's back reference: <gitdir>/gitdir must name
			// the worktree's .git file; an edited registration fails.
			f.expectNil(t, f.env, f.top)
		})

		t.Run(".git names a different gitdir", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			f.installStore(t, f.storeRoot())
			elsewhere := filepath.Join(f.tmp, "elsewhere-gitdir")
			if err := os.MkdirAll(elsewhere, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(f.top, ".git"), []byte("gitdir: "+elsewhere+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// worktreeRegistered's forward reference: <top>/.git must name
			// the measured git dir; a replaced gitdir line fails.
			f.expectNil(t, f.env, f.top)
		})
	})

	t.Run("cwd outside any repository", func(t *testing.T) {
		f := newOwnWorktreeFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		plain := filepath.Join(f.tmp, "plain")
		if err := os.MkdirAll(plain, 0o700); err != nil {
			t.Fatal(err)
		}
		// ownCheckoutGitQuery's error status: git rev-parse fails outside a
		// repository, so the query reports not ok and no anchor exists.
		f.expectNil(t, f.env, plain)
	})

	t.Run("unrelated repo with the same layout and copied state", func(t *testing.T) {
		src := newOwnWorkspaceFixture(t, "j1")
		src.installStore(t, src.storeRoot())
		data, err := os.ReadFile(filepath.Join(src.storeRoot(), "jobs", src.id, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		dst := newOwnWorkspaceFixture(t, "j1")
		jobDir := filepath.Join(dst.storeRoot(), "jobs", dst.id)
		if err := os.MkdirAll(jobDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(jobDir, "state.json"), data, 0o600); err != nil {
			t.Fatal(err)
		}
		// realAbs(st.Checkout) != checkout (and realAbs(st.Dir) != top): a
		// verbatim copy of another job's state names that job's checkout,
		// not the measured one.
		dst.expectNil(t, dst.env, dst.checkout)
	})

	t.Run("copied state rewritten to name the foreign dir", func(t *testing.T) {
		src := newOwnWorkspaceFixture(t, "j1")
		src.installStore(t, src.storeRoot())
		dst := newOwnWorkspaceFixture(t, "j1")
		writeStateFile(t, filepath.Join(dst.storeRoot(), "jobs", dst.id), ownStateValues(dst.id, src.checkout, dst.checkout, "workspace"))
		// realAbs(st.Checkout) != checkout alone: the dir matches the
		// measured top and the modo matches the measured mode, but the
		// checkout names the source checkout.
		dst.expectNil(t, dst.env, dst.checkout)
	})

	t.Run("custom state root", func(t *testing.T) {
		t.Run("user config state_dir", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			custom := filepath.Join(f.tmp, "custom-state")
			f.installStore(t, custom)
			confDir := filepath.Join(f.env["XDG_CONFIG_HOME"], "soho")
			if err := os.MkdirAll(confDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(confDir, "config"), []byte("state_dir="+custom+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			// StateDirSetting: the user layer's state_dir names the store
			// root under the caller's own configuration; a hard-coded
			// <checkout>/.soho would find nothing here.
			f.expectStore(t, f.env, f.top, filepath.Clean(custom))
		})

		t.Run("SOHO_DIR names the store root", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			custom := filepath.Join(f.tmp, "env-state")
			f.installStore(t, custom)
			env := f.env.Clone()
			env["SOHO_DIR"] = custom
			// StateRootPath: SOHO_DIR wins over every config layer.
			f.expectStore(t, env, f.top, filepath.Clean(custom))
		})

		t.Run("SOHO_DIR without the state", func(t *testing.T) {
			f := newOwnWorktreeFixture(t, "j1")
			f.installStore(t, f.storeRoot())
			env := f.env.Clone()
			env["SOHO_DIR"] = filepath.Join(f.tmp, "absent-state")
			// StateRootPath: a pinned root is used as is; the valid default
			// store at <checkout>/.soho is not a fallback.
			f.expectNil(t, env, f.top)
		})
	})

	t.Run("parseOwnCheckoutGit", func(t *testing.T) {
		// git prints absolute paths with forward slashes on every OS; on
		// Windows they carry a drive letter, and a rooted path without one
		// is not absolute there. vol is the drive of the fixture paths: git's
		// spelling is in(...) and the parsed value is want(...), which
		// filepath.FromSlash turns into the OS spelling.
		vol := ""
		if runtime.GOOS == "windows" {
			vol = "C:"
		}
		in := func(p string) string { return vol + p }
		want := func(p string) string { return filepath.FromSlash(vol + p) }
		long := in("/" + strings.Repeat("a", 4096))
		over := long + "\n" + long + "\n" + long + "\n"
		type parseCase struct {
			name   string
			out    string
			top    string
			gitDir string
			common string
			ok     bool
		}
		cases := []parseCase{
			{
				name:   "three absolute lines with trailing newline",
				out:    in("/repo/top") + "\n" + in("/repo/top/.git") + "\n" + in("/repo/top/.git") + "\n",
				top:    want("/repo/top"),
				gitDir: want("/repo/top/.git"),
				common: want("/repo/top/.git"),
				ok:     true,
			},
			{
				name:   "three absolute lines without trailing newline",
				out:    in("/repo/top") + "\n" + in("/repo/top/.git") + "\n" + in("/repo/top/.git"),
				top:    want("/repo/top"),
				gitDir: want("/repo/top/.git"),
				common: want("/repo/top/.git"),
				ok:     true,
			},
			{
				name:   "CRLF endings",
				out:    in("/repo/top") + "\r\n" + in("/repo/top/.git") + "\r\n" + in("/repo/top/.git") + "\r\n",
				top:    want("/repo/top"),
				gitDir: want("/repo/top/.git"),
				common: want("/repo/top/.git"),
				ok:     true,
			},
			{
				name: "two lines",
				out:  in("/repo/top") + "\n" + in("/repo/top/.git") + "\n",
			},
			{
				name: "four lines",
				out:  in("/repo/a") + "\n" + in("/repo/b") + "\n" + in("/repo/c") + "\n" + in("/repo/d") + "\n",
			},
			{
				name: "relative line",
				out:  in("/repo/top") + "\nrel/.git\n" + in("/repo/top/.git") + "\n",
			},
			{
				name: "empty line",
				out:  in("/repo/top") + "\n\n" + in("/repo/top/.git") + "\n",
			},
			{
				name: "empty output",
				out:  "",
			},
			{
				name: "output longer than the cap",
				out:  over,
			},
		}
		if runtime.GOOS == "windows" {
			// A rooted path without a drive letter is not absolute on
			// Windows: the POSIX spelling of the first row is refused there.
			cases = append(cases, parseCase{
				name: "rooted lines without a drive are refused on Windows",
				out:  "/repo/top\n/repo/top/.git\n/repo/top/.git\n",
			})
		}
		// Each row discriminates one production check of
		// parseOwnCheckoutGit: the ok rows fail if the CRLF normalization, the
		// trailing-newline handling or (on Windows) the forward-slash
		// conversion regresses; "output longer than the cap" fails if the
		// len(out) > ownCheckoutGitOutputMax guard is removed (its three fake
		// absolute lines, drive-qualified on Windows, pass every remaining
		// check); "two lines" and "four lines" fail if the len(lines) != 3
		// guard is removed (the third line access panics); "relative line",
		// "empty line" and "empty output" fail if the per-line empty and
		// absolute checks are removed; the Windows-only row fails if a path
		// without a drive is accepted as absolute.
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				gotTop, gotGitDir, gotCommon, gotOk := parseOwnCheckoutGit(c.out)
				if gotOk != c.ok || gotTop != c.top || gotGitDir != c.gitDir || gotCommon != c.common {
					t.Fatalf("parseOwnCheckoutGit = (%q, %q, %q, %v), want (%q, %q, %q, %v)", gotTop, gotGitDir, gotCommon, gotOk, c.top, c.gitDir, c.common, c.ok)
				}
			})
		}
	})

	t.Run("git missing from PATH", func(t *testing.T) {
		f := newOwnWorktreeFixture(t, "j1")
		f.installStore(t, f.storeRoot())
		empty := filepath.Join(f.tmp, "empty-bin")
		if err := os.MkdirAll(empty, 0o700); err != nil {
			t.Fatal(err)
		}
		env := f.env.Clone()
		env["PATH"] = empty
		// ownCheckoutGitQuery's FindExecutable: without a git on the env's
		// PATH the query reports not ok, so no anchor exists even for a
		// fully valid fixture.
		f.expectNil(t, env, f.top)
	})

	t.Run("cappedOutput", func(t *testing.T) {
		// cappedOutput keeps at most max bytes and records that more
		// arrived: writes beyond max are not stored and over is set.
		c := &cappedOutput{max: 10}
		if _, err := c.Write([]byte("01234")); err != nil {
			t.Fatal(err)
		}
		if c.buf.String() != "01234" || c.over {
			t.Fatalf("after 5 of 10 bytes: stored %q, over %v", c.buf.String(), c.over)
		}
		if _, err := c.Write([]byte("56789")); err != nil {
			t.Fatal(err)
		}
		if c.buf.Len() != 10 || c.over {
			t.Fatalf("after exactly max: len %d, over %v", c.buf.Len(), c.over)
		}
		if _, err := c.Write([]byte("abcdef")); err != nil {
			t.Fatal(err)
		}
		if c.buf.Len() != 10 || !c.over {
			t.Fatalf("after over max: len %d, over %v", c.buf.Len(), c.over)
		}
		if _, err := c.Write([]byte("z")); err != nil {
			t.Fatal(err)
		}
		if c.buf.Len() != 10 || !c.over {
			t.Fatalf("after a full buffer: len %d, over %v", c.buf.Len(), c.over)
		}
		big := &cappedOutput{max: 4}
		if _, err := big.Write([]byte("012345678")); err != nil {
			t.Fatal(err)
		}
		if big.buf.String() != "0123" || !big.over {
			t.Fatalf("single oversized write: stored %q, over %v", big.buf.String(), big.over)
		}
	})
}
