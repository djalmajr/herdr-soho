package core

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/djalmajr/soho/internal/platform"
)

// LegacyUserConfigPath is the former user configuration file, kept so the
// doctor and the setup migration can detect a leftover old installation.
// Nothing reads the file itself: the binary only reads the soho names.
func LegacyUserConfigPath(platformName string, env platform.Env) string {
	if xdg := env.Get("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr-soho", "config")
	}
	if platformName == "win32" {
		if appdata := env.Get("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "herdr-soho", "config")
		}
		return filepath.Join(platform.HomeDir(platformName, env), "AppData", "Roaming", "herdr-soho", "config")
	}
	return filepath.Join(platform.HomeDir(platformName, env), ".config", "herdr-soho", "config")
}

func LegacyProjectConfigPath(root string) string {
	return filepath.Join(root, ".agents", "herdr-soho.conf")
}

func LegacyStatePath(root string) string { return filepath.Join(root, ".herdr-soho") }

// LegacyLeftovers names the former herdr-soho artifacts an old
// installation left behind, for the doctor's leftover warnings: the
// former HERDR_SOHO_ environment variables (non-empty, alphabetical),
// then the former user config, project config and state dir, each when
// present. Nothing it names is read: they are detection-only paths.
func LegacyLeftovers(env platform.Env, cwd, platformName string) []string {
	out := make([]string, 0)
	names := make([]string, 0)
	for name, value := range env {
		if strings.HasPrefix(name, "HERDR_SOHO_") && value != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		out = append(out, fmt.Sprintf("environment variable %s is no longer read; rename it to SOHO_%s", name, strings.TrimPrefix(name, "HERDR_SOHO_")))
	}
	root := platform.ProjectRoot(env, cwd)
	if isFile(LegacyUserConfigPath(platformName, env)) {
		out = append(out, fmt.Sprintf("former user config %s is no longer read: run 'soho setup' to move it to %s", LegacyUserConfigPath(platformName, env), platform.UserConfigPath(platformName, env)))
	}
	if isFile(LegacyProjectConfigPath(root)) {
		out = append(out, fmt.Sprintf("former project config %s is no longer read: run 'soho setup' to move it to %s", LegacyProjectConfigPath(root), filepath.Join(root, ".agents", "soho.conf")))
	}
	if isDir(LegacyStatePath(root)) {
		out = append(out, fmt.Sprintf("former state dir %s is no longer used: run 'soho setup' to move it to %s once no worker is live", LegacyStatePath(root), filepath.Join(root, ".soho")))
	}
	return out
}
