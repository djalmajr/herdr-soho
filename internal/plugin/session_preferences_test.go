package plugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func preferenceEnv(t *testing.T) platform.Env {
	t.Helper()
	return platform.Env{"XDG_CONFIG_HOME": t.TempDir()}
}

func preferenceFrame(s *BoardState) string {
	return renderSessionModal(s, 100, 24, "")
}

func TestSessionPreferenceDefaultAndReopen(t *testing.T) {
	env := preferenceEnv(t)
	first := newSessionState(env, "darwin", true)
	if !strings.Contains(preferenceFrame(first), "Tree") {
		t.Fatal("first native popup did not start in Tree")
	}
	if _, err := os.Stat(first.viewPreferencePath); !os.IsNotExist(err) {
		t.Fatalf("opening wrote preference: %v", err)
	}
	if action := first.ApplyKey("v"); action != "" {
		t.Fatalf("view toggle returned %q", action)
	}
	first.ApplyKey("esc")
	reopened := newSessionState(env, "darwin", true)
	if !strings.Contains(preferenceFrame(reopened), "Table") {
		t.Fatal("reopening lost the explicit Table choice")
	}
	data, err := os.ReadFile(reopened.viewPreferencePath)
	if err != nil || string(data) != "table\n" {
		t.Fatalf("saved preference %q: %v", data, err)
	}
	reopened.ApplyKey("v")
	again := newSessionState(env, "darwin", true)
	if !strings.Contains(preferenceFrame(again), "Tree") {
		t.Fatal("reopening lost the explicit Tree choice")
	}
}

func TestSessionPreferenceReadFallbackDoesNotRewrite(t *testing.T) {
	for _, raw := range []string{"", "invalid\n", "table\nextra", strings.Repeat("x", 1024), "table" + strings.Repeat(" ", sessionViewPreferenceLimit)} {
		t.Run(raw[:min(len(raw), 10)], func(t *testing.T) {
			env := preferenceEnv(t)
			path := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "session-view")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
				t.Fatal(err)
			}
			state := newSessionState(env, "darwin", true)
			if state.ViewState.View != sessionViewTree {
				t.Fatalf("invalid data chose %q", state.ViewState.View)
			}
			data, err := os.ReadFile(path)
			if err != nil || string(data) != raw {
				t.Fatalf("invalid preference overwritten: %v", err)
			}
		})
	}
	for _, raw := range []string{"table", "table\r\n", "tree\n"} {
		path := filepath.Join(t.TempDir(), "session-view")
		if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := loadSessionView(path); got != strings.TrimSpace(raw) {
			t.Fatalf("valid preference %q read as %q", raw, got)
		}
	}
}

func TestSessionPreferenceOnlyExplicitViewChoiceWrites(t *testing.T) {
	env := preferenceEnv(t)
	state := newSessionState(env, "darwin", true)
	state.ApplyKey("v")
	path := state.viewPreferencePath
	old := time.Unix(1, 0)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	state = newSessionState(env, "darwin", true)
	state.Entries = unifiedTestEntries()
	for _, key := range []string{"p", "w", "s", "f", "t", "tab", "v", "tab"} {
		state.ApplyKey(key)
	}
	if state.Query != "v" {
		t.Fatalf("search v became a control: %q", state.Query)
	}
	update := NewBoardState()
	update.Entries = unifiedTestEntries()
	update.UpdatedAt = "12:00:00"
	applyBoardUpdate(state, update)
	state.ApplyKey("esc")
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("open/search/sort/filter/refresh/cancel rewrote preference")
	}
	if state.ViewState.View != sessionViewTable {
		t.Fatal("refresh replaced the user's view")
	}
	legacy := newSessionState(env, "darwin", false)
	legacy.ApplyKey("v")
	if legacy.Query != "v" || legacy.ViewState.Enabled {
		t.Fatal("non-TTY contract changed")
	}
}

func TestSessionPreferenceSharesUserPathAcrossPlatforms(t *testing.T) {
	for _, osName := range []string{"darwin", "linux", "win32"} {
		t.Run(osName, func(t *testing.T) {
			env := preferenceEnv(t)
			s := newSessionState(env, osName, true)
			s.ApplyKey("v")
			// A new independent popup restores the same user's choice.
			next := newSessionState(env, osName, true)
			if next.ViewState.View != sessionViewTable {
				t.Fatal("independent popup lost preference")
			}
			if next.viewPreferencePath != filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho", "session-view") {
				t.Fatalf("unexpected preference path %s", next.viewPreferencePath)
			}
		})
	}
	env := platform.Env{"APPDATA": t.TempDir()}
	s := newSessionState(env, "win32", true)
	s.ApplyKey("v")
	if got := newSessionState(env, "win32", true); got.ViewState.View != sessionViewTable || got.viewPreferencePath != filepath.Join(env.Get("APPDATA"), "herdr-soho", "session-view") {
		t.Fatal("Windows APPDATA preference not restored")
	}
}

func TestSessionPreferenceSaveFailureKeepsModalUsable(t *testing.T) {
	env := preferenceEnv(t)
	blocker := filepath.Join(env.Get("XDG_CONFIG_HOME"), "herdr-soho")
	if err := os.WriteFile(blocker, []byte("owned blocker"), 0o600); err != nil {
		t.Fatal(err)
	}
	state := newSessionState(env, "darwin", true)
	state.Entries = unifiedTestEntries()
	state.ApplyKey("v")
	if state.ViewState.View != sessionViewTable || !state.viewPreferenceFailed {
		t.Fatal("save failure lost the current view or was silent")
	}
	output, err := os.CreateTemp(t.TempDir(), "modal-")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	redrawSession(output, state, 100)
	data, err := os.ReadFile(output.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "could not be saved") {
		t.Fatal("save failure had no visible notice")
	}
	if action := state.ApplyKey("c"); action != "copy" || state.Copied == nil || state.Exit != "" {
		t.Fatal("save failure blocked selection copy")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	state = newSessionState(env, "darwin", true)
	state.ApplyKey("v")
	if state.viewPreferenceFailed || newSessionState(env, "darwin", true).ViewState.View != sessionViewTable {
		t.Fatal("a later explicit save did not recover")
	}
}
