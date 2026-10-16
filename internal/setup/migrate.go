package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// formerUserConfigDirs and formerProjectConfigNames list the names the
// herdr-soho and herdr-agents generations wrote, in the order the
// migration visits them.
var (
	formerUserConfigDirs   = []string{"herdr-soho", "herdr-agents"}
	formerProjectConfNames = []string{"herdr-soho.conf", "herdr-agents.conf"}
)

// migrateNames moves what the herdr-soho and herdr-agents generations
// wrote to the soho names. It returns one line per step; with dry it
// only reports what it would do.
func migrateNames(ctx *core.Config, env platform.Env, cwd string, dry bool) []string {
	lines := []string{}
	moved := false
	verb := "moved"
	if dry {
		verb = "would move"
	}

	// User config: each entry of a former config dir goes to the soho
	// config dir; an entry that already exists there is kept for a manual
	// merge, and a former dir left empty is removed.
	novo := filepath.Dir(platform.UserConfigPath(platform.Current(), env))
	for _, base := range formerUserConfigDirs {
		antigo := filepath.Join(filepath.Dir(novo), base)
		entries, err := os.ReadDir(antigo)
		if err != nil {
			continue
		}
		kept := 0
		for _, entry := range entries {
			src := filepath.Join(antigo, entry.Name())
			dst := filepath.Join(novo, entry.Name())
			if _, lerr := os.Lstat(dst); lerr == nil || !os.IsNotExist(lerr) {
				kept++
				lines = append(lines, fmt.Sprintf("migrate: kept %s: %s already exists; merge by hand", src, dst))
				continue
			}
			if dry {
				lines = append(lines, fmt.Sprintf("migrate: %s %s to %s", verb, src, dst))
				continue
			}
			if err := os.MkdirAll(novo, 0o700); err != nil {
				kept++
				lines = append(lines, fmt.Sprintf("migrate: kept %s: %v", src, err))
				continue
			}
			if err := os.Rename(src, dst); err != nil {
				kept++
				lines = append(lines, fmt.Sprintf("migrate: kept %s: %v", src, err))
				continue
			}
			moved = true
			lines = append(lines, fmt.Sprintf("migrate: %s %s to %s", verb, src, dst))
		}
		if kept == 0 {
			if dry {
				lines = append(lines, fmt.Sprintf("migrate: would remove the empty %s", antigo))
				continue
			}
			if err := os.Remove(antigo); err == nil {
				lines = append(lines, fmt.Sprintf("migrate: removed the empty %s", antigo))
			} else {
				lines = append(lines, fmt.Sprintf("migrate: kept %s: %v", antigo, err))
			}
		}
	}

	// Project config: each former .agents config moves to .agents/soho.conf;
	// once the first one is moved the second finds the new file in place.
	root := platform.ProjectRoot(env, cwd)
	novoProject := filepath.Join(root, ".agents", "soho.conf")
	for _, base := range formerProjectConfNames {
		antigo := filepath.Join(root, ".agents", base)
		info, err := os.Stat(antigo)
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if _, lerr := os.Lstat(novoProject); lerr != nil && os.IsNotExist(lerr) {
			if !dry {
				if err := os.Rename(antigo, novoProject); err != nil {
					lines = append(lines, fmt.Sprintf("migrate: kept %s: %v", antigo, err))
					continue
				}
				moved = true
			}
			lines = append(lines, fmt.Sprintf("migrate: %s %s to %s", verb, antigo, novoProject))
		} else {
			lines = append(lines, fmt.Sprintf("migrate: kept %s: %s already exists; merge by hand", antigo, novoProject))
		}
	}

	// Reload after the config steps so the rest of the command reads the
	// migrated files; a dry run moved nothing, so it never reloads.
	if moved && !dry {
		*ctx = core.LoadConfig(env, cwd)
	}

	// State: the former state dir moves to the soho one only when the state
	// root is the default, the soho dir is absent, and no worker is still
	// listed in a roster.
	stateRoot := platform.StateProjectRoot(env, cwd)
	antigoState := filepath.Join(stateRoot, ".herdr-soho")
	novoState := filepath.Join(stateRoot, ".soho")
	if info, err := os.Stat(antigoState); err == nil && info.IsDir() {
		efetivo := core.StateRootPath(ctx, env, cwd)
		switch {
		case efetivo != novoState:
			lines = append(lines, fmt.Sprintf("migrate: kept %s: the state dir is set to %s", antigoState, efetivo))
		case lstatExists(novoState):
			lines = append(lines, fmt.Sprintf("migrate: kept %s: %s already exists; remove %s once its reports are no longer needed", antigoState, novoState, antigoState))
		case formerRosterHasWorkers(antigoState):
			lines = append(lines, fmt.Sprintf("migrate: kept %s: workers are still in its roster; release them with 'soho release <name> --close' and run 'soho setup' again", antigoState))
		default:
			if !dry {
				if err := os.Rename(antigoState, novoState); err != nil {
					lines = append(lines, fmt.Sprintf("migrate: kept %s: %v", antigoState, err))
					break
				}
			}
			lines = append(lines, fmt.Sprintf("migrate: %s %s to %s", verb, antigoState, novoState))
		}
	}
	return lines
}

func lstatExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// formerRosterHasWorkers reports whether any workspace under the former
// state dir lists a worker in its agents.tsv: a non-empty line that does
// not start with '#'.
func formerRosterHasWorkers(stateDir string) bool {
	sub, err := os.ReadDir(stateDir)
	if err != nil {
		return false
	}
	for _, entry := range sub {
		data, err := os.ReadFile(filepath.Join(stateDir, entry.Name(), "agents.tsv"))
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				return true
			}
		}
	}
	return false
}
