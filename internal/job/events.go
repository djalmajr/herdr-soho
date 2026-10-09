package job

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"unicode/utf8"
)

// Event is one events.jsonl line.
type Event struct {
	Seq    int               `json:"seq"`
	TS     string            `json:"ts"`
	Tipo   string            `json:"tipo"`
	Resumo string            `json:"resumo"`
	Escopo string            `json:"escopo,omitempty"`
	Motivo string            `json:"motivo,omitempty"`
	Refs   map[string]string `json:"refs"`
}

// EventIn is an event to append. Seq and ts are assigned under the job lock.
type EventIn struct {
	Tipo   string
	Resumo string
	Escopo string
	Motivo string
	Refs   map[string]string
}

const resumoLimit = 280

var eventTypes = map[string]bool{
	"accepted": true, "preparing": true, "worker_spawned": true, "worker_done": true,
	"commit": true, "push": true, "pr_opened": true, "checkpoint": true,
	"review_verdict": true, "question": true, "blocked": true, "unblocked": true,
	"amend_received": true, "decision": true, "decision_acked": true,
	"timeout_warning": true, "failure": true, "note": true, "terminal": true,
	"cleanup": true,
}

var refKeys = map[string]bool{
	"agente": true, "papel": true, "pane": true, "sha": true, "pr": true,
	"report": true, "brief": true, "motivo": true, "exit": true, "seq_ref": true,
}

func formatTS(t time.Time) string {
	_, offset := t.Zone()
	sign := byte('+')
	if offset < 0 {
		sign = '-'
		offset = -offset
	}
	return fmt.Sprintf("%s%c%02d:%02d", t.Format("2006-01-02T15:04:05"), sign, offset/3600, (offset%3600)/60)
}

func cutResumo(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", errUsage("job: resumo is not valid UTF-8")
	}
	if utf8.RuneCountInString(value) <= resumoLimit {
		return value, nil
	}
	runes := []rune(value)
	return string(runes[:resumoLimit]), nil
}

// checkTornTail refuses to append onto a final line that has lost its
// newline: a torn append, and writing past it would glue the new record to
// the torn bytes and make the log unreadable. The file is never truncated,
// rewritten or repaired; reads keep ignoring the torn tail.
func checkTornTail(dir string) error {
	path := filepath.Join(dir, "events.jsonl")
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("job: events unreadable: %w", err)
	}
	defer file.Close()
	last := make([]byte, 1)
	if _, err = file.ReadAt(last, info.Size()-1); err != nil {
		return fmt.Errorf("job: events unreadable: %w", err)
	}
	if last[0] != '\n' {
		return fmt.Errorf("job: events.jsonl ends with an incomplete line; refusing to append")
	}
	return nil
}

func appendEvent(dir string, now time.Time, in EventIn) (Event, error) {
	if !eventTypes[in.Tipo] {
		return Event{}, errUsage("job: unknown event type")
	}
	if in.Tipo == "decision" {
		if in.Escopo != "global" && in.Escopo != "projeto" {
			return Event{}, errUsage("job: decision escopo must be global or projeto")
		}
	} else if in.Escopo != "" {
		return Event{}, errUsage("job: escopo is only valid on a decision")
	}
	resumo, err := cutResumo(in.Resumo)
	if err != nil {
		return Event{}, err
	}
	refs := make(map[string]string, len(in.Refs))
	for key, value := range in.Refs {
		if !refKeys[key] {
			return Event{}, errUsage("job: unknown refs key")
		}
		refs[key] = value
	}
	if err := checkTornTail(dir); err != nil {
		return Event{}, err
	}
	existing, err := readEvents(dir)
	if err != nil {
		return Event{}, err
	}
	event := Event{
		Seq:    lastSeq(existing) + 1,
		TS:     formatTS(now),
		Tipo:   in.Tipo,
		Resumo: resumo,
		Escopo: in.Escopo,
		Motivo: in.Motivo,
		Refs:   refs,
	}
	line, err := json.Marshal(event)
	if err != nil {
		return Event{}, err
	}
	line = append(line, '\n')
	// TODO(DJA-194): verify the wake hook runs only after this sync.
	file, err := os.OpenFile(filepath.Join(dir, "events.jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return Event{}, err
	}
	defer file.Close()
	if _, err = file.Write(line); err != nil {
		return Event{}, err
	}
	if err = file.Sync(); err != nil {
		return Event{}, err
	}
	return event, nil
}

// appendEventFn is the event append behind the job lock. Tests point it at a
// failing function to simulate a crash before the event is published; the
// tests restore it with t.Cleanup.
var appendEventFn = appendEvent

func readEvents(dir string) ([]Event, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("job: events unreadable: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	// A final line without its newline is a torn append: it is not a complete
	// line and is ignored. The file is never rewritten or truncated, so the
	// torn suffix stays on disk and the next seq comes from the last complete
	// line.
	if end := bytes.LastIndexByte(raw, '\n'); end+1 < len(raw) {
		raw = raw[:end+1]
		if len(raw) == 0 {
			return nil, nil
		}
	}
	var out []Event
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("job: events unreadable: %w", err)
		}
		out = append(out, event)
	}
	return out, nil
}

func lastSeq(events []Event) int {
	n := 0
	for _, event := range events {
		if event.Seq > n {
			n = event.Seq
		}
	}
	return n
}

