package cli

import (
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
)

func TestTM10ReleaseTitleMirrors(t *testing.T) {
	cases := []struct {
		title string
		run   func(*testing.T)
	}{
		{`release: a plain worker keeps its pane, the title is cleared, the row is gone`, TestReleaseKeepsPlainPaneAndClearsTitle},
		{`release --close closes the pane this skill created (plain message)`, TestReleaseClosesCreatedPaneWithFlag},
		{`release: a temporary (burst) worker closes its pane without --close, (temporary) suffix`, TestReleaseClosesBurstPaneWithoutFlag},
		{`release: a working burst worker with a pending report dies 3, --force releases it`, TestReleaseForceWorkingBurstAndCleanup},
		{`release: guards — not in roster (3), unqueryable without --force (4), not-created pane never closes`, func(t *testing.T) {
			TestReleaseRosterAndOptionGuards(t)
			TestReleaseUnavailableNeedsForce(t)
			TestReleaseNeverClosesExternalPane(t)
		}},
	}
	for _, tc := range cases {
		t.Run(`JS: "`+tc.title+`"`, tc.run)
	}
	t.Run(`JS: "release: an unknown option is a usage error (2)"`, func(t *testing.T) {
		f := newReleaseFixture(t, "", "")
		r := f.run(t, "worker", "--bogus")
		if r.code != 2 || r.stderr != "herdr-soho: release: unknown option --bogus\n" {
			t.Fatalf("code=%d stdout=%q stderr=%q", r.code, r.stdout, r.stderr)
		}
		if core.RosterLine(f.state, "worker") == "" || len(callsTo(r.calls, "pane", "close", "p-worker")) != 0 {
			t.Fatalf("guard changed state or closed pane: %#v", r)
		}
	})
}
