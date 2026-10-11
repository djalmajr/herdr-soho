package platform

import (
	"os"
	"os/exec"
	"testing"
)

// TestProcExec reads a child's executable and exact arguments, and fails
// for a pid that exited.
func TestProcExec(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self, "-test.run=^TestProcExecHelper$", "--", "job", "supervise", "--id", "job 1")
	cmd.Env = append(os.Environ(), "PROC_EXEC_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	exe, argv, readErr := ProcExec(pid)
	_ = stdin.Close()
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if readErr != nil {
		t.Fatalf("ProcExec(%d): %v", pid, readErr)
	}
	want := []string{"-test.run=^TestProcExecHelper$", "--", "job", "supervise", "--id", "job 1"}
	if len(argv) != len(want)+1 {
		t.Fatalf("argv = %q, want %q after argv[0]", argv, want)
	}
	for i, arg := range want {
		if argv[i+1] != arg {
			t.Fatalf("argv = %q, want %q after argv[0]", argv, want)
		}
	}
	a, errA := os.Stat(exe)
	b, errB := os.Stat(self)
	if errA != nil || errB != nil || !os.SameFile(a, b) {
		t.Fatalf("exe = %q, want the file %q (%v %v)", exe, self, errA, errB)
	}
	if _, _, err := ProcExec(pid); err == nil {
		t.Fatalf("ProcExec of the exited pid %d succeeded", pid)
	}
	if _, _, err := ProcExec(0); err == nil {
		t.Fatal("ProcExec(0) succeeded")
	}
}

// TestProcExecHelper blocks on stdin when run as TestProcExec's child.
func TestProcExecHelper(t *testing.T) {
	if os.Getenv("PROC_EXEC_HELPER") != "1" {
		return
	}
	buf := make([]byte, 1)
	_, _ = os.Stdin.Read(buf)
}
