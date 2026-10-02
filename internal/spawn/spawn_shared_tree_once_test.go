package spawn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func Test_spawn_shared_tree_once_same_pair_warns_once(t *testing.T) {
	// JS: "spawn: a second spawn of the same pair in the same cwd stays silent (M3)"
	f := newSpawnFixture(t, []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"old","pane_id":"p-old","agent_status":"working"}]}}`}})
	f.roster(t, strings.Join([]string{"old", "p-old", "grok", "implementer", "xai", "1", "/tmp/work", "now"}, "\t"))
	sd := filepath.Join(f.state, "ws")
	warns := func(t *testing.T, this, cwd string) int {
		t.Helper()
		oldErr := platform.Stderr
		var stderr strings.Builder
		platform.Stderr = &stderr
		t.Cleanup(func() { platform.Stderr = oldErr })
		sameTreeEditors(this, "implementer", cwd, sd, f.env, f.cwd, f.ctx)
		return strings.Count(stderr.String(), "both edit "+cwd)
	}
	if got := warns(t, "new", "/tmp/work"); got != 1 {
		t.Fatalf("first spawn warned %d times, want 1", got)
	}
	if marker := sharedTreeMarker(t, sd); marker == "" {
		t.Fatal("the warn left no shared-tree marker in the wait dir")
	}
	if got := warns(t, "new", "/tmp/work"); got != 0 {
		t.Fatalf("second spawn of the same pair warned %d times, want 0", got)
	}
}

func Test_spawn_shared_tree_once_new_pair_warns_again(t *testing.T) {
	// JS: "spawn: a new pair in the same cwd warns again (M3)"
	f := newSpawnFixture(t, []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"old","pane_id":"p-old","agent_status":"working"}]}}`}})
	f.roster(t, strings.Join([]string{"old", "p-old", "grok", "implementer", "xai", "1", "/tmp/work", "now"}, "\t"))
	sd := filepath.Join(f.state, "ws")
	warns := func(t *testing.T, this, cwd string) int {
		t.Helper()
		oldErr := platform.Stderr
		var stderr strings.Builder
		platform.Stderr = &stderr
		t.Cleanup(func() { platform.Stderr = oldErr })
		sameTreeEditors(this, "implementer", cwd, sd, f.env, f.cwd, f.ctx)
		return strings.Count(stderr.String(), "both edit "+cwd)
	}
	if got := warns(t, "new", "/tmp/work"); got != 1 {
		t.Fatalf("first spawn warned %d times, want 1", got)
	}
	if got := warns(t, "other", "/tmp/work"); got != 1 {
		t.Fatalf("a new pair stayed silent, got %d, want 1", got)
	}
	if got := warns(t, "other", "/tmp/work"); got != 0 {
		t.Fatalf("the new pair warned again on its second spawn, got %d, want 0", got)
	}
}

func Test_spawn_shared_tree_once_same_pair_other_cwd_warns_again(t *testing.T) {
	// JS: "spawn: the same pair in another cwd warns again, the first cwd stays silent (M3)"
	f := newSpawnFixture(t, []fakecli.Rule{{Argv: []string{"agent", "list"}, Stdout: `{"result":{"agents":[{"name":"old","pane_id":"p-old","agent_status":"working"},{"name":"old","pane_id":"p-old2","agent_status":"working"}]}}`}})
	f.roster(t,
		strings.Join([]string{"old", "p-old", "grok", "implementer", "xai", "1", "/tmp/work", "now"}, "\t"),
		strings.Join([]string{"old", "p-old2", "grok", "implementer", "xai", "1", "/tmp/other", "now"}, "\t"),
	)
	sd := filepath.Join(f.state, "ws")
	warns := func(t *testing.T, this, cwd string) int {
		t.Helper()
		oldErr := platform.Stderr
		var stderr strings.Builder
		platform.Stderr = &stderr
		t.Cleanup(func() { platform.Stderr = oldErr })
		sameTreeEditors(this, "implementer", cwd, sd, f.env, f.cwd, f.ctx)
		return strings.Count(stderr.String(), "both edit "+cwd)
	}
	if got := warns(t, "new", "/tmp/work"); got != 1 {
		t.Fatalf("first spawn warned %d times, want 1", got)
	}
	if got := warns(t, "new", "/tmp/other"); got != 1 {
		t.Fatalf("the same pair in another cwd stayed silent, got %d, want 1", got)
	}
	if got := warns(t, "new", "/tmp/work"); got != 0 {
		t.Fatalf("the first cwd warned again after its marker, got %d, want 0", got)
	}
}

func sharedTreeMarker(t *testing.T, sd string) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(sd, "wait"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "shared-tree-") && strings.HasSuffix(entry.Name(), ".warned") {
			return entry.Name()
		}
	}
	return ""
}
