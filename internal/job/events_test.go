package job

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

var tsPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[+-][0-9]{2}:[0-9]{2}$`)

func TestEvents(t *testing.T) {
	t.Run("consecutive seq under concurrent writers", func(t *testing.T) {
		s, _ := startedJob(t)
		const n = 20
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := s.Note("job-1", EventIn{Tipo: "note", Resumo: "n"})
				if err != nil {
					errs <- err
				}
			}()
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatal(err)
		}
		page := mustPage(t, s, 0, 0)
		if len(page.Events) != n+1 {
			t.Fatalf("events = %d", len(page.Events))
		}
		for i, event := range page.Events {
			if event.Seq != i+1 {
				t.Fatalf("seq[%d] = %d", i, event.Seq)
			}
			if !tsPattern.MatchString(event.TS) || strings.Contains(event.TS, "Z") {
				t.Fatalf("ts %q", event.TS)
			}
		}
	})

	t.Run("since filters and the trailer is last", func(t *testing.T) {
		s, _ := startedJob(t)
		for _, text := range []string{"one", "two"} {
			if _, err := s.Note("job-1", EventIn{Tipo: "note", Resumo: text}); err != nil {
				t.Fatal(err)
			}
		}
		page := mustPage(t, s, 1, 0)
		if len(page.Events) != 2 || page.Events[0].Seq != 2 || page.Events[1].Seq != 3 || page.UltimoSeq != 3 || page.Estado != StatusAccepted {
			t.Fatalf("page = %+v", page)
		}
		lines := page.Lines()
		if len(lines) != 3 || !strings.Contains(lines[2], `"eventos":"fim"`) {
			t.Fatalf("lines = %#v", lines)
		}
		beyond := mustPage(t, s, 3, 0)
		if len(beyond.Events) != 0 || beyond.UltimoSeq != 3 {
			t.Fatalf("beyond = %+v", beyond)
		}
		only := beyond.Lines()
		if len(only) != 1 {
			t.Fatalf("trailer lines = %#v", only)
		}
		var tail struct {
			Eventos string `json:"eventos"`
			Ultimo  int    `json:"ultimo_seq"`
			Estado  string `json:"estado"`
		}
		if err := json.Unmarshal([]byte(only[0]), &tail); err != nil {
			t.Fatal(err)
		}
		if tail.Eventos != "fim" || tail.Ultimo != 3 || tail.Estado != StatusAccepted {
			t.Fatalf("trailer = %+v", tail)
		}
		further := mustPage(t, s, 99, 0)
		if len(further.Lines()) != 1 || further.UltimoSeq != 3 {
			t.Fatalf("further = %+v", further)
		}
	})

	t.Run("long-poll returns on a new event and on timeout", func(t *testing.T) {
		s, root := startedJob(t)
		_ = root
		last := mustPage(t, s, 0, 0).UltimoSeq
		started := make(chan struct{})
		release := make(chan struct{})
		s.Clock = &gateClock{wall: time.Now(), started: started, release: release}
		done := make(chan EventsPage, 1)
		go func() {
			page, err := s.Events("job-1", last, time.Second)
			if err != nil {
				t.Errorf("poll: %v", err)
				return
			}
			done <- page
		}()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("long-poll did not wait")
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "checkpoint", Resumo: "mile"}); err != nil {
			t.Fatal(err)
		}
		close(release)
		select {
		case page := <-done:
			if len(page.Events) != 1 || page.Events[0].Tipo != "checkpoint" {
				t.Fatalf("poll page = %+v", page)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("long-poll hung after the event")
		}

		s.Clock = &backClock{wall: time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)}
		last = mustPage(t, s, 0, 0).UltimoSeq
		page, err := s.Events("job-1", last, 100*time.Millisecond)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Events) != 0 || len(page.Lines()) != 1 {
			t.Fatalf("timeout page = %+v", page)
		}
		if got := s.Clock.(*backClock).mono; got != int64(100*time.Millisecond) {
			t.Fatalf("monotonic elapsed %d", got)
		}
	})

	t.Run("wait uses the monotonic clock", func(t *testing.T) {
		s, root := startedJob(t)
		clock := &backClock{wall: time.Now()}
		s.Clock = clock
		_, err := s.Wait("job-1", 100*time.Millisecond)
		if code := exitCode(t, err); code != ExitTimeout {
			t.Fatalf("code %d err %v", code, err)
		}
		if clock.mono != int64(100*time.Millisecond) {
			t.Fatalf("wait elapsed %d", clock.mono)
		}
		if err := writeStatus(root, "job-1", StatusDone); err != nil {
			t.Fatal(err)
		}
		doneClock := &backClock{wall: time.Now()}
		s.Clock = doneClock
		st, err := s.Wait("job-1", time.Second)
		if err != nil || st.Status != StatusDone || doneClock.mono != 0 {
			t.Fatalf("terminal wait %+v elapsed %d err %v", st, doneClock.mono, err)
		}
	})

	t.Run("wait returns on blocked", func(t *testing.T) {
		s, root := startedJob(t)
		if err := writeStatus(root, "job-1", StatusBlocked); err != nil {
			t.Fatal(err)
		}
		clock := &backClock{wall: time.Now()}
		s.Clock = clock
		st, err := s.Wait("job-1", time.Second)
		if err != nil || st.Status != StatusBlocked || clock.mono != 0 {
			t.Fatalf("blocked wait %+v elapsed %d err %v", st, clock.mono, err)
		}
	})

	t.Run("timestamps use a numeric offset", func(t *testing.T) {
		s, _ := startedJob(t)
		s.Clock = fixedClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
		event, err := s.Note("job-1", EventIn{Tipo: "note", Resumo: "utc"})
		if err != nil || event.TS != "2026-01-01T12:00:00+00:00" {
			t.Fatalf("utc ts %q err %v", event.TS, err)
		}
		s.Clock = fixedClock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.FixedZone("local", -3*3600))}
		event, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: "local"})
		if err != nil || event.TS != "2026-01-01T12:00:00-03:00" {
			t.Fatalf("offset ts %q err %v", event.TS, err)
		}
	})

	t.Run("unknown refs are refused and oversize resumo is cut", func(t *testing.T) {
		s, root := startedJob(t)
		path := filepath.Join(root, "jobs", "job-1", "events.jsonl")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: "x", Refs: map[string]string{"nope": "1"}})
		if code := exitCode(t, err); code != ExitUsage {
			t.Fatalf("code %d err %v", code, err)
		}
		after, err := os.ReadFile(path)
		if err != nil || string(after) != string(before) {
			t.Fatal("refs refusal changed the log")
		}
		event, err := s.Note("job-1", EventIn{Tipo: "note", Resumo: "ok", Refs: map[string]string{"sha": "abc"}})
		if err != nil || event.Refs["sha"] != "abc" || event.Seq != 2 {
			t.Fatalf("known ref = %+v err %v", event, err)
		}
		long := strings.Repeat("é", 281)
		event, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: long})
		if err != nil || event.Resumo != strings.Repeat("é", 280) || utf8.RuneCountInString(event.Resumo) != 280 {
			t.Fatalf("cut resumo runes=%d err=%v", utf8.RuneCountInString(event.Resumo), err)
		}
		exact := strings.Repeat("a", 280)
		event, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: exact})
		if err != nil || event.Resumo != exact {
			t.Fatalf("exact resumo changed: %v", err)
		}
	})

	t.Run("note accepts only checkpoint question decision and note", func(t *testing.T) {
		s, _ := startedJob(t)
		for _, tipo := range []string{"checkpoint", "question", "note"} {
			if _, err := s.Note("job-1", EventIn{Tipo: tipo, Resumo: tipo}); err != nil {
				t.Fatalf("%s: %v", tipo, err)
			}
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "keep it", Escopo: "global", Motivo: "because"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "missing scope"}); exitCode(t, err) != ExitUsage {
			t.Fatalf("missing escopo: %v", err)
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "failure", Resumo: "no"}); exitCode(t, err) != ExitUsage {
			t.Fatalf("failure note: %v", err)
		}
	})

	t.Run("restart continues seq without a gap", func(t *testing.T) {
		s, root := startedJob(t)
		if _, err := s.Note("job-1", EventIn{Tipo: "note", Resumo: "first"}); err != nil {
			t.Fatal(err)
		}
		reopened := Open(root)
		page := mustPage(t, reopened, 0, 0)
		if len(page.Events) != 2 || page.Events[1].Seq != 2 {
			t.Fatalf("reopened = %+v", page.Events)
		}
		event, err := reopened.Note("job-1", EventIn{Tipo: "note", Resumo: "second"})
		if err != nil || event.Seq != 3 {
			t.Fatalf("next = %+v err %v", event, err)
		}
	})
}

func TestEventsRefusesAppendOnTornTail(t *testing.T) {
	s, root := startedJob(t)
	path := filepath.Join(root, "jobs", "job-1", "events.jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write([]byte(`{"seq":99,"tipo":"note","resumo":"torn`)); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	torn, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Reads keep ignoring the torn tail (fix A).
	page := mustPage(t, s, 0, 0)
	if len(page.Events) != 1 || page.Events[0].Seq != 1 || page.UltimoSeq != 1 {
		t.Fatalf("read before the refused append: %+v", page)
	}
	_, err = s.Note("job-1", EventIn{Tipo: "note", Resumo: "after tear"})
	if err == nil {
		t.Fatal("note onto the torn tail succeeded")
	}
	if err.Error() != "job: events.jsonl ends with an incomplete line; refusing to append" {
		t.Fatalf("error = %v, want the torn-tail refusal", err)
	}
	if _, ok := err.(*ExitError); ok {
		t.Fatalf("torn-tail refusal carries an exit code: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, torn) {
		t.Fatalf("events.jsonl changed by the refusal:\ntorn  %s\nafter %s", torn, after)
	}
	page = mustPage(t, s, 0, 0)
	if len(page.Events) != 1 || page.Events[0].Seq != 1 || page.UltimoSeq != 1 {
		t.Fatalf("events after the refused append: %+v", page)
	}
}

func startedJob(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	return s, root
}

func mustPage(t *testing.T, s *Store, since int, wait time.Duration) EventsPage {
	t.Helper()
	page, err := s.Events("job-1", since, wait)
	if err != nil {
		t.Fatal(err)
	}
	return page
}

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time      { return f.t }
func (f fixedClock) Monotonic() int64    { return 0 }
func (f fixedClock) Sleep(time.Duration) {}

type backClock struct {
	wall time.Time
	mono int64
}

func (b *backClock) Now() time.Time {
	b.wall = b.wall.Add(-time.Hour)
	return b.wall
}

func (b *backClock) Monotonic() int64 { return b.mono }

func (b *backClock) Sleep(d time.Duration) {
	if d > time.Second {
		panic("sleep exceeded the monotonic bound")
	}
	b.mono += int64(d)
}

type gateClock struct {
	wall    time.Time
	mono    int64
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (g *gateClock) Now() time.Time   { return g.wall }
func (g *gateClock) Monotonic() int64 { return g.mono }

func (g *gateClock) Sleep(d time.Duration) {
	g.once.Do(func() { close(g.started) })
	<-g.release
	g.mono += int64(d)
}
