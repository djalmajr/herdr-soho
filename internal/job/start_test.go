package job

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/dispatch"
)

const briefA = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`

const briefB = `{
  "aceite": [{"prova": "go test ./internal/job", "criterio": "tests pass"}],
  "objetivo": "Ship the change.",
  "repo": "example-org/example-repo",
  "origem": {"ref": "CARD-1", "tipo": "card"},
  "id": "job-1",
  "schema": 1
}`

const briefChanged = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship another change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`

const briefEscaped = `{"schema":1,"id":"job-2","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the \u003cchange.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`

const briefRawAngle = `{"aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}],"id":"job-2","objetivo":"Ship the <change.","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","schema":1}`

func TestStartIdempotency(t *testing.T) {
	t.Run("reordered keys are the same brief", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		first, err := s.Start("job-1", []byte(briefA))
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		if first.DuplicateOf != "" || first.Status != StatusAccepted || first.ID != "job-1" {
			t.Fatalf("first start: %+v", first)
		}
		second, err := s.Start("job-1", []byte(briefB))
		if err != nil {
			t.Fatalf("duplicate start: %v", err)
		}
		if second.DuplicateOf != "job-1" || second.BriefSHA256 != first.BriefSHA256 || second.Status != StatusAccepted {
			t.Fatalf("duplicate start: %+v", second)
		}
		stored, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(stored, []byte("duplicate_of")) {
			t.Fatalf("duplicate_of was persisted: %s", stored)
		}
		dir, err := Dir(root, "job-1")
		if err != nil || dir != filepath.Join(root, "jobs", "job-1") {
			t.Fatalf("Dir = %q err=%v", dir, err)
		}
		raw, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != first.BriefSHA256 {
			t.Fatalf("brief.json hash %x, state %s", sum, first.BriefSHA256)
		}
		if !bytes.Contains(raw, []byte(`"aceite"`)) || bytes.Contains(raw, []byte("\n")) {
			t.Fatalf("brief.json is not compact canonical JSON: %s", raw)
		}
		if !bytes.Contains(raw, []byte(`{"ref":"CARD-1","tipo":"card"}`)) {
			t.Fatalf("nested keys were not sorted: %s", raw)
		}
		events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(events, []byte("\n")) != 1 || !bytes.Contains(events, []byte(`"tipo":"accepted"`)) {
			t.Fatalf("accepted events = %s", events)
		}
		md, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil {
			t.Fatal(err)
		}
		if missing := dispatch.BriefMissingSections(string(md), false, nil); missing != "" {
			t.Fatalf("brief.md lint gaps:%s", missing)
		}
		third, err := s.Start("job-1", []byte(briefA))
		if err != nil || third.DuplicateOf != "job-1" {
			t.Fatalf("retry start: %+v err=%v", third, err)
		}
		events, err = os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Count(events, []byte("\n")) != 1 {
			t.Fatalf("retry appended an event: %s", events)
		}
	})

	t.Run("escaped and raw JSON share one hash", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		first, err := s.Start("job-2", []byte(briefEscaped))
		if err != nil {
			t.Fatalf("start: %v", err)
		}
		second, err := s.Start("job-2", []byte(briefRawAngle))
		if err != nil || second.DuplicateOf != "job-2" || second.BriefSHA256 != first.BriefSHA256 {
			t.Fatalf("duplicate: %+v err=%v", second, err)
		}
		raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-2", "brief.json"))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(`\u003c`)) || !bytes.Contains(raw, []byte(`Ship the <change.`)) {
			t.Fatalf("canonical brief = %s", raw)
		}
	})

	t.Run("changed field reuses the id", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefA)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "jobs", "job-1", "state.json")
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Start("job-1", []byte(briefChanged))
		if code := exitCode(t, err); code != ExitBriefConflict {
			t.Fatalf("code %d, err %v", code, err)
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("state changed on hash mismatch\nbefore %s\nafter %s", before, after)
		}
		entries, err := os.ReadDir(filepath.Join(root, "jobs"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("jobs = %v err=%v", entries, err)
		}
	})

	t.Run("concurrent starts create one job", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		const n = 8
		var wg sync.WaitGroup
		var mu sync.Mutex
		created, dups := 0, 0
		errs := make([]error, 0)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				st, err := s.Start("job-1", []byte(briefA))
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					errs = append(errs, err)
					return
				}
				if st.DuplicateOf == "" {
					created++
				} else if st.DuplicateOf == "job-1" {
					dups++
				}
			}()
		}
		wg.Wait()
		if len(errs) != 0 || created != 1 || dups != n-1 {
			t.Fatalf("created=%d dups=%d errs=%v", created, dups, errs)
		}
		entries, err := os.ReadDir(filepath.Join(root, "jobs"))
		if err != nil || len(entries) != 1 || entries[0].Name() != "job-1" {
			t.Fatalf("jobs = %v err=%v", entries, err)
		}
		events, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "events.jsonl"))
		if err != nil || bytes.Count(events, []byte("\n")) != 1 {
			t.Fatalf("events = %s err=%v", events, err)
		}
	})

	t.Run("concurrent different briefs still create one job", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		var wg sync.WaitGroup
		var mu sync.Mutex
		ok, conflicts := 0, 0
		for i := 0; i < 8; i++ {
			body := briefA
			if i%2 == 1 {
				body = briefChanged
			}
			wg.Add(1)
			go func(raw string) {
				defer wg.Done()
				_, err := s.Start("job-1", []byte(raw))
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					ok++
					return
				}
				var exit *ExitError
				if errors.As(err, &exit) && exit.Code == ExitBriefConflict {
					conflicts++
				}
			}(body)
		}
		wg.Wait()
		if ok+conflicts != 8 || ok < 1 || conflicts < 1 {
			t.Fatalf("ok=%d conflicts=%d", ok, conflicts)
		}
		entries, err := os.ReadDir(filepath.Join(root, "jobs"))
		if err != nil || len(entries) != 1 {
			t.Fatalf("jobs = %v err=%v", entries, err)
		}
	})

	t.Run("invalid brief writes nothing", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		cases := []struct {
			id   string
			body string
		}{
			{id: "job-1", body: `{}`},
			{id: "job-1", body: `{"schema":1,"id":"other","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[]}`},
			{id: "bad id", body: briefA},
			{id: "..", body: `{"schema":1,"id":"..","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: `{"schema":2,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: `{"schema":1,"id":"job-1","origem":{"tipo":"email","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-repo","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","base":"a/../b","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","modo":"dirty","objetivo":"Ship the change.","aceite":[]}`},
			{id: "job-1", body: string(bytes.Repeat([]byte("x"), MaxBriefBytes+1))},
		}
		for _, tc := range cases {
			_, err := s.Start(tc.id, []byte(tc.body))
			if code := exitCode(t, err); code != ExitUsage {
				t.Fatalf("id %q code %d err %v", tc.id, code, err)
			}
		}
		if _, err := os.Stat(filepath.Join(root, "jobs")); !os.IsNotExist(err) {
			t.Fatalf("invalid start created jobs: %v", err)
		}
		const withBase = `{"schema":1,"id":"job-3","origem":{"tipo":"cron","ref":"nightly"},"repo":"example-org/example-repo","base":"release/1","modo":"workspace","objetivo":"Read only.","aceite":[]}`
		if _, err := s.Start("job-3", []byte(withBase)); err != nil {
			t.Fatal(err)
		}
	})
}

