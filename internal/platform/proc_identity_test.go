package platform

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestProcStartIdentity pins the start identity reader: a running process
// reads the same identity twice, a different process reads another one, a
// process that exited reads as gone, and the name is the executable's base.
func TestProcStartIdentity(t *testing.T) {
	env := EnvFromOS()
	self1, name, live := ProcStartIdentity(os.Getpid(), env)
	if live != ProcRunning || self1 == "" || name == "" {
		t.Fatalf("self: identity=%q name=%q live=%v", self1, name, live)
	}
	self2, _, _ := ProcStartIdentity(os.Getpid(), env)
	if self1 != self2 {
		t.Fatalf("self identity changed: %q then %q", self1, self2)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSuffix(filepath.Base(exe), ".exe")
	if !strings.HasPrefix(base, strings.TrimSuffix(name, ".exe")) {
		t.Fatalf("name = %q, want a prefix of the executable base %q", name, base)
	}
	parent, _, live := ProcStartIdentity(os.Getppid(), env)
	if live != ProcRunning || parent == "" || parent == self1 {
		t.Fatalf("parent: identity=%q live=%v (self %q)", parent, live, self1)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	child := cmd.Process.Pid
	childID, _, childLive := ProcStartIdentity(child, env)
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if childLive == ProcRunning && childID == self1 {
		t.Fatalf("child read the parent's identity %q", childID)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, _, after := ProcStartIdentity(child, env)
		if after == ProcGone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("exited child pid %d reads %v, want gone", child, after)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if id, n, l := ProcStartIdentity(0, env); id != "" || n != "" || l != ProcUnknown {
		t.Fatalf("pid 0 = %q %q %v", id, n, l)
	}
}
