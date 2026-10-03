package plugin

import (
	"bufio"
	contextpkg "context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

const PickerFindTimeoutMs = 60_000

var pickerProcessStarted func(int)

var pickerFilterFields = []string{"ref", "name", "kind", "status", "workspace_label", "tab_label", "cwd", "machine"}

type PickerEntry map[string]any

type PickerFailure struct {
	Label string `json:"label"`
	Cause string `json:"cause"`
}

type PickerState struct {
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
}

func NewPickerState() *PickerState {
	return &PickerState{Entries: []PickerEntry{}, Failures: []PickerFailure{}}
}

func pickerString(value any) string {
	text, _ := value.(string)
	return text
}

func filterPickerEntries(entries []PickerEntry, query string, fields []string) []PickerEntry {
	terms := strings.Fields(strings.ToLower(query))
	if len(terms) == 0 {
		return entries
	}
	out := make([]PickerEntry, 0, len(entries))
	for _, entry := range entries {
		var fieldsText []string
		for _, key := range fields {
			fieldsText = append(fieldsText, strings.ToLower(pickerString(entry[key])))
		}
		joined := strings.Join(fieldsText, " ")
		match := true
		for _, term := range terms {
			if !strings.Contains(joined, term) {
				match = false
				break
			}
		}
		if match {
			out = append(out, entry)
		}
	}
	return out
}

func FilterPickerEntries(entries []PickerEntry, query string) []PickerEntry {
	return filterPickerEntries(entries, query, pickerFilterFields)
}

func (s *PickerState) visible() []PickerEntry { return FilterPickerEntries(s.Entries, s.Query) }

func (s *PickerState) clamp() {
	n := len(s.visible())
	if s.Selected > n-1 {
		s.Selected = n - 1
	}
	if s.Selected < 0 {
		s.Selected = 0
	}
}

func StripPickerControls(text string) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		b := text[i]
		if b == 0x1b {
			if i+1 < len(text) && (text[i+1] == '[' || text[i+1] == ']') {
				kind := text[i+1]
				i += 2
				if kind == '[' {
					for i < len(text) && !(text[i] >= 0x40 && text[i] <= 0x7e) {
						i++
					}
					if i < len(text) {
						i++
					}
				} else {
					for i < len(text) && text[i] != 0x07 && !(text[i] == 0x1b && i+1 < len(text) && text[i+1] == '\\') {
						i++
					}
					if i < len(text) {
						if text[i] == 0x07 {
							i++
						} else {
							i += 2
						}
					}
				}
				continue
			}
			i++
			continue
		}
		if b < 0x20 {
			if b == '\t' || b == '\n' {
				out.WriteByte(' ')
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(text[i:])
		if size == 0 {
			break
		}
		if r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			i += size
			continue
		}
		out.WriteString(text[i : i+size])
		i += size
	}
	return out.String()
}

func PickerCopyPayload(entry PickerEntry) string {
	display := func(v any) string {
		value := StripPickerControls(pickerString(v))
		if value == "" {
			return "-"
		}
		return value
	}
	return fmt.Sprintf("%s (%s, %s, %s) %s", display(entry["ref"]), display(entry["name"]), display(entry["kind"]), display(entry["status"]), display(entry["cwd"]))
}

func jsLength(text string) int { return len(utf16.Encode([]rune(text))) }
func jsSlice(text string, length int) string {
	units := utf16.Encode([]rune(text))
	if length < 0 {
		length = len(units) + length
	}
	if length < 0 {
		length = 0
	}
	if length > len(units) {
		length = len(units)
	}
	return string(utf16.Decode(units[:length]))
}

