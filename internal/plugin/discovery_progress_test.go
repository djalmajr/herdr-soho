package plugin

import (
	contextpkg "context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/peer"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

func TestBoardCompletedEmptyMachineDropsOnlyItsOldRows(t *testing.T) {
	state := NewBoardState()
	state.Entries = []PickerEntry{{"ref": "local/w1:p1", "machine": "local"}, {"ref": "slow/w2:p1", "machine": "slow"}}
	update := NewBoardState()
	update.Loading = 1
	update.FinishedMachines = batchMachines(peer.SessionResult{Machine: "local"})
	applyBoardUpdate(state, update)
	if len(state.Entries) != 1 || pickerString(state.Entries[0]["machine"]) != "slow" {
		t.Fatalf("empty answered machine retained stale rows or erased pending machine: %#v", state.Entries)
	}
}

// TestPickerDiscoveryPublishesLocalBeforeEnumeration: the local snapshot
// publishes before the machine enumeration even starts - the first
// publication with rows is the local one, while the machine list is still
// blocked on its gate, and the call order is local snapshot, machine list,
// remote snapshot.
func TestPickerDiscoveryPublishesLocalBeforeEnumeration(t *testing.T) {
	dir := t.TempDir()
	gates := filepath.Join(dir, "gates")
	if err := os.MkdirAll(gates, 0o755); err != nil {
		t.Fatal(err)
	}
	listGate := filepath.Join(gates, "list")
	remoteGate := filepath.Join(gates, "remote")
	t.Cleanup(func() {
		_ = os.WriteFile(listGate, []byte("go"), 0o644)
		_ = os.WriteFile(remoteGate, []byte("go"), 0o644)
	})
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "w1:p1", name: "local", agent: "codex", status: "idle"}})},
		{Argv: []string{"machine", "list", "--json"}, WaitFile: listGate, Stdout: `[{"label":"remote","enabled":true}]`},
		{Argv: []string{"--machine", "remote", "api", "snapshot"}, WaitFile: remoteGate, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "w1:p1", name: "remote", agent: "grok", status: "idle"}})},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewPickerState()
	type discoveryPub struct {
		local  bool
		remote bool
	}
	var mu sync.Mutex
	pubs := []discoveryPub{}
	publish := func() {
		mu.Lock()
		defer mu.Unlock()
		local, remote := false, false
		for _, entry := range clonePickerState(loaded).Entries {
			switch pickerString(entry["ref"]) {
			case "local/w1:p1":
				local = true
			case "remote/w1:p1":
				remote = true
			}
		}
		pubs = append(pubs, discoveryPub{local: local, remote: remote})
	}
	done := make(chan struct{})
	go func() {
		loadPickerEntries(contextpkg.Background(), loaded, boardEnv(t, dir, herdr), platform.Current(), publish)
		close(done)
	}()
	// The first publication with rows must be the local one, while the
	// machine list is still blocked on its gate.
	localOnly := false
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && !localOnly {
		mu.Lock()
		for _, pub := range pubs {
			if pub.local && !pub.remote {
				localOnly = true
				break
			}
		}
		mu.Unlock()
		if !localOnly {
			time.Sleep(10 * time.Millisecond)
		}
	}
	if !localOnly {
		mu.Lock()
		got := append([]discoveryPub(nil), pubs...)
		mu.Unlock()
		t.Fatalf("no publication carried the local row alone before the enumeration: %#v", got)
	}
	if err := os.WriteFile(listGate, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(remoteGate, []byte("go"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the discovery did not finish after the releases")
	}
	mu.Lock()
	last := pubs[len(pubs)-1]
	mu.Unlock()
	if !last.local || !last.remote {
		t.Fatalf("the final publication is missing rows (local=%v remote=%v)", last.local, last.remote)
	}
	// The call order proves the enumeration started after the local
	// snapshot answered.
	calls, err := fakecli.ReadCallsForConfig(filepath.Join(dir, "herdr.json"))
	if err != nil {
		t.Fatal(err)
	}
	wantArgv := [][]string{
		{"api", "snapshot"},
		{"machine", "list", "--json"},
		{"--machine", "remote", "api", "snapshot"},
	}
	if len(calls) != len(wantArgv) {
		t.Fatalf("Herdr calls=%#v want %d calls", calls, len(wantArgv))
	}
	for i, want := range wantArgv {
		if !stringsEqual(calls[i].Argv, want) {
			t.Fatalf("call %d argv=%#v want %#v", i, calls[i].Argv, want)
		}
	}
}

// TestPickerDiscoveryCapsRemoteQueriesAtFour: with six pending machines the
// pool keeps exactly four remote snapshots in flight (plus the local
// snapshot and the machine list); a fifth remote query does not start while
// four are pending.
func TestPickerDiscoveryCapsRemoteQueriesAtFour(t *testing.T) {
	dir := t.TempDir()
	gates := filepath.Join(dir, "gates")
	if err := os.MkdirAll(gates, 0o755); err != nil {
		t.Fatal(err)
	}
	machines := []string{"m1", "m2", "m3", "m4", "m5", "m6"}
	rules := []fakecli.Rule{{Argv: []string{"api", "snapshot"}, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "w1:p1", name: "local", agent: "codex", status: "idle"}})}}
	listRows := make([]string, 0, len(machines))
	for i, machine := range machines {
		gate := filepath.Join(gates, machine)
		t.Cleanup(func() { _ = os.WriteFile(gate, []byte("go"), 0o644) })
		row := boardSnapshotRow{paneID: "w1:p" + strconv.Itoa(i+1), name: machine, agent: "codex", status: "idle"}
		rules = append(rules, fakecli.Rule{
			Argv:     []string{"--machine", machine, "api", "snapshot"},
			WaitFile: gate,
			Stdout:   boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{row}),
		})
		listRows = append(listRows, fmt.Sprintf(`{"label":%q,"enabled":true}`, machine))
	}
	rules = append(rules, fakecli.Rule{Argv: []string{"machine", "list", "--json"}, Stdout: "[" + strings.Join(listRows, ",") + "]"})
	herdr, err := fakecli.Install(t, dir, "herdr", rules)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &pickerPIDRecorder{}
	oldObserver := pickerProcessStarted
	pickerProcessStarted = recorder.add
	defer func() { pickerProcessStarted = oldObserver }()
	loaded := NewPickerState()
	done := make(chan struct{})
	go func() {
		loadPickerEntries(contextpkg.Background(), loaded, boardEnv(t, dir, herdr), platform.Current(), func() {})
		close(done)
	}()
	// Local snapshot + machine list + the pool's four pending remotes =
	// six children; the fifth remote must not start while four are pending.
	deadline := time.Now().Add(30 * time.Second) // generous: Windows process starts can be slow
	for time.Now().Before(deadline) && len(recorder.snapshot()) < 6 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := len(recorder.snapshot()); got != 6 {
		t.Fatalf("children=%d want 6 (local snapshot, machine list, four remote snapshots)", got)
	}
	time.Sleep(300 * time.Millisecond)
	if got := len(recorder.snapshot()); got != 6 {
		t.Fatalf("children grew to %d: the pool started a fifth remote query while four are pending", got)
	}
	for _, machine := range machines {
		if err := os.WriteFile(filepath.Join(gates, machine), []byte("go"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("the discovery did not finish after the releases")
	}
	if got := len(loaded.Entries); got != 7 {
		t.Fatalf("entries=%d want 7 (local + six remotes)", got)
	}
}

// TestPickerDiscoveryDeadlineCoversMachineList: one global deadline covers
// the local snapshot and the machine list - a slow list is cut off by the
// deadline and reported as a machines failure, the local rows stay, and the
// load settles instead of running for minutes.
func TestPickerDiscoveryDeadlineCoversMachineList(t *testing.T) {
	old := pickerDiscoveryTimeoutMS
	pickerDiscoveryTimeoutMS = 1500
	defer func() { pickerDiscoveryTimeoutMS = old }()
	dir := t.TempDir()
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "w1:p1", name: "local", agent: "codex", status: "idle"}})},
		{Argv: []string{"machine", "list", "--json"}, Delay: 8000},
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded := NewPickerState()
	started := time.Now()
	loadPickerEntries(contextpkg.Background(), loaded, boardEnv(t, dir, herdr), platform.Current(), func() {})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the deadline did not cut the load short: %s", elapsed)
	}
	if len(loaded.Entries) != 1 || pickerString(loaded.Entries[0]["ref"]) != "local/w1:p1" {
		t.Fatalf("entries=%#v want the local row", loaded.Entries)
	}
	if len(loaded.Failures) != 1 || loaded.Failures[0].Label != "máquinas" || !strings.Contains(loaded.Failures[0].Cause, "timed out") {
		t.Fatalf("failures=%#v want the machines list failure", loaded.Failures)
	}
	if loaded.Loading != 0 || loaded.LoadingLocal {
		t.Fatalf("loading state after the deadline: loading=%d local=%v", loaded.Loading, loaded.LoadingLocal)
	}
}

