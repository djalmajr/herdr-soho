package plugin

// The transport regression tests of the shared modal frame: the complete
// frame is one bounded buffered write, nothing erases the visible
// viewport before the paint, stale cells and stale rows are cleaned after
// the paint, and an identical frame for the same viewport is not written
// again. Every failure message carries the diagnostics the brief asks
// for: the writes (count and size each), the escape order and the
// viewport dimensions.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

// frameRecording captures every write the transport makes, so a test can
// assert the write count (one complete frame, no per-row writes) and the
// exact stream order.
type frameRecording struct {
	writes [][]byte
}

func (f *frameRecording) Write(p []byte) (int, error) {
	b := make([]byte, len(p))
	copy(b, p)
	f.writes = append(f.writes, b)
	return len(p), nil
}

func (f *frameRecording) stream() string {
	var b bytes.Buffer
	for _, w := range f.writes {
		b.Write(w)
	}
	return b.String()
}

var frameCSIRe = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)

// escapeOrder lists the CSI sequences of a stream in the order they were
// written (cursor addresses, line/display erases, SGR, DEC modes).
func escapeOrder(stream string) []string {
	return frameCSIRe.FindAllString(stream, -1)
}

// frameDiagnostics is the failure diagnostic: the viewport dimensions the
// frame was drawn against, the writes (count and byte size of each) and
// the escape order of the whole stream.
func frameDiagnostics(rec *frameRecording, width, height int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "viewport=%dx%d writes=%d", width, height, len(rec.writes))
	for i, w := range rec.writes {
		fmt.Fprintf(&b, " write[%d]=%dB", i, len(w))
	}
	fmt.Fprintf(&b, " escapes=[%s]", strings.Join(escapeOrder(rec.stream()), " "))
	return b.String()
}

// legacyRedrawModalSeq is the pre-fix transport, kept in the test only as
// the reference the regression proof discriminates against: the
// home+erase-display write before the paint, then one write per row. It
// also measures the pre-fix write count for the report.
func legacyRedrawModalSeq(w io.Writer, rendered string) int {
	_, _ = fmt.Fprint(w, "\x1b[H\x1b[J")
	writes := 1
	for row, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		_, _ = fmt.Fprintf(w, "\x1b[%d;1H%s", row+1, line)
		writes++
	}
	return writes
}

// elAfterFrameSeq is the intermediate layout the right-margin amendment
// discriminates against: cursor address, content, and only then the
// end-of-line erase. On a terminal that retains a pending auto-wrap at
// the right margin, that trailing erase erases the last content cell of
// a row that fills the width exactly.
func elAfterFrameSeq(rendered string) string {
	var b strings.Builder
	for i, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		fmt.Fprintf(&b, "\x1b[%d;1H%s\x1b[K", i+1, line)
	}
	return b.String()
}

// isFrameWide is the cell-width rule of the mini screen: a wide rune
// occupies two columns, like the display math the renderer uses.
func isFrameWide(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F,
		r >= 0x2E80 && r <= 0xA4CF,
		r >= 0xAC00 && r <= 0xD7A3,
		r >= 0xF900 && r <= 0xFAFF,
		r >= 0xFE30 && r <= 0xFE4F,
		r >= 0xFF00 && r <= 0xFF60,
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x20000 && r <= 0x3FFFD:
		return true
	}
	return false
}

// miniScreen is the minimal ANSI grid behind the frame proofs: it
// understands exactly the subset the shared modal transport emits -
// cursor addressing (CUP), erase to end of line (EL), erase to end of
// display (ED) and SGR (recorded, it never moves the cursor) - and places
// printable runes by cell width. It models the deferred auto-wrap of a
// standard viewport: writing the right-margin column leaves the cursor ON
// the last column (pending wrap), so an EL/ED from there acts from the
// last column, and CUP past the bottom row clamps to the last row.
type miniScreen struct {
	width, height int
	cells         [][]rune
	row, col      int
	pendingWrap   bool // the previous write filled the right margin
	sgr           []string
}

func newMiniScreen(width, height int) *miniScreen {
	cells := make([][]rune, height)
	for i := range cells {
		cells[i] = make([]rune, width)
		for j := range cells[i] {
			cells[i][j] = ' '
		}
	}
	return &miniScreen{width: width, height: height, cells: cells, row: 1, col: 1}
}

func (s *miniScreen) line(n int) string { return string(s.cells[n-1]) }

