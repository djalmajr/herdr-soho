package spawn

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/core"
	"github.com/djalmajr/soho/internal/platform"
	"github.com/djalmajr/soho/internal/testutil/fakecli"
)

// DJA-392: above the harder pressure limits (pressure_refuse_disk_free_
// percent, pressure_refuse_swap_percent) the spawn refuses to open a new
// pane (exit 25) before any herdr call that creates a pane or agent; reuse
// is never refused; --force skips only the refusal (the warning stays).
// The core parts rule is covered in internal/core, and the exact stderr
// line (with the "soho: " prefix cli.Run adds) in internal/cli.

const (
	// refuseMessage is the Die message of the refusal; cli.Run prints it as
	// "soho: " + refuseMessage + "\n" on stderr.
	refuseMessage    = "spawn: refusing to open a new pane under resource pressure: "
	pressureWarnLine = "soho: resource pressure: "

	// rosterHeaderOnly is the pre-existing core.StateDir initialization of
	// the roster (it runs before the refusal point, in every spawn); the
	// refusal itself must add nothing to it.
	rosterHeaderOnly = "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n"
)

const pressureGB = int64(1024 * 1024 * 1024)

// fakePressureMachine injects the platform measurements and restores them
// after the test, so no test ever reads the real disk or swap.
func fakePressureMachine(t *testing.T, diskFree, diskTotal int64, diskOK bool, swapUsed, swapTotal int64, swapOK bool) {
	t.Helper()
	oldDisk, oldSwap := platform.DiskFree, platform.SwapUsage
	platform.DiskFree = func(string) (int64, int64, bool) { return diskFree, diskTotal, diskOK }
	platform.SwapUsage = func(platform.Env) (int64, int64, bool) { return swapUsed, swapTotal, swapOK }
	t.Cleanup(func() { platform.DiskFree, platform.SwapUsage = oldDisk, oldSwap })
}

// pressureSpawn runs the real CmdSpawn over the fresh-spawn fixture (no
// caller pane, empty roster) with the measurements faked, the warning
// limits explicit and the refusal limits at the defaults (5/95) unless the
// given KEY=VALUE items override them. message is the Die message of a
// non-zero exit (the panic's, without the "soho: " prefix cli.Run adds).
func pressureSpawn(t *testing.T, args []string, diskFree, diskTotal int64, diskOK bool, swapUsed, swapTotal int64, swapOK bool, limits ...string) (code int, stdout, stderr, message string, calls []fakecli.Call, state string) {
	t.Helper()
	f := newSpawnFixture(t, freshSpawnRules())
	configureSpawnFixture(t, &f)
	f.env["SOHO_PRESSURE_DISK_FREE_PERCENT"] = "15"
	f.env["SOHO_PRESSURE_SWAP_PERCENT"] = "80"
	for _, item := range limits {
		key, value, _ := strings.Cut(item, "=")
		f.env[key] = value
	}
	fakePressureMachine(t, diskFree, diskTotal, diskOK, swapUsed, swapTotal, swapOK)
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut strings.Builder
	platform.Stdout, platform.Stderr = &out, &errOut
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	code = 0
	func() {
		defer func() {
			if value := recover(); value != nil {
				if e, ok := value.(*platform.ExitError); ok {
					code, message = e.Code, e.Msg
					return
				}
				panic(value)
			}
		}()
		CmdSpawn(args, f.ctx, f.env, f.cwd)
	}()
	calls, _ = fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	return code, out.String(), errOut.String(), message, calls, f.state
}

// assertNoPaneOrAgentCreation proves the logged herdr calls contain nothing
// on the creation side (the refusal point runs before any of them).
func assertNoPaneOrAgentCreation(t *testing.T, calls []fakecli.Call) {
	t.Helper()
	for _, call := range calls {
		if len(call.Argv) < 2 {
			continue
		}
		verb := call.Argv[0] + " " + call.Argv[1]
		switch verb {
		case "agent list", "agent get", "pane list", "pane layout", "agent read":
		default:
			t.Fatalf("a herdr call beyond the pre-refusal reads ran: %#v", call.Argv)
		}
	}
}

