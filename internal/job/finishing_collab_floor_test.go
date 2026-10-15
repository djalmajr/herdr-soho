package job

import (
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// finishing_collab_floor_test.go pins that a bounded team adapter never
// launches a subprocess without a deadline: a positive remainder below one
// millisecond is a one-millisecond deadline, not the "no timeout" a zero
// would mean to the platform runner.

// floorHang is how long the fake soho sleeps: far beyond the bound the
// regressions allow, short enough that the unfixed code ends the test on its
// own instead of hanging the suite.
const floorHang = 20000

// floorBound is the most a sub-millisecond-bounded command may take: the
// 1 ms deadline plus the platform WaitDelay of 1 s for the killed command,
// plus 1 s of room for the spawn.
const floorBound = 2 * time.Second

func hangingSelf(t *testing.T) *teamOpsFix {
	t.Helper()
	return newTeamOpsFix(t, []fakecli.Rule{{AnyArgs: true, Delay: floorHang}})
}

// A selfCLI bounded to 500 µs that runs a command which would hang returns
// the operation error quickly, through the adapter's runStepIn path (Roster)
// and through its own Status path.
func TestSelfCLIBoundedSubMillisecondRemainderStillHasADeadline(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(ops teamOps) error
	}{
		{"roster (runStepIn)", func(ops teamOps) error { _, err := ops.Roster(); return err }},
		{"status (own RunOptions)", func(ops teamOps) error { _, _, err := ops.Status("w1"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := hangingSelf(t)
			cli := f.cli()
			cli.Timeout = 120 * time.Second
			ops := cli.bounded(500 * time.Microsecond)
			start := time.Now()
			err := tc.run(ops)
			elapsed := time.Since(start)
			t.Logf("%s bounded to 500µs: returned in %v (err=%v)", tc.name, elapsed, err)
			if err == nil {
				t.Errorf("a command that hangs returned no error")
			}
			if elapsed > floorBound {
				t.Errorf("took %v, want at most %v: the command ran without a deadline", elapsed, floorBound)
			}
		})
	}
}

// The same through the supervisor's collabOps: a collaboration step with
// 500 µs left hands the dispatch and roster reads an adapter that is still
// bounded.
func TestCollabOpsSubMillisecondRemainderStillHasADeadline(t *testing.T) {
	f := hangingSelf(t)
	cli := f.cli()
	cli.Timeout = 120 * time.Second
	clock := &fakeClock{now: time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)}
	s := &Supervisor{Ops: cli, Clock: clock, askDeadline: int64(500 * time.Microsecond)}
	ops, ok := s.collabOps()
	if !ok {
		t.Fatal("collabOps reported no time left for a 500µs remainder")
	}
	start := time.Now()
	_, err := ops.Roster()
	elapsed := time.Since(start)
	t.Logf("collabOps with 500µs left: roster returned in %v (err=%v)", elapsed, err)
	if err == nil {
		t.Errorf("a command that hangs returned no error")
	}
	if elapsed > floorBound {
		t.Errorf("took %v, want at most %v: the command ran without a deadline", elapsed, floorBound)
	}
}

// The conversion keeps the explicit zero as "no deadline" and gives every
// positive duration, however small or fractional, a finite whole-millisecond
// deadline of at least one: the value no bounded adapter may hand the runner
// is TimeoutMs 0.
func TestTimeoutMsRoundsPositiveDurationsUpAndKeepsExplicitZero(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want int
	}{
		{0, 0},
		{-time.Second, 0},
		{time.Nanosecond, 1},
		{500 * time.Microsecond, 1},
		{time.Millisecond - 1, 1},
		{time.Millisecond, 1},
		{time.Millisecond + 1, 2},
		{1500 * time.Microsecond, 2},
		{2 * time.Second, 2000},
		{120 * time.Second, 120000},
	} {
		if got := timeoutMs(tc.d); got != tc.want {
			t.Errorf("timeoutMs(%v) = %d, want %d", tc.d, got, tc.want)
		}
	}
}

// A selfCLI bounded to a sub-millisecond remainder computes a finite, positive
// deadline for its subprocess: the adapter's timeout is the remainder itself
// and the platform deadline derived from it is 1 ms, never 0.
func TestSelfCLIBoundedSubMillisecondComputesAFiniteDeadline(t *testing.T) {
	for _, remainder := range []time.Duration{time.Nanosecond, 500 * time.Microsecond, time.Millisecond - 1} {
		bounded, ok := selfCLI{Exe: "x", Dir: "d", Timeout: 120 * time.Second}.bounded(remainder).(selfCLI)
		if !ok || bounded.timeout() != remainder {
			t.Fatalf("bounded(%v) = %+v, want the remainder as the adapter timeout", remainder, bounded)
		}
		if got := timeoutMs(bounded.timeout()); got != 1 {
			t.Errorf("bounded(%v) runs with TimeoutMs %d, want 1 (0 would run without a deadline)", remainder, got)
		}
	}
}
