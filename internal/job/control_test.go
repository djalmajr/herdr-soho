package job

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// controlDir is the control directory of the startedJob test job.
func controlDir(root string) string {
	return filepath.Join(root, "jobs", "job-1", "control")
}

// setControlStatus writes the job's state.json directly (no lifecycle
// validation) so the control tests can pin any status.
func setControlStatus(t *testing.T, root, id, status string) {
	t.Helper()
	body := fmt.Sprintf(`{"schema":1,"id":%q,"status":%q,"brief_sha256":"x","decisions_acked_seq":0,"motivo":null}`, id, status)
	if err := os.WriteFile(filepath.Join(root, "jobs", id, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// controlEntries lists the file names under the control directory, including
// the done/ children as "done/<name>".
func controlEntries(t *testing.T, ctl string) []string {
	t.Helper()
	entries, err := os.ReadDir(ctl)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			sub, err := os.ReadDir(filepath.Join(ctl, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range sub {
				names = append(names, "done/"+item.Name())
			}
			continue
		}
		names = append(names, entry.Name())
	}
	return names
}

// TestControlAmendSendSequenceAndFiles: amend and send draw consecutive Seq
// values across kinds, publish the body verbatim, keep the modes, and
// PendingControl lists them in Seq order.
func TestControlAmendSendSequenceAndFiles(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	a, err := s.RequestAmend("job-1", []byte("amend one"))
	if err != nil {
		t.Fatal(err)
	}
	if a.Kind != "amend" || a.Name != "amend-000001.md" || a.Seq != 1 || a.Grace != 0 || string(a.Body) != "amend one" {
		t.Fatalf("amend item = %+v", a)
	}
	if a.Path != filepath.Join(ctl, "amend-000001.md") {
		t.Fatalf("amend path = %q", a.Path)
	}
	if raw, err := os.ReadFile(a.Path); err != nil || string(raw) != "amend one" {
		t.Fatalf("amend file = %q err=%v", raw, err)
	}

	sd, err := s.RequestSend("job-1", []byte("send two\n"))
	if err != nil || sd.Seq != 2 || sd.Name != "send-000002.md" || sd.Kind != "send" {
		t.Fatalf("send item = %+v err=%v", sd, err)
	}
	if raw, err := os.ReadFile(sd.Path); err != nil || string(raw) != "send two\n" {
		t.Fatalf("send file = %q err=%v", raw, err)
	}

	a2, err := s.RequestAmend("job-1", []byte("amend three"))
	if err != nil || a2.Seq != 3 || a2.Name != "amend-000003.md" {
		t.Fatalf("second amend item = %+v err=%v", a2, err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "seq")); err != nil || strings.TrimSpace(string(raw)) != "3" {
		t.Fatalf("seq file = %q err=%v", raw, err)
	}

	// The writers keep the contract modes on POSIX.
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(ctl); err != nil || info.Mode().Perm() != 0o700 {
			t.Fatalf("control dir mode: %v err=%v", info, err)
		}
		for _, name := range []string{"seq", "amend-000001.md", "send-000002.md", "amend-000003.md"} {
			info, err := os.Stat(filepath.Join(ctl, name))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("%s mode: %v err=%v", name, info, err)
			}
		}
	}

	items, err := s.PendingControl("job-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("pending = %+v", items)
	}
	if items[0].Kind != "amend" || items[0].Seq != 1 || string(items[0].Body) != "amend one" {
		t.Fatalf("pending[0] = %+v", items[0])
	}
	if items[1].Kind != "send" || items[1].Seq != 2 || string(items[1].Body) != "send two\n" {
		t.Fatalf("pending[1] = %+v", items[1])
	}
	if items[2].Kind != "amend" || items[2].Seq != 3 || string(items[2].Body) != "amend three" {
		t.Fatalf("pending[2] = %+v", items[2])
	}
}

// TestControlPendingOrderAndIgnoresUnknown: PendingControl orders cancel,
// then checkpoint, then the bodies by Seq; unknown files are ignored; an
// unknown id is not found; and nothing is created.
func TestControlPendingOrderAndIgnoresUnknown(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	queued, err := s.RequestCancel("job-1", 30)
	if err != nil || !queued {
		t.Fatalf("cancel: %v err=%v", queued, err)
	}
	queued, err = s.RequestCheckpoint("job-1")
	if err != nil || !queued {
		t.Fatalf("checkpoint: %v err=%v", queued, err)
	}
	if _, err := s.RequestAmend("job-1", []byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RequestSend("job-1", []byte("b")); err != nil {
		t.Fatal(err)
	}

	// Unknown files (a stray name and a temp remnant) are ignored.
	if err := os.WriteFile(filepath.Join(ctl, "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctl, ".amend-000042.md-12345.tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	items, err := s.PendingControl("job-1")
	if err != nil || len(items) != 4 {
		t.Fatalf("pending = %+v err=%v", items, err)
	}
	if items[0].Kind != "cancel" || items[0].Name != "cancel.json" || string(items[0].Body) != `{"grace":30}` || items[0].Grace != 30 || items[0].Seq != 0 {
		t.Fatalf("pending[0] = %+v", items[0])
	}
	if items[1].Kind != "checkpoint" || items[1].Name != "checkpoint" || len(items[1].Body) != 0 {
		t.Fatalf("pending[1] = %+v", items[1])
	}
	if items[2].Kind != "amend" || items[2].Seq != 1 || items[3].Kind != "send" || items[3].Seq != 2 {
		t.Fatalf("pending[2:] = %+v", items[2:])
	}

	// Unknown ids are not found, and an absent control directory is empty
	// without being created.
	if _, err := s.PendingControl("nope"); exitCode(t, err) != ExitNotFound {
		t.Fatalf("unknown id: %v", err)
	}
	dir := filepath.Join(root, "jobs", "job-2")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(`{"schema":1,"id":"job-2","status":"running","brief_sha256":"x","decisions_acked_seq":0,"motivo":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	items, err = s.PendingControl("job-2")
	if err != nil || !reflect.DeepEqual(items, []ControlItem{}) {
		t.Fatalf("pending job-2 = %+v err=%v", items, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "control")); !os.IsNotExist(err) {
		t.Fatalf("PendingControl created the control directory: %v", err)
	}
}

// TestControlCancelCheckpointRetry: a repeated cancel or checkpoint while one
// is pending is a no-op with false and one file; after the retire a new
// request is accepted and the retired copy is not overwritten.
func TestControlCancelCheckpointRetry(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	queued, err := s.RequestCancel("job-1", 120)
	if err != nil || !queued {
		t.Fatalf("cancel: %v err=%v", queued, err)
	}
	queued, err = s.RequestCancel("job-1", 30)
	if err != nil || queued {
		t.Fatalf("second cancel: %v err=%v", queued, err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "cancel.json")); err != nil || string(raw) != `{"grace":120}` {
		t.Fatalf("cancel file after retry = %q err=%v", raw, err)
	}
	queued, err = s.RequestCheckpoint("job-1")
	if err != nil || !queued {
		t.Fatalf("checkpoint: %v err=%v", queued, err)
	}
	queued, err = s.RequestCheckpoint("job-1")
	if err != nil || queued {
		t.Fatalf("second checkpoint: %v err=%v", queued, err)
	}

	cancel, err := s.PendingControl("job-1")
	if err != nil || len(cancel) != 2 || cancel[0].Kind != "cancel" || cancel[1].Kind != "checkpoint" {
		t.Fatalf("pending = %+v err=%v", cancel, err)
	}
	if err := s.RetireControl("job-1", cancel[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.RetireControl("job-1", cancel[1]); err != nil {
		t.Fatal(err)
	}

	// A new request after the retire is accepted, with its own suffix: the
	// retired copy keeps the old grace bytes.
	queued, err = s.RequestCancel("job-1", 30)
	if err != nil || !queued {
		t.Fatalf("cancel after retire: %v err=%v", queued, err)
	}
	queued, err = s.RequestCheckpoint("job-1")
	if err != nil || !queued {
		t.Fatalf("checkpoint after retire: %v err=%v", queued, err)
	}
	pending, err := s.PendingControl("job-1")
	if err != nil || len(pending) != 2 || pending[0].Kind != "cancel" || pending[1].Kind != "checkpoint" {
		t.Fatalf("pending after retire = %+v err=%v", pending, err)
	}
	if err := s.RetireControl("job-1", pending[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.RetireControl("job-1", pending[1]); err != nil {
		t.Fatal(err)
	}

	old, err := os.ReadFile(filepath.Join(ctl, "done", "cancel.json-000001"))
	if err != nil || string(old) != `{"grace":120}` {
		t.Fatalf("retired cancel overwritten: %q err=%v", old, err)
	}
	fresh, err := os.ReadFile(filepath.Join(ctl, "done", "cancel.json-000003"))
	if err != nil || string(fresh) != `{"grace":30}` {
		t.Fatalf("second retired cancel: %q err=%v", fresh, err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "done", "checkpoint-000002")); err != nil || len(raw) != 0 {
		t.Fatalf("retired checkpoint: %q err=%v", raw, err)
	}
	if items, err := s.PendingControl("job-1"); err != nil || len(items) != 0 {
		t.Fatalf("pending after retires = %+v err=%v", items, err)
	}
}

// TestControlRetireNoopAndContainment: RetireControl moves the file into
// done/ under the same name, a second call is a no-op, and a source outside
// the job's control directory is refused.
func TestControlRetireNoopAndContainment(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	item, err := s.RequestAmend("job-1", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RetireControl("job-1", item); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(item.Path); !os.IsNotExist(err) {
		t.Fatalf("source still present: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "done", "amend-000001.md")); err != nil || string(raw) != "body" {
		t.Fatalf("retired file = %q err=%v", raw, err)
	}
	// Retry after a completed retire: a no-op that changes nothing.
	if err := s.RetireControl("job-1", item); err != nil {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "done", "amend-000001.md")); err != nil || string(raw) != "body" {
		t.Fatalf("retired file after no-op = %q err=%v", raw, err)
	}
	if items, err := s.PendingControl("job-1"); err != nil || len(items) != 0 {
		t.Fatalf("pending = %+v err=%v", items, err)
	}

	// A hostile item pointing outside control/ is refused and its file is
	// left in place.
	outside := filepath.Join(root, "outside.md")
	if err := os.WriteFile(outside, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	hostile := ControlItem{Kind: "amend", Name: "amend-000001.md", Path: outside}
	if err := s.RetireControl("job-1", hostile); exitCode(t, err) != ExitUsage {
		t.Fatalf("hostile retire: %v", err)
	}
	if raw, err := os.ReadFile(outside); err != nil || string(raw) != "keep" {
		t.Fatalf("hostile file moved: %q err=%v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(ctl, "done", "outside.md")); !os.IsNotExist(err) {
		t.Fatalf("hostile file retired: %v", err)
	}

	// An unknown id is not found.
	if err := s.RetireControl("nope", item); exitCode(t, err) != ExitNotFound {
		t.Fatalf("unknown id: %v", err)
	}
}

// TestControlConcurrentRequests: eight writers racing for the counter leave
// eight files with consecutive Seq values, no gap or duplicate.
func TestControlConcurrentRequests(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	var wg sync.WaitGroup
	results := make([]struct {
		item ControlItem
		err  error
	}, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			body := fmt.Sprintf("body-%d", i)
			var item ControlItem
			var err error
			if i%2 == 0 {
				item, err = s.RequestAmend("job-1", []byte(body))
			} else {
				item, err = s.RequestSend("job-1", []byte(body))
			}
			results[i] = struct {
				item ControlItem
				err  error
			}{item, err}
		}(i)
	}
	wg.Wait()

	seen := map[int]bool{}
	for i, res := range results {
		if res.err != nil {
			t.Fatalf("goroutine %d: %v", i, res.err)
		}
		if res.item.Seq < 1 || res.item.Seq > 8 || seen[res.item.Seq] {
			t.Fatalf("seq collision: %v", results)
		}
		seen[res.item.Seq] = true
		body := fmt.Sprintf("body-%d", i)
		if raw, err := os.ReadFile(res.item.Path); err != nil || string(raw) != body {
			t.Fatalf("goroutine %d file = %q err=%v", i, raw, err)
		}
	}
	for seq := 1; seq <= 8; seq++ {
		if !seen[seq] {
			t.Fatalf("seq gap: %v", seen)
		}
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "seq")); err != nil || strings.TrimSpace(string(raw)) != "8" {
		t.Fatalf("seq file = %q err=%v", raw, err)
	}
	if items, err := s.PendingControl("job-1"); err != nil || len(items) != 8 {
		t.Fatalf("pending = %+v err=%v", items, err)
	}
}

// TestControlTerminalRefusal: a terminal, collected or closed job refuses
// amend and send with exit 2 and treats cancel and checkpoint as no-ops that
// write nothing.
func TestControlTerminalRefusal(t *testing.T) {
	for _, status := range []string{StatusDone, StatusFailed, StatusTimeout, StatusCanceled, StatusCollected, StatusClosed} {
		t.Run(status, func(t *testing.T) {
			s, root := startedJob(t)
			setControlStatus(t, root, "job-1", status)
			ctl := controlDir(root)

			_, err := s.RequestAmend("job-1", []byte("body"))
			if exitCode(t, err) != ExitUsage || err.Error() != "job: job is not active" {
				t.Fatalf("amend: %v", err)
			}
			_, err = s.RequestSend("job-1", []byte("body"))
			if exitCode(t, err) != ExitUsage || err.Error() != "job: job is not active" {
				t.Fatalf("send: %v", err)
			}
			queued, err := s.RequestCancel("job-1", 30)
			if err != nil || queued {
				t.Fatalf("cancel: %v err=%v", queued, err)
			}
			queued, err = s.RequestCheckpoint("job-1")
			if err != nil || queued {
				t.Fatalf("checkpoint: %v err=%v", queued, err)
			}
			if got := controlEntries(t, ctl); len(got) != 0 {
				t.Fatalf("terminal job wrote control files: %v", got)
			}

			// An active job of the same shape still accepts the requests.
			s2, root2 := startedJob(t)
			if _, err := s2.Transition("job-1", StatusPreparing); err != nil {
				t.Fatal(err)
			}
			if _, err := s2.Transition("job-1", StatusRunning); err != nil {
				t.Fatal(err)
			}
			if _, err := s2.RequestAmend("job-1", []byte("body")); err != nil {
				t.Fatal(err)
			}
			queued, err = s2.RequestCancel("job-1", 30)
			if err != nil || !queued {
				t.Fatalf("running cancel: %v err=%v", queued, err)
			}
			if got := controlEntries(t, controlDir(root2)); len(got) != 3 {
				t.Fatalf("running job entries: %v", got)
			}
		})
	}
}

// crashCounterRename points the rename hook at a failure of only the
// counter rename: the request bodies still publish, the seq file never
// lands.
func crashCounterRename(t *testing.T) {
	t.Helper()
	previous := renameAtomic
	renameAtomic = func(from, to string) error {
		if filepath.Base(to) == "seq" {
			return errors.New("crash after body publish")
		}
		return previous(from, to)
	}
	t.Cleanup(func() { renameAtomic = previous })
}

// controlNumbers lists every counter number in use under the control
// directory: the live and retired amend-/send- names and the retired
// cancel.json/checkpoint suffixes.
func controlNumbers(t *testing.T, ctl string) []int {
	t.Helper()
	suffix := regexp.MustCompile(`-([0-9]{1,12})(?:\.md)?$`)
	nums := []int{}
	for _, dir := range []string{ctl, filepath.Join(ctl, "done")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			t.Fatal(err)
		}
		for _, entry := range entries {
			match := suffix.FindStringSubmatch(entry.Name())
			if match == nil {
				continue
			}
			n, err := strconv.Atoi(match[1])
			if err == nil {
				nums = append(nums, n)
			}
		}
	}
	sort.Ints(nums)
	return nums
}

// TestControlSeqReuseAfterCrashedCounter: a crash after the body publishes
// and before the counter rename leaves the counter stale; the next request
// of either kind takes a fresh number, PendingControl lists distinct
// numbers, and the retry converges to one new request with a fresh number
// and a published counter.
func TestControlSeqReuseAfterCrashedCounter(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	crashCounterRename(t)

	_, err := s.RequestAmend("job-1", []byte("amend one"))
	if err == nil {
		t.Fatal("expected the counter crash")
	}
	if _, err := os.Stat(filepath.Join(ctl, "amend-000001.md")); err != nil {
		t.Fatalf("published body missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctl, "seq")); !os.IsNotExist(err) {
		t.Fatalf("crash wrote the counter: %v", err)
	}

	// The other kind must not reuse the published number.
	_, err = s.RequestSend("job-1", []byte("send two"))
	if err == nil {
		t.Fatal("expected the counter crash")
	}
	if _, err := os.Stat(filepath.Join(ctl, "send-000002.md")); err != nil {
		t.Fatalf("send took a reused number: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctl, "send-000001.md")); !os.IsNotExist(err) {
		t.Fatalf("reused number published: %v", err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "seq")); err == nil {
		t.Fatalf("counter written: %q", raw)
	}

	items, err := s.PendingControl("job-1")
	if err != nil || len(items) != 2 {
		t.Fatalf("pending = %+v err=%v", items, err)
	}
	if items[0].Seq == items[1].Seq {
		t.Fatalf("pending seqs are not distinct: %+v", items)
	}
	if items[0].Name != "amend-000001.md" || items[1].Name != "send-000002.md" {
		t.Fatalf("pending = %+v", items)
	}

	// The retry converges to one new request with a fresh number and a
	// published counter.
	renameAtomic = os.Rename
	item, err := s.RequestAmend("job-1", []byte("retry"))
	if err != nil || item.Seq != 3 || item.Name != "amend-000003.md" {
		t.Fatalf("retry: %+v err=%v", item, err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "seq")); err != nil || strings.TrimSpace(string(raw)) != "3" {
		t.Fatalf("seq after retry = %q err=%v", raw, err)
	}
	if nums := controlNumbers(t, ctl); !reflect.DeepEqual(nums, []int{1, 2, 3}) {
		t.Fatalf("control numbers = %v", nums)
	}
}

// TestControlRetireSuffixNoCollision: after a crashed counter, the retired
// cancel.json and checkpoint suffixes and the fresh request numbers never
// collide, and a lost counter still climbs past the retired suffixes.
func TestControlRetireSuffixNoCollision(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	crashCounterRename(t)
	_, err := s.RequestAmend("job-1", []byte("amend one"))
	if err == nil {
		t.Fatal("expected the counter crash")
	}
	renameAtomic = os.Rename

	queued, err := s.RequestCancel("job-1", 60)
	if err != nil || !queued {
		t.Fatalf("cancel: %v err=%v", queued, err)
	}
	queued, err = s.RequestCheckpoint("job-1")
	if err != nil || !queued {
		t.Fatalf("checkpoint: %v err=%v", queued, err)
	}
	cancel, err := s.PendingControl("job-1")
	if err != nil || len(cancel) != 3 || cancel[0].Kind != "cancel" || cancel[1].Kind != "checkpoint" || cancel[2].Name != "amend-000001.md" {
		t.Fatalf("pending = %+v err=%v", cancel, err)
	}
	if err := s.RetireControl("job-1", cancel[0]); err != nil {
		t.Fatal(err)
	}
	if err := s.RetireControl("job-1", cancel[1]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(ctl, "done", "cancel.json-000002")); err != nil {
		t.Fatalf("retired cancel suffix: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ctl, "done", "checkpoint-000003")); err != nil {
		t.Fatalf("retired checkpoint suffix: %v", err)
	}

	// Fresh requests keep climbing past the retired suffixes.
	sd, err := s.RequestSend("job-1", []byte("send"))
	if err != nil || sd.Seq != 4 || sd.Name != "send-000004.md" {
		t.Fatalf("send: %+v err=%v", sd, err)
	}
	a2, err := s.RequestAmend("job-1", []byte("amend two"))
	if err != nil || a2.Seq != 5 || a2.Name != "amend-000005.md" {
		t.Fatalf("amend: %+v err=%v", a2, err)
	}
	if nums := controlNumbers(t, ctl); !reflect.DeepEqual(nums, []int{1, 2, 3, 4, 5}) {
		t.Fatalf("control numbers = %v", nums)
	}

	// A lost counter still climbs past the retired suffixes: with the live
	// bodies retired into done/, only the done/ names carry the maximum.
	bodies, err := s.PendingControl("job-1")
	if err != nil || len(bodies) != 3 {
		t.Fatalf("bodies = %+v err=%v", bodies, err)
	}
	for _, body := range bodies {
		if err := s.RetireControl("job-1", body); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(ctl, "seq")); err != nil {
		t.Fatal(err)
	}
	a3, err := s.RequestAmend("job-1", []byte("amend three"))
	if err != nil || a3.Seq != 6 || a3.Name != "amend-000006.md" {
		t.Fatalf("amend after lost counter: %+v err=%v", a3, err)
	}
	if raw, err := os.ReadFile(filepath.Join(ctl, "seq")); err != nil || strings.TrimSpace(string(raw)) != "6" {
		t.Fatalf("seq after lost counter = %q err=%v", raw, err)
	}
}

// TestControlSymlinkRefusal: a symlinked request file is not read or
// retired, its target stays untouched, and a symlinked control/done is
// refused (POSIX only).
func TestControlSymlinkRefusal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink refusal is exercised on POSIX")
	}
	s, root := startedJob(t)
	ctl := controlDir(root)

	// A symlinked amend body is not listed and its target stays untouched.
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("SECRET-BODY"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(ctl, "amend-000009.md")); err != nil {
		t.Fatal(err)
	}
	items, err := s.PendingControl("job-1")
	if err != nil || len(items) != 0 {
		t.Fatalf("pending = %+v err=%v", items, err)
	}
	if raw, err := os.ReadFile(secret); err != nil || string(raw) != "SECRET-BODY" {
		t.Fatalf("symlink target read: %q err=%v", raw, err)
	}

	// A symlinked source is refused by the retire, not moved.
	hostile := ControlItem{Kind: "amend", Name: "amend-000009.md", Path: filepath.Join(ctl, "amend-000009.md")}
	if err := s.RetireControl("job-1", hostile); exitCode(t, err) != ExitUsage || err.Error() != "job: control path is not a regular file inside control/" {
		t.Fatalf("symlinked source: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(ctl, "amend-000009.md")); err != nil {
		t.Fatalf("symlinked source moved: %v", err)
	}

	// A symlinked done directory is refused and the request stays put.
	item, err := s.RequestAmend("job-1", []byte("body"))
	if err != nil {
		t.Fatal(err)
	}
	outsideDone := filepath.Join(root, "outside-done")
	if err := os.MkdirAll(outsideDone, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideDone, filepath.Join(ctl, "done")); err != nil {
		t.Fatal(err)
	}
	if err := s.RetireControl("job-1", item); exitCode(t, err) != ExitUsage || err.Error() != "job: control path is not a regular file inside control/" {
		t.Fatalf("symlinked done: %v", err)
	}
	if _, err := os.Stat(item.Path); err != nil {
		t.Fatalf("request moved out of control: %v", err)
	}
	if entries, err := os.ReadDir(outsideDone); err != nil || len(entries) != 0 {
		t.Fatalf("outside done received the request: %v err=%v", entries, err)
	}
}

// TestControlCrashBeforeBodyRename: a crash after the body temp file is
// written and before the rename leaves no visible request and the counter
// unchanged; the retry publishes exactly one request.
func TestControlCrashBeforeBodyRename(t *testing.T) {
	s, root := startedJob(t)
	ctl := controlDir(root)

	previous := renameAtomic
	renameAtomic = func(from, to string) error { return errors.New("crash") }
	t.Cleanup(func() { renameAtomic = previous })

	_, err := s.RequestAmend("job-1", []byte("amend body"))
	if err == nil {
		t.Fatal("expected the crash")
	}
	if got := controlEntries(t, ctl); len(got) != 0 {
		t.Fatalf("crash left control files: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ctl, "seq")); !os.IsNotExist(err) {
		t.Fatalf("crash changed the counter: %v", err)
	}
	if items, err := s.PendingControl("job-1"); err != nil || len(items) != 0 {
		t.Fatalf("pending after crash = %+v err=%v", items, err)
	}

	// The retry succeeds and PendingControl shows exactly one request.
	renameAtomic = previous
	item, err := s.RequestAmend("job-1", []byte("retry body"))
	if err != nil || item.Seq != 1 || item.Name != "amend-000001.md" {
		t.Fatalf("retry: %+v err=%v", item, err)
	}
	if raw, err := os.ReadFile(item.Path); err != nil || string(raw) != "retry body" {
		t.Fatalf("retry file = %q err=%v", raw, err)
	}
	items, err := s.PendingControl("job-1")
	if err != nil || len(items) != 1 || items[0].Seq != 1 || string(items[0].Body) != "retry body" {
		t.Fatalf("pending after retry = %+v err=%v", items, err)
	}
}