func TestStartIdempotencyInterrupted(t *testing.T) {
	t.Run("resume after brief.json publication with no events", func(t *testing.T) {
		s, root := startedJob(t)
		dir := filepath.Join(root, "jobs", "job-1")
		if err := os.Remove(filepath.Join(dir, "state.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(filepath.Join(dir, "events.jsonl")); err != nil {
			t.Fatal(err)
		}
		briefJSON, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		if err != nil {
			t.Fatal(err)
		}
		briefMD, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil {
			t.Fatal(err)
		}
		st, err := s.Start("job-1", []byte(briefA))
		if err != nil || st.Status != StatusAccepted {
			t.Fatalf("resume: %+v err=%v", st, err)
		}
		afterJSON, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		if err != nil || !bytes.Equal(afterJSON, briefJSON) {
			t.Fatalf("brief.json was rewritten:\nbefore %s\nafter %s", briefJSON, afterJSON)
		}
		afterMD, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil || !bytes.Equal(afterMD, briefMD) {
			t.Fatalf("brief.md changed:\nbefore %s\nafter %s", briefMD, afterMD)
		}
		events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil || bytes.Count(events, []byte("\n")) != 1 || !bytes.Contains(events, []byte(`"tipo":"accepted"`)) {
			t.Fatalf("accepted events = %s err=%v", events, err)
		}
		saved, err := readState(dir)
		if err != nil || saved.BriefSHA256 != st.BriefSHA256 || saved.Status != StatusAccepted {
			t.Fatalf("state: %+v err=%v", saved, err)
		}
	})

	t.Run("resume after an accepted event keeps one accepted", func(t *testing.T) {
		s, root := startedJob(t)
		dir := filepath.Join(root, "jobs", "job-1")
		if err := os.Remove(filepath.Join(dir, "state.json")); err != nil {
			t.Fatal(err)
		}
		st, err := s.Start("job-1", []byte(briefA))
		if err != nil || st.Status != StatusAccepted {
			t.Fatalf("resume: %+v err=%v", st, err)
		}
		events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil || bytes.Count(events, []byte("\n")) != 1 {
			t.Fatalf("accepted events = %s err=%v, want exactly one", events, err)
		}
		saved, err := readState(dir)
		if err != nil || saved.BriefSHA256 != st.BriefSHA256 {
			t.Fatalf("state: %+v err=%v", saved, err)
		}
	})

	t.Run("different brief is refused and changes nothing", func(t *testing.T) {
		s, root := startedJob(t)
		dir := filepath.Join(root, "jobs", "job-1")
		if err := os.Remove(filepath.Join(dir, "state.json")); err != nil {
			t.Fatal(err)
		}
		briefJSON, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		if err != nil {
			t.Fatal(err)
		}
		briefMD, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil {
			t.Fatal(err)
		}
		events, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Start("job-1", []byte(briefChanged))
		if code := exitCode(t, err); code != ExitBriefConflict {
			t.Fatalf("code %d err %v", code, err)
		}
		if _, err := os.Stat(filepath.Join(dir, "state.json")); !os.IsNotExist(err) {
			t.Fatalf("state was written: %v", err)
		}
		afterJSON, err := os.ReadFile(filepath.Join(dir, "brief.json"))
		if err != nil {
			t.Fatal(err)
		}
		afterMD, err := os.ReadFile(filepath.Join(dir, "brief.md"))
		if err != nil {
			t.Fatal(err)
		}
		afterEvents, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(afterJSON, briefJSON) {
			t.Fatalf("brief.json changed:\nbefore %s\nafter %s", briefJSON, afterJSON)
		}
		if !bytes.Equal(afterMD, briefMD) {
			t.Fatalf("brief.md changed:\nbefore %s\nafter %s", briefMD, afterMD)
		}
		if !bytes.Equal(afterEvents, events) {
			t.Fatalf("events.jsonl changed:\nbefore %s\nafter %s", events, afterEvents)
		}
	})
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	var exit *ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("error %v, want ExitError", err)
	}
	return exit.Code
}
