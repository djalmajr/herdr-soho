package plugin

import (
	"bufio"
	contextpkg "context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// boardRefreshInterval is the team board's auto-update cadence; tests replace it.
var boardRefreshInterval = 10 * time.Second

// boardNow is the board clock; tests replace it.
var boardNow = time.Now

// boardFilterFields filters like the picker's fields, plus the task title.
var boardFilterFields = []string{"ref", "name", "kind", "status", "title", "workspace_label", "tab_label", "cwd", "machine"}

// BoardState holds the team board's view: every agent of every machine,
// grouped by machine and workspace, refreshed in place.
type BoardState struct {
	Entries      []PickerEntry
	Query        string
	Selected     int
	Loading      int
	LoadingLocal bool
	Failures     []PickerFailure
	LastEntry    PickerEntry
	Copied       *string
	Exit         string
	EscPending   bool
	CSIPending   bool
	UpdatedAt    string
	selectedRef  string
}

func NewBoardState() *BoardState {
	return &BoardState{Entries: []PickerEntry{}, Failures: []PickerFailure{}}
}

// boardAgents keeps only the panes that have an agent (kind not null), in
// board order: the machine (local first, then arrival order), within it the
// workspace by workspace_label, and within the workspace the names that
// start with "orchestrator" before the rest, by name.
func boardAgents(entries []PickerEntry) []PickerEntry {
	out := make([]PickerEntry, 0, len(entries))
	for _, entry := range entries {
		if pickerString(entry["kind"]) != "" {
			out = append(out, entry)
		}
	}
	rank := map[string]int{}
	next := 1
	for _, entry := range out {
		machine := pickerString(entry["machine"])
		if _, ok := rank[machine]; !ok {
			if machine == "local" {
				rank[machine] = 0
			} else {
				rank[machine] = next
				next++
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		mi, mj := pickerString(out[i]["machine"]), pickerString(out[j]["machine"])
		if rank[mi] != rank[mj] {
			return rank[mi] < rank[mj]
		}
		wi, wj := pickerString(out[i]["workspace_label"]), pickerString(out[j]["workspace_label"])
		if wi != wj {
			return wi < wj
		}
		ni, nj := pickerString(out[i]["name"]), pickerString(out[j]["name"])
		oi, oj := strings.HasPrefix(ni, "orchestrator"), strings.HasPrefix(nj, "orchestrator")
		if oi != oj {
			return oi
		}
		return ni < nj
	})
	return out
}

func (s *BoardState) visible() []PickerEntry {
	return filterPickerEntries(boardAgents(s.Entries), s.Query, boardFilterFields)
}

func (s *BoardState) clamp() {
	n := len(s.visible())
	if s.Selected > n-1 {
		s.Selected = n - 1
	}
	if s.Selected < 0 {
		s.Selected = 0
	}
}

// BoardTitle strips the leading "<role>: " prefix the CLI puts on pane
// titles ("orchestrator: <task>", "<role>: <brief task>") and removes
// control characters, like the picker does.
func BoardTitle(value any) string {
	text := StripPickerControls(pickerString(value))
	idx := strings.Index(text, ": ")
	if idx > 0 && !strings.ContainsAny(text[:idx], " \t") {
		return text[idx+2:]
	}
	return text
}

func boardMarker(entry PickerEntry) string {
	switch StripPickerControls(pickerString(entry["status"])) {
	case "blocked":
		return "!"
	case "working":
		return "*"
	default:
		return " "
	}
}

// BoardEntryLine renders one agent row: name, kind, status and the title,
// cut at the width the way the picker cuts its rows (the last column takes
// the remaining budget, ellipsized, then a hard cut at the width).
func BoardEntryLine(entry PickerEntry, width int) string {
	display := func(v any) string {
		s := StripPickerControls(pickerString(v))
		if s == "" {
			return "-"
		}
		return s
	}
	cols := []string{display(entry["name"]), display(entry["kind"]), display(entry["status"]), display(BoardTitle(entry["title"]))}
	lead := strings.Join(cols[:len(cols)-1], " ")
	budget := width - jsLength(lead) - 1
	if budget < 0 {
		budget = 0
	}
	last := cols[len(cols)-1]
	if jsLength(last) > budget {
		if budget > 0 {
			last = jsSlice(last, budget-1) + "…"
		} else {
			last = ""
		}
	}
	line := lead + " " + last
	if jsLength(line) > width {
		line = jsSlice(line, width)
	}
	return line
}

// boardTotals renders the board's top line: the total agent count, the
// per-status counts (working, blocked, idle, done, when present) and the
// local time (HH:MM:SS) of the last completed update, when there is one.
func boardTotals(agents []PickerEntry, updatedAt string) string {
	counts := map[string]int{}
	for _, entry := range agents {
		counts[StripPickerControls(pickerString(entry["status"]))]++
	}
	line := fmt.Sprintf("%d agents", len(agents))
	for _, status := range []string{"working", "blocked", "idle", "done"} {
		if counts[status] > 0 {
			line += fmt.Sprintf(" · %d %s", counts[status], status)
		}
	}
	if updatedAt != "" {
		line += " · " + updatedAt
	}
	return line
}

func RenderBoard(state *BoardState, width int) string {
	agents := boardAgents(state.Entries)
	display := func(v any) string {
		s := StripPickerControls(pickerString(v))
		if s == "" {
			return "-"
		}
		return s
	}
	lines := []string{boardTotals(agents, state.UpdatedAt), "> " + state.Query}
	currentMachine, currentWorkspace := "", ""
	for i, entry := range state.visible() {
		machine := pickerString(entry["machine"])
		workspace := pickerString(entry["workspace_label"])
		if machine != currentMachine || workspace != currentWorkspace {
			lines = append(lines, fmt.Sprintf("%s · %s (%s)", display(entry["machine"]), display(entry["workspace_label"]), display(entry["workspace_id"])))
			currentMachine = machine
			currentWorkspace = workspace
		}
		cursor := " "
		if i == state.Selected {
			cursor = ">"
		}
		rowWidth := width - 3
		if rowWidth < 2 {
			rowWidth = 2
		}
		lines = append(lines, cursor+boardMarker(entry)+" "+BoardEntryLine(entry, rowWidth))
	}
	list := state.visible()
	if len(list) == 0 && len(agents) > 0 {
		lines = append(lines, "nenhum resultado para \""+state.Query+"\"")
	}
	if state.Loading > 0 {
		if state.LoadingLocal {
			lines = append(lines, "carregando local…")
		} else {
			lines = append(lines, "carregando windows…")
		}
	}
	for _, failure := range state.Failures {
		lines = append(lines, "máquina "+StripPickerControls(failure.Label)+": falhou ("+StripPickerControls(failure.Cause)+")")
	}
	if len(agents) == 0 && state.Loading == 0 && len(state.Failures) == 0 {
		lines = append(lines, "nenhum agente")
	}
	// No agent count at the bottom: the top totals line already gives it.
	return strings.Join(lines, "\n") + "\n"
}

func (s *BoardState) ApplyKey(key string) string {
	switch key {
	case "enter":
		if s.Exit != "" {
			return s.Exit
		}
		list := s.visible()
		if len(list) == 0 {
			return ""
		}
		entry := list[min(s.Selected, len(list)-1)]
		s.LastEntry = entry
		copied := PickerCopyPayload(entry)
		s.Copied = &copied
		s.Exit = "copy"
		return "copy"
	case "esc", "ctrl-c":
		if s.Exit == "" {
			s.Exit = "esc"
		}
		return s.Exit
	case "backspace":
		s.Query = jsSlice(s.Query, jsLength(s.Query)-1)
		s.clamp()
		return ""
	case "up":
		if len(s.visible()) > 0 && s.Selected > 0 {
			s.Selected--
		}
		return ""
	case "down":
		if len(s.visible()) > 1 && s.Selected < len(s.visible())-1 {
			s.Selected++
		}
		return ""
	case "update":
		return "update"
	default:
		if jsLength(key) == 1 {
			r, _ := utf8.DecodeRuneInString(key)
			if r >= 0x20 && r != 0x7f {
				if r == 'r' && s.Query == "" {
					return "update"
				}
				s.Query += key
				s.clamp()
			}
		}
		return ""
	}
}

func (s *BoardState) FeedChunk(chunk string) string {
	for _, r := range chunk {
		ch := string(r)
		if s.CSIPending {
			s.CSIPending = !(r >= 0x40 && r <= 0x7e)
			if r >= 0x40 && r <= 0x7e {
				if r == 'A' {
					s.ApplyKey("up")
				}
				if r == 'B' {
					s.ApplyKey("down")
				}
			}
			if s.Exit != "" {
				return s.Exit
			}
			continue
		}
		if s.EscPending {
			s.EscPending = false
			if r == '[' {
				s.CSIPending = true
				continue
			}
			s.ApplyKey("esc")
			return s.Exit
		}
		switch r {
		case 0x1b:
			s.EscPending = true
		case '\r', '\n':
			s.ApplyKey("enter")
			if s.Exit != "" {
				return s.Exit
			}
		case 0x7f, 0x08:
			s.ApplyKey("backspace")
		case 0x03:
			s.ApplyKey("ctrl-c")
			return s.Exit
		case 0x12:
			if action := s.ApplyKey("update"); action != "" {
				return action
			}
		default:
			if r >= 0x20 && r != 0x7f {
				if action := s.ApplyKey(ch); action != "" {
					return action
				}
			}
		}
	}
	return s.Exit
}

func (s *BoardState) FlushEsc() string {
	if !s.EscPending {
		return ""
	}
	s.EscPending = false
	s.ApplyKey("esc")
	return s.Exit
}

func cloneBoardState(source *BoardState) *BoardState {
	clone := *source
	clone.Entries = append([]PickerEntry(nil), source.Entries...)
	clone.Failures = append([]PickerFailure(nil), source.Failures...)
	return &clone
}

// loadBoardEntries runs one board refresh (local find, machine list, one
// find per enabled machine) onto a fresh state, publishing after each
// phase, like the picker's load.
func loadBoardEntries(ctx contextpkg.Context, state *BoardState, exe string, env platform.Env, platformName string, publish func()) {
	state.LoadingLocal = true
	state.Loading = 1
	publish()
	local, err := runLocalFind(ctx, exe, env, platformName)
	state.Loading = 0
	state.LoadingLocal = false
	if err != nil {
		state.Failures = append(state.Failures, PickerFailure{Label: "local", Cause: err.Error()})
		publish()
		return
	}
	state.Entries = appendUniquePicker(state.Entries, local)
	publish()
	state.Loading = 1
	publish()
	machines, err := runMachineList(ctx, env, platformName)
	state.Loading = 0
	if err != nil {
		state.Failures = append(state.Failures, PickerFailure{Label: "máquinas", Cause: err.Error()})
		publish()
		return
	}
	if len(machines) == 0 {
		publish()
		return
	}
	state.Loading = len(machines)
	publish()
	runRemoteFinds(ctx, exe, env, platformName, machines, func(machine string, entries []PickerEntry, err error) {
		state.Loading--
		if err != nil {
			state.Failures = append(state.Failures, PickerFailure{Label: machine, Cause: err.Error()})
		} else {
			state.Entries = appendUniquePicker(state.Entries, entries)
		}
		publish()
	})
}

// applyBoardUpdate replaces the board's live state with a fresh load,
// keeping the query and the selection pinned to the same ref when it
// still exists.
func applyBoardUpdate(state, update *BoardState) {
	query, exit, copied, lastEntry, escPending, csiPending := state.Query, state.Exit, state.Copied, state.LastEntry, state.EscPending, state.CSIPending
	list := state.visible()
	keepRef := ""
	if state.Selected < len(list) {
		keepRef = pickerString(list[state.Selected]["ref"])
	}
	*state = *update
	state.Query, state.Exit, state.Copied, state.LastEntry, state.EscPending, state.CSIPending = query, exit, copied, lastEntry, escPending, csiPending
	if keepRef != "" {
		for i, entry := range state.visible() {
			if pickerString(entry["ref"]) == keepRef {
				state.Selected = i
				break
			}
		}
	}
	state.clamp()
}

// requestBoardUpdate queues one refresh on the loader. The loader runs
// loads one at a time (it only starts the next after the current one
// ends), and the one-slot buffer coalesces requests made while a load is
// in progress into the single refresh that follows it.
func requestBoardUpdate(updateReq chan<- struct{}) {
	select {
	case updateReq <- struct{}{}:
	default:
	}
}

func redrawBoard(w io.Writer, state *BoardState, width int) {
	redrawArea(w, RenderBoard(state, width))
}

func RunBoard(env platform.Env, platformName, executable string) (code int) {
	stdin, stdout := os.Stdin, os.Stdout
	isTTY := false
	if info, err := stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		isTTY = true
	}
	return withPickerTerminal(isTTY, func() (func() error, error) { return setPickerRaw(stdin) }, stdout, func() int {
		return runBoardLoop(env, platformName, executable, stdin, stdout, isTTY)
	})
}

func runBoardLoop(env platform.Env, platformName, executable string, stdin, stdout *os.File, isTTY bool) int {
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, pickerShutdownSignals()...)
	defer signal.Stop(shutdownSignals)
	return runBoardLoopWithSignals(env, platformName, executable, stdin, stdout, isTTY, shutdownSignals)
}