// paintPlain pre-paints one stale previous frame's rows (the content the
// proof must clean).
func (s *miniScreen) paintPlain(rows []string) {
	for i, line := range rows {
		s.row, s.col, s.pendingWrap = i+1, 1, false
		for _, r := range line {
			s.place(r)
		}
	}
}

// apply runs one frame stream through the grid.
func (s *miniScreen) apply(stream string) {
	for i := 0; i < len(stream); {
		if stream[i] != 0x1b {
			r, size := utf8.DecodeRuneInString(stream[i:])
			if size == 0 {
				i++
				continue
			}
			s.place(r)
			i += size
			continue
		}
		j := i + 1
		if j >= len(stream) || stream[j] != '[' {
			i++
			continue
		}
		j++
		for j < len(stream) && !(stream[j] >= 0x40 && stream[j] <= 0x7e) {
			j++
		}
		if j >= len(stream) {
			break
		}
		body := stream[i+2 : j]
		switch stream[j] {
		case 'H':
			s.cup(body)
		case 'K':
			for c := s.col; c <= s.width; c++ {
				s.cells[s.row-1][c-1] = ' '
			}
		case 'J':
			for c := s.col; c <= s.width; c++ {
				s.cells[s.row-1][c-1] = ' '
			}
			for r := s.row; r <= s.height; r++ {
				for c := 1; c <= s.width; c++ {
					s.cells[r-1][c-1] = ' '
				}
			}
		case 'm':
			s.sgr = append(s.sgr, body)
		}
		i = j + 1
	}
}