func PickerEntryLine(entry PickerEntry, width int) string {
	display := func(v any) string {
		s := StripPickerControls(pickerString(v))
		if s == "" {
			return "-"
		}
		return s
	}
	cols := []string{display(entry["ref"]), display(entry["name"]), display(entry["kind"]), display(entry["status"]), display(entry["workspace_label"]) + "/" + display(entry["tab_label"]), display(entry["cwd"])}
	lead := strings.Join(cols[:len(cols)-1], "  ")
	budget := width - jsLength(lead) - 2
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
	line := lead + "  " + last
	if jsLength(line) > width {
		line = jsSlice(line, width)
	}
	return line
}

func RenderPicker(state *PickerState, width int) string {
	list := state.visible()
	lines := []string{"> " + state.Query}
	for i, entry := range list {
		marker := " "
		if i == state.Selected {
			marker = "*"
		}
		rowWidth := width - 2
		if rowWidth < 2 {
			rowWidth = 2
		}
		lines = append(lines, marker+" "+PickerEntryLine(entry, rowWidth))
	}
	if len(list) == 0 && len(state.Entries) > 0 {
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
	if len(state.Entries) == 0 && state.Loading == 0 && len(state.Failures) == 0 {
		lines = append(lines, "nenhum pane")
	}
	count := "panes"
	if len(list) == 1 {
		count = "pane"
	}
	lines = append(lines, fmt.Sprintf("%d %s", len(list), count))
	return strings.Join(lines, "\n") + "\n"
}

func (s *PickerState) ApplyKey(key string) string {
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
	default:
		if jsLength(key) == 1 {
			r, _ := utf8.DecodeRuneInString(key)
			if r >= 0x20 && r != 0x7f {
				s.Query += key
				s.clamp()
			}
		}
		return ""
	}
}

func (s *PickerState) FeedChunk(chunk string) string {
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
		default:
			if r >= 0x20 && r != 0x7f {
				s.ApplyKey(ch)
			}
		}
	}
	return s.Exit
}

func (s *PickerState) FlushEsc() string {
	if !s.EscPending {
		return ""
	}
	s.EscPending = false
	s.ApplyKey("esc")
	return s.Exit
}

func parsePickerFind(text, label string) ([]PickerEntry, error) {
	var entries []PickerEntry
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil || entry == nil {
			return nil, fmt.Errorf("%s: invalid JSON line", label)
		}
		for _, key := range []string{"ref", "machine", "workspace_id", "tab_id", "pane_id"} {
			if pickerString(entry[key]) == "" {
				return nil, fmt.Errorf("%s: line without '%s'", label, key)
			}
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func parsePickerMachines(text string) ([]string, error) {
	var value any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &value); err != nil {
		return nil, fmt.Errorf("machine list: invalid JSON")
	}
	var list []any
	switch v := value.(type) {
	case []any:
		list = v
	case map[string]any:
		if items, ok := v["machines"].([]any); ok {
			list = items
		}
	}
	if list == nil {
		return nil, fmt.Errorf("machine list: no machine array")
	}
	labels := make([]string, 0)
	for _, item := range list {
		machine, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("machine list: invalid item")
		}
		label := pickerString(machine["label"])
		if label == "" {
			label = pickerString(machine["id"])
		}
		if label == "" {
			return nil, fmt.Errorf("machine list: item without label")
		}
		if machine["enabled"] == true {
			labels = append(labels, label)
		}
	}
	return labels, nil
}

func pickerRun(parent contextpkg.Context, exe string, args []string, env platform.Env, platformName string, timeout int, input string) platform.RunResult {
	opts := platform.RunOptions{
		Context:   parent,
		Env:       env,
		OnStart:   pickerProcessStarted,
		Platform:  platformName,
		Input:     input,
		TimeoutMs: timeout,
	}
	if filepath.IsAbs(exe) {
		return platform.RunExecutable(exe, args, opts)
	}
	return platform.RunCli(exe, args, opts)
}

