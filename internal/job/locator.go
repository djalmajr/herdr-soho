package job

import (
	"path/filepath"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
)

// locator.go builds the private environment the job start hands to the
// supervisor through `herdr workspace create --env`, and reads it back on
// the supervisor's side. Herdr sets those variables only for the
// workspace's root pane, where the supervisor runs; workers open in other
// panes and never receive them. The supervisor re-resolves the job through
// the user configuration, so it must select the same user configuration
// file (current or legacy) and the same job_repos_root as the starter did,
// whatever the Herdr server's own environment holds.
//
// The two names are private to soho and are not a configuration
// interface: no generic selector (XDG_CONFIG_HOME, HOME, USERPROFILE,
// APPDATA) is forwarded or rewritten, so the supervisor's external tools
// (git, gh, herdr, the wake command) keep the server's own environment.
// Each value is an absolute path only: no other variable, no configuration
// content and no machine-policy value travels. Only the supervisor and the
// team commands it runs and binds (managed.go) use them; every soho
// command drops them from the environment it hands to anything else.

const (
	// launchConfigBaseVar carries the base directory of the starter's own
	// user-configuration selection: the directory that holds soho/config.
	launchConfigBaseVar = "_SOHO_LAUNCH_CONFIG_BASE"
	// launchHomeVar carries the home the starter expands a home-relative
	// job_repos_root from; it is sent only for such a root.
	launchHomeVar = "_SOHO_LAUNCH_HOME"
)

// maxLocatorBytes caps one forwarded locator value.
const maxLocatorBytes = 4096

// errLocator is the start failure for a locator that cannot be handed to
// the supervisor as a clean absolute path.
func errLocator() error {
	return errUsage("job: cannot hand the user configuration location to the supervisor")
}

// supervisorLocatorEnv returns the private locators for the supervisor:
//
//   - launchConfigBaseVar, always, set to the base directory of the
//     starter's own selection (this one value fixes the selection and the
//     file choice made from it);
//   - launchHomeVar, the home resolved as the starter resolved it, only
//     when job_repos_root is home-relative (reposRootHome), so ~ expands to
//     the same root.
//
// Each value is made absolute with abs (the starter's own working directory
// is the base a relative value was read against) and cleaned. A value that
// is still not absolute, is longer than maxLocatorBytes or carries a control
// byte is refused with exit 2. No value is ever empty.
func supervisorLocatorEnv(osName string, env platform.Env, reposRootHome bool, abs func(string) (string, error)) (map[string]string, error) {
	base := filepath.Dir(filepath.Dir(platform.UserConfigPath(osName, env)))
	value, err := locatorValue(base, abs)
	if err != nil {
		return nil, err
	}
	out := map[string]string{launchConfigBaseVar: value}
	if reposRootHome {
		home, err := locatorValue(platform.HomeDir(osName, env), abs)
		if err != nil {
			return nil, err
		}
		out[launchHomeVar] = home
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

// LaunchLocators is what a soho process found under the two private
// names when it started: Present is true when either name was set (even to
// an unusable value), and Selection is the validated selection, zero when
// the values are absent or unusable.
type LaunchLocators struct {
	Present   bool
	Selection core.UserSelection
}

// ReadLaunchLocators reads the two private names from env without changing
// it. A usable pair is an absolute, clean, control-free base of at most
// maxLocatorBytes and, optionally, a home of the same shape; anything else
// (a home without a base, an empty, relative, unclean or over-long value)
// is ignored as a whole: an unusable value selects nothing and refuses
// nothing.
func ReadLaunchLocators(env platform.Env) LaunchLocators {
	base, hasBase := env.Lookup(launchConfigBaseVar)
	home, hasHome := env.Lookup(launchHomeVar)
	out := LaunchLocators{Present: hasBase || hasHome}
	if !hasBase || !usableLocator(base) {
		return out
	}
	if hasHome && !usableLocator(home) {
		return out
	}
	out.Selection = core.UserSelection{Base: base}
	if hasHome {
		out.Selection.Home = home
	}
	return out
}

// usableLocator reports whether value is a forwarded locator as the starter
// writes it: clean bytes, absolute and already cleaned.
func usableLocator(value string) bool {
	return cleanLocatorBytes(value) && filepath.IsAbs(value) && filepath.Clean(value) == value
}

// WithoutLaunchLocators returns a copy of env without the two private
// names: the environment every soho command uses for itself and hands
// to external processes. env is not changed.
func WithoutLaunchLocators(env platform.Env) platform.Env {
	out := env.Clone()
	delete(out, launchConfigBaseVar)
	delete(out, launchHomeVar)
	return out
}

// withLaunchLocators returns a copy of env carrying sel under the two
// private names: the environment the supervisor builds for the team
// commands it runs (and binds, managed.go). The zero selection adds
// nothing.
func withLaunchLocators(env platform.Env, sel core.UserSelection) platform.Env {
	out := WithoutLaunchLocators(env)
	if sel.Base != "" {
		out[launchConfigBaseVar] = sel.Base
		if sel.Home != "" {
			out[launchHomeVar] = sel.Home
		}
	}
	return out
}