// TestBoardDiscoveryDeadlineCoversMachineList: the board's refresh passes
// the same global deadline to the engine - a slow machine list is cut off
// and reported as a machines failure, the local rows stay, and the refresh
// settles instead of running for minutes.
func TestBoardDiscoveryDeadlineCoversMachineList(t *testing.T) {
	old := pickerDiscoveryTimeoutMS
	pickerDiscoveryTimeoutMS = 1500
	defer func() { pickerDiscoveryTimeoutMS = old }()
	dir := t.TempDir()
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "w1:p1", name: "local", agent: "codex", status: "idle"}})},
		{Argv: []string{"machine", "list", "--json"}, Delay: 8000},
	})
	if err != nil {
		t.Fatal(err)
	}
	state := NewBoardState()
	started := time.Now()
	loadBoardEntries(contextpkg.Background(), state, boardEnv(t, dir, herdr), platform.Current(), func() {})
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the deadline did not cut the refresh short: %s", elapsed)
	}
	if len(state.Entries) != 1 || pickerString(state.Entries[0]["ref"]) != "local/w1:p1" {
		t.Fatalf("entries=%#v want the local row", state.Entries)
	}
	if len(state.Failures) != 1 || state.Failures[0].Label != "máquinas" || !strings.Contains(state.Failures[0].Cause, "timed out") {
		t.Fatalf("failures=%#v want the machines list failure", state.Failures)
	}
}