func pickerFailure(result platform.RunResult, timeout int, find bool) error {
	if result.NotFound {
		return fmt.Errorf("%s not found", map[bool]string{true: "find", false: "herdr"}[find])
	}
	if result.TimedOut {
		if find {
			return fmt.Errorf("find timed out after %ds", timeout/1000)
		}
		return fmt.Errorf("timed out after %ds", timeout/1000)
	}
	if result.Error != "" {
		return fmt.Errorf("spawn failed: %s", result.Error)
	}
	if result.Status == nil {
		return fmt.Errorf("killed")
	}
	if find {
		if result.Stderr != "" {
			return fmt.Errorf("exit %d: %s", *result.Status, truncateUTF16(strings.TrimSpace(result.Stderr), 120))
		}
		return fmt.Errorf("exit %d", *result.Status)
	}
	if result.Stderr != "" {
		return fmt.Errorf("exit %d: %s", *result.Status, truncateUTF16(strings.TrimSpace(result.Stderr), 120))
	}
	return fmt.Errorf("exit %d", *result.Status)
}

func truncateUTF16(text string, length int) string {
	if jsLength(text) > length {
		return jsSlice(text, length)
	}
	return text
}

func clonePickerState(source *PickerState) *PickerState {
	clone := *source
	clone.Entries = append([]PickerEntry(nil), source.Entries...)
	clone.Failures = append([]PickerFailure(nil), source.Failures...)
	return &clone
}

func loadPickerEntries(ctx contextpkg.Context, state *PickerState, exe string, env platform.Env, platformName string, publish func()) {
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

// runLocalFind runs the local `find --json` and parses its entries.
func runLocalFind(ctx contextpkg.Context, exe string, env platform.Env, platformName string) ([]PickerEntry, error) {
	result := pickerRun(ctx, exe, []string{"find", "--json"}, env, platformName, PickerFindTimeoutMs, "")
	if result.Status == nil || *result.Status != 0 {
		return nil, pickerFailure(result, PickerFindTimeoutMs, true)
	}
	return parsePickerFind(result.Stdout, "local")
}

// runMachineList runs `herdr machine list --json` and returns the enabled
// machine labels, in Herdr's order.
func runMachineList(ctx contextpkg.Context, env platform.Env, platformName string) ([]string, error) {
	herdr := env.Get("HERDR_BIN_PATH")
	if herdr == "" {
		herdr = "herdr"
	}
	result := pickerRun(ctx, herdr, []string{"machine", "list", "--json"}, env, platformName, 30_000, "")
	if result.Status == nil || *result.Status != 0 {
		return nil, pickerFailure(result, 30_000, false)
	}
	return parsePickerMachines(result.Stdout)
}

// runRemoteFinds runs one `find --json --machine <label>` per machine in
// parallel and invokes onRemote as each result completes (entries are kept
// only when they belong to that machine).
func runRemoteFinds(ctx contextpkg.Context, exe string, env platform.Env, platformName string, machines []string, onRemote func(machine string, entries []PickerEntry, err error)) {
	type remoteResult struct {
		machine string
		result  platform.RunResult
		entries []PickerEntry
		err     error
	}
	results := make(chan remoteResult, len(machines))
	var searches sync.WaitGroup
	for _, machine := range machines {
		machine := machine
		searches.Add(1)
		go func() {
			defer searches.Done()
			result := pickerRun(ctx, exe, []string{"find", "--json", "--machine", machine}, env, platformName, PickerFindTimeoutMs, "")
			remote := remoteResult{machine: machine, result: result}
			if result.Status != nil && *result.Status == 0 {
				entries, parseErr := parsePickerFind(result.Stdout, machine)
				if parseErr != nil {
					remote.err = parseErr
				} else {
					for _, entry := range entries {
						if pickerString(entry["machine"]) == machine {
							remote.entries = append(remote.entries, entry)
						}
					}
				}
			}
			results <- remote
		}()
	}
	go func() {
		searches.Wait()
		close(results)
	}()
	for remote := range results {
		if remote.result.Status == nil || *remote.result.Status != 0 {
			onRemote(remote.machine, nil, pickerFailure(remote.result, PickerFindTimeoutMs, true))
			continue
		}
		onRemote(remote.machine, remote.entries, remote.err)
	}
}

func appendUniquePicker(dest, next []PickerEntry) []PickerEntry {
	refs := make(map[string]bool, len(dest))
	for _, entry := range dest {
		refs[pickerString(entry["ref"])] = true
	}
	for _, entry := range next {
		ref := pickerString(entry["ref"])
		if !refs[ref] {
			refs[ref] = true
			dest = append(dest, entry)
		}
	}
	return dest
}

func redrawArea(w io.Writer, rendered string) {
	_, _ = fmt.Fprint(w, "\x1b[H")
	for _, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		_, _ = fmt.Fprint(w, line, "\x1b[K\n")
	}
	_, _ = fmt.Fprint(w, "\x1b[J")
}