// assertRefusedNoCreation proves the refusal touched neither Herdr
// (creation side) nor the roster: the logged calls are only the reads that
// already happen before the refusal point, and the roster holds no line
// (only the pre-existing header initialization of core.StateDir).
func assertRefusedNoCreation(t *testing.T, calls []fakecli.Call, state string) {
	t.Helper()
	assertNoPaneOrAgentCreation(t, calls)
	data, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
	if err == nil && strings.TrimSpace(string(data)) != strings.TrimSpace(rosterHeaderOnly) {
		t.Fatalf("the refusal wrote roster state: %q", data)
	}
}

func TestSpawnRefusesToOpenANewPaneUnderExtremePressure(t *testing.T) {
	t.Run("disk under the 5 default refuses with the disk part", func(t *testing.T) {
		code, _, stderr, message, calls, state := pressureSpawn(t, []string{"worker"}, 3*pressureGB, 100*pressureGB, true, 0, 0, false)
		if code != 25 {
			t.Fatalf("exit=%d, want 25: message=%q", code, message)
		}
		stateDir := filepath.Join(state, "ws")
		want := refuseMessage + "disk " + stateDir + " has 3% free (3 GB of 100 GB); close idle panes or run 'soho gc', or pass --force"
		if message != want {
			t.Fatalf("message=%q, want %q", message, want)
		}
		// The warning of today still goes out (3% is also under the 15
		// warning limit).
		if !strings.Contains(stderr, pressureWarnLine+"disk "+stateDir+" has 3% free (3 GB of 100 GB); run 'soho gc' and close idle panes before opening more\n") {
			t.Fatalf("the warning line is missing: %q", stderr)
		}
		assertRefusedNoCreation(t, calls, state)
	})

	t.Run("swap above the 95 default refuses with the swap part", func(t *testing.T) {
		code, _, _, message, calls, state := pressureSpawn(t, []string{"worker"}, 90*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true)
		if code != 25 {
			t.Fatalf("exit=%d, want 25: message=%q", code, message)
		}
		want := refuseMessage + "swap 96% used (24 GB of 25 GB); close idle panes or run 'soho gc', or pass --force"
		if message != want {
			t.Fatalf("message=%q, want %q", message, want)
		}
		assertRefusedNoCreation(t, calls, state)
	})

	t.Run("both limits passed refuses with both parts in the warning order", func(t *testing.T) {
		code, _, _, message, calls, state := pressureSpawn(t, []string{"worker"}, 3*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true)
		if code != 25 {
			t.Fatalf("exit=%d, want 25: message=%q", code, message)
		}
		stateDir := filepath.Join(state, "ws")
		want := refuseMessage +
			"disk " + stateDir + " has 3% free (3 GB of 100 GB); swap 96% used (24 GB of 25 GB); " +
			"close idle panes or run 'soho gc', or pass --force"
		if message != want {
			t.Fatalf("message=%q, want %q", message, want)
		}
		assertRefusedNoCreation(t, calls, state)
	})

	t.Run("--force opens and keeps the warning", func(t *testing.T) {
		code, stdout, stderr, _, calls, state := pressureSpawn(t, []string{"worker", "--force"}, 90*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true)
		if code != 0 {
			t.Fatalf("exit=%d, want 0: stderr=%q", code, stderr)
		}
		if !strings.Contains(stdout, `"status"`) {
			t.Fatalf("the forced spawn printed no JSON: %q", stdout)
		}
		if !strings.Contains(stderr, pressureWarnLine+"swap 96% used (24 GB of 25 GB); run 'soho gc' and close idle panes before opening more\n") {
			t.Fatalf("--force dropped the warning: %q", stderr)
		}
		created := false
		for _, call := range calls {
			if len(call.Argv) >= 2 && ((call.Argv[0] == "pane" && call.Argv[1] == "split") || (call.Argv[0] == "tab" && call.Argv[1] == "create")) {
				created = true
			}
		}
		if !created {
			t.Fatalf("--force did not open the pane: calls=%#v", calls)
		}
		data, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
		if err != nil || strings.Count(string(data), "\n") < 2 {
			t.Fatalf("the forced spawn wrote no roster line: %q err=%v", data, err)
		}
	})

	t.Run("a limit of 0 disables the refusal and the spawn opens", func(t *testing.T) {
		// Each case disables one part while only that part is under
		// pressure; the other resource stays calm.
		for _, tc := range []struct {
			limit string
			disk  [3]int64 // free, total, ok (ok=1)
			swap  [3]int64
		}{
			{"SOHO_PRESSURE_REFUSE_SWAP_PERCENT=0", [3]int64{90 * pressureGB, 100 * pressureGB, 1}, [3]int64{24 * pressureGB, 25 * pressureGB, 1}},
			{"SOHO_PRESSURE_REFUSE_DISK_FREE_PERCENT=0", [3]int64{3 * pressureGB, 100 * pressureGB, 1}, [3]int64{0, 0, 0}},
		} {
			code, _, stderr, _, _, state := pressureSpawn(t, []string{"worker"}, tc.disk[0], tc.disk[1], tc.disk[2] == 1, tc.swap[0], tc.swap[1], tc.swap[2] == 1, tc.limit)
			if code != 0 {
				t.Fatalf("%s still refused: exit=%d stderr=%q", tc.limit, code, stderr)
			}
			// The warning keeps firing (the warning limits are untouched).
			if !strings.Contains(stderr, pressureWarnLine) {
				t.Fatalf("the warning was lost: %q", stderr)
			}
			data, err := os.ReadFile(filepath.Join(state, "ws", "agents.tsv"))
			if err != nil || strings.Count(string(data), "\n") < 2 {
				t.Fatalf("the spawn wrote no roster line: %q err=%v", data, err)
			}
		}
	})

	t.Run("a measurement failure never refuses", func(t *testing.T) {
		code, _, stderr, _, _, _ := pressureSpawn(t, []string{"worker"}, 0, 0, false, 0, 0, false)
		if code != 0 {
			t.Fatalf("a failed measurement refused: exit=%d stderr=%q", code, stderr)
		}
		if strings.Contains(stderr, pressureWarnLine) {
			t.Fatalf("a failed measurement spoke: %q", stderr)
		}
	})

	t.Run("the band between the warning and the refusal limit only warns", func(t *testing.T) {
		// 10% free warns (15) and does not refuse (5); 90% swap warns (80)
		// and does not refuse (95).
		code, stdout, stderr, _, _, state := pressureSpawn(t, []string{"worker"}, 10*pressureGB, 100*pressureGB, true, 9*pressureGB, 10*pressureGB, true)
		if code != 0 {
			t.Fatalf("the warning band refused: exit=%d stderr=%q", code, stderr)
		}
		if !strings.Contains(stdout, `"status"`) {
			t.Fatalf("the band spawn printed no JSON: %q", stdout)
		}
		stateDir := filepath.Join(state, "ws")
		if !strings.Contains(stderr, pressureWarnLine+"disk "+stateDir+" has 10% free (10 GB of 100 GB); swap 90% used (9 GB of 10 GB); run 'soho gc' and close idle panes before opening more\n") {
			t.Fatalf("the band did not warn with both parts: %q", stderr)
		}
	})

	t.Run("an invalid refusal limit uses the default (refuses)", func(t *testing.T) {
		code, _, stderr, message, _, _ := pressureSpawn(t, []string{"worker"}, 90*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true, "SOHO_PRESSURE_REFUSE_SWAP_PERCENT=abc")
		if code != 25 {
			t.Fatalf("exit=%d, want 25 (the invalid value falls back to the 95 default): message=%q", code, message)
		}
		if !strings.Contains(stderr, "soho: warning: invalid value 'abc' for pressure_refuse_swap_percent; using the default 95\n") {
			t.Fatalf("the invalid-value warning is missing: %q", stderr)
		}
		want := refuseMessage + "swap 96% used (24 GB of 25 GB); close idle panes or run 'soho gc', or pass --force"
		if message != want {
			t.Fatalf("message=%q, want %q", message, want)
		}
	})
}