func (s *miniScreen) cup(body string) {
	parts := strings.Split(body, ";")
	row, col := 1, 1
	if len(parts) > 0 && parts[0] != "" {
		row, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 && parts[1] != "" {
		col, _ = strconv.Atoi(parts[1])
	}
	if row < 1 {
		row = 1
	}
	if row > s.height {
		row = s.height
	}
	if col < 1 {
		col = 1
	}
	if col > s.width {
		col = s.width
	}
	s.row, s.col, s.pendingWrap = row, col, false
}

func (s *miniScreen) place(r rune) {
	if s.pendingWrap {
		// The previous write filled the right margin: the next printable
		// character wraps to the next line (deferred auto-wrap).
		s.pendingWrap = false
		s.row++
		if s.row > s.height {
			s.row = s.height
		}
		s.col = 1
	}
	w := 1
	if isFrameWide(r) {
		w = 2
	}
	if s.col+w-1 > s.width {
		// A rune that overruns the right margin is clipped, like the
		// physical terminal (rows are cut to the viewport width, so this
		// only guards stale pre-painted content).
		return
	}
	for c := 0; c < w; c++ {
		s.cells[s.row-1][s.col-1+c] = r
	}
	if s.col+w-1 == s.width {
		// Writing the right margin leaves the cursor on the last column
		// with pending auto-wrap: an EL/ED from here acts from the last
		// column, so the last cell is not skipped.
		s.col = s.width
		s.pendingWrap = true
		return
	}
	s.col += w
}

// modalFrameTestState is the enhanced-view runtime fixture: n working
// agent sessions under one workspace.
func modalFrameTestState(n int) *BoardState {
	s := NewBoardState()
	s.ViewState.Enabled = true
	for i := 0; i < n; i++ {
		s.Entries = append(s.Entries, PickerEntry{
			"ref":             fmt.Sprintf("local/w1:p%d", i+1),
			"name":            fmt.Sprintf("agent-%d", i+1),
			"kind":            "claude",
			"status":          "working",
			"workspace_label": "alpha",
			"tab_label":       "1",
			"cwd":             fmt.Sprintf("/srv/agent-%d", i+1),
			"machine":         "local",
		})
	}
	return s
}

// TestModalFramePaintsNoBlankBeforePaint is the discriminating regression
// proof against the old blank-before-paint behavior: the frame carries no
// home and no display erase (nothing clears the visible viewport before
// the paint), one write carries the whole frame, each row is erased at
// its addressed start immediately before its own content, and the stale
// cells of a previous frame are gone in the final screen. The old
// transport, run through the same checks, keeps its blank-before-paint
// signature. The one buffered write removes the old separate clear and
// per-row writes - and their guaranteed whole-screen blank - but it is
// not pixel-level proof: a terminal may still process the rows of the one
// write incrementally (no synchronized-output mode is used), so the worst
// observable intermediate state is a partial repaint; a blank screen is
// gone by construction.
func TestModalFramePaintsNoBlankBeforePaint(t *testing.T) {
	const width, height = 80, 24
	frame := "12345678\nabcdefgh\nEsc     \n"
	var rec frameRecording
	if err := redrawModal(&rec, frame); err != nil {
		t.Fatalf("redrawModal: %v: %s", err, frameDiagnostics(&rec, width, height))
	}
	if len(rec.writes) != 1 {
		t.Fatalf("the frame is not one complete write: %s", frameDiagnostics(&rec, width, height))
	}
	stream := rec.stream()
	// No whole-viewport erase before the paint: no home, no display
	// erase inside the frame, no raw line feeds.
	if strings.Contains(stream, "\x1b[H") || strings.Contains(stream, "\x1b[J") || strings.ContainsAny(stream, "\r\n") {
		t.Fatalf("the frame clears the viewport or scrolls before the paint: %s", frameDiagnostics(&rec, width, height))
	}
	// Each row is erased at its addressed start, immediately before its
	// own content, in one bounded stream.
	var want strings.Builder
	for i, line := range []string{"12345678", "abcdefgh", "Esc     "} {
		fmt.Fprintf(&want, "\x1b[%d;1H\x1b[K%s", i+1, line)
	}
	if stream != want.String() {
		t.Fatalf("frame stream is not the exact erase-then-paint row sequence:\n got=%s\nwant=%s\n%s", stream, want.String(), frameDiagnostics(&rec, width, height))
	}
	// Discriminating reference: the old transport keeps the
	// blank-before-paint signature - home+erase-display before the first
	// content byte - so the checks above would catch a regression to it.
	var legacy frameRecording
	legacyWrites := legacyRedrawModalSeq(&legacy, frame)
	if !strings.HasPrefix(legacy.stream(), "\x1b[H\x1b[J") {
		t.Fatalf("the legacy reference lost its blank-before-paint signature: %q", legacy.stream())
	}
	if legacyWrites != 4 { // clear + one write per row
		t.Fatalf("the legacy reference must make one clear write plus one per row: %d", legacyWrites)
	}
	// Grid proof (a simulated consumer's final state, not physical
	// pixels): the previous frame's stale cells are gone, and no
	// processing prefix of this stream is a blank screen.
	screen := newMiniScreen(width, height)
	screen.paintPlain([]string{
		"stale row one, much longer than the fresh row that replaces it",
		"stale row two",
		"stale row three",
	})
	screen.apply(stream)
	for i, wantRow := range []string{"12345678", "abcdefgh", "Esc     "} {
		if got := strings.TrimRight(screen.line(i+1), " "); got != strings.TrimRight(wantRow, " ") {
			t.Fatalf("stale cells survived row %d: got=%q want=%q: %s", i+1, got, wantRow, frameDiagnostics(&rec, width, height))
		}
	}
	for i := 4; i <= height; i++ {
		if got := screen.line(i); got != strings.Repeat(" ", width) {
			t.Fatalf("stale row %d survived below the frame: %q: %s", i, got, frameDiagnostics(&rec, width, height))
		}
	}
}

// TestModalFrameSingleCompleteWrite proves one bounded write carries a
// frame exactly as tall as the viewport, byte for byte: no clear before
// the paint, no raw LF/CRLF (no scrolling of the final viewport row), and
// the measured write count collapses from one-per-row-plus-clear to one.
func TestModalFrameSingleCompleteWrite(t *testing.T) {
	const width, height = 80, 24
	lines := make([]string, height)
	for i := range lines {
		lines[i] = fmt.Sprintf("row-%02d", i)
	}
	frame := strings.Join(lines, "\n") + "\n"
	var rec frameRecording
	if err := redrawModal(&rec, frame); err != nil {
		t.Fatalf("redrawModal: %v: %s", err, frameDiagnostics(&rec, width, height))
	}
	if len(rec.writes) != 1 {
		t.Fatalf("a full-viewport frame must be one write: %s", frameDiagnostics(&rec, width, height))
	}
	stream := rec.stream()
	if strings.ContainsAny(stream, "\r\n") {
		t.Fatalf("raw line feeds in the frame (the final row must not scroll): %s", frameDiagnostics(&rec, width, height))
	}
	var want strings.Builder
	for i, line := range lines {
		fmt.Fprintf(&want, "\x1b[%d;1H\x1b[K%s", i+1, line)
	}
	if stream != want.String() {
		t.Fatalf("frame stream is not the exact row sequence:\n got=%s\nwant=%s\n%s", stream, want.String(), frameDiagnostics(&rec, width, height))
	}
	// Measured before/after: the old transport wrote the clear plus one
	// write per row; the corrected transport writes one.
	legacyWrites := legacyRedrawModalSeq(io.Discard, frame)
	if legacyWrites != height+1 {
		t.Fatalf("legacy write count changed under the proof: got=%d want=%d", legacyWrites, height+1)
	}
	if len(rec.writes) != 1 {
		t.Fatalf("write count did not collapse to one: legacy=%d new=%d", legacyWrites, len(rec.writes))
	}
}

// TestModalFrameFullWidthRowKeepsFinalColumn is the discriminating
// terminal-consumer proof for the right margin: a row that fills the
// terminal width exactly leaves the cursor on the last column (pending
// auto-wrap), so a trailing end-of-line erase from there would erase the
// row's last content cell. The corrected frame erases each row at its
// addressed start, immediately before the content, so the final-column
// glyph survives; the EL-after layout, run through the same consumer, is
// shown to lose it. Shorter replacement rows and stale trailing rows are
// cleaned by the same frame and the caller's below-frame cleanup.
func TestModalFrameFullWidthRowKeepsFinalColumn(t *testing.T) {
	const width, height = 80, 24
	legacy := func(names ...string) *BoardState {
		s := NewBoardState() // enhanced view off: the legacy modal, shorter than the viewport
		for i, name := range names {
			s.Entries = append(s.Entries, PickerEntry{
				"ref": fmt.Sprintf("local/w1:p%d", i+1), "name": name,
				"kind": "claude", "status": "working", "workspace_label": "alpha",
				"tab_label": "1", "title": strings.Repeat("t", 60),
				"cwd": fmt.Sprintf("/srv/a%d", i+1), "machine": "local",
			})
		}
		return s
	}
	full := legacy(strings.Repeat("n", 24), "short-b", "short-c")
	var rec frameRecording
	redrawSession(&rec, full, width)
	if len(rec.writes) != 1 {
		t.Fatalf("the first draw must paint once: %s", frameDiagnostics(&rec, width, height))
	}
	frameRows := strings.Split(strings.TrimSuffix(renderSessionModal(full, width, height, ""), "\n"), "\n")
	// Fixture guard: a data row fills the right margin and its last
	// column is a visible glyph, so the final column is a real content
	// cell.
	fullWidthRow := -1
	for i, line := range frameRows {
		stripped := StripPickerControls(line)
		if displayWidth(stripped) == width && !strings.HasSuffix(stripped, " ") {
			fullWidthRow = i
			break
		}
	}
	if fullWidthRow < 0 {
		t.Fatalf("the fixture has no full-width row ending in a glyph (the final column cannot be proven): %s", frameDiagnostics(&rec, width, height))
	}
	// A screen staled with full-width stale rows, including below the
	// frame.
	screen := newMiniScreen(width, height)
	for i := 0; i < height; i++ {
		screen.paintPlain([]string{strings.Repeat("s", width)})
	}
	screen.apply(rec.stream())
	// The final-column glyph of the full-width row survives.
	want := StripPickerControls(frameRows[fullWidthRow])
	if got := screen.line(fullWidthRow + 1); got != want {
		t.Fatalf("the final column of the full-width row was lost or wrong:\n got=%q\nwant=%q\n%s", got, want, frameDiagnostics(&rec, width, height))
	}
	for i, wantRow := range frameRows {
		plain := strings.TrimRight(StripPickerControls(wantRow), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("stale cells survive row %d:\n got=%q\nwant=%q\n%s", i+1, got, plain, frameDiagnostics(&rec, width, height))
		}
	}
	for i := len(frameRows) + 1; i <= height; i++ {
		if got := screen.line(i); got != strings.Repeat(" ", width) {
			t.Fatalf("stale trailing row %d survived the below-frame cleanup: %q: %s", i, got, frameDiagnostics(&rec, width, height))
		}
	}
	// Shorter replacement rows: the same screen, a shorter frame with
	// short rows - no stale cell to the right of the new content.
	short := legacy("x")
	var shortRec frameRecording
	redrawSession(&shortRec, short, width)
	if len(shortRec.writes) != 1 {
		t.Fatalf("the second draw must paint once: %s", frameDiagnostics(&shortRec, width, height))
	}
	screen.apply(shortRec.stream())
	shortRows := strings.Split(strings.TrimSuffix(renderSessionModal(short, width, height, ""), "\n"), "\n")
	for i, wantRow := range shortRows {
		plain := strings.TrimRight(StripPickerControls(wantRow), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("stale cells survive row %d of the shorter frame:\n got=%q\nwant=%q\n%s", i+1, got, plain, frameDiagnostics(&shortRec, width, height))
		}
	}
	for i := len(shortRows) + 1; i <= height; i++ {
		if got := screen.line(i); got != strings.Repeat(" ", width) {
			t.Fatalf("stale trailing row %d survived the shorter frame's cleanup: %q: %s", i, got, frameDiagnostics(&shortRec, width, height))
		}
	}
	// Discriminating: the EL-after layout (erase trailing the content)
	// loses the final-column glyph on the same consumer.
	screen2 := newMiniScreen(width, height)
	screen2.paintPlain([]string{strings.Repeat("s", width)})
	screen2.apply(elAfterFrameSeq(strings.Join(frameRows, "\n") + "\n"))
	got := strings.TrimRight(screen2.line(fullWidthRow+1), " ")
	wantLost := strings.TrimRight(StripPickerControls(frameRows[fullWidthRow]), " ")
	if got == wantLost {
		t.Fatalf("the EL-after reference preserves the final-column glyph on this consumer (the hazard is no longer discriminating): %q", got)
	}
}

// TestRedrawSessionSkipsIdenticalFramePerRuntime proves the duplicate
// guard: an identical frame for the same viewport is not written again,
// a changed frame or viewport is, and the record is per runtime - two
// independent sessions never share it.
func TestRedrawSessionSkipsIdenticalFramePerRuntime(t *testing.T) {
	const width, height = 80, 24
	state := modalFrameTestState(3)
	var rec frameRecording
	redrawSession(&rec, state, width)
	if len(rec.writes) != 1 {
		t.Fatalf("the first draw must paint: %s", frameDiagnostics(&rec, width, height))
	}
	redrawSession(&rec, state, width)
	redrawSession(&rec, state, width)
	if len(rec.writes) != 1 {
		t.Fatalf("an identical frame for the same viewport must not be repainted: %s", frameDiagnostics(&rec, width, height))
	}
	// A changed state (the selection moves) repaints.
	state.ApplyKey("down")
	redrawSession(&rec, state, width)
	if len(rec.writes) != 2 {
		t.Fatalf("a changed frame must repaint: %s", frameDiagnostics(&rec, width, height))
	}
	// A changed viewport repaints even the same content (the cleanup
	// guarantee travels with the geometry).
	other := modalFrameTestState(3) // a fresh, independent runtime
	var otherRec frameRecording
	redrawSession(&otherRec, other, width)
	redrawSession(&otherRec, other, width+20)
	if len(otherRec.writes) != 2 {
		t.Fatalf("a changed viewport must repaint the same content: %s", frameDiagnostics(&otherRec, width+20, height))
	}
	// Clipboard/failure feedback stays current against the skip: the
	// notice shows, and the navigation that supersedes it repaints.
	notice := modalFrameTestState(3)
	var noticeRec frameRecording
	notice.CopyNotice = "Copied local/w1:p1"
	redrawSession(&noticeRec, notice, width)
	if !strings.Contains(noticeRec.stream(), "Copied local/w1:p1") {
		t.Fatalf("the copy notice is not visible in the frame: %s", frameDiagnostics(&noticeRec, width, height))
	}
	notice.NavPending = true
	notice.NavTarget = notice.Entries[0]
	redrawSession(&noticeRec, notice, width)
	if len(noticeRec.writes) != 2 || !strings.Contains(noticeRec.stream(), "Focusing selected pane") {
		t.Fatalf("the navigation status must supersede the notice and repaint: %s", frameDiagnostics(&noticeRec, width, height))
	}
}

// TestRedrawSessionCleansStaleShortRows proves the per-row cleanup: when
// a fresh frame's rows are shorter than the previous frame's, no stale
// cell survives to the right of the new content (grid proof, including a
// wide-rune row), and the frame count of rows does not grow.
func TestRedrawSessionCleansStaleShortRows(t *testing.T) {
	const width, height = 80, 24
	long := NewBoardState()
	long.ViewState.Enabled = true
	for i := 0; i < 4; i++ {
		long.Entries = append(long.Entries, PickerEntry{
			"ref":             fmt.Sprintf("local/w1:p%d", i+1),
			"name":            fmt.Sprintf("a very long worker name %d that fills the row width entirely", i+1) + " 日本語",
			"kind":            "claude",
			"status":          "working",
			"workspace_label": "alpha",
			"tab_label":       "1",
			"cwd":             strings.Repeat("x", 40),
			"machine":         "local",
		})
	}
	var rec frameRecording
	redrawSession(&rec, long, width)
	if len(rec.writes) != 1 {
		t.Fatalf("the first draw must paint: %s", frameDiagnostics(&rec, width, height))
	}
	screen := newMiniScreen(width, height)
	screen.apply(rec.stream())

	short := modalFrameTestState(4) // same row count, short rows
	var shortRec frameRecording
	redrawSession(&shortRec, short, width)
	if len(shortRec.writes) != 1 {
		t.Fatalf("the second draw must paint once: %s", frameDiagnostics(&shortRec, width, height))
	}
	screen.apply(shortRec.stream())
	frame := renderSessionModal(short, width, height, "")
	for i, want := range strings.Split(strings.TrimSuffix(frame, "\n"), "\n") {
		plain := strings.TrimRight(StripPickerControls(want), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("stale cells survive row %d after shorter rows:\n got=%q\nwant=%q\n%s", i+1, got, plain, frameDiagnostics(&shortRec, width, height))
		}
	}
}

// TestRedrawSessionCleansShrinkingHeight proves the below-frame cleanup:
// the enhanced popup always fills the viewport, so it must erase nothing
// below itself (the final row survives intact); a legacy (non-TTY) modal
// is shorter than the viewport, so a list change that shrinks it must
// clear the stale rows left below it, from the first row it does not
// paint, after the paint (grid proof over a pre-staled screen).
func TestRedrawSessionCleansShrinkingHeight(t *testing.T) {
	const width, height = 80, 24
	// Enhanced popup: a dense frame fills the whole viewport.
	tall := modalFrameTestState(40)
	var rec frameRecording
	redrawSession(&rec, tall, width)
	if len(rec.writes) != 1 {
		t.Fatalf("the first draw must paint: %s", frameDiagnostics(&rec, width, height))
	}
	tallRows := modalFrameRows(renderSessionModal(tall, width, height, ""))
	if tallRows != height {
		t.Fatalf("the enhanced frame must fill the viewport: rows=%d want=%d", tallRows, height)
	}
	if strings.Contains(rec.stream(), "\x1b[J") {
		t.Fatalf("a full-viewport frame must not erase below itself: %s", frameDiagnostics(&rec, width, height))
	}

	// Legacy (non-TTY) modal: shorter than the viewport, over a screen
	// staled by a taller previous frame.
	legacy := func(n int) *BoardState {
		s := NewBoardState() // enhanced view off: the legacy modal contract
		for i := 0; i < n; i++ {
			s.Entries = append(s.Entries, PickerEntry{
				"ref": fmt.Sprintf("local/w1:p%d", i+1), "name": fmt.Sprintf("agent-%d", i+1),
				"kind": "claude", "status": "working", "workspace_label": "alpha",
				"tab_label": "1", "cwd": fmt.Sprintf("/srv/agent-%d", i+1), "machine": "local",
			})
		}
		return s
	}
	long := legacy(6)
	longRows := modalFrameRows(renderSessionModal(long, width, height, ""))
	if longRows <= 0 || longRows >= height {
		t.Fatalf("the legacy fixture must be shorter than the viewport: rows=%d height=%d", longRows, height)
	}
	screen := newMiniScreen(width, height)
	for i := 0; i < height; i++ {
		screen.paintPlain([]string{fmt.Sprintf("stale row %02d that must be cleared by the shorter frame", i+1)})
	}
	var longRec frameRecording
	redrawSession(&longRec, long, width)
	if len(longRec.writes) != 1 {
		t.Fatalf("the legacy draw must paint once: %s", frameDiagnostics(&longRec, width, height))
	}
	screen.apply(longRec.stream())
	for i := longRows + 1; i <= height; i++ {
		if got := screen.line(i); got != strings.Repeat(" ", width) {
			t.Fatalf("stale row %d survived below the legacy frame: %q: %s", i, got, frameDiagnostics(&longRec, width, height))
		}
	}

	// The list shrinks: the stale rows the shorter frame does not paint
	// are cleared from the first unpainted row, after the paint.
	short := legacy(2)
	shortRows := modalFrameRows(renderSessionModal(short, width, height, ""))
	if shortRows >= longRows {
		t.Fatalf("the short fixture must be shorter than the long one: rows=%d want<%d", shortRows, longRows)
	}
	var shortRec frameRecording
	redrawSession(&shortRec, short, width)
	if len(shortRec.writes) != 1 {
		t.Fatalf("the second draw must paint once: %s", frameDiagnostics(&shortRec, width, height))
	}
	stream := shortRec.stream()
	wantCleanup := fmt.Sprintf("\x1b[%d;1H\x1b[J", shortRows+1)
	if i := strings.Index(stream, wantCleanup); i < 0 || i != len(stream)-len(wantCleanup) {
		t.Fatalf("the stale rows below the shorter frame are not cleared from the first unpainted row, after the paint:\nwant suffix=%q\n%s", wantCleanup, frameDiagnostics(&shortRec, width, height))
	}
	screen.apply(stream)
	for i := shortRows + 1; i <= height; i++ {
		if got := screen.line(i); got != strings.Repeat(" ", width) {
			t.Fatalf("stale row %d survived below the shorter frame: %q: %s", i, got, frameDiagnostics(&shortRec, width, height))
		}
	}
	for i, want := range strings.Split(strings.TrimSuffix(renderSessionModal(short, width, height, ""), "\n"), "\n") {
		plain := strings.TrimRight(StripPickerControls(want), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("row %d of the shorter frame is wrong:\n got=%q\nwant=%q\n%s", i+1, got, plain, frameDiagnostics(&shortRec, width, height))
		}
	}
}

// failingWriter always fails (a broken pipe): a failed frame is never
// recorded as painted.
type failingWriter struct{ err error }

func (f failingWriter) Write(p []byte) (int, error) { return 0, f.err }

// shortWriter accepts exactly half of each write: a short frame write is
// a failed frame, never a painted one.
type shortWriter struct{}

func (s shortWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	return len(p) / 2, nil
}

// partialFailingWriter is the A/B/A writer: writes succeed in full, except
// the one numbered failAt (1-based), which fails partway (a short write
// plus an error). It records exactly what reached the wire - complete
// writes and the partial bytes - so the test can prove what the consumer
// saw.
type partialFailingWriter struct {
	frameRecording // records every write attempt, complete or partial
	failAt         int
	count          int
}

func (p *partialFailingWriter) Write(b []byte) (int, error) {
	p.count++
	if p.count == p.failAt {
		partial := append([]byte(nil), b[:len(b)/2]...)
		p.writes = append(p.writes, partial)
		return len(b) / 2, errors.New("partial frame write")
	}
	p.writes = append(p.writes, append([]byte(nil), b...))
	return len(b), nil
}

// TestRedrawSessionFailedWriteNotRecorded proves the I/O honesty: a
// failed or short frame write is not recorded as painted, so the next
// draw trigger retries the complete frame, byte-identical.
func TestRedrawSessionFailedWriteNotRecorded(t *testing.T) {
	const width, height = 80, 24
	state := modalFrameTestState(3)
	redrawSession(failingWriter{err: errors.New("broken pipe")}, state, width)
	var good frameRecording
	redrawSession(&good, state, width)
	if len(good.writes) != 1 {
		t.Fatalf("a frame that failed to write must be retried on the next draw: %s", frameDiagnostics(&good, width, height))
	}
	state2 := modalFrameTestState(3)
	redrawSession(shortWriter{}, state2, width)
	var good2 frameRecording
	redrawSession(&good2, state2, width)
	if len(good2.writes) != 1 {
		t.Fatalf("a short frame write must be retried on the next draw: %s", frameDiagnostics(&good2, width, height))
	}
	if good2.stream() != good.stream() {
		t.Fatalf("the retried frame is not byte-identical to the first successful one:\n%s\n%s", frameDiagnostics(&good, width, height), frameDiagnostics(&good2, width, height))
	}
}

// TestRedrawSessionInvalidatesCacheAfterPartialWrite proves the cache
// invalidation: A paints successfully, a different B fails partway, and
// the next request of A is not suppressed as identical to the old painted
// cache - it is written completely again and restores the screen (the
// consumer ends with the complete frame A, not the partial B).
func TestRedrawSessionInvalidatesCacheAfterPartialWrite(t *testing.T) {
	const width, height = 80, 24
	state := modalFrameTestState(3)       // frame A: 3 sessions
	w := &partialFailingWriter{failAt: 2} // the second write (B) fails partway
	redrawSession(w, state, width)        // A: complete
	if len(w.writes) != 1 {
		t.Fatalf("A must paint completely on the first draw: %s", frameDiagnostics(&w.frameRecording, width, height))
	}
	aStream := w.writes[0]
	// The state changes: frame B (5 sessions) is requested and fails partway.
	for i := 4; i <= 5; i++ {
		state.Entries = append(state.Entries, PickerEntry{
			"ref": fmt.Sprintf("local/w1:p%d", i), "name": fmt.Sprintf("agent-%d", i),
			"kind": "claude", "status": "working", "workspace_label": "alpha",
			"tab_label": "1", "cwd": fmt.Sprintf("/srv/agent-%d", i), "machine": "local",
		})
	}
	redrawSession(w, state, width) // B: partial write + failure
	if len(w.writes) != 2 {
		t.Fatalf("the failed B must still be attempted once: %s", frameDiagnostics(&w.frameRecording, width, height))
	}
	wantB := buildModalPatch(renderSessionModal(modalFrameTestState(3), width, height, ""), renderSessionModal(state, width, height, ""))
	if len(w.writes[1]) != len(wantB)/2 || !strings.HasPrefix(string(wantB), string(w.writes[1])) {
		t.Fatalf("the failed B is not a partial write of frame B: %s", frameDiagnostics(&w.frameRecording, width, height))
	}
	// The state returns to A: the byte-identical frame must be written
	// completely again, not suppressed by the stale painted cache.
	state.Entries = state.Entries[:3]
	redrawSession(w, state, width)
	if len(w.writes) != 3 {
		t.Fatalf("the re-requested A is suppressed by the stale painted cache after the failed partial B: writes=%d: %s", len(w.writes), frameDiagnostics(&w.frameRecording, width, height))
	}
	if !bytes.Equal(w.writes[2], aStream) {
		t.Fatalf("the re-requested A is not the complete first frame A: %s", frameDiagnostics(&w.frameRecording, width, height))
	}
	// Consumer proof: the stream as the terminal saw it (A, partial B, A)
	// ends with the complete frame A on screen.
	screen := newMiniScreen(width, height)
	for _, wr := range w.writes {
		screen.apply(string(wr))
	}
	for i, wantRow := range strings.Split(strings.TrimSuffix(renderSessionModal(state, width, height, ""), "\n"), "\n") {
		plain := strings.TrimRight(StripPickerControls(wantRow), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("the screen is not restored to the complete frame A after the partial B (row %d): got=%q want=%q: %s", i+1, got, plain, frameDiagnostics(&w.frameRecording, width, height))
		}
	}
}

// TestRedrawSessionKeepsStylesInRowOrder proves the style/control
// handling: a styled (banded) row is painted as cursor address, band on,
// text, band off, and only then the end-of-line erase - and the grid ends
// with the styled text intact, no control byte leaked into the cells.
func TestRedrawSessionKeepsStylesInRowOrder(t *testing.T) {
	const width, height = 80, 24
	state := modalFrameTestState(3)
	var rec frameRecording
	redrawSession(&rec, state, width)
	if len(rec.writes) != 1 {
		t.Fatalf("the draw must paint once: %s", frameDiagnostics(&rec, width, height))
	}
	stream := rec.stream()
	// The selected row is banded (reverse video): the row is erased at
	// its addressed start, then the band on, the row text, the band off.
	bandRe := regexp.MustCompile(`\x1b\[\d+;1H\x1b\[K *\x1b\[7m[^\x1b]+\x1b\[0m`)
	if bandRe.FindString(stream) == "" {
		t.Fatalf("no styled row painted as row erase+band on+text+band off: %s", frameDiagnostics(&rec, width, height))
	}
	screen := newMiniScreen(width, height)
	screen.apply(stream)
	frame := renderSessionModal(state, width, height, "")
	for i, want := range strings.Split(strings.TrimSuffix(frame, "\n"), "\n") {
		plain := strings.TrimRight(StripPickerControls(want), " ")
		if got := strings.TrimRight(screen.line(i+1), " "); got != plain {
			t.Fatalf("styled row %d painted the wrong cells:\n got=%q\nwant=%q\n%s", i+1, got, plain, frameDiagnostics(&rec, width, height))
		}
	}
}
