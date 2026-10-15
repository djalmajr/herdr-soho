package plugin

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var remotePaintRowRE = regexp.MustCompile(`\x1b\[(\d+);1H\x1b\[K`)

func remotePaintRows(stream string) []int {
	var rows []int
	for _, match := range remotePaintRowRE.FindAllStringSubmatch(stream, -1) {
		row, _ := strconv.Atoi(match[1])
		rows = append(rows, row)
	}
	return rows
}

// Regression: repainting every row for a remote batch erases unchanged local rows.
func TestRedrawSessionRemoteRefreshPaintsOnlyChangedRows(t *testing.T) {
	for _, view := range []string{sessionViewTree, sessionViewTable} {
		t.Run(view, func(t *testing.T) {
			const width, height = 80, 24
			state := modalFrameTestState(2)
			state.ViewState.View = view
			state.CopyNotice = "Copied local/w1:p1"
			var rec frameRecording
			redrawSession(&rec, state, width)
			screen := newMiniScreen(width, height)
			screen.apply(rec.stream())
			previous := renderSessionModal(state, width, height, state.CopyNotice)
			localRows := []int{}
			for i, row := range strings.Split(strings.TrimSuffix(previous, "\n"), "\n") {
				if strings.Contains(row, "agent-") && !strings.Contains(row, "/srv/") {
					localRows = append(localRows, i+1)
				}
			}
			if len(localRows) != 2 {
				t.Fatalf("fixture local rows=%v", localRows)
			}
			remote := PickerEntry{"ref": "mac/w2:p1", "name": "remote-agent", "machine": "mac", "workspace_label": "beta", "kind": "pi", "status": "working"}
			publications := []*BoardState{
				{Loading: 1, LoadingLocal: true},
				{Entries: append([]PickerEntry(nil), state.Entries...), Loading: 1, FinishedMachines: []string{"local"}},
				{Entries: append(append([]PickerEntry(nil), state.Entries...), remote), Loading: 1, FinishedMachines: []string{"local", "mac"}},
				{Entries: append(append([]PickerEntry(nil), state.Entries...), remote), UpdatedAt: "12:01:01", FinishedMachines: []string{"local", "mac"}},
			}
			for i, update := range publications {
				before := len(rec.writes)
				applyBoardUpdate(state, update)
				redrawSession(&rec, state, width)
				current := renderSessionModal(state, width, height, state.CopyNotice)
				oldRows := strings.Split(strings.TrimSuffix(previous, "\n"), "\n")
				newRows := strings.Split(strings.TrimSuffix(current, "\n"), "\n")
				var want []int
				for row := 0; row < len(newRows); row++ {
					if row >= len(oldRows) || oldRows[row] != newRows[row] {
						want = append(want, row+1)
					}
				}
				var stream string
				if len(rec.writes) == before+1 {
					stream = string(rec.writes[before])
				} else if len(want) != 0 || len(rec.writes) != before {
					t.Fatalf("publication %d writes=%d before=%d changed=%v", i, len(rec.writes), before, want)
				}
				if got := remotePaintRows(stream); !reflect.DeepEqual(got, want) {
					t.Fatalf("publication %d repainted rows=%v; changed rows=%v", i, got, want)
				}
				for _, row := range localRows {
					if oldRows[row-1] == newRows[row-1] && strings.Contains(stream, fmt.Sprintf("\x1b[%d;1H", row)) {
						t.Fatalf("publication %d touched unchanged local row %d", i, row)
					}
				}
				if strings.Contains(stream, "\x1b[J") || strings.ContainsAny(stream, "\r\n") {
					t.Fatalf("publication %d erased the display or scrolled: %q", i, stream)
				}
				screen.apply(stream)
				for row, expected := range newRows {
					want := strings.TrimRight(StripPickerControls(expected), " ")
					if got := strings.TrimRight(screen.line(row+1), " "); got != want {
						t.Fatalf("publication %d row %d=%q want=%q", i, row+1, got, want)
					}
				}
				if entry := state.visible()[state.Selected]; pickerString(entry["ref"]) != "local/w1:p1" || state.CopyNotice != "Copied local/w1:p1" {
					t.Fatalf("publication %d lost selection/copy status", i)
				}
				previous = current
			}
		})
	}
}

// A patch must remove stale rows and keep the right-margin glyph, including wide text.
func TestModalFramePatchClearsStaleRowsAndFinalColumn(t *testing.T) {
	const width, height = 80, 24
	previous := strings.Repeat("a", width) + "\nlong stale row\nstale final row\n"
	current := strings.Repeat("界", width/2) + "\nshort\n"
	screen := newMiniScreen(width, height)
	screen.apply(buildModalFrame(previous))
	patch := buildModalPatch(previous, current)
	if strings.Contains(patch, "\x1b[J") || strings.ContainsAny(patch, "\r\n") {
		t.Fatalf("patch erased display or scrolled: %q", patch)
	}
	if got := remotePaintRows(patch); !reflect.DeepEqual(got, []int{1, 2, 3}) {
		t.Fatalf("patch rows=%v", got)
	}
	screen.apply(patch)
	if got := screen.line(1); got != strings.Repeat("界", width) {
		t.Fatalf("full-width wide row lost its last glyph: %q", got)
	}
	if got := strings.TrimRight(screen.line(2), " "); got != "short" {
		t.Fatalf("shorter row retained stale cells: %q", got)
	}
	if got := screen.line(3); got != strings.Repeat(" ", width) {
		t.Fatalf("removed row retained stale cells: %q", got)
	}
	if patch := buildModalPatch(current, current); patch != "" {
		t.Fatalf("unchanged rows repainted: %q", patch)
	}
}
