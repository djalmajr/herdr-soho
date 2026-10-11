package job

import (
	"path/filepath"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// locator.go builds the environment the job start hands to the supervisor
// through `herdr workspace create --env`. Herdr sets those variables only
// for the workspace's root pane, where the supervisor runs; workers open
// in other panes and never receive them. The supervisor re-resolves the job
// through its own user configuration, so it must select the same user
// configuration file (current or legacy) and the same job_repos_root as the
// starter did, whatever the Herdr server's own environment holds. The map
// holds only configuration locators, each an absolute path: no other
// variable, no configuration content and no machine-policy value travels.

// maxLocatorBytes caps one forwarded locator value.
const maxLocatorBytes = 4096

// errLocator is the start failure for a locator that cannot be handed to
// the supervisor as a clean absolute path.
func errLocator() error {
	return errUsage("job: cannot hand the user configuration location to the supervisor")
}

// supervisorLocatorEnv returns the locators for the supervisor:
//
//   - XDG_CONFIG_HOME, always, set to the base directory of the starter's
//     own selection: the directory that holds herdr-soho/config and the
//     legacy herdr-agents/config (UserConfigPath and LegacyUserConfigPath
//     share it on every platform). XDG_CONFIG_HOME wins over APPDATA and
//     the home on every platform, so this one value pins the starter's
//     selection — and the current-then-legacy file choice made from it —
//     even when the Herdr server's environment selects another base;
//   - the home variable (HOME; USERPROFILE on win32), resolved as the
//     starter resolved it, only when job_repos_root is home-relative
//     (reposRootHome), so ~ expands to the same root.
//
// Each value is made absolute with abs (the starter's own working directory
// is the base a relative value was read against) and cleaned. A value that
// is still not absolute, is longer than maxLocatorBytes or carries a control
// byte is refused with exit 2.
func supervisorLocatorEnv(osName string, env platform.Env, reposRootHome bool, abs func(string) (string, error)) (map[string]string, error) {
	base := filepath.Dir(filepath.Dir(platform.UserConfigPath(osName, env)))
	value, err := locatorValue(base, abs)
	if err != nil {
		return nil, err
	}
	out := map[string]string{"XDG_CONFIG_HOME": value}
	if reposRootHome {
		homeVar := "HOME"
		if osName == "win32" {
			homeVar = "USERPROFILE"
		}
		home, err := locatorValue(platform.HomeDir(osName, env), abs)
		if err != nil {
			return nil, err
		}
		out[homeVar] = home
	}
	return out, nil
}

// locatorValue normalizes one locator value to a clean absolute path.
func locatorValue(raw string, abs func(string) (string, error)) (string, error) {
	if !cleanLocatorBytes(raw) {
		return "", errLocator()
	}
	value, err := abs(raw)
	if err != nil || !cleanLocatorBytes(value) {
		return "", errLocator()
	}
	return value, nil
}

// cleanLocatorBytes reports whether value is non-empty, at most
// maxLocatorBytes long and free of control bytes.
func cleanLocatorBytes(value string) bool {
	if value == "" || len(value) > maxLocatorBytes {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return false
		}
	}
	return true
}

// absLocator is the production abs: filepath.Abs (relative to the process
// working directory), refused when the result is still not absolute.
func absLocator(value string) (string, error) {
	out, err := filepath.Abs(value)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(out) {
		return "", errLocator()
	}
	return filepath.Clean(out), nil
}
