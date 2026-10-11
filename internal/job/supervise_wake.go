// supervise_wake.go wires the generic wake hook (wake.go) into the
// supervisor's watch loop: a persisted cursor (jobs/<id>/wake.seq) and a
// background worker run the machine's job_wake_cmd for every queued event
// in order, after each run. The loop only enqueues (a full queue defers
// the rest to the next tick and never blocks), the worker never touches
// job state other than the cursor, and the terminal path drains the
// worker with a bounded wait before the workspace close.
package job

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// wakeQueueCap is the capacity of the supervisor's wake queue: a full
// queue leaves the rest of the events for the next tick, so the watch
// loop is never blocked.
const wakeQueueCap = 256

// wakeStop is how long the terminal path waits for the wake worker to
// drain the queue before the workspace close.
const wakeStop = 10 * time.Second

// startWake starts the wake worker when the supervisor has a wake hook
// (Supervisor.Wake is non-nil and Cmd is non-empty): one goroutine runs
// WakeHook.Run for each queued event in order and, after each Run returns
// (success or final failure), writes the persisted cursor
// jobs/<id>/wake.seq (decimal seq, atomic, 0600). The cursor initializes
// the process's high-water mark, so a restarted supervisor re-runs only
// the events after it (at-least-once; the idempotency key <id>:<seq> lets
// the receiver deduplicate). A disabled hook (nil Wake or empty Cmd)
// starts nothing: dispatchWakes and stopWake stay no-ops. It is called
// once at the start of Run, after the start wait.
func (s *Supervisor) startWake() {
	if s.Wake == nil || strings.TrimSpace(s.Wake.Cmd) == "" {
		return
	}
	if seq, err := s.readWakeSeq(); err != nil {
		// A corrupt or unreadable cursor re-runs from the start
		// (at-least-once; the idempotency key deduplicates): a wake is
		// best effort and never fails the job.
		s.wakeFriction("job: the supervisor could not read the wake cursor")
	} else {
		s.wakeQueued = seq
	}
	s.wakeQueue = make(chan Event, wakeQueueCap)
	s.wakeDone = make(chan struct{})
	go func() {
		defer close(s.wakeDone)
		for ev := range s.wakeQueue {
			s.Wake.Run(ev)
			if err := s.writeWakeSeq(ev.Seq); err != nil {
				s.wakeFriction("job: the supervisor could not record the wake cursor")
			}
		}
	}()
}

// dispatchWakes queues every event with a seq greater than the process's
// high-water mark (wakeQueued, initialized from the cursor) for the wake
// worker and returns how many events it queued. The hook itself filters
// the events that actually run (wakes). A full queue leaves the rest for
// the next tick: the loop is never blocked. done is always false — a wake
// never ends the job — and err is a store failure. It is called from
// tick.
func (s *Supervisor) dispatchWakes() (int, bool, error) {
	if s.wakeQueue == nil {
		return 0, false, nil
	}
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return 0, false, err
	}
	events, err := readEvents(jobDir)
	if err != nil {
		return 0, false, err
	}
	queued := 0
	for _, ev := range events {
		if ev.Seq <= s.wakeQueued {
			continue
		}
		select {
		case s.wakeQueue <- ev:
			s.wakeQueued = ev.Seq
			queued++
		default:
			// Queue full: the rest waits for the next tick.
			return queued, false, nil
		}
	}
	return queued, false, nil
}

// stopWake closes the wake queue and waits at most timeout for the worker
// to drain it. A positive timeout bounds the wait with a bounded timer —
// no wall-clock deadline is computed; a zero or negative timeout still
// waits for the drain, which is bounded anyway (each Run is bounded by
// the hook's per-attempt timeout). It is a no-op when the hook is
// disabled. The terminal path calls it with wakeStop before the
// workspace close.
func (s *Supervisor) stopWake(timeout time.Duration) {
	if s.wakeQueue == nil {
		return
	}
	close(s.wakeQueue)
	if timeout > 0 {
		timer := time.NewTimer(timeout)
		defer timer.Stop()
		select {
		case <-s.wakeDone:
		case <-timer.C:
		}
		return
	}
	<-s.wakeDone
}

// readWakeSeq reads the persisted cursor: the highest event seq the
// previous supervisor run finished processing. A missing file is 0 (a
// fresh job).
func (s *Supervisor) readWakeSeq() (int, error) {
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return 0, err
	}
	raw, err := os.ReadFile(filepath.Join(jobDir, "wake.seq"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	text := strings.TrimSpace(string(raw))
	seq, err := strconv.Atoi(text)
	if err != nil || seq < 0 {
		return 0, fmt.Errorf("job: wake cursor %q is not a non-negative integer", text)
	}
	return seq, nil
}

// writeWakeSeq writes the cursor atomically (temp file plus rename, 0600).
func (s *Supervisor) writeWakeSeq(seq int) error {
	jobDir, err := s.Store.readDir(s.ID)
	if err != nil {
		return err
	}
	return writeAtomic0600(filepath.Join(jobDir, "wake.seq"), []byte(strconv.Itoa(seq)))
}

// wakeFriction reports to friction when one is set.
func (s *Supervisor) wakeFriction(message string) {
	if s.Friction != nil {
		s.Friction(message)
	}
}

// finishWakes queues the events appended since the last tick — the failure
// and terminal events of the terminal path — and then drains the worker
// within wakeStop. It is idempotent: the queue is closed once and later
// calls do nothing.
func (s *Supervisor) finishWakes() {
	if s.wakeQueue == nil {
		return
	}
	if _, _, err := s.dispatchWakes(); err != nil && s.Friction != nil {
		s.Friction("job: cannot read the events for the final wake")
	}
	s.stopWake(wakeStop)
	s.wakeQueue = nil
}
