package core

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

// TestLegacyLeftovers: LegacyLeftovers reports the former herdr-soho
// names with the exact decision-7 messages, in the exact order, and
// nothing when there are no leftovers.
func TestLegacyLeftovers(t *testing.T) {
	run := func(t *testing.T, mut func(env platform.Env, root string)) []string {
		t.Helper()
		env, cwd := formerNamesEnv(t)
		if mut != nil {
			mut(env, cwd)
		}
		return LegacyLeftovers(env, cwd, platform.Current())
	}

	t.Run("no leftovers: nothing", func(t *testing.T) {
		got := run(t, nil)
		if len(got) != 0 {
			t.Fatalf("leftovers = %v, want none", got)
		}
	})

	t.Run("former env names only, alphabetical, empty values skipped", func(t *testing.T) {
		got := run(t, func(env platform.Env, root string) {
			env["HERDR_SOHO_BETA"] = "2"
			env["HERDR_SOHO_ALPHA"] = "1"
			env["HERDR_SOHO_EMPTY"] = ""
			env["SOHO_LAYOUT"] = "columns"
		})
		want := []string{
			"environment variable HERDR_SOHO_ALPHA is no longer read; rename it to SOHO_ALPHA",
			"environment variable HERDR_SOHO_BETA is no longer read; rename it to SOHO_BETA",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leftovers = %v, want %v", got, want)
		}
	})

	t.Run("former user config file only", func(t *testing.T) {
		env, cwd := formerNamesEnv(t)
		oldUser := LegacyUserConfigPath(platform.Current(), env)
		write(t, oldUser, "max_workers=7\n")
		got := LegacyLeftovers(env, cwd, platform.Current())
		want := []string{
			"former user config " + oldUser + " is no longer read: run 'soho setup' to move it to " + platform.UserConfigPath(platform.Current(), env),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leftovers = %v, want %v", got, want)
		}
	})

	t.Run("former project config file only", func(t *testing.T) {
		env, cwd := formerNamesEnv(t)
		oldProject := filepath.Join(cwd, ".agents", "herdr-soho.conf")
		write(t, oldProject, "panes=3\n")
		got := LegacyLeftovers(env, cwd, platform.Current())
		want := []string{
			"former project config " + oldProject + " is no longer read: run 'soho setup' to move it to " + filepath.Join(cwd, ".agents", "soho.conf"),
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leftovers = %v, want %v", got, want)
		}
	})

	t.Run("former state dir only", func(t *testing.T) {
		env, cwd := formerNamesEnv(t)
		if err := os.MkdirAll(filepath.Join(cwd, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		got := LegacyLeftovers(env, cwd, platform.Current())
		want := []string{
			"former state dir " + filepath.Join(cwd, ".herdr-soho") + " is no longer used: run 'soho setup' to move it to " + filepath.Join(cwd, ".soho") + " once no worker is live",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leftovers = %v, want %v", got, want)
		}
	})

	t.Run("everything at once, exact order", func(t *testing.T) {
		env, cwd := formerNamesEnv(t)
		env["HERDR_SOHO_ZETA"] = "1"
		env["HERDR_SOHO_ALPHA"] = "2"
		oldUser := LegacyUserConfigPath(platform.Current(), env)
		write(t, oldUser, "max_workers=7\n")
		oldProject := filepath.Join(cwd, ".agents", "herdr-soho.conf")
		write(t, oldProject, "panes=3\n")
		if err := os.MkdirAll(filepath.Join(cwd, ".herdr-soho"), 0o700); err != nil {
			t.Fatal(err)
		}
		got := LegacyLeftovers(env, cwd, platform.Current())
		want := []string{
			"environment variable HERDR_SOHO_ALPHA is no longer read; rename it to SOHO_ALPHA",
			"environment variable HERDR_SOHO_ZETA is no longer read; rename it to SOHO_ZETA",
			"former user config " + oldUser + " is no longer read: run 'soho setup' to move it to " + platform.UserConfigPath(platform.Current(), env),
			"former project config " + oldProject + " is no longer read: run 'soho setup' to move it to " + filepath.Join(cwd, ".agents", "soho.conf"),
			"former state dir " + filepath.Join(cwd, ".herdr-soho") + " is no longer used: run 'soho setup' to move it to " + filepath.Join(cwd, ".soho") + " once no worker is live",
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("leftovers = %v, want %v", got, want)
		}
	})
}
