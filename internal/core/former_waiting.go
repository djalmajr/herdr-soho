package core

import (
	"os"
	"path/filepath"

	"github.com/djalmajr/soho/internal/platform"
)

// StateDirExplicitlyPinned reports whether the state dir was fixed by the
// environment or the configuration, even when the pin resolves to the
// default <root>/.soho: an explicit pin must not be overridden by the
// migration. It moved here from the setup package so the migration and the
// pre-setup refusal share the same rule.
func StateDirExplicitlyPinned(ctx *Config, env platform.Env) bool {
	if env.Get("SOHO_DIR") != "" {
		return true
	}
	if env.Get("SOHO_STATE_DIR") != "" {
		return true
	}
	entry, ok := ctx.Entries["state_dir"]
	return ok && entry.Source != "defaults" && entry.Value != ""
}

// FormerStateWaiting reports whether the former .herdr-soho state dir still
// waits for the soho setup migration: the same conditions the migration
// itself requires (internal/setup/migrate.go) — the former dir exists as a
// directory at the state project root, the state dir is not pinned by the
// environment or the configuration (StateDirExplicitlyPinned), the state
// root resolves to the default <root>/.soho, and the target is either
// absent or a real empty directory. While it returns true, a command that
// opens the state dir would block the setup move, so the commands refuse or
// write nothing (decision: DJA-318).
func FormerStateWaiting(ctx *Config, env platform.Env, cwd string) (former, target string, waiting bool) {
	root := platform.StateProjectRoot(env, cwd)
	former = filepath.Join(root, ".herdr-soho")
	target = filepath.Join(root, ".soho")
	if info, err := os.Stat(former); err != nil || !info.IsDir() {
		return former, target, false
	}
	if StateDirExplicitlyPinned(ctx, env) {
		return former, target, false
	}
	if StateRootPath(ctx, env, cwd) != target {
		return former, target, false
	}
	return former, target, formerTargetGoneOrEmpty(target)
}

// formerTargetGoneOrEmpty is the migration's own target rule: the soho dir
// is absent (an Lstat IsNotExist) or a real empty directory — a directory
// Lstat that is not a symlink with zero ReadDir entries. A symlinked,
// non-directory or non-empty target is in use, so the former dir must not
// be treated as waiting for the move.
func formerTargetGoneOrEmpty(target string) bool {
	info, err := os.Lstat(target)
	if err != nil {
		return os.IsNotExist(err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	entries, err := os.ReadDir(target)
	return err == nil && len(entries) == 0
}
