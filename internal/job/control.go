package job

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Control requests are queued by the dispatcher (job amend, send, cancel,
// checkpoint) under jobs/<id>/control and picked up later by the supervisor.
// The writers run under the job lock and publish through writeAtomic0600;
// PendingControl is a lock-free read that creates nothing; RetireControl
// moves a delivered request into control/done. No event is appended by the
// writers: the supervisor appends amend_received when it delivers.

// ControlItem is one pending control request under jobs/<id>/control.
type ControlItem struct {
	Kind  string // amend, send, cancel or checkpoint
	Name  string // file name in control/: amend-000001.md, send-000002.md, cancel.json, checkpoint
	Path  string // path of the request file
	Seq   int    // counter value of the amend/send request; 0 for cancel and checkpoint
	Body  []byte // file content, verbatim
	Grace int    // cancel grace in seconds; 0 for the other kinds
}

// controlFilePattern matches the amend and send request names the writers
// publish: the kind, the counter zero-padded to six digits, and .md.
var controlFilePattern = regexp.MustCompile(`^(amend|send)-([0-9]{1,12})\.md$`)

// retiredControlPattern matches the names a retired cancel.json or
// checkpoint takes: the base name and the counter value zero-padded to six
// digits.
var retiredControlPattern = regexp.MustCompile(`^(cancel\.json|checkpoint)-([0-9]{1,12})$`)

// controlRefused lists the states that refuse new amend and send requests:
// the terminal outcomes plus collected and closed.
func controlRefused(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusTimeout, StatusCanceled, StatusCollected, StatusClosed:
		return true
	default:
		return false
	}
}

// readControlSeq reads the control/seq decimal counter; an absent file is 0.
func readControlSeq(ctl string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(ctl, "seq"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("job: control seq unreadable: %w", err)
	}
	seq, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || seq < 0 {
		return 0, errUsage("job: control seq unreadable")
	}
	return seq, nil
}

// nextControlSeq is the next control number under the job lock: one past
// the maximum of the counter, every amend-/send- suffix published in
// control/ and every suffix retired into control/done/. A crash between the
// body rename and the counter rename leaves the counter stale; the
// published and retired suffixes keep their numbers, so the next value is
// always fresh and no number is ever reused.
func nextControlSeq(ctl string) (int, error) {
	seq, err := readControlSeq(ctl)
	if err != nil {
		return 0, err
	}
	for _, dir := range []string{ctl, filepath.Join(ctl, "done")} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, fmt.Errorf("job: control unreadable: %w", err)
		}
		for _, entry := range entries {
			match := controlFilePattern.FindStringSubmatch(entry.Name())
			if match == nil {
				match = retiredControlPattern.FindStringSubmatch(entry.Name())
			}
			if match == nil {
				continue
			}
			n, convErr := strconv.Atoi(match[2])
			if convErr == nil && n > seq {
				seq = n
			}
		}
	}
	return seq + 1, nil
}

// writeControlSeq publishes the counter after increment.
func writeControlSeq(ctl string, next int) error {
	return writeAtomic0600(filepath.Join(ctl, "seq"), []byte(fmt.Sprintf("%d\n", next)))
}

// RequestAmend queues the amendment body verbatim as
// control/amend-<NNNNNN>.md and returns the item, its counter value being
// the incremented seq. A job in a terminal outcome, collected or closed is
// refused with exit 2 "job: job is not active".
func (s *Store) RequestAmend(id string, body []byte) (ControlItem, error) {
	return s.requestControlBody(id, "amend", body)
}

// RequestSend queues the note body verbatim as control/send-<NNNNNN>.md and
// returns the item. The refusal of a job that is no longer active is the
// same as RequestAmend.
func (s *Store) RequestSend(id string, body []byte) (ControlItem, error) {
	return s.requestControlBody(id, "send", body)
}

func (s *Store) requestControlBody(id, kind string, body []byte) (ControlItem, error) {
	var item ControlItem
	err := s.withExisting(id, func(dir string) error {
		st, err := readState(dir)
		if err != nil {
			return err
		}
		if controlRefused(st.Status) {
			return errUsage("job: job is not active")
		}
		ctl := filepath.Join(dir, "control")
		if err := os.MkdirAll(ctl, 0o700); err != nil {
			return err
		}
		next, err := nextControlSeq(ctl)
		if err != nil {
			return err
		}
		num := fmt.Sprintf("%06d", next)
		item = ControlItem{
			Kind: kind,
			Name: kind + "-" + num + ".md",
			Path: filepath.Join(ctl, kind+"-"+num+".md"),
			Seq:  next,
			Body: body,
		}
		// The body publishes before the counter: a crash between the two
		// leaves the published body with a stale counter, and the next
		// request climbs past the published suffix, so no number is ever
		// reused.
		if err := writeAtomic0600(item.Path, body); err != nil {
			return err
		}
		return writeControlSeq(ctl, next)
	})
	if err != nil {
		return ControlItem{}, err
	}
	return item, nil
}

// RequestCancel queues control/cancel.json ({"grace":<seconds>}); a single
// pending cancel is the queue. A job in a terminal outcome, collected or
// closed, or one with a pending cancel, is a no-op that reports false.
func (s *Store) RequestCancel(id string, grace int) (bool, error) {
	return s.requestControlFlag(id, "cancel.json", grace)
}

// RequestCheckpoint queues the empty control/checkpoint marker; a single
// pending marker is the queue. The no-op refusals are the same as
// RequestCancel.
func (s *Store) RequestCheckpoint(id string) (bool, error) {
	return s.requestControlFlag(id, "checkpoint", 0)
}