func hasTipo(events []Event, tipo string) bool {
	for _, event := range events {
		if event.Tipo == tipo {
			return true
		}
	}
	return false
}

var noteTypes = map[string]bool{
	"checkpoint": true, "question": true, "decision": true, "note": true,
}

// pollInterval is the file poll used by long-poll and wait. Deadlines use
// Clock.Monotonic, so this is only how often the files are re-read.
var pollInterval = 25 * time.Millisecond

// EventsPage is the events with seq greater than since, plus the trailer fields.
type EventsPage struct {
	Events    []Event
	UltimoSeq int
	Estado    string
}

// Lines renders the page as JSON lines. The last line is the trailer.
func (p EventsPage) Lines() []string {
	lines := make([]string, 0, len(p.Events)+1)
	for _, event := range p.Events {
		raw, err := json.Marshal(event)
		if err != nil {
			continue
		}
		lines = append(lines, string(raw))
	}
	raw, err := json.Marshal(struct {
		Eventos string `json:"eventos"`
		Ultimo  int    `json:"ultimo_seq"`
		Estado  string `json:"estado"`
	}{Eventos: "fim", Ultimo: p.UltimoSeq, Estado: p.Estado})
	if err == nil {
		lines = append(lines, string(raw))
	}
	return lines
}

// Note appends one orchestrator event. The tipo must be checkpoint, question, decision, or note.
func (s *Store) Note(id string, in EventIn) (Event, error) {
	if !noteTypes[in.Tipo] {
		return Event{}, errUsage("job: note tipo must be checkpoint, question, decision, or note")
	}
	return s.Append(id, in)
}

// Append writes one validated event under the job lock. A write to a job in
// a report-carrying state (terminal outcome, collected or closed) keeps the
// stored report current.
func (s *Store) Append(id string, in EventIn) (Event, error) {
	var event Event
	err := s.withExisting(id, func(dir string) error {
		var err error
		event, err = appendEventFn(dir, s.clock().Now(), in)
		if err != nil {
			return err
		}
		st, err := readState(dir)
		if err != nil {
			return err
		}
		return s.refreshIfWrote(dir, st, true)
	})
	return event, err
}

// Events returns events with seq > since. A positive wait long-polls until a
// matching event arrives or the monotonic deadline passes.
func (s *Store) Events(id string, since int, wait time.Duration) (EventsPage, error) {
	if since < 0 {
		return EventsPage{}, errUsage("job: since must be >= 0")
	}
	if wait < 0 {
		return EventsPage{}, errUsage("job: wait must be >= 0")
	}
	var page EventsPage
	err := s.poll(wait, func() (bool, error) {
		snap, err := s.snapshot(id, since)
		if err != nil {
			return false, err
		}
		page = snap
		return wait == 0 || len(snap.Events) > 0, nil
	})
	return page, err
}

// Wait blocks until the job is in a terminal state or the monotonic deadline passes.
// The deadline does not change the job. Expiry is exit 9. The read is
// lock-free: state.json is replaced atomically, so a writer holding the lock
// never delays the wait past its bound.
func (s *Store) Wait(id string, timeout time.Duration) (State, error) {
	if timeout < 0 {
		return State{}, errUsage("job: timeout must be >= 0")
	}
	var st State
	reached := false
	err := s.poll(timeout, func() (bool, error) {
		dir, err := s.readDir(id)
		if err != nil {
			return false, err
		}
		var readErr error
		st, readErr = readState(dir)
		if readErr != nil {
			return false, readErr
		}
		if waitTerminal(st.Status) {
			reached = true
			return true, nil
		}
		return false, nil
	})
	if err != nil {
		return State{}, err
	}
	if !reached {
		return st, &ExitError{Code: ExitTimeout, Msg: "job: wait timed out"}
	}
	return st, nil
}

// waitTerminal reports the states job wait returns on: the terminal
// outcomes, collected/closed, and blocked — a blocked job needs a person
// (contract exit 7), so waiting on it must not run to the bound.
func waitTerminal(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusTimeout, StatusCanceled, StatusBlocked, StatusCollected, StatusClosed:
		return true
	default:
		return false
	}
}

// snapshot reads the job without the writer lock: state.json is replaced
// atomically and events.jsonl is append-only, so a torn write is at most a
// partial final line, which readEvents ignores. The poll loop checks the
// deadline of the injected clock on every iteration and no lock is held
// across a sleep.
func (s *Store) snapshot(id string, since int) (EventsPage, error) {
	var page EventsPage
	dir, err := s.readDir(id)
	if err != nil {
		return page, err
	}
	st, err := readState(dir)
	if err != nil {
		return page, err
	}
	events, err := readEvents(dir)
	if err != nil {
		return page, err
	}
	page.Estado = st.Status
	page.UltimoSeq = lastSeq(events)
	for _, event := range events {
		if event.Seq > since {
			page.Events = append(page.Events, event)
		}
	}
	return page, nil
}

func (s *Store) poll(timeout time.Duration, ready func() (bool, error)) error {
	clock := s.clock()
	deadline := clock.Monotonic() + int64(timeout)
	for {
		ok, err := ready()
		if err != nil || ok {
			return err
		}
		now := clock.Monotonic()
		if now >= deadline {
			return nil
		}
		slice := pollInterval
		if remain := time.Duration(deadline - now); remain < slice {
			slice = remain
		}
		clock.Sleep(slice)
	}
}
