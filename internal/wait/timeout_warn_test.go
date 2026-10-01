package wait

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestWaitShortTimeoutWarnsOnceAndProceeds(t *testing.T) { // --timeout 45 means 45 ms: one warning with the seconds fix, then the wait loop still runs
	f := newQueuedProbeFixture(t, "working", "5", "Busy on the task 30%\n", nil)
	f.env["HERDR_SOHO_DIR"] = filepath.Dir(f.sd)
	f.env["HERDR_WORKSPACE_ID"] = filepath.Base(f.sd)
	SetFrictionLogFile(filepath.Join(f.sd, "friction.log"))
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, stderr bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &stderr
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	code := CmdWait([]string{"worker", "--timeout", "45"}, f.ctx, f.env, "")
	if code != 9 || !strings.Contains(out.String(), `"status":"timeout"`) {
		t.Fatalf("code=%d out=%q stderr=%q; want the 45 ms wait loop running until its timeout", code, out.String(), stderr.String())
	}
	if n := strings.Count(stderr.String(), "wait: --timeout is in milliseconds; 45 is under a second (for 45 seconds pass 45000)"); n != 1 {
		t.Fatalf("warning count=%d stderr=%q", n, stderr.String())
	}
	log, err := os.ReadFile(filepath.Join(f.sd, "friction.log"))
	if err != nil || !strings.Contains(string(log), "warning\twait\twait: --timeout is in milliseconds; 45 is under a second (for 45 seconds pass 45000)") {
		t.Fatalf("friction log=%q err=%v", log, err)
	}
}
