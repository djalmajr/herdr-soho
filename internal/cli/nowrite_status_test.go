package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Mutation captured: removing status from the NOWRITE allowlist rejects both empty and populated reads.
func TestNowriteStatusThroughCLI(t *testing.T) {
	for _, withState := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "previous report"}[withState], func(t *testing.T) {
			nr := newNowriteReads(t, withState)
			code, out, errText := nr.runNowrite(t, "status")
			if code != 0 {
				t.Fatalf("status exit=%d stdout=%q stderr=%q", code, out, errText)
			}
			if withState {
				if !strings.Contains(out, "worker\tdone\t"+nr.report) {
					t.Fatalf("previous report missing: %q", out)
				}
				code, out, errText = nr.runNowrite(t, "status", "worker", "worker")
				if code != 0 || strings.Count(out, "worker\tdone\t") != 2 {
					t.Fatalf("named status exit=%d stdout=%q stderr=%q", code, out, errText)
				}
			} else if out != "" || !strings.Contains(errText, "no agents in the roster") {
				t.Fatalf("empty status stdout=%q stderr=%q", out, errText)
			}
		})
	}
}

// Mutation captured: skipping the phase reader incorrectly exposes a completed report from the previous task.
func TestNowriteStatusCompactPhaseThroughCLI(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		code        int
	}{
		{"live", "compacting", 0},
		{"reused pid", "compact-interrupted", 9},
		{"invalid record", "compact-unknown", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			nr := newNowriteReads(t, true)
			started, live := platform.ReadProc(os.Getpid(), nr.f.env)
			if live != platform.ProcRunning || started == "" {
				t.Fatalf("cannot read test process identity: %q/%v", started, live)
			}
			if tc.name == "reused pid" {
				started = "different process start"
			}
			brief := filepath.Join(nr.f.root, "pending.md")
			raw, err := json.Marshal(map[string]any{
				"version": 1, "token": "probe", "pane": "p-worker", "brief": brief,
				"pid": os.Getpid(), "started": started, "created_at": "2026-10-04T12:00:00Z",
			})
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "invalid record" {
				raw = []byte("{")
			}
			marker := core.CompactPendingPath(nr.sd, "worker")
			if err := os.MkdirAll(filepath.Dir(marker), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(marker, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			code, out, errText := nr.runNowrite(t, "status", "worker")
			if code != tc.code || !strings.HasPrefix(out, "worker\t"+tc.state+"\t\t") || !strings.Contains(out, "brief was not sent") {
				t.Fatalf("exit=%d stdout=%q stderr=%q; want %d/%s", code, out, errText, tc.code, tc.state)
			}
			if strings.Contains(out, nr.report) || (tc.name != "invalid record" && !strings.Contains(out, brief)) {
				t.Fatalf("phase must identify unsent brief, not old report: %q", out)
			}
		})
	}
}

// Mutation captured: removing argument validation lets options bypass the NOWRITE invocation contract.
func TestNowriteStatusRejectsOptions(t *testing.T) {
	for _, arg := range []string{"--force", "--help", "-h", ""} {
		t.Run(arg, func(t *testing.T) {
			nr := newNowriteReads(t, true)
			code, out, errText := nr.runNowrite(t, "status", arg)
			if code != 2 || out != "" || !strings.Contains(errText, "rejected: status") {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, errText)
			}
		})
	}
}
