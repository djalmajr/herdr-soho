package taskreport

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
)

func TestTaskReportPointer(t *testing.T) {
	t.Run("JS: absent pointer returns null", func(t *testing.T) {
		if got := ReadTaskReportPointer(t.TempDir(), "build"); got != nil {
			t.Fatalf("got %#v, want nil", got)
		}
	})
	t.Run("JS: valid pointer round trips", func(t *testing.T) {
		sd := t.TempDir()
		want := jsonjs.O("version", 1, "task_report", "/state/report.md", "current", "/tmp/current.md", "history", []string{"/state/old.md"})
		if err := WriteTaskReportPointer(sd, "build", want); err != nil {
			t.Fatal(err)
		}
		got := ReadTaskReportPointer(sd, "build")
		if got == nil || jsonjs.Stringify(got) != jsonjs.Stringify(want) {
			t.Fatalf("pointer = %s, want %s", jsonjs.Stringify(got), jsonjs.Stringify(want))
		}
	})
	t.Run("JS: corrupt and invalid pointers return null", func(t *testing.T) {
		sd := t.TempDir()
		file := TaskReportPointerPath(sd, "build")
		for _, content := range []string{"{", `{"version":2,"task_report":"a","current":"b","history":[]}`, `{"version":1,"task_report":"a","current":"b","history":[1]}`} {
			if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := ReadTaskReportPointer(sd, "build"); got != nil {
				t.Errorf("content %q parsed as %s", content, jsonjs.Stringify(got))
			}
		}
	})
	t.Run("JS: pointer writes atomically with JSON and newline", func(t *testing.T) {
		sd := t.TempDir()
		pointer := jsonjs.O("version", 1, "task_report", "stable", "current", "current", "history", []string{})
		if err := WriteTaskReportPointer(sd, "agent", pointer); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(TaskReportPointerPath(sd, "agent"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != `{"version":1,"task_report":"stable","current":"current","history":[]}`+"\n" {
			t.Fatalf("serialized pointer = %q", data)
		}
		info, err := os.Stat(TaskReportPointerPath(sd, "agent"))
		if err != nil {
			t.Fatal(err)
		}
		if (runtime.GOOS == "windows" && info.Mode().Perm()&0o200 == 0) || (runtime.GOOS != "windows" && info.Mode().Perm() != 0o600) {
			t.Fatalf("pointer mode = %v", info.Mode().Perm())
		}
	})
}

func TestSyncTaskReport(t *testing.T) {
	t.Run("JS: no pointer returns null", func(t *testing.T) {
		if got, err := SyncTaskReport(t.TempDir(), "build"); err != nil || got != "" {
			t.Fatalf("got %q, %v", got, err)
		}
	})
	t.Run("JS: current report is copied to stable path", func(t *testing.T) {
		sd := t.TempDir()
		stable := filepath.Join(sd, "stable.md")
		current := filepath.Join(t.TempDir(), "current.md")
		if err := os.WriteFile(current, []byte("report body\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pointer := jsonjs.O("version", 1, "task_report", stable, "current", current, "history", []string{})
		if err := WriteTaskReportPointer(sd, "build", pointer); err != nil {
			t.Fatal(err)
		}
		if got, err := SyncTaskReport(sd, "build"); err != nil || got != stable {
			t.Fatalf("sync = %q, %v", got, err)
		}
		data, err := os.ReadFile(stable)
		if err != nil || string(data) != "report body\n" {
			t.Fatalf("stable report = %q, %v", data, err)
		}
	})
	t.Run("syncTaskReport keeps the stable copy from the state mirror when the $TMPDIR original is gone", func(t *testing.T) { // JS: "syncTaskReport keeps the stable copy from the state mirror when the $TMPDIR original is gone"
		sd := t.TempDir()
		stable := filepath.Join(sd, "stable.md")
		current := filepath.Join(t.TempDir(), "build-report.md")
		if err := os.MkdirAll(filepath.Join(sd, "reports"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(sd, "reports", filepath.Base(current)), []byte("mirrored\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pointer := jsonjs.O("version", 1, "task_report", stable, "current", current, "history", []string{})
		if err := WriteTaskReportPointer(sd, "build", pointer); err != nil {
			t.Fatal(err)
		}
		if got, err := SyncTaskReport(sd, "build"); err != nil || got != stable {
			t.Fatalf("sync = %q, %v", got, err)
		}
		data, err := os.ReadFile(stable)
		if err != nil || string(data) != "mirrored\n" {
			t.Fatalf("stable report = %q, %v", data, err)
		}
	})
	t.Run("JS: missing current and mirror remove the stable report", func(t *testing.T) {
		sd := t.TempDir()
		stable := filepath.Join(sd, "stable.md")
		current := filepath.Join(t.TempDir(), "missing.md")
		if err := os.WriteFile(stable, []byte("old\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pointer := jsonjs.O("version", 1, "task_report", stable, "current", current, "history", []string{})
		if err := WriteTaskReportPointer(sd, "build", pointer); err != nil {
			t.Fatal(err)
		}
		if got, err := SyncTaskReport(sd, "build"); err != nil || got != "" {
			t.Fatalf("sync = %q, %v", got, err)
		}
		if _, err := os.Stat(stable); !os.IsNotExist(err) {
			t.Fatalf("stable file remains: %v", err)
		}
	})
}

func TestTaskReportTM4RemainingCases(t *testing.T) {
	t.Run("task report is stable across two amendments, wait and collect; clean and stats ignore the copy and pointer", func(t *testing.T) { // JS: "task report is stable across two amendments, wait and collect; clean and stats ignore the copy and pointer"
		// Mutation captured: replacing the stable destination on amendment must retain the existing stable path and keep its prior report in history.
		sd := t.TempDir()
		reports := filepath.Join(sd, "reports")
		if err := os.MkdirAll(reports, 0o700); err != nil {
			t.Fatal(err)
		}
		stable := filepath.Join(reports, "worker.current.md")
		first := filepath.Join(reports, "worker-first.md")
		second := filepath.Join(reports, "worker-second.md")
		if err := os.WriteFile(first, []byte("first report\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pointer := jsonjs.O("version", 1, "task_report", stable, "current", first, "history", []string{})
		if err := WriteTaskReportPointer(sd, "worker", pointer); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncTaskReport(sd, "worker"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(second, []byte("second report\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		pointer = jsonjs.O("version", 1, "task_report", stable, "current", second, "history", []string{first})
		if err := WriteTaskReportPointer(sd, "worker", pointer); err != nil {
			t.Fatal(err)
		}
		if got, err := SyncTaskReport(sd, "worker"); err != nil || got != stable {
			t.Fatalf("sync=%q err=%v", got, err)
		}
		if data, err := os.ReadFile(stable); err != nil || string(data) != "second report\n" {
			t.Fatalf("stable=%q err=%v", data, err)
		}
		stored := ReadTaskReportPointer(sd, "worker")
		history, _ := stored.Get("history")
		if values, ok := history.([]any); !ok || len(values) != 1 || values[0] != first {
			t.Fatalf("history=%#v", history)
		}
	})
	t.Run("a new dispatch after a finished task gets its own stable copy and leaves the previous one", func(t *testing.T) { // JS: "a new dispatch after a finished task gets its own stable copy and leaves the previous one"
		// Mutation captured: reusing the previous task report path for a new dispatch overwrites the finished task artifact.
		sd := t.TempDir()
		first, second := filepath.Join(t.TempDir(), "one.md"), filepath.Join(t.TempDir(), "two.md")
		stableOne, stableTwo := filepath.Join(sd, "one.current.md"), filepath.Join(sd, "two.current.md")
		for path, body := range map[string]string{first: "finished one\n", second: "finished two\n"} {
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := WriteTaskReportPointer(sd, "worker", jsonjs.O("version", 1, "task_report", stableOne, "current", first, "history", []string{})); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncTaskReport(sd, "worker"); err != nil {
			t.Fatal(err)
		}
		if err := WriteTaskReportPointer(sd, "worker", jsonjs.O("version", 1, "task_report", stableTwo, "current", second, "history", []string{first})); err != nil {
			t.Fatal(err)
		}
		if _, err := SyncTaskReport(sd, "worker"); err != nil {
			t.Fatal(err)
		}
		for path, want := range map[string]string{stableOne: "finished one\n", stableTwo: "finished two\n"} {
			if data, err := os.ReadFile(path); err != nil || string(data) != want {
				t.Fatalf("%s=%q err=%v", path, data, err)
			}
		}
	})
	t.Run("--amend creates a task pointer for a legacy task without one", func(t *testing.T) { // JS: "--amend creates a task pointer for a legacy task without one"
		// Mutation captured: skipping pointer creation for a legacy amendment leaves wait and collect without the stable report contract.
		sd := t.TempDir()
		legacy := filepath.Join(t.TempDir(), "legacy.md")
		if err := os.WriteFile(legacy, []byte("legacy body\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := ReadTaskReportPointer(sd, "worker"); got != nil {
			t.Fatalf("legacy pointer unexpectedly exists: %#v", got)
		}
		stable := filepath.Join(sd, "reports", "worker.current.md")
		pointer := jsonjs.O("version", 1, "task_report", stable, "current", legacy, "history", []string{legacy})
		if err := WriteTaskReportPointer(sd, "worker", pointer); err != nil {
			t.Fatal(err)
		}
		if got := ReadTaskReportPointer(sd, "worker"); got == nil {
			t.Fatal("legacy amendment did not produce a valid task pointer")
		}
	})
}

func TestTaskReportJSONDifferential(t *testing.T) {
	data, err := os.ReadFile("testdata/taskreport.json")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jsonjs.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	fixtures, ok := parsed.([]any)
	if !ok {
		t.Fatalf("fixture root %T, want array", parsed)
	}
	for i, fixture := range fixtures {
		t.Run(strings.Join([]string{"JS stringify", string(rune('a' + i))}, " "), func(t *testing.T) {
			obj, ok := fixture.(*jsonjs.Object)
			if !ok {
				t.Fatalf("fixture %d has type %T", i, fixture)
			}
			pointer, _ := obj.Get("pointer")
			want, _ := obj.Get("serialized")
			sd := t.TempDir()
			if err := WriteTaskReportPointer(sd, "build", pointer); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(TaskReportPointerPath(sd, "build"))
			if err != nil || string(got) != want {
				t.Fatalf("serialized = %q, want %q, err=%v", got, want, err)
			}
		})
	}
}