func (s *Store) requestControlFlag(id, name string, grace int) (bool, error) {
	queued := false
	err := s.withExisting(id, func(dir string) error {
		st, err := readState(dir)
		if err != nil {
			return err
		}
		if controlRefused(st.Status) {
			return nil
		}
		ctl := filepath.Join(dir, "control")
		if err := os.MkdirAll(ctl, 0o700); err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(ctl, name)); err == nil {
			return nil
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("job: control unreadable: %w", err)
		}
		var body []byte
		if name == "cancel.json" {
			body, err = json.Marshal(struct {
				Grace int `json:"grace"`
			}{grace})
			if err != nil {
				return err
			}
		}
		queued = true
		return writeAtomic0600(filepath.Join(ctl, name), body)
	})
	return queued, err
}

// PendingControl lists the pending control requests: cancel first (when
// present), then checkpoint (when present), then the amend and send bodies in
// ascending Seq. Unknown files in control/ are ignored, an absent control
// directory is empty, and nothing is created. The read is lock-free like the
// other reads.
func (s *Store) PendingControl(id string) ([]ControlItem, error) {
	dir, err := s.readDir(id)
	if err != nil {
		return nil, err
	}
	ctl := filepath.Join(dir, "control")
	entries, err := os.ReadDir(ctl)
	if err != nil {
		if os.IsNotExist(err) {
			return []ControlItem{}, nil
		}
		return nil, fmt.Errorf("job: control unreadable: %w", err)
	}
	// ReadDir orders by name: "cancel.json" precedes "checkpoint", which
	// precedes every amend- / send- name, so the fixed kinds land first in
	// the requested order.
	out := []ControlItem{}
	var bodyItems []ControlItem
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(ctl, name)
		match := controlFilePattern.FindStringSubmatch(name)
		var kind string
		seq := 0
		if match != nil {
			kind = match[1]
			seq, _ = strconv.Atoi(match[2])
		} else {
			switch name {
			case "cancel.json":
				kind = "cancel"
			case "checkpoint":
				kind = "checkpoint"
			default:
				continue
			}
		}
		info, err := os.Lstat(path)
		if err != nil {
			// A retire moving the file mid-read is a normal interleave;
			// every other stat failure is a real I/O error.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("job: control unreadable: %w", err)
		}
		// A symlinked request points outside the job and is skipped, not
		// read; only a regular file inside control/ is listed.
		if !info.Mode().IsRegular() {
			continue
		}
		body, err := os.ReadFile(path)
		if err != nil {
			// A retire moving the file between the lstat and the read is a
			// normal interleave; every other read failure is a real I/O
			// error.
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("job: control unreadable: %w", err)
		}
		item := ControlItem{Kind: kind, Name: name, Path: path, Seq: seq, Body: body}
		if kind == "cancel" {
			var doc struct {
				Grace int `json:"grace"`
			}
			if err := json.Unmarshal(body, &doc); err != nil {
				return nil, errUsage("job: control unreadable: cancel.json")
			}
			item.Grace = doc.Grace
		}
		if kind == "amend" || kind == "send" {
			bodyItems = append(bodyItems, item)
			continue
		}
		out = append(out, item)
	}
	sort.Slice(bodyItems, func(i, j int) bool {
		if bodyItems[i].Seq != bodyItems[j].Seq {
			return bodyItems[i].Seq < bodyItems[j].Seq
		}
		return bodyItems[i].Name < bodyItems[j].Name
	})
	return append(out, bodyItems...), nil
}

// RetireControl moves a delivered request into control/done under the job
// lock, with the same name; a retired cancel.json or checkpoint takes the
// next counter value as a -<NNNNNN> suffix so retries never overwrite an
// earlier retired copy. A missing source is a no-op: the request was already
// retired and the retry converges. A symlinked source or a symlinked
// control/done is refused with exit 2: only regular files inside control/
// retire into a real done directory.
func (s *Store) RetireControl(id string, item ControlItem) error {
	return s.withExisting(id, func(dir string) error {
		ctl := filepath.Join(dir, "control")
		ctlAbs, err := filepath.Abs(ctl)
		if err != nil {
			return fmt.Errorf("job: control unreadable: %w", err)
		}
		srcAbs, err := filepath.Abs(item.Path)
		if err != nil {
			return fmt.Errorf("job: control unreadable: %w", err)
		}
		// The item must point at a direct child of this job's control
		// directory; a source outside it is refused, not moved.
		if filepath.Dir(srcAbs) != ctlAbs {
			return errUsage("job: control path is outside control/")
		}
		srcInfo, err := os.Lstat(srcAbs)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return fmt.Errorf("job: control unreadable: %w", err)
		}
		// A symlinked source points outside the job and is refused, not
		// moved: only a regular file inside control/ retires.
		if !srcInfo.Mode().IsRegular() {
			return errUsage("job: control path is not a regular file inside control/")
		}
		done := filepath.Join(ctl, "done")
		if info, err := os.Lstat(done); err == nil {
			// A symlinked done directory would receive the file outside
			// the job and is refused; only a real directory collects it.
			if !info.IsDir() {
				return errUsage("job: control path is not a regular file inside control/")
			}
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("job: control unreadable: %w", err)
		}
		if err := os.MkdirAll(done, 0o700); err != nil {
			return err
		}
		name := filepath.Base(srcAbs)
		if name == "cancel.json" || name == "checkpoint" {
			next, err := nextControlSeq(ctl)
			if err != nil {
				return err
			}
			if err := writeControlSeq(ctl, next); err != nil {
				return err
			}
			name += "-" + fmt.Sprintf("%06d", next)
		}
		return renameAtomic(srcAbs, filepath.Join(done, name))
	})
}