// TestPickerDiscoveryCancelStopsSubprocesses: canceling the load context
// (the close) kills the running snapshot subprocess, the load settles with a
// visible failure instead of hanging, and no child is left running.
func TestPickerDiscoveryCancelStopsSubprocesses(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "gate")
	t.Cleanup(func() { _ = os.WriteFile(gate, []byte("go"), 0o644) })
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, WaitFile: gate},
		{Argv: []string{"machine", "list", "--json"}, Stdout: "[]"},
	})
	if err != nil {
		t.Fatal(err)
	}
	recorder := &pickerPIDRecorder{}
	oldObserver := pickerProcessStarted
	pickerProcessStarted = recorder.add
	defer func() { pickerProcessStarted = oldObserver }()
	ctx, cancel := contextpkg.WithCancel(contextpkg.Background())
	loaded := NewPickerState()
	done := make(chan struct{})
	go func() {
		loadPickerEntries(ctx, loaded, boardEnv(t, dir, herdr), platform.Current(), func() {})
		close(done)
	}()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) && len(recorder.snapshot()) < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	pids := recorder.snapshot()
	if len(pids) == 0 {
		t.Fatal("the local snapshot subprocess never started")
	}
	started := time.Now()
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the discovery did not finish after the cancel")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("the cancel did not stop the snapshot in time: %s", elapsed)
	}
	for _, pid := range pids {
		if pickerPIDAlive(pid) {
			t.Errorf("child PID %d still alive after the cancel", pid)
		}
	}
	if len(loaded.Failures) == 0 {
		t.Fatalf("the canceled discovery recorded no failure: %#v", loaded)
	}
}

// TestBoardDiscoveryKeepsOldRowsUntilTheMachineAnswers: while the refresh's
// remote snapshot is in flight, the machine that has not answered keeps all
// of its old rows; once it answers, only the new rows survive.
func TestBoardDiscoveryKeepsOldRowsUntilTheMachineAnswers(t *testing.T) {
	dir := t.TempDir()
	gate := filepath.Join(dir, "slow-gate")
	t.Cleanup(func() { _ = os.WriteFile(gate, []byte("go"), 0o644) })
	herdr, err := fakecli.Install(t, dir, "herdr", []fakecli.Rule{
		{Argv: []string{"api", "snapshot"}, Stdout: boardSnapshotJSON("w1", "alpha", []boardSnapshotRow{{paneID: "n1", name: "local-2", agent: "codex", status: "idle"}})},
		{Argv: []string{"--machine", "slow", "api", "snapshot"}, WaitFile: gate, Stdout: boardSnapshotJSON("w1", "beta", []boardSnapshotRow{{paneID: "n1", name: "slow-2", agent: "codex", status: "working"}})},
		{Argv: []string{"machine", "list", "--json"}, Stdout: `[{"label":"slow","enabled":true}]`},
	})
	if err != nil {
		t.Fatal(err)
	}
	live := NewBoardState()
	live.Entries = []PickerEntry{
		boardEntry("local/o1", "local", "w1", "alpha", "local-1", "codex", "idle", ""),
		boardEntry("slow/o1", "slow", "w1", "beta", "slow-1", "codex", "idle", ""),
		boardEntry("slow/o2", "slow", "w1", "beta", "slow-3", "codex", "idle", ""),
	}
	loaded := NewBoardState()
	midRows := -1
	released := false
	loadBoardEntries(contextpkg.Background(), loaded, boardEnv(t, dir, herdr), platform.Current(), func() {
		applyBoardUpdate(live, cloneBoardState(loaded))
		if !loaded.LoadingLocal && len(loaded.Entries) > 0 {
			if midRows < 0 {
				midRows = len(boardRefs(live.Entries))
			}
			if !released {
				released = true
				_ = os.WriteFile(gate, []byte("go"), 0o644)
			}
		}
	})
	loaded.UpdatedAt = boardNow().Format("15:04:05")
	applyBoardUpdate(live, cloneBoardState(loaded))
	if midRows != 3 {
		t.Fatalf("mid-load rows=%d want 3 (the old slow rows stay until slow answers)", midRows)
	}
	if got := boardRefs(live.Entries); len(got) != 2 || got[0] != "local/n1" || got[1] != "slow/n1" {
		t.Fatalf("final rows=%v want only the new load's rows", got)
	}
}

// stringsEqual compares two argv slices.
func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
