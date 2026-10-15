// auto_close.go is the pure decision of the job's automatic close watch:
// given the workspace's panes at one moment, the ids of the job's own
// panes, the monotonic now, the human input wait, and the previous
// observation's memory, it decides to close the job workspace, to keep
// waiting, or to ask for attention. It reads nothing of the world: every
// input is an argument and every output is a return value, so the later
// finalize slice and the tests drive it with the same pure calls.

package job

import "time"

// closeWatchCap is how many waits the automatic close watch may total
// before it stops waiting and asks for attention: 6 waits (30 minutes at
// the default 5-minute wait).
const closeWatchCap = 6

// The close watch decisions and the needs_attention motivo.
const (
	CloseWatchClose          = "close"
	CloseWatchWait           = "wait"
	CloseWatchNeedsAttention = "needs_attention"

	// closeWatchMotivoHumanInput names the motivo of a needs_attention
	// decision: the watch waited the whole cap and a human must look.
	closeWatchMotivoHumanInput = "human_input"
)

// closeWatch is the memory the automatic close watch keeps between
// observations: the open episode of foreign panes, when the episode
// started, when the foreign set last changed, and each foreign pane's
// last seen revision. The zero value is a fresh watch with no open
// episode.
type closeWatch struct {
	// Start is the monotonic time of the episode's first observation: a
	// continuous span of observations with at least one foreign pane.
	Start time.Time
	// Changed is the monotonic time of the foreign set's last change — a
	// pane's first observation (now), a revision change, or a pane
	// appearing or disappearing.
	Changed time.Time
	// Panes maps each foreign pane id to its last seen revision.
	Panes map[string]int
}

// closeOutcome is the decision of one automatic close observation. Motivo
// names the reason of a needs_attention decision and is empty otherwise.
type closeOutcome struct {
	Decision string
	Motivo   string
}

// closeWatchStep advances the automatic close watch with one observation
// and returns the decision and the watch's memory for the next
// observation. It is pure over the monotonic clock and needs wait > 0.
//
// A pane is foreign when it is listed and its id is not in jobPanes (the
// job's own panes, ignored). With no foreign pane — every listed pane is
// the job's, or there is no pane — the decision is close and the watch
// resets. With foreign panes, the first observation of the episode counts
// as a change now, and any later change of the foreign set — a revision
// change, or a pane appearing or disappearing — moves Changed to now. The
// decision is then needs_attention (motivo human_input) once the
// episode's total wait passes closeWatchCap × wait, wait while the last
// change is less than wait ago, and close when every foreign pane has
// stayed unchanged for a full wait.
func closeWatchStep(panes []herdrPane, jobPanes map[string]bool, now time.Time, wait time.Duration, watch closeWatch) (closeOutcome, closeWatch) {
	foreign := make(map[string]int, len(panes))
	for _, pane := range panes {
		if !jobPanes[pane.PaneID] {
			foreign[pane.PaneID] = pane.Revision
		}
	}
	if len(foreign) == 0 {
		return closeOutcome{Decision: CloseWatchClose}, closeWatch{}
	}
	next := closeWatch{Start: watch.Start, Changed: watch.Changed, Panes: foreign}
	if watch.Panes == nil {
		// The episode opens: the first observation counts as a change
		// now.
		next.Start = now
		next.Changed = now
	} else {
		changed := false
		for id, revision := range foreign {
			if prev, seen := watch.Panes[id]; !seen || prev != revision {
				changed = true // a pane appeared or changed revision
				break
			}
		}
		if !changed {
			for id := range watch.Panes {
				if _, seen := foreign[id]; !seen {
					changed = true // a foreign pane disappeared
					break
				}
			}
		}
		if changed {
			next.Changed = now
		}
	}
	if now.Sub(next.Start) > closeWatchCap*wait {
		return closeOutcome{Decision: CloseWatchNeedsAttention, Motivo: closeWatchMotivoHumanInput}, next
	}
	if now.Sub(next.Changed) < wait {
		return closeOutcome{Decision: CloseWatchWait}, next
	}
	return closeOutcome{Decision: CloseWatchClose}, next
}
