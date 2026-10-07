package install

import (
	"fmt"
	"path/filepath"
	"runtime"
)

// Environment variables that parameterize the root CLI's install command.
// The core never reads the process environment itself: these rules are
// exposed over an injected lookup so the root CLI keeps owning the
// environment and its precedence rules.
const (
	// EnvInstallDir overrides the default install directory.
	EnvInstallDir = "HERDR_SOHO_INSTALL_DIR"
	// EnvReleaseBase overrides the release download base URL.
	EnvReleaseBase = "HERDR_SOHO_RELEASE_BASE"
)

// DefaultInstallDir resolves the install directory when no explicit --dir is
// given: $HERDR_SOHO_INSTALL_DIR when set and nonempty, otherwise
// $HOME/.local/bin on POSIX hosts and %LOCALAPPDATA%/Programs/herdr-soho on
// Windows. A missing required variable is a clear error: the resolver never
// falls back to a process-global home or guesses a directory.
func DefaultInstallDir(lookupEnv func(string) string) (string, error) {
	if dir := lookupEnv(EnvInstallDir); dir != "" {
		return dir, nil
	}
	switch runtime.GOOS {
	case "windows":
		base := lookupEnv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("install: LOCALAPPDATA is not set and no --dir was given; set %s or --dir explicitly", EnvInstallDir)
		}
		return filepath.Join(base, "Programs", "herdr-soho"), nil
	default:
		home := lookupEnv("HOME")
		if home == "" {
			return "", fmt.Errorf("install: HOME is not set and no --dir was given; set %s or --dir explicitly", EnvInstallDir)
		}
		return filepath.Join(home, ".local", "bin"), nil
	}
}

// ResolveInstallDir returns the directory an install writes to: the explicit
// dir (the --dir flag) when nonempty, otherwise DefaultInstallDir.
func ResolveInstallDir(explicit string, lookupEnv func(string) string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	return DefaultInstallDir(lookupEnv)
}
