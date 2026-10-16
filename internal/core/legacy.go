package core

import (
	"fmt"
	"os"
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

// LegacyLeftovers names the former herdr-soho and herdr-agents artifacts
// an old installation left behind, for the doctor's leftover warnings: the
// former HERDR_SOHO_ environment variables (non-empty, alphabetical), then
// the former project config and state dir, then the former user config
// dirs, the former herdr-agents project config and state dir, and an old
// herdr-soho binary still on PATH, each when present. Nothing it names is
// read: they are detection-only paths.
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
	if isFile(LegacyProjectConfigPath(root)) {
		out = append(out, fmt.Sprintf("former project config %s is no longer read: run 'soho setup' to move it to %s", LegacyProjectConfigPath(root), filepath.Join(root, ".agents", "soho.conf")))
	}
	if isDir(LegacyStatePath(root)) {
		out = append(out, fmt.Sprintf("former state dir %s is no longer used: run 'soho setup' to move it to %s once no worker is live", LegacyStatePath(root), filepath.Join(root, ".soho")))
	}
	formerUserDir := filepath.Dir(LegacyUserConfigPath(platformName, env))
	for _, dir := range []string{formerUserDir, filepath.Join(filepath.Dir(formerUserDir), "herdr-agents")} {
		if dirHasContent(dir) {
			out = append(out, fmt.Sprintf("former user config dir %s is still there: run 'soho setup' to move it to %s", dir, filepath.Dir(platform.UserConfigPath(platformName, env))))
		}
	}
	if agentsProject := filepath.Join(root, ".agents", "herdr-agents.conf"); isFile(agentsProject) {
		out = append(out, fmt.Sprintf("former project config %s is no longer read: run 'soho setup' to move it to %s", agentsProject, filepath.Join(root, ".agents", "soho.conf")))
	}
	if agentsState := filepath.Join(root, ".herdr-agents"); isDir(agentsState) {
		out = append(out, fmt.Sprintf("former state dir %s is no longer used: remove it once its reports are no longer needed", agentsState))
	}
	if binary, found := platform.FindExecutable("herdr-soho", env, platformName); found {
		out = append(out, fmt.Sprintf("an old herdr-soho binary is still on PATH at %s: remove it; soho replaces it", binary))
	}
	return out
}

// dirHasContent reports a directory that exists and holds at least one
// entry: an empty former dir has nothing to migrate.
func dirHasContent(dir string) bool {
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}
