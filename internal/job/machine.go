package job

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Machine is the per-machine job configuration.
// Orgs is empty when job_orgs is empty, which allows no organization.
// ReposRoot is absolute when the configured value starts with ~ or %USERPROFILE%.
// TimeoutMin is the default job budget in minutes.
type Machine struct {
	Label      string
	Orgs       []string
	ReposRoot  string
	TimeoutMin int
	WakeCmd    string
}

// Machine defaults: the user configuration file's values win, and a missing
// file or key falls back to these.
const (
	defaultMachineLabel = ""
	defaultJobOrgs      = ""
	defaultJobReposRoot = "~/repo.git"
	defaultJobTimeout   = "120"
	defaultJobWakeCmd   = ""
)

// LoadMachine reads machine_label, job_orgs, job_repos_root, job_timeout and
// job_wake_cmd only from the user configuration file (the current path, else
// the legacy one) through core.ReadConfigFilePairs, falling back to the
// defaults above. Project and session files and HERDR_SOHO_* environment
// variables never change these five values. osName selects the user file
// and the home directory used to expand a leading ~ or %USERPROFILE%. An
// unreadable user file, an invalid timeout, or an invalid organization token
// is *platform.ExitError with code 2. An unresolvable ~ panics with the same
// exit platform.HomeDir uses.
func LoadMachine(osName string, env platform.Env) (Machine, error) {
	file := core.EffectiveConfigFile(platform.UserConfigPath(osName, env), core.LegacyUserConfigPath(osName, env))
	pairs, err := core.ReadConfigFilePairs(file)
	if err != nil {
		if !os.IsNotExist(err) {
			return Machine{}, exitErr(2, "job: cannot read user configuration")
		}
	}
	values := map[string]string{
		"machine_label":  defaultMachineLabel,
		"job_orgs":       defaultJobOrgs,
		"job_repos_root": defaultJobReposRoot,
		"job_timeout":    defaultJobTimeout,
		"job_wake_cmd":   defaultJobWakeCmd,
	}
	for _, pair := range pairs {
		if _, known := values[pair.Key]; known {
			values[pair.Key] = pair.Value
		}
	}
	orgs, err := parseOrgs(values["job_orgs"])
	if err != nil {
		return Machine{}, err
	}
	timeout, err := parseTimeout(values["job_timeout"])
	if err != nil {
		return Machine{}, err
	}
	return Machine{
		Label:      values["machine_label"],
		Orgs:       orgs,
		ReposRoot:  expandReposRoot(osName, env, values["job_repos_root"]),
		TimeoutMin: timeout,
		WakeCmd:    values["job_wake_cmd"],
	}, nil
}

// LabelMatches accepts an absent brief maquina. A present value must equal Label.
func (m Machine) LabelMatches(briefMachine string) error {
	if briefMachine == "" || briefMachine == m.Label {
		return nil
	}
	return exitErr(2, "job: machine_label does not match brief maquina")
}

// AllowsOrg reports whether org is listed in job_orgs.
func (m Machine) AllowsOrg(org string) error {
	for _, candidate := range m.Orgs {
		if candidate == org {
			return nil
		}
	}
	return exitErr(2, "job: organization out of scope")
}

// Admit checks brief maquina, the repository name, and organization membership.
func (m Machine) Admit(briefMachine, org, repo string) error {
	if err := m.LabelMatches(briefMachine); err != nil {
		return err
	}
	if !pathSegment(repo) {
		return exitErr(2, "job: invalid repository")
	}
	if !pathSegment(org) {
		return exitErr(2, "job: invalid organization")
	}
	return m.AllowsOrg(org)
}

func parseOrgs(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		org := strings.TrimSpace(part)
		if org == "" {
			return nil, exitErr(2, "job: empty organization in job_orgs")
		}
		if !pathSegment(org) {
			return nil, exitErr(2, "job: invalid organization in job_orgs")
		}
		out = append(out, org)
	}
	return out, nil
}

func parseTimeout(raw string) (int, error) {
	if raw == "" {
		raw = "120"
	}
	if !core.ConfigValueOk("job_timeout", raw, nil, "") {
		return 0, exitErr(2, "job: invalid job_timeout '"+raw+"'")
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, exitErr(2, "job: invalid job_timeout '"+raw+"'")
	}
	return n, nil
}

func expandReposRoot(osName string, env platform.Env, raw string) string {
	if raw == "" {
		raw = "~/repo.git"
	}
	if raw == "~" {
		return platform.HomeDir(osName, env)
	}
	if rest, ok := strings.CutPrefix(raw, "~/"); ok {
		return joinHome(osName, env, rest)
	}
	if rest, ok := strings.CutPrefix(raw, `~\`); ok {
		return joinHome(osName, env, rest)
	}
	if rest, ok := cutUserProfile(raw); ok {
		return joinHome(osName, env, rest)
	}
	return raw
}

func cutUserProfile(raw string) (string, bool) {
	const prefix = "%USERPROFILE%"
	if len(raw) < len(prefix) || !strings.EqualFold(raw[:len(prefix)], prefix) {
		return "", false
	}
	return raw[len(prefix):], true
}

func joinHome(osName string, env platform.Env, rest string) string {
	home := platform.HomeDir(osName, env)
	rest = strings.TrimLeft(rest, `/\`)
	if rest == "" {
		return home
	}
	rest = strings.ReplaceAll(rest, `\`, "/")
	parts := strings.Split(rest, "/")
	return filepath.Join(append([]string{home}, parts...)...)
}
