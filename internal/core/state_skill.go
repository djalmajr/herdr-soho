package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// StateInSkill returns the skill directory when the state root — the path
// StateRootPath resolves to — is the skill directory or lives inside it, and
// "" otherwise. The skill lookup never dies: a project without an installed
// skill keeps its state wherever it resolves. Both sides compare after
// filepath.EvalSymlinks (the skill may be a symlink or junction), a path that
// does not exist yet compares by its nearest existing ancestor, and the
// comparison is case-insensitive on Windows.
func StateInSkill(ctx *Config, env platform.Env, cwd string) string {
	return StatePathInSkill(env, StateRootPath(ctx, env, cwd))
}

// StatePathInSkill is the StateInSkill check for an already resolved state
// path: it returns the skill directory when statePath is the skill directory
// or lives inside it, and "" otherwise. It never dies.
func StatePathInSkill(env platform.Env, statePath string) string {
	skill, _ := platform.SkillDirSourceOrEmpty(env)
	if skill == "" || statePath == "" {
		return ""
	}
	state := skillComparablePath(statePath)
	sk := skillComparablePath(skill)
	if runtime.GOOS == "windows" {
		state = strings.ToLower(state)
		sk = strings.ToLower(sk)
	}
	if state == sk || strings.HasPrefix(state, sk+string(filepath.Separator)) {
		return skill
	}
	return ""
}

// StateInSkillMessage is the refusal sentence for a write whose path would
// land inside the installed skill: the same sentence StateDir uses, with
// command prefixed like the command's other errors when it is not "".
func StateInSkillMessage(command, path, skill string) string {
	phrase := fmt.Sprintf("the state dir '%s' would be inside the herdr-soho skill ('%s'); run herdr-soho from the project's directory (nothing was written)", path, skill)
	if command == "" {
		return phrase
	}
	return command + ": " + phrase
}

// skillComparablePath resolves p for the skill comparison: EvalSymlinks when
// p exists, otherwise the nearest existing ancestor resolved with the missing
// suffix appended (the state dir does not exist before StateDir creates it).
func skillComparablePath(p string) string {
	target := filepath.Clean(p)
	cur := target
	for {
		if _, err := os.Lstat(cur); err == nil {
			if resolved, err := filepath.EvalSymlinks(cur); err == nil {
				if suffix, err := filepath.Rel(cur, target); err == nil && suffix != "." {
					return filepath.Join(resolved, suffix)
				}
				return resolved
			}
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return target
		}
		cur = parent
	}
}