func runBoardLoopWithSignals(env platform.Env, platformName, executable string, stdin, stdout *os.File, isTTY bool, shutdownSignals <-chan os.Signal) int {
	state := NewBoardState()
	state.LoadingLocal = true
	state.Loading = 1
	if isTTY {
		_, _ = fmt.Fprint(stdout, "\x1b[?25l")
	}
	width := 80
	if isTTY {
		if n, err := pickerTerminalWidth(stdout.Fd()); err == nil && n > 0 {
			width = n
		}
	}
	redrawBoard(stdout, state, width)
	loadCtx, cancelLoad := contextpkg.WithCancel(contextpkg.Background())
	updates := make(chan *BoardState, 16)
	updateReq := make(chan struct{}, 1)
	loadDone := make(chan struct{})
	go func() {
		defer close(updates)
		defer close(loadDone)
		for {
			loaded := NewBoardState()
			loaded.LoadingLocal = true
			loaded.Loading = 1
			publish := func() {
				select {
				case updates <- cloneBoardState(loaded):
				case <-loadCtx.Done():
					return
				}
			}
			loadBoardEntries(loadCtx, loaded, executable, env, platformName, publish)
			loaded.UpdatedAt = boardNow().Format("15:04:05")
			select {
			case updates <- cloneBoardState(loaded):
			case <-loadCtx.Done():
				return
			}
			select {
			case <-updateReq:
			case <-loadCtx.Done():
				return
			}
		}
	}()
	defer func() { cancelLoad(); <-loadDone }()
	type inputResult struct {
		value string
		err   error
	}
	input := make(chan inputResult)
	doneInput := make(chan struct{})
	defer close(doneInput)
	go func() {
		reader := bufio.NewReader(stdin)
		for {
			r, _, err := reader.ReadRune()
			select {
			case input <- inputResult{value: string(r), err: err}:
			case <-doneInput:
				return
			}
			if err != nil {
				return
			}
		}
	}()
	refresh := time.NewTicker(boardRefreshInterval)
	defer refresh.Stop()
	var escTimer <-chan time.Time
	var updateChannel <-chan *BoardState = updates
	for {
		select {
		case <-shutdownSignals:
			state.ApplyKey("esc")
		case <-refresh.C:
			requestBoardUpdate(updateReq)
		case update, ok := <-updateChannel:
			if !ok {
				updateChannel = nil
				continue
			}
			applyBoardUpdate(state, update)
			redrawBoard(stdout, state, width)
		case item := <-input:
			if item.err != nil {
				if item.err == io.EOF {
					if state.EscPending {
						state.FlushEsc()
					} else {
						state.ApplyKey("esc")
					}
					break
				}
				return 1
			}
			action := state.FeedChunk(item.value)
			if action == "copy" {
				CopyText(*state.Copied, env, platformName)
				if state.LastEntry != nil {
					bin := env.Get("HERDR_BIN_PATH")
					if bin == "" {
						bin = "herdr"
					}
					_ = pickerRun(contextpkg.Background(), bin, []string{"notification", "show", "herdr-soho", "--body", "copied " + StripPickerControls(pickerString(state.LastEntry["ref"])), "--sound", "none"}, env, platformName, 30_000, "")
				}
			}
			if action == "update" {
				requestBoardUpdate(updateReq)
				redrawBoard(stdout, state, width)
				break
			}
			if action != "" {
				break
			}
			if state.EscPending {
				escTimer = time.After(50 * time.Millisecond)
			} else {
				escTimer = nil
			}
			redrawBoard(stdout, state, width)
		case <-escTimer:
			if state.FlushEsc() != "" {
				break
			}
			redrawBoard(stdout, state, width)
		}
		if state.Exit != "" {
			break
		}
	}
	if state.Exit == "copy" || state.Exit == "esc" {
		return 0
	}
	return 1
}