func TestSpawnRefusalDoesNotTouchIdleReuse(t *testing.T) {
	// Reuse is never refused: under extreme pressure a spawn that finds an
	// idle worker of the same role, kind and model reuses it (no new pane).
	f := newSpawnFixture(t, []fakecli.Rule{idleHerdrRule("idle-worker")})
	configureSpawnFixture(t, &f)
	f.env["SOHO_PRESSURE_DISK_FREE_PERCENT"] = "15"
	f.env["SOHO_PRESSURE_SWAP_PERCENT"] = "80"
	fakePressureMachine(t, 3*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true)
	row := strings.Join([]string{"idle-worker", "p-idle-worker", "grok", "worker", "xai", "1", f.cwd, "now", "grok-4.7", "ask", "worker", "", "", "", "high"}, "\t")
	f.roster(t, row)
	before, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut strings.Builder
	platform.Stdout, platform.Stderr = &out, &errOut
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	code := 0
	func() {
		defer func() {
			if value := recover(); value != nil {
				if e, ok := value.(*platform.ExitError); ok {
					code = e.Code
					return
				}
				panic(value)
			}
		}()
		CmdSpawn([]string{"worker", "--model", "grok-4.7"}, f.ctx, f.env, f.cwd)
	}()
	calls, _ := fakecli.ReadCalls(filepath.Join(f.bin, "herdr.calls.jsonl"))
	if code != 0 {
		t.Fatalf("reuse under pressure refused: exit=%d stderr=%q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "reusing idle worker 'idle-worker'") {
		t.Fatalf("the spawn did not reuse the idle worker: stdout=%q stderr=%q", out.String(), errOut.String())
	}
	assertNoPaneOrAgentCreation(t, calls)
	after, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("reuse under pressure changed the roster: before=%q after=%q err=%v", before, after, err)
	}
}

