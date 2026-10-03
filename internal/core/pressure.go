package core

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Resource pressure for `spawn`, `wait`, and `status`: one line on stderr
// (never stdout, never the friction log) when the state dir's disk or the
// swap passed the configured limits. Every measurement failure is
// silent, the command's stdout and exit code never change, and under
// HERDR_SOHO_NOWRITE nothing is written and no warning goes out.

const (
	pressureDiskKey = "pressure_disk_free_percent"
	pressureSwapKey = "pressure_swap_percent"

	pressureDiskDefault = 15
	pressureSwapDefault = 80

	// wait/status warn at most once per 10 minutes per state dir; the
	// stamp is the mtime of <state>/wait/pressure-warned.
	pressureWarnInterval = 10 * time.Minute
	pressureStampName    = "pressure-warned"
)

// PressureWarn is the spawn entry point: it warns whenever the machine is
// under pressure (no throttle), before the pane is created. The spawn
// proceeds as usual.
func PressureWarn(ctx *Config, env platform.Env, cwd string) {
	pressureCheck(ctx, env, cwd, false)
}

// PressureWarnThrottled is the wait/status entry point: at most one
// warning per 10 minutes per state dir.
func PressureWarnThrottled(ctx *Config, env platform.Env, cwd string) {
	pressureCheck(ctx, env, cwd, true)
}

func pressureCheck(ctx *Config, env platform.Env, cwd string, throttle bool) {
	if Nowrite(env) {
		return
	}
	stateDir := pressureStateDir(ctx, env, cwd)
	if stateDir == "" {
		return
	}
	parts := pressureParts(ctx, env, stateDir)
	if len(parts) == 0 {
		return
	}
	if throttle && recentlyWarned(stateDir, platform.Now()) {
		return
	}
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: resource pressure: %s; run 'herdr-soho gc' and close idle panes before opening more\n", strings.Join(parts, "; "))
	if throttle {
		stampPressure(stateDir)
	}
}

// pressureStateDir resolves <state dir> without dying or spending a herdr
// call: the workspace id comes from HERDR_WORKSPACE_ID; without it there is
// no pressure check (a command that cannot resolve its state dir cannot be
// opened more work for). Any failure means silent skip.
func pressureStateDir(ctx *Config, env platform.Env, cwd string) string {
	workspace := env.Get("HERDR_WORKSPACE_ID")
	if workspace == "" {
		return ""
	}
	return filepath.Join(StateRootPath(ctx, env, cwd), workspace)
}

// pressureParts holds only the parts that passed their limit: the disk
// part when the free percent is below pressure_disk_free_percent (0
// disables), the swap part when the used percent is above
// pressure_swap_percent (0 disables, and a zero swap total never warns).
func pressureParts(ctx *Config, env platform.Env, stateDir string) []string {
	var parts []string
	diskLimit := pressureThreshold(ctx, pressureDiskKey, pressureDiskDefault, env)
	if diskLimit > 0 {
		if free, total, ok := platform.DiskFree(stateDir); ok && total > 0 {
			pct := free * 100 / total
			if pct < int64(diskLimit) {
				parts = append(parts, fmt.Sprintf("disk %s has %d%% free (%s of %s)", stateDir, pct, platform.HumanSize(free), platform.HumanSize(total)))
			}
		}
	}
	swapLimit := pressureThreshold(ctx, pressureSwapKey, pressureSwapDefault, env)
	if swapLimit > 0 {
		if used, total, ok := platform.SwapUsage(env); ok && total > 0 {
			pct := used * 100 / total
			if pct > int64(swapLimit) {
				parts = append(parts, fmt.Sprintf("swap %d%% used (%s of %s)", pct, platform.HumanSize(used), platform.HumanSize(total)))
			}
		}
	}
	return parts
}

// pressureThreshold reads the key through the usual layers (env override
// included). Only a non-negative decimal integer is valid, the domain that
// config set accepts; anything else (a negative value included) falls back
// to the default with the project's warning line, so only 0 turns it off.
func pressureThreshold(ctx *Config, key string, limit int, env platform.Env) int {
	raw := Cfg(ctx, key, strconv.Itoa(limit), env)
	if decimalRE.MatchString(raw) {
		if value, err := strconv.Atoi(raw); err == nil {
			return value
		}
	}
	_, _ = fmt.Fprintf(platform.Stderr, "herdr-soho: warning: invalid value '%s' for %s; using the default %d\n", raw, key, limit)
	return limit
}

func recentlyWarned(stateDir string, now time.Time) bool {
	info, err := os.Stat(filepath.Join(stateDir, "wait", pressureStampName))
	return err == nil && now.Sub(info.ModTime()) < pressureWarnInterval
}

// stampPressure touches <state>/wait/pressure-warned (best effort: a
// failed stamp still warned once, and the next run retries the stamp).
func stampPressure(stateDir string) {
	dir := filepath.Join(stateDir, "wait")
	if err := os.MkdirAll(dir, 0o777); err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, pressureStampName), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o666)
	if err != nil {
		return
	}
	_ = f.Close()
}