func redrawPicker(w io.Writer, state *PickerState, width int) {
	redrawArea(w, RenderPicker(state, width))
}

func RunPicker(env platform.Env, platformName, executable string) (code int) {
	stdin, stdout := os.Stdin, os.Stdout
	isTTY := false
	if info, err := stdin.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 {
		isTTY = true
	}
	return withPickerTerminal(isTTY, func() (func() error, error) { return setPickerRaw(stdin) }, stdout, func() int {
		return runPickerLoop(env, platformName, executable, stdin, stdout, isTTY)
	})
}

func withPickerTerminal(terminal bool, setRaw func() (func() error, error), stdout io.Writer, run func() int) (code int) {
	var restore func() error
	if terminal {
		var err error
		restore, err = setRaw()
		if err != nil {
			return 1
		}
	}
	defer func() {
		if restore != nil {
			_ = restore()
		}
		if terminal {
			_, _ = fmt.Fprint(stdout, "\x1b[?25h")
		}
		if value := recover(); value != nil {
			panic(value)
		}
	}()
	return run()
}

func runPickerLoop(env platform.Env, platformName, executable string, stdin, stdout *os.File, isTTY bool) int {
	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, pickerShutdownSignals()...)
	defer signal.Stop(shutdownSignals)
	return runPickerLoopWithSignals(env, platformName, executable, stdin, stdout, isTTY, shutdownSignals)
}

func runPickerLoopWithSignals(env platform.Env, platformName, executable string, stdin, stdout *os.File, isTTY bool, shutdownSignals <-chan os.Signal) int {
	state := NewPickerState()
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
	redrawPicker(stdout, state, width)
	loadCtx, cancelLoad := contextpkg.WithCancel(contextpkg.Background())
	updates := make(chan *PickerState, 16)
	loadDone := make(chan struct{})
	go func() {
		defer close(updates)
		defer close(loadDone)
		loaded := NewPickerState()
		publish := func() {
			select {
			case updates <- clonePickerState(loaded):
			case <-loadCtx.Done():
			}
		}
		loadPickerEntries(loadCtx, loaded, executable, env, platformName, publish)
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
	var escTimer <-chan time.Time
	var updateChannel <-chan *PickerState = updates
	for {
		select {
		case <-shutdownSignals:
			state.ApplyKey("esc")
		case update, ok := <-updateChannel:
			if !ok {
				updateChannel = nil
				continue
			}
			query, selected, exit, copied, lastEntry, escPending, csiPending := state.Query, state.Selected, state.Exit, state.Copied, state.LastEntry, state.EscPending, state.CSIPending
			*state = *update
			state.Query, state.Selected, state.Exit, state.Copied, state.LastEntry, state.EscPending, state.CSIPending = query, selected, exit, copied, lastEntry, escPending, csiPending
			state.clamp()
			redrawPicker(stdout, state, width)
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
			if action != "" {
				break
			}
			if state.EscPending {
				escTimer = time.After(50 * time.Millisecond)
			} else {
				escTimer = nil
			}
			redrawPicker(stdout, state, width)
		case <-escTimer:
			if state.FlushEsc() != "" {
				break
			}
			redrawPicker(stdout, state, width)
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
