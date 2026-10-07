package peer_test

import (
	"context"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestDiscoverContextCancellationPreservesPublishedLocalAndStopsEnumeration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	code := 0
	calls := 0
	result := peer.DiscoverSessions(peer.DiscoverOptions{Context: ctx, All: true, Run: func(exe string, args []string, opts platform.RunOptions) platform.RunResult {
		calls++
		if exe != "herdr" || strings.Join(args, " ") != "api snapshot" || opts.Context == nil {
			t.Fatalf("unexpected query: %s %v", exe, args)
		}
		return platform.RunResult{Status: &code, Stdout: `{"result":{"snapshot":{"workspaces":[{"workspace_id":"ws","label":"local"}],"tabs":[],"panes":[{"pane_id":"ws:p1","workspace_id":"ws"}],"agents":[]}}}`}
	}}, func(batch peer.SessionResult) {
		if len(batch.Entries) != 1 {
			t.Fatalf("local was not published: %+v", batch)
		}
		cancel()
	})
	if calls != 1 || len(result.Entries) != 1 || result.ListCause != "discovery canceled" {
		t.Fatalf("cancellation lost local or queried after close: %+v calls=%d", result, calls)
	}
}

func TestDiscoverEmptyBatchesStillIdentifyTheirMachine(t *testing.T) {
	code := 0
	machines := []string{}
	result := peer.DiscoverSessions(peer.DiscoverOptions{Machines: []string{"remote"}, Run: func(string, []string, platform.RunOptions) platform.RunResult {
		return platform.RunResult{Status: &code, Stdout: `{"result":{"snapshot":{"panes":[],"agents":[],"tabs":[],"workspaces":[]}}}`}
	}}, func(batch peer.SessionResult) {
		if len(batch.Entries) != 0 || len(batch.Failures) != 0 {
			t.Fatalf("expected successful empty batch: %+v", batch)
		}
		machines = append(machines, batch.Machine)
	})
	if strings.Join(machines, ",") != "local,remote" || result.Machine != "" {
		t.Fatalf("empty batches lost their origin: %v aggregate=%+v", machines, result)
	}
}
