package core

import (
	"path/filepath"

	"github.com/djalmajr/soho/internal/platform"
)

// UserSelection is an explicit choice of the user configuration, handed
// from the job supervisor to the commands it runs: Base is the directory
// that holds soho/config and the legacy herdr-soho/config, and Home
// is the directory a home-relative job_repos_root expands from. The zero
// value selects exactly what the environment selects. A selection never
// changes the environment: the generic selectors stay as they are for
// every external process.
type UserSelection struct {
	Base string
	Home string
}

// IsZero reports whether s selects nothing.
func (s UserSelection) IsZero() bool { return s.Base == "" && s.Home == "" }

// UserConfigFiles returns the current and the legacy user configuration
// paths: under sel.Base when it is set, otherwise the environment's
// selection (platform.UserConfigPath and LegacyUserConfigPath share one
// base on every platform, so a base fixes both).
func UserConfigFiles(platformName string, env platform.Env, sel UserSelection) (string, string) {
	if sel.Base != "" {
		return filepath.Join(sel.Base, "soho", "config"), filepath.Join(sel.Base, "herdr-soho", "config")
	}
	return platform.UserConfigPath(platformName, env), LegacyUserConfigPath(platformName, env)
}

// EffectiveUserConfigFile is the user file read for sel: the current path
// when it exists, else the legacy one.
func EffectiveUserConfigFile(platformName string, env platform.Env, sel UserSelection) string {
	current, legacy := UserConfigFiles(platformName, env, sel)
	return EffectiveConfigFile(current, legacy)
}

// SelectedHomeDir is the home sel.Home names, else platform.HomeDir.
func SelectedHomeDir(platformName string, env platform.Env, sel UserSelection) string {
	if sel.Home != "" {
		return sel.Home
	}
	return platform.HomeDir(platformName, env)
}
