package job

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// managed_helper_test.go runs this test binary as the two processes of a
// managed team command (managed_test.go): a parent started as
// `<test binary> job supervise --id <ID>` that binds the launch like the
// job supervisor (BindLaunch), optionally breaks one part of the binding,
// and runs the child; and the child, which classifies itself with
// ManagedSelection and prints the outcome. Both modes are selected by
// managedHelperVar and exit before any test runs.

const (
	managedHelperVar   = "JOB_MANAGED_HELPER"
	managedRootVar     = "JOB_MANAGED_ROOT"
	managedBindVar     = "JOB_MANAGED_BIND"
	managedMutateVar   = "JOB_MANAGED_MUTATE"
	managedArgsVar     = "JOB_MANAGED_ARGS"
	managedChildEnvVar = "JOB_MANAGED_CHILD_ENV"
	managedChildDirVar = "JOB_MANAGED_CHILD_DIR"
	managedChildExeVar = "JOB_MANAGED_CHILD_EXE"
)

func managedHelperMain() {
	switch os.Getenv(managedHelperVar) {
	case "parent":
		os.Exit(managedHelperParent())
	case "child":
		os.Exit(managedHelperChild())
	case "block":
		buf := make([]byte, 1)
		_, _ = os.Stdin.Read(buf)
		os.Exit(0)
	}
}

func managedHelperEnv() platform.Env {
	env := platform.Env{}
	for _, kv := range os.Environ() {
		if i := strings.IndexByte(kv, '='); i > 0 {
			env[kv[:i]] = kv[i+1:]
		}
	}
	return env
}

func managedHelperParent() int {
	full := managedHelperEnv()
	launch := ReadLaunchLocators(full)
	env := WithoutLaunchLocators(full)
	root := env.Get(managedRootVar)
	id := "job-1"
	if len(os.Args) == 5 {
		id = os.Args[4]
	}
	if env.Get(managedBindVar) == "1" {
		store := Open(root)
		bound, reason := BindLaunch(store, id, env, launch.Selection)
		fmt.Printf("bind=%v reason=%s\n", bound, reason)
		if err := managedMutate(root, id, env.Get(managedMutateVar)); err != nil {
			fmt.Printf("mutate error: %v\n", err)
			return 1
		}
	}
	childEnv := withLaunchLocators(env, launch.Selection)
	if launch.Selection.IsZero() {
		childEnv = full.Clone()
	}
	childEnv[managedHelperVar] = "child"
	for _, kv := range strings.Split(env.Get(managedChildEnvVar), ";") {
		if kv == "" {
			continue
		}
		if i := strings.IndexByte(kv, '='); i > 0 {
			if kv[i+1:] == "<unset>" {
				delete(childEnv, kv[:i])
			} else {
				childEnv[kv[:i]] = kv[i+1:]
			}
		}
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Printf("executable: %v\n", err)
		return 1
	}
	if exe := env.Get(managedChildExeVar); exe != "" {
		self = exe
	}
	cmd := exec.Command(self)
	cmd.Dir = env.Get(managedChildDirVar)
	cmd.Env = childEnv.List()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Printf("child: %v\n", err)
		return 1
	}
	return 0
}

func managedHelperChild() int {
	full := managedHelperEnv()
	launch := ReadLaunchLocators(full)
	env := WithoutLaunchLocators(full)
	cwd, _ := os.Getwd()
	args := strings.Fields(env.Get(managedArgsVar))
	sel, outcome := ManagedSelection(args, launch, env, cwd)
	fmt.Printf("outcome=%d base=%s\n", outcome, sel.Base)
	return 0
}

// managedMutate breaks one part of the binding after BindLaunch.
func managedMutate(root, id, what string) error {
	dir, err := Dir(root, id)
	if err != nil || what == "" {
		return err
	}
	store := Open(root)
	edit := func(fn func(*launchRecord)) error {
		rec, ok, err := readLaunch(dir)
		if err != nil || !ok {
			return fmt.Errorf("read launch: ok=%v err=%v", ok, err)
		}
		fn(&rec)
		return writeLaunch(dir, rec)
	}
	moveAndLink := func(path string) error {
		moved := path + ".real"
		if err := os.Rename(path, moved); err != nil {
			return err
		}
		return os.Symlink(moved, path)
	}
	switch what {
	case "revoke":
		return edit(func(r *launchRecord) { r.State = launchRevoked })
	case "nonce":
		return edit(func(r *launchRecord) { r.Nonce = "not-a-nonce" })
	case "pid":
		return edit(func(r *launchRecord) { r.PID++ })
	case "started":
		return edit(func(r *launchRecord) { r.Started += "0" })
	case "exe":
		return edit(func(r *launchRecord) { r.Exe += ".other" })
	case "base":
		return edit(func(r *launchRecord) { r.ConfigBase += "-other" })
	case "workspace":
		return edit(func(r *launchRecord) { r.Workspace = "w-other" })
	case "store":
		return edit(func(r *launchRecord) { r.Store += "-other" })
	case "launch-missing":
		return os.Remove(filepath.Join(dir, launchFileName))
	case "launch-symlink":
		return moveAndLink(filepath.Join(dir, launchFileName))
	case "launch-fifo":
		path := filepath.Join(dir, launchFileName)
		if err := os.Remove(path); err != nil {
			return err
		}
		return makeTestFIFO(path)
	case "state-dir":
		_, err := store.Record(id, func(s *State) { s.Dir = filepath.Join(s.Dir, "elsewhere") })
		return err
	case "state-pane":
		_, err := store.Record(id, func(s *State) { s.RootPane = "p-other" })
		return err
	case "state-symlink":
		return moveAndLink(filepath.Join(dir, "state.json"))
	case "jobdir-symlink":
		return moveAndLink(dir)
	case "jobs-symlink":
		return moveAndLink(filepath.Dir(dir))
	}
	return fmt.Errorf("unknown mutation %q", what)
}