// DJA-392 review: on a lane with a gone worker, a refused spawn still drops
// the gone row (housekeeping every spawn does) but writes no friction line:
// the "opening a new pane" note would be false under the refusal.
func TestLanePressureRefusalWritesNoFriction(t *testing.T) {
	rules := []fakecli.Rule{{Argv: []string{"agent", "get", "gone-worker"}, Code: 1, Stderr: `{"error":{"code":"agent_not_found","message":"gone"}}`}}
	rules = append(rules, freshSpawnRules()...)
	f := newSpawnFixture(t, rules)
	configureSpawnFixture(t, &f)
	f.env["SOHO_LANES"] = "on"
	f.ctx.Entries["lane_build_roles"] = core.ConfigEntry{Value: "worker", Source: "project"}
	f.ctx.Entries["lane_build_kind"] = core.ConfigEntry{Value: "grok", Source: "project"}
	f.ctx.Entries["lane_build_panes"] = core.ConfigEntry{Value: "2", Source: "project"}
	f.ctx.Order = append(f.ctx.Order, "lane_build_roles", "lane_build_kind", "lane_build_panes")
	row := strings.Join([]string{"gone-worker", "p-old", "grok", "worker", "xai", "1", f.cwd, "now", "grok-4.7", "ask", "worker", "build", "", "", "high"}, "\t")
	f.roster(t, row)
	fakePressureMachine(t, 3*pressureGB, 100*pressureGB, true, 24*pressureGB, 25*pressureGB, true)
	code, _, stderr, calls := runCmdSpawn(t, f, []string{"worker"})
	if code != 25 {
		t.Fatalf("exit=%d stderr=%q, want the refusal", code, stderr)
	}
	assertNoPaneOrAgentCreation(t, calls)
	if friction, err := os.ReadFile(filepath.Join(f.state, "ws", "friction.log")); err == nil && len(friction) > 0 {
		t.Fatalf("the refusal appended friction: %s", friction)
	}
	roster, err := os.ReadFile(filepath.Join(f.state, "ws", "agents.tsv"))
	if err != nil || strings.Contains(string(roster), "gone-worker") {
		t.Fatalf("the gone row should still be dropped: %q err=%v", roster, err)
	}
}
