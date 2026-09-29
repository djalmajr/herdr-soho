package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/platform"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

var legacyCopied []string

func LegacyEnvCopied() []string { return append([]string(nil), legacyCopied...) }

func ApplyLegacyEnv(env platform.Env) []string {
	copied := make([]string, 0)
	for name, old := range env.Clone() {
		if !strings.HasPrefix(name, "HERDR_AGENTS_") || old == "" {
			continue
		}
		fresh := "HERDR_SOHO_" + strings.TrimPrefix(name, "HERDR_AGENTS_")
		if env.Get(fresh) == "" {
			env[fresh] = old
			copied = append(copied, name)
		}
	}
	textutil.SortUTF16(copied)
	legacyCopied = copied
	return append([]string(nil), copied...)
}

func LegacyUserConfigPath(platformName string, env platform.Env) string {
	if xdg := env.Get("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr-agents", "config")
	}
	if platformName == "win32" {
		if appdata := env.Get("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "herdr-agents", "config")
		}
		return filepath.Join(platform.HomeDir(platformName, env), "AppData", "Roaming", "herdr-agents", "config")
	}
	return filepath.Join(platform.HomeDir(platformName, env), ".config", "herdr-agents", "config")
}

func LegacyProjectConfigPath(root string) string {
	return filepath.Join(root, ".agents", "herdr-agents.conf")
}

func LegacyStatePath(root string) string { return filepath.Join(root, ".herdr-agents") }

func EffectiveConfigFile(newPath, legacyPath string) string {
	if isFile(newPath) {
		return newPath
	}
	if isFile(legacyPath) {
		return legacyPath
	}
	return newPath
}

func MigrateLegacyConfigFile(dest string, env platform.Env, cwd string) bool {
	root := platform.ProjectRoot(env, cwd)
	legacy := ""
	if dest == platform.UserConfigPath(platform.Current(), env) {
		legacy = LegacyUserConfigPath(platform.Current(), env)
	} else if dest == filepath.Join(root, ".agents", "herdr-soho.conf") {
		legacy = LegacyProjectConfigPath(root)
	}
	if legacy == "" {
		return false
	}
	if _, err := os.Lstat(dest); err == nil {
		return false
	}
	info, err := os.Stat(legacy)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		platform.Die(fmt.Sprintf("config set: could not read legacy config %s (file left untouched)", legacy), 4)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil {
		platform.Die(fmt.Sprintf("config set: could not rewrite %s (file left untouched)", dest), 4)
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		platform.Die(fmt.Sprintf("config set: could not rewrite %s (file left untouched)", dest), 4)
	}
	tmp := filepath.Join(filepath.Dir(dest), fmt.Sprintf(".%s.%d.%s.tmp", filepath.Base(dest), os.Getpid(), hex.EncodeToString(suffix[:])))
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		platform.Die(fmt.Sprintf("config set: could not rewrite %s (file left untouched)", dest), 4)
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err = f.Write(data); err == nil {
		err = f.Chmod(info.Mode().Perm())
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp, dest)
	}
	if err != nil {
		cleanup()
		platform.Die(fmt.Sprintf("config set: could not rewrite %s (file left untouched)", dest), 4)
	}
	fmt.Fprintf(platform.Stderr, "herdr-soho: warning: copied legacy config %s to %s; the legacy file is no longer read\n", legacy, dest)
	return true
}

func DefaultStateDirName(root string) string {
	newDir := filepath.Join(root, ".herdr-soho")
	oldDir := LegacyStatePath(root)
	if _, err := os.Stat(newDir); os.IsNotExist(err) && isDir(oldDir) {
		return ".herdr-agents"
	}
	return ".herdr-soho"
}

func LegacyDoctorWarnings(env platform.Env, cwd, platformName string) []string {
	out := make([]string, 0)
	for _, name := range LegacyEnvCopied() {
		out = append(out, fmt.Sprintf("legacy environment variable %s is read as HERDR_SOHO_%s; rename it", name, strings.TrimPrefix(name, "HERDR_AGENTS_")))
	}
	root := platform.ProjectRoot(env, cwd)
	newUser := platform.UserConfigPath(platformName, env)
	oldUser := LegacyUserConfigPath(platformName, env)
	newProject := filepath.Join(root, ".agents", "herdr-soho.conf")
	oldProject := LegacyProjectConfigPath(root)
	if isFile(oldUser) {
		if isFile(newUser) {
			out = append(out, fmt.Sprintf("legacy user config %s is no longer read (%s exists); remove it once you no longer need it", oldUser, newUser))
		} else {
			out = append(out, fmt.Sprintf("legacy user config in use: %s (the next 'config set --user' copies it to %s)", oldUser, newUser))
		}
	}
	if isFile(oldProject) {
		if isFile(newProject) {
			out = append(out, fmt.Sprintf("legacy project config %s is no longer read (%s exists); remove it once you no longer need it", oldProject, newProject))
		} else {
			out = append(out, fmt.Sprintf("legacy project config in use: %s (the next 'config set' copies it to %s)", oldProject, newProject))
		}
	}
	ctx := LoadConfig(env, cwd)
	entry, found := ctx.Entries["state_dir"]
	statePinned := env.Get("HERDR_SOHO_DIR") != "" || env.Get("HERDR_SOHO_STATE_DIR") != "" || (found && entry.Source != "defaults" && entry.Value != "")
	if !statePinned && DefaultStateDirName(root) == ".herdr-agents" {
		out = append(out, fmt.Sprintf("legacy state dir in use: %s (once no worker is live, rename it to .herdr-soho and ignore .herdr-soho/ in git)", LegacyStatePath(root)))
	}
	if isDir(LegacyStatePath(root)) && isDir(filepath.Join(root, ".herdr-soho")) {
		out = append(out, fmt.Sprintf("legacy state dir %s is no longer used (%s exists); clean it once its reports are no longer needed", LegacyStatePath(root), filepath.Join(root, ".herdr-soho")))
	}
	return out
}
