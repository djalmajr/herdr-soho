package wait

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// runCmdWait runs CmdWait against the fixture and captures its outputs.
func runCmdWait(t *testing.T, f *queuedProbeFixture, argv []string) (int, string, string) {
	t.Helper()
	f.env["SOHO_DIR"] = filepath.Dir(f.sd)
	f.env["HERDR_WORKSPACE_ID"] = filepath.Base(f.sd)
	var out, stderr bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &out, &stderr
	code := CmdWait(argv, f.ctx, f.env, "")
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), stderr.String()
}

func TestWaitTimeoutDurationUnits(t *testing.T) {
	t.Run("--timeout 2s waits the full two seconds before exit 9", func(t *testing.T) {
		f := newQueuedProbeFixture(t, "working", "5", "Busy on the task 30%\n", nil)
		f.env["SOHO_WAIT_POLL_MS"] = "100"
		start := time.Now()
		code, out, stderr := runCmdWait(t, f, []string{"worker", "--timeout", "2s"})
		elapsed := time.Since(start)
		if code != 9 || !strings.Contains(out, `"status":"timeout"`) {
			t.Fatalf("code=%d out=%q stderr=%q; want the 2 s wait to time out with 9", code, out, stderr)
		}
		if elapsed < 1800*time.Millisecond || elapsed > 20*time.Second {
			t.Fatalf("elapsed=%v; the 2 s unit deadline was not applied (a 2 ms read would end at once, 2000 ms as seconds would take 33 minutes)", elapsed)
		}
	})
	t.Run("the timeout hint shows the doubled value with the unit: --timeout 1s suggests --timeout 2s", func(t *testing.T) {
		f := newQueuedProbeFixture(t, "working", "5", "Busy on the task 30%\n", nil)
		f.env["SOHO_WAIT_POLL_MS"] = "100"
		start := time.Now()
		code, out, stderr := runCmdWait(t, f, []string{"worker", "--timeout", "1s"})
		elapsed := time.Since(start)
		if code != 9 || !strings.Contains(out, `"status":"timeout"`) {
			t.Fatalf("code=%d out=%q stderr=%q; want the 1 s wait to time out with 9", code, out, stderr)
		}
		if !strings.Contains(stderr, "Run: soho wait worker --timeout 2s") {
			t.Fatalf("stderr=%q; want the run hint with the doubled unit value", stderr)
		}
		if strings.Contains(stderr, "Run: soho wait worker --timeout 2000") {
			t.Fatalf("stderr=%q; the hint must keep the unit, not the bare milliseconds", stderr)
		}
		if elapsed < 900*time.Millisecond || elapsed > 20*time.Second {
			t.Fatalf("elapsed=%v; the 1 s unit deadline was not applied", elapsed)
		}
	})
	// CmdWait is called directly, not through cli.Run: the friction die of the
	// refusal re-panics here, as it does for every friction die of the wait.
	t.Run("an invalid duration exits 2 with the unit message", func(t *testing.T) {
		f := newQueuedProbeFixture(t, "working", "5", "Busy on the task 30%\n", nil)
		code, msg := 0, ""
		var out bytes.Buffer
		func() {
			defer func() {
				r := recover()
				if ee, ok := r.(*platform.ExitError); ok {
					code, msg = ee.Code, ee.Msg
				} else if r != nil {
					t.Fatalf("unexpected panic: %v", r)
				}
			}()
			oldOut, oldErr := platform.Stdout, platform.Stderr
			platform.Stdout, platform.Stderr = &out, io.Discard
			CmdWait([]string{"worker", "--timeout", "1h30m"}, f.ctx, f.env, "")
			platform.Stdout, platform.Stderr = oldOut, oldErr
		}()
		if code != 2 || out.Len() != 0 {
			t.Fatalf("code=%d out=%q; want exit 2 before any probe", code, out.String())
		}
		if msg != "wait: --timeout expects milliseconds or a duration such as 90s, 30m or 2h, got '1h30m'" {
			t.Fatalf("msg=%q", msg)
		}
	})
}

func TestWaitShortTimeoutWarnsOnlyForUnitlessIntegers(t *testing.T) {
	run := func(t *testing.T, timeout string) (string, string) {
		t.Helper()
		f := newQueuedProbeFixture(t, "working", "5", "Busy on the task 30%\n", nil)
		f.env["SOHO_WAIT_POLL_MS"] = "100"
		SetFrictionLogFile(filepath.Join(f.sd, "friction.log"))
		code, _, stderr := runCmdWait(t, f, []string{"worker", "--timeout", timeout})
		if code != 9 {
			t.Fatalf("timeout=%s code=%d; want the sub-second wait to time out with 9", timeout, code)
		}
		log, _ := os.ReadFile(filepath.Join(f.sd, "friction.log"))
		return stderr, string(log)
	}
	t.Run("--timeout 500 still warns, once, with the seconds fix", func(t *testing.T) {
		stderr, log := run(t, "500")
		if n := strings.Count(stderr, "wait: --timeout is in milliseconds; 500 is under a second (for 500 seconds pass 500000)"); n != 1 {
			t.Fatalf("warning count=%d stderr=%q", n, stderr)
		}
		if !strings.Contains(log, "warning\twait\twait: --timeout is in milliseconds; 500 is under a second (for 500 seconds pass 500000)") {
			t.Fatalf("friction log=%q", log)
		}
	})
	t.Run("--timeout 500ms never warns", func(t *testing.T) {
		stderr, log := run(t, "500ms")
		if strings.Contains(stderr, "--timeout is in milliseconds") {
			t.Fatalf("a unit value must not warn: stderr=%q", stderr)
		}
		if strings.Contains(log, "--timeout is in milliseconds") {
			t.Fatalf("a unit value must not reach the friction log: %q", log)
		}
	})
}
