package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A value that begins with "--" (lane.review.args --add-dir /x) is a value
// after the key, and "--" ends option parsing; before the key, "--x" is still
// an unknown option.
func TestSetValuesWithDoubleDash(t *testing.T) {
	sessionFile := func(t *testing.T, state string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(state, "ws", "session.conf"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	projectFile := func(t *testing.T, cwd string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(cwd, ".agents", "herdr-soho.conf"))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	t.Run("session set: a value that begins with -- is joined", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"session", "set", "lane.review.args", "--add-dir", "/x/shared"}, env, cwd)
		if got := sessionFile(t, env.Get("HERDR_SOHO_DIR")); code != 0 || errOut != "" || got != "lane.review.args=--add-dir /x/shared\n" {
			t.Fatalf("code=%d err=%q file=%q", code, errOut, got)
		}
	})
	t.Run("session set: -- before the value", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"session", "set", "lane.review.args", "--", "--add-dir /x/shared"}, env, cwd)
		if got := sessionFile(t, env.Get("HERDR_SOHO_DIR")); code != 0 || errOut != "" || got != "lane.review.args=--add-dir /x/shared\n" {
			t.Fatalf("code=%d err=%q file=%q", code, errOut, got)
		}
	})
	t.Run("session set: --x before the key is still an unknown option", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"session", "set", "--bogus", "layout", "tab"}, env, cwd)
		if code != 2 || !strings.Contains(errOut, "session set: unknown option '--bogus'") {
			t.Fatalf("code=%d err=%q", code, errOut)
		}
	})
	t.Run("config set: a value that begins with -- and --project after it", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"config", "set", "lane.review.args", "--add-dir", "/x/shared", "--project"}, env, cwd)
		if got := projectFile(t, cwd); code != 0 || errOut != "" || !strings.Contains(got, "lane.review.args=--add-dir /x/shared\n") {
			t.Fatalf("code=%d err=%q file=%q", code, errOut, got)
		}
	})
	t.Run("config set: -- makes --project part of the value", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"config", "set", "lane.review.args", "--", "--add-dir", "--project"}, env, cwd)
		if got := projectFile(t, cwd); code != 0 || errOut != "" || !strings.Contains(got, "lane.review.args=--add-dir --project\n") {
			t.Fatalf("code=%d err=%q file=%q", code, errOut, got)
		}
	})
	t.Run("config set: --x before the key is still an unknown option", func(t *testing.T) {
		env, cwd := commandFixture(t)
		code, _, errOut := runIn(t, []string{"config", "set", "--bogus", "layout", "tab"}, env, cwd)
		if code != 2 || !strings.Contains(errOut, "config set: unknown option '--bogus'") {
			t.Fatalf("code=%d err=%q", code, errOut)
		}
	})
}
