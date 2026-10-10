// wake.go is the generic, argv-only wake hook of the job contract
// ("Waking (best effort)"): for an event type that wakes the dispatcher it
// runs the machine's job_wake_cmd without a shell, with the event JSON on
// stdin and the four HERDR_SOHO_JOB_* variables, bounded per attempt,
// retried at most three times, and logs the final failure to friction. It
// never fails the job and never touches job state.
package job

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

// defaultWakeTimeout bounds one hook attempt when WakeHook.Timeout is zero
// (or negative, which would otherwise disable the platform deadline).
const defaultWakeTimeout = 30 * time.Second

// defaultWakeDelays are the pauses between failed attempts when
// WakeHook.Delays is nil: at most three retries, after 10, 30, and 90
// seconds (contract "Waking (best effort)").
var defaultWakeDelays = []time.Duration{10 * time.Second, 30 * time.Second, 90 * time.Second}

// wakes reports whether the event type wakes the dispatcher (contract
// "Waking (best effort)"): pr_opened, review_verdict, question, blocked,
// decision, timeout_warning, failure, and terminal always do; push does
// only on rejection, which is the only push carrying refs.motivo; every
// other type (accepted, preparing, worker_spawned, worker_done, commit,
// checkpoint, unblocked, amend_received, decision_acked, note, cleanup, and
// unknown types) does not.
func wakes(ev Event) bool {
	switch ev.Tipo {
	case "pr_opened", "review_verdict", "question", "blocked", "decision", "timeout_warning", "failure", "terminal":
		return true
	case "push":
		return ev.Refs["motivo"] != ""
	}
	return false
}

// WakeHook runs the machine's job_wake_cmd for the events that wake the
// dispatcher. Cmd is argv only, run without a shell (no sh -c, no cmd /c):
// strings.Fields(Cmd) is the argument vector, an absolute argv[0] runs as is
// (platform.RunExecutable), a bare name is resolved through Env's PATH
// (platform.RunCli), and a value with spaces is not supported — there are
// no quoting rules to honor.
// Zero Timeout = 30 s per attempt; nil Delays = {10s, 30s, 90s};
// nil Sleep = time.Sleep; nil Friction drops the final failure message.
//
// TODO(DJA-194): the hook's real receiver and signature on dispatcher machines are not verified (premise 2); only this generic argv contract is implemented.
type WakeHook struct {
	Cmd      string
	JobID    string
	Env      platform.Env
	Timeout  time.Duration
	Delays   []time.Duration
	Sleep    func(time.Duration)
	Friction func(message string)
}

// Run runs the hook for the event when its type wakes the dispatcher and
// returns how many times the command ran (0 when it did not run). It never
// returns an error and never touches job state. Attempt 1 runs immediately;
// after a failed attempt k the sleep Sleep(Delays[k-1]) precedes the retry,
// for at most 1+len(Delays) attempts; the first success stops the loop.
// Only the platform context deadline (TimeoutMs) bounds each attempt — no
// wall-clock schedule is computed, so the retry pauses come only from Sleep
// and a fake clock going backwards cannot affect the run.
func (h WakeHook) Run(ev Event) (attempts int) {
	if strings.TrimSpace(h.Cmd) == "" || !wakes(ev) {
		return 0
	}
	argv := strings.Fields(h.Cmd)
	delays := h.Delays
	if delays == nil {
		delays = defaultWakeDelays
	}
	sleep := h.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	timeout := h.Timeout
	if timeout <= 0 {
		timeout = defaultWakeTimeout
	}
	env := h.Env.Clone()
	env["HERDR_SOHO_JOB_ID"] = h.JobID
	env["HERDR_SOHO_JOB_SEQ"] = strconv.Itoa(ev.Seq)
	env["HERDR_SOHO_JOB_EVENT"] = ev.Tipo
	env["HERDR_SOHO_JOB_IDEMPOTENCY_KEY"] = h.JobID + ":" + strconv.Itoa(ev.Seq)
	// The event line is the same json.Marshal + "\n" format events.jsonl
	// uses, so the receiver sees the stored line verbatim.
	line, err := json.Marshal(ev)
	if err != nil {
		// An Event is a flat JSON document, so marshaling it cannot fail;
		// the command did not run, which is the only observable fact.
		return 0
	}
	line = append(line, '\n')
	// A positive timeout below 1 ms must not truncate to 0, which RunCli reads
	// as "no deadline".
	timeoutMs := int(timeout / time.Millisecond)
	if timeoutMs < 1 {
		timeoutMs = 1
	}
	maxAttempts := 1 + len(delays)
	cause := ""
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attempts = attempt
		opts := platform.RunOptions{
			Env:       env,
			Input:     string(line),
			TimeoutMs: timeoutMs,
		}
		var result platform.RunResult
		if filepath.IsAbs(argv[0]) {
			result = platform.RunExecutable(argv[0], argv[1:], opts)
		} else {
			result = platform.RunCli(argv[0], argv[1:], opts)
		}
		if !result.NotFound && !result.TimedOut && result.Status != nil && *result.Status == 0 {
			return attempts
		}
		cause = wakeCause(result)
		if attempt < maxAttempts {
			sleep(delays[attempt-1])
		}
	}
	if h.Friction != nil {
		h.Friction(fmt.Sprintf("job: wake hook failed for %s:%d after %d attempts (%s)", h.JobID, ev.Seq, attempts, cause))
	}
	return attempts
}

// wakeCause names the failed attempt within the closed cause set —
// exit <code>, timeout, or not found — and never the hook's output or the
// event text. A nil status that is neither a timeout nor a missing
// executable means the process never ran (a start failure), which is
// reported as not found.
func wakeCause(result platform.RunResult) string {
	switch {
	case result.TimedOut:
		return "timeout"
	case result.NotFound:
		return "not found"
	case result.Status != nil:
		return fmt.Sprintf("exit %d", *result.Status)
	default:
		return "not found"
	}
}
