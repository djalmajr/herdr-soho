package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/job"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// jobFix is one machine with an isolated HOME/XDG and a job_repos_root under
// a temporary tree; jobs are created through the internal/job API.
type jobFix struct {
	root, repos, home string
	env               platform.Env
}

func newJobFix(t *testing.T) *jobFix {
	t.Helper()
	root := t.TempDir()
	f := &jobFix{
		root:  root,
		repos: filepath.Join(root, "repos"),
		home:  filepath.Join(root, "home"),
	}
	for _, dir := range []string{f.home, filepath.Join(root, "xdg"), filepath.Join(root, "bin")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	f.env = platform.Env{
		"HOME": f.home, "USERPROFILE": f.home, "XDG_CONFIG_HOME": filepath.Join(root, "xdg"),
		"HERDR_SOHO_SKILL_DIR": "../../skills/herdr-soho", "PATH": filepath.Join(root, "bin"),
	}
	userFile := platform.UserConfigPath(platform.Current(), f.env)
	if err := os.MkdirAll(filepath.Dir(userFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(userFile, []byte("job_repos_root="+f.repos+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *jobFix) checkout(t *testing.T, org, repo string) string {
	t.Helper()
	dir := filepath.Join(f.repos, org, repo)
	if err := os.MkdirAll(filepath.Join(dir, ".herdr-soho", "jobs"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (f *jobFix) store(t *testing.T, checkout string) *job.Store {
	t.Helper()
	return job.Open(filepath.Join(checkout, ".herdr-soho"))
}

const jobTestBrief = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/cli"}]}`

func (f *jobFix) startJob(t *testing.T, checkout string) {
	t.Helper()
	if _, err := f.store(t, checkout).Start("job-1", []byte(jobTestBrief)); err != nil {
		t.Fatal(err)
	}
}

// setStatus writes the job's state.json directly (no lifecycle validation).
func (f *jobFix) setStatus(t *testing.T, checkout, id, status string) {
	t.Helper()
	body := fmt.Sprintf(`{"schema":1,"id":%q,"status":%q,"brief_sha256":"x","decisions_acked_seq":0,"motivo":null}`, id, status)
	if err := os.WriteFile(filepath.Join(checkout, ".herdr-soho", "jobs", id, "state.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *jobFix) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return f.runEnv(t, f.env, args...)
}

func (f *jobFix) runEnv(t *testing.T, env platform.Env, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &out, &errOut
	code := Run(append([]string{"job"}, args...), env)
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return code, out.String(), errOut.String()
}

type treeEntry struct {
	size    int64
	modTime int64
	mode    os.FileMode
}

// treeSnapshot records (path, size, mtime, mode) of every entry below root,
// files and directories. It stats each path with os.Lstat instead of the
// WalkDir entry's d.Info, whose metadata on Windows comes from the directory
// enumeration and is updated lazily, so two walks of an unchanged tree can
// differ.
func treeSnapshot(t *testing.T, root string) map[string]treeEntry {
	t.Helper()
	snap := map[string]treeEntry{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		snap[path] = treeEntry{size: info.Size(), modTime: info.ModTime().UnixNano(), mode: info.Mode()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// jobTreeChanges lists the paths that differ between two tree snapshots,
// sorted: paths present in after that are new (as-is) or whose entry moved
// (with each changed field as "field old->new"), and paths of before missing
// from after (suffixed " (removed)").
func jobTreeChanges(before, after map[string]treeEntry) []string {
	changed := []string{}
	for path, entry := range after {
		prev, ok := before[path]
		if !ok {
			changed = append(changed, path)
			continue
		}
		fields := []string{}
		if prev.size != entry.size {
			fields = append(fields, fmt.Sprintf("size %d->%d", prev.size, entry.size))
		}
		if prev.modTime != entry.modTime {
			fields = append(fields, fmt.Sprintf("mtime %d->%d", prev.modTime, entry.modTime))
		}
		if prev.mode != entry.mode {
			fields = append(fields, fmt.Sprintf("mode %v->%v", prev.mode, entry.mode))
		}
		if len(fields) > 0 {
			changed = append(changed, fmt.Sprintf("%s (%s)", path, strings.Join(fields, ", ")))
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			changed = append(changed, path+" (removed)")
		}
	}
	sort.Strings(changed)
	return changed
}

// assertJobTreeUnchanged fails t if the two tree snapshots differ, naming the
// changed paths so a CI failure shows what moved.
func assertJobTreeUnchanged(t *testing.T, before, after map[string]treeEntry, msg string) {
	t.Helper()
	if changed := jobTreeChanges(before, after); len(changed) > 0 {
		t.Fatalf("%s: %v", msg, changed)
	}
}

func TestJobTreeSnapshotReportsChangedPaths(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	c := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(a, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("bye"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := treeSnapshot(t, dir)
	// Grow a (size and a fixed mtime), remove b, add c.
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.WriteFile(a, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(a, fixed, fixed); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(b); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	after := treeSnapshot(t, dir)
	// The root's own mtime and size move when its children are created or
	// removed; the report under test is about the three entries, so drop the
	// root from both snapshots.
	delete(before, dir)
	delete(after, dir)
	want := []string{
		a + " (size 5->11, mtime " + strconv.FormatInt(before[a].modTime, 10) + "->" + strconv.FormatInt(fixed.UnixNano(), 10) + ")",
		b + " (removed)",
		c,
	}
	sort.Strings(want)
	if got := jobTreeChanges(before, after); !reflect.DeepEqual(got, want) {
		t.Fatalf("jobTreeChanges=%v want=%v", got, want)
	}
}

func TestJobStatusPrintsTheSnapshotLine(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	code, out, errOut := f.run(t, "status", "--id", "job-1")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	want := `{"id":"job-1","status":"accepted","motivo":null,"eventos":{"total":1,"ultimo_seq":1},"decisions_acked_seq":0,"decisions_pendentes":[]}`
	if out != want+"\n" {
		t.Fatalf("out=%q", out)
	}
}

func TestJobWaitExitByState(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	jobDir := filepath.Join(co, ".herdr-soho", "jobs", "job-1")

	// A blocked job needs a person: exit 7.
	f.setStatus(t, co, "job-1", "blocked")
	code, out, errOut := f.run(t, "wait", "--id", "job-1", "--timeout", "1000")
	if code != 7 || errOut != "" || !strings.Contains(out, `"status":"blocked"`) {
		t.Fatalf("blocked code=%d out=%q err=%q", code, out, errOut)
	}

	for _, tc := range []struct {
		status string
		exit   int
	}{
		{status: "done", exit: 0},
		{status: "failed", exit: 19},
		{status: "timeout", exit: 9},
		{status: "canceled", exit: 21},
	} {
		f.setStatus(t, co, "job-1", tc.status)
		code, _, _ := f.run(t, "wait", "--id", "job-1")
		if code != tc.exit {
			t.Fatalf("%s code=%d", tc.status, code)
		}
	}

	// Collected/closed use the stored report's terminal outcome; no report is 0.
	f.setStatus(t, co, "job-1", "collected")
	code, out, _ = f.run(t, "wait", "--id", "job-1")
	if code != 0 || !strings.Contains(out, `"status":"collected"`) {
		t.Fatalf("collected code=%d out=%q", code, out)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "report.json"), []byte(`{"schema":1,"id":"job-1","status":"failed","motivo":"gate"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ = f.run(t, "wait", "--id", "job-1")
	if code != 19 {
		t.Fatalf("collected failed report code=%d", code)
	}
	f.setStatus(t, co, "job-1", "closed")
	code, _, _ = f.run(t, "wait", "--id", "job-1")
	if code != 19 {
		t.Fatalf("closed failed report code=%d", code)
	}

	// The wait bound expires first: same JSON, exit 9.
	f.setStatus(t, co, "job-1", "accepted")
	code, out, _ = f.run(t, "wait", "--id", "job-1", "--timeout", "150")
	if code != 9 || !strings.Contains(out, `"status":"accepted"`) {
		t.Fatalf("expired code=%d out=%q", code, out)
	}
}

func TestJobEventsPagesAndFlagLimits(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	if _, err := f.store(t, co).Note("job-1", job.EventIn{Tipo: "note", Resumo: "second"}); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := f.run(t, "events", "--id", "job-1")
	if code != 0 || errOut != "" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%q", lines)
	}
	if lines[2] != `{"eventos":"fim","ultimo_seq":2,"estado":"accepted"}` {
		t.Fatalf("trailer=%q", lines[2])
	}

	code, out, _ = f.run(t, "events", "--id", "job-1", "--since", "1")
	if code != 0 || len(strings.Split(strings.TrimSuffix(out, "\n"), "\n")) != 2 {
		t.Fatalf("since 1 code=%d out=%q", code, out)
	}

	// A --since beyond the last seq prints only the trailer.
	code, out, _ = f.run(t, "events", "--id", "job-1", "--since", "99")
	if code != 0 || out != `{"eventos":"fim","ultimo_seq":2,"estado":"accepted"}`+"\n" {
		t.Fatalf("since 99 code=%d out=%q", code, out)
	}

	// The 600000 bound is accepted.
	code, _, _ = f.run(t, "events", "--id", "job-1", "--wait", "600000")
	if code != 0 {
		t.Fatalf("wait 600000 code=%d", code)
	}

	for _, args := range [][]string{
		{"wait", "--id", "job-1", "--timeout", "600001"},
		{"wait", "--id", "job-1", "--timeout", "-1"},
		{"events", "--id", "job-1", "--wait", "600001"},
		{"events", "--id", "job-1", "--since", "-1"},
		{"status", "--id", "bad id"},
		{"status", "--id", "."},
		{"status", "--id", ".."},
		{"status", "--id", "job-1", "--nope"},
		{"status", "--id"},
		{"status", "--id", "job-1", "extra"},
		{"status", "--id", "job-1", "--id", "job-1"},
		{"list", "--state", "bogus"},
	} {
		code, out, errOut := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
}

func TestJobCollect(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	jobDir := filepath.Join(co, ".herdr-soho", "jobs", "job-1")

	// No report.json yet.
	code, out, errOut := f.run(t, "collect", "--id", "job-1")
	if code != 0 || errOut != "" || out != `{"id":"job-1","status":"accepted","report":null}`+"\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}

	// A stored report prints with decisions_pendentes added.
	reportMD := "# report\n"
	if err := os.WriteFile(filepath.Join(jobDir, "report.md"), []byte(reportMD), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(reportMD))
	reportJSON := fmt.Sprintf(`{"schema":1,"id":"job-1","status":"done","motivo":null,"resumo":"ok","pr":null,"artefatos":[{"path":"report.md","sha256":"%s"}]}`, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(filepath.Join(jobDir, "report.json"), []byte(reportJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = f.run(t, "collect", "--id", "job-1")
	if code != 0 {
		t.Fatalf("code=%d out=%q", code, out)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSuffix(out, "\n")), &got); err != nil {
		t.Fatal(err)
	}
	if got["status"] != "done" || got["resumo"] != "ok" || got["id"] != "job-1" {
		t.Fatalf("collect = %#v", got)
	}
	pend, ok := got["decisions_pendentes"]
	if !ok || !reflect.DeepEqual(pend, []any{}) {
		t.Fatalf("decisions_pendentes = %#v", got["decisions_pendentes"])
	}
	if artefatos, ok := got["artefatos"].([]any); !ok || len(artefatos) != 1 {
		t.Fatalf("artefatos = %#v", got["artefatos"])
	}

	// --verify passes, then catches a changed and a missing artefato.
	code, _, _ = f.run(t, "collect", "--id", "job-1", "--verify")
	if code != 0 {
		t.Fatalf("verify code=%d", code)
	}
	if err := os.WriteFile(filepath.Join(jobDir, "report.md"), []byte("# changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errOut = f.run(t, "collect", "--id", "job-1", "--verify")
	if code != 16 || !strings.Contains(errOut, "report.md") {
		t.Fatalf("changed code=%d err=%q", code, errOut)
	}
	missingJSON := `{"schema":1,"id":"job-1","status":"done","artefatos":[{"path":"gone.md","sha256":"` + strings.Repeat("0", 64) + `"}]}`
	if err := os.WriteFile(filepath.Join(jobDir, "report.json"), []byte(missingJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ = f.run(t, "collect", "--id", "job-1", "--verify")
	if code != 16 {
		t.Fatalf("missing code=%d", code)
	}

	// --md prints report.md verbatim; absent report.md exits 4.
	if err := os.WriteFile(filepath.Join(jobDir, "report.md"), []byte(reportMD), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = f.run(t, "collect", "--id", "job-1", "--md")
	if code != 0 || out != reportMD {
		t.Fatalf("md code=%d out=%q", code, out)
	}
	if err := os.Remove(filepath.Join(jobDir, "report.md")); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "collect", "--id", "job-1", "--md")
	if code != 4 || out != "" || !strings.Contains(errOut, "report.md") {
		t.Fatalf("md absent code=%d out=%q err=%q", code, out, errOut)
	}

	// A JSON null report body is a refused exit 2, never a panic, and the
	// file is left untouched.
	nullBody := "null\n"
	if err := os.WriteFile(filepath.Join(jobDir, "report.json"), []byte(nullBody), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "collect", "--id", "job-1")
	if code != 2 || out != "" || !strings.Contains(errOut, "report.json unreadable: expected an object") {
		t.Fatalf("null report code=%d out=%q err=%q", code, out, errOut)
	}
	if got, err := os.ReadFile(filepath.Join(jobDir, "report.json")); err != nil || string(got) != nullBody {
		t.Fatalf("null report file changed: %q (%v)", got, err)
	}

	// Another non-object body keeps the existing exit-2 path (refused while
	// the store reads the report; no panic, empty stdout).
	if err := os.WriteFile(filepath.Join(jobDir, "report.json"), []byte("42\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "collect", "--id", "job-1")
	if code != 2 || out != "" || !strings.Contains(errOut, "cannot unmarshal") {
		t.Fatalf("non-object report code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestJobListCounts(t *testing.T) {
	f := newJobFix(t)
	co1 := f.checkout(t, "example-org", "example-repo")
	co2 := f.checkout(t, "other-org", "other-repo")
	f.startJob(t, co1)
	f.startJob(t, co2)
	f.setStatus(t, co2, "job-1", "blocked")
	brief2 := strings.Replace(jobTestBrief, `"job-1"`, `"job-2"`, 1)
	if _, err := f.store(t, co1).Start("job-2", []byte(brief2)); err != nil {
		t.Fatal(err)
	}
	f.setStatus(t, co1, "job-2", "done")

	code, out, errOut := f.run(t, "list")
	if code != 0 || errOut != "" || out != `{"total":3,"por_estado":{"accepted":1,"blocked":1,"done":1}}`+"\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, _ = f.run(t, "list", "--state", "done")
	if code != 0 || out != `{"total":1,"por_estado":{"done":1}}`+"\n" {
		t.Fatalf("state code=%d out=%q", code, out)
	}
	code, _, _ = f.run(t, "list", "--state", "bogus")
	if code != 2 {
		t.Fatalf("bogus state code=%d", code)
	}

	f2 := newJobFix(t)
	code, out, _ = f2.run(t, "list")
	if code != 0 || out != `{"total":0,"por_estado":{}}`+"\n" {
		t.Fatalf("empty code=%d out=%q", code, out)
	}
}

func TestJobUnknownIdIsNotFound(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	for _, sub := range [][]string{
		{"status", "--id", "nope"},
		{"wait", "--id", "nope"},
		{"events", "--id", "nope"},
		{"collect", "--id", "nope"},
	} {
		code, out, errOut := f.run(t, sub...)
		if code != 3 || out != `{"status":"not_found"}`+"\n" || errOut != "" {
			t.Fatalf("%v code=%d out=%q err=%q", sub, code, out, errOut)
		}
	}

	// A machine without checkouts reports the same.
	f2 := newJobFix(t)
	code, out, _ := f2.run(t, "status", "--id", "job-1")
	if code != 3 || out != `{"status":"not_found"}`+"\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}

	// The same id in two checkouts is refused.
	f3 := newJobFix(t)
	f3.startJob(t, f3.checkout(t, "example-org", "repo-a"))
	f3.startJob(t, f3.checkout(t, "example-org", "repo-b"))
	code, out, errOut := f3.run(t, "status", "--id", "job-1")
	if code != 2 || out != "" || !strings.Contains(errOut, "exists in more than one checkout") {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestJobPhase2SubsValidateThenRefuse(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("amend body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Only supervise still validates and refuses; amend, send, cancel and
	// checkpoint now queue control requests (TestJobControl* below). That
	// is the intentional adaptation of the phase-1 rows for DJA-194 slice 7b.
	for _, args := range [][]string{
		{"supervise", "--id", "job-1"},
	} {
		code, out, errOut := f.run(t, args...)
		want := "herdr-soho: job " + args[0] + ": not available yet\n"
		if code != 2 || out != "" || errOut != want {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, errOut)
		}
	}

	// Unknown ids are not_found before the refusal; missing bodies and bad
	// grace values are exit 2.
	for _, args := range [][]string{
		{"amend", "--id", "nope", bodyFile},
		{"send", "--id", "nope", "-"},
		{"cancel", "--id", "nope"},
		{"checkpoint", "--id", "nope"},
		{"supervise", "--id", "nope"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 3 || out != `{"status":"not_found"}`+"\n" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}
	for _, args := range [][]string{
		{"amend", "--id", "job-1"},
		{"send", "--id", "job-1"},
		{"cancel", "--id", "job-1", "--grace", "-1"},
	} {
		code, out, _ := f.run(t, args...)
		if code != 2 || out != "" {
			t.Fatalf("%v code=%d out=%q", args, code, out)
		}
	}

	code, out, errOut := f.run(t, "gate", "--id", "job-1")
	if code != 2 || out != "" || !strings.Contains(errOut, "unknown subcommand") {
		t.Fatalf("gate code=%d out=%q err=%q", code, out, errOut)
	}
	code, _, _ = f.run(t)
	if code != 2 {
		t.Fatalf("bare job code=%d", code)
	}
}

// jobControlStatusLine is the fresh status line of the fixture job after a
// control request.
const jobControlStatusLine = `{"id":"job-1","status":"accepted","motivo":null,"eventos":{"total":1,"ultimo_seq":1},"decisions_acked_seq":0,"decisions_pendentes":[]}` + "\n"

// TestJobControlAmendSendWriteRequests: amend and send queue the request
// file with the exact body, print the fresh status line and exit 0; the
// body caps, unknown ids and supervise refusal are unchanged.
func TestJobControlAmendSendWriteRequests(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	ctl := filepath.Join(co, ".herdr-soho", "jobs", "job-1", "control")
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("amend body\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	code, out, errOut := f.run(t, "amend", "--id", "job-1", bodyFile)
	if code != 0 || out != jobControlStatusLine || errOut != "" {
		t.Fatalf("amend code=%d out=%q err=%q", code, out, errOut)
	}
	data, err := os.ReadFile(filepath.Join(ctl, "amend-000001.md"))
	if err != nil || string(data) != "amend body\n" {
		t.Fatalf("amend file: %q err=%v", data, err)
	}

	// send from stdin, then amend again: the counter crosses kinds.
	old := jobStdin
	jobStdin = strings.NewReader("send note\n")
	t.Cleanup(func() { jobStdin = old })
	code, out, errOut = f.run(t, "send", "--id", "job-1", "-")
	if code != 0 || out != jobControlStatusLine || errOut != "" {
		t.Fatalf("send code=%d out=%q err=%q", code, out, errOut)
	}
	data, err = os.ReadFile(filepath.Join(ctl, "send-000002.md"))
	if err != nil || string(data) != "send note\n" {
		t.Fatalf("send file: %q err=%v", data, err)
	}
	bodyFile2 := filepath.Join(t.TempDir(), "body2.md")
	if err := os.WriteFile(bodyFile2, []byte("second amend"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, _ = f.run(t, "amend", "--id", "job-1", bodyFile2)
	if code != 0 || out != jobControlStatusLine {
		t.Fatalf("amend 2 code=%d out=%q", code, out)
	}
	data, err = os.ReadFile(filepath.Join(ctl, "amend-000003.md"))
	if err != nil || string(data) != "second amend" {
		t.Fatalf("amend 2 file: %q err=%v", data, err)
	}

	// The caps are unchanged: over 64 KiB amend and over 16 KiB send refuse
	// with exit 2; the exact caps are accepted.
	bigFile := filepath.Join(t.TempDir(), "big.md")
	if err := os.WriteFile(bigFile, []byte(strings.Repeat("a", 64<<10+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f.run(t, "amend", "--id", "job-1", bigFile)
	if code != 2 || out != "" || !strings.Contains(errOut, "exceeds 64 KiB") {
		t.Fatalf("big amend code=%d out=%q err=%q", code, out, errOut)
	}
	if err := os.WriteFile(bigFile, []byte(strings.Repeat("a", 64<<10)), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, _ = f.run(t, "amend", "--id", "job-1", bigFile)
	if code != 0 {
		t.Fatalf("64 KiB amend code=%d", code)
	}
	old2 := jobStdin
	jobStdin = strings.NewReader(strings.Repeat("b", 16<<10+1))
	t.Cleanup(func() { jobStdin = old2 })
	code, out, errOut = f.run(t, "send", "--id", "job-1", "-")
	if code != 2 || out != "" || !strings.Contains(errOut, "exceeds 16 KiB") {
		t.Fatalf("big send code=%d out=%q err=%q", code, out, errOut)
	}

	// Unknown ids are not_found, before any write.
	code, out, errOut = f.run(t, "amend", "--id", "nope", bodyFile)
	if code != 3 || out != `{"status":"not_found"}`+"\n" || errOut != "" {
		t.Fatalf("unknown amend code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = f.run(t, "send", "--id", "nope", "-")
	if code != 3 || out != `{"status":"not_found"}`+"\n" || errOut != "" {
		t.Fatalf("unknown send code=%d out=%q err=%q", code, out, errOut)
	}

	// supervise still validates and refuses.
	code, out, errOut = f.run(t, "supervise", "--id", "job-1")
	if code != 2 || out != "" || errOut != "herdr-soho: job supervise: not available yet\n" {
		t.Fatalf("supervise code=%d out=%q err=%q", code, out, errOut)
	}
}

// TestJobControlCancelCheckpoint: cancel queues {"grace":120} by default
// (or the --grace value), checkpoint queues the empty marker; a repeat while
// one is pending is a no-op with exit 0; a terminal job refuses amend/send
// with exit 2 and writes nothing for cancel/checkpoint.
func TestJobControlCancelCheckpoint(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	ctl := filepath.Join(co, ".herdr-soho", "jobs", "job-1", "control")

	code, out, errOut := f.run(t, "cancel", "--id", "job-1")
	if code != 0 || out != jobControlStatusLine || errOut != "" {
		t.Fatalf("cancel code=%d out=%q err=%q", code, out, errOut)
	}
	data, err := os.ReadFile(filepath.Join(ctl, "cancel.json"))
	if err != nil || string(data) != `{"grace":120}` {
		t.Fatalf("cancel file: %q err=%v", data, err)
	}

	// A second cancel while one is pending is a no-op with exit 0: the
	// first file stays with its original grace.
	code, out, errOut = f.run(t, "cancel", "--id", "job-1", "--grace", "30")
	if code != 0 || out != jobControlStatusLine || errOut != "" {
		t.Fatalf("cancel again code=%d out=%q err=%q", code, out, errOut)
	}
	data, _ = os.ReadFile(filepath.Join(ctl, "cancel.json"))
	if string(data) != `{"grace":120}` {
		t.Fatalf("cancel overwritten: %q", data)
	}

	// --grace applies to a fresh queue: retire the file, then ask again.
	if err := os.Remove(filepath.Join(ctl, "cancel.json")); err != nil {
		t.Fatal(err)
	}
	code, out, _ = f.run(t, "cancel", "--id", "job-1", "--grace", "30")
	if code != 0 || out != jobControlStatusLine {
		t.Fatalf("cancel grace code=%d out=%q", code, out)
	}
	data, err = os.ReadFile(filepath.Join(ctl, "cancel.json"))
	if err != nil || string(data) != `{"grace":30}` {
		t.Fatalf("cancel grace file: %q err=%v", data, err)
	}

	code, out, errOut = f.run(t, "checkpoint", "--id", "job-1")
	if code != 0 || out != jobControlStatusLine || errOut != "" {
		t.Fatalf("checkpoint code=%d out=%q err=%q", code, out, errOut)
	}
	data, err = os.ReadFile(filepath.Join(ctl, "checkpoint"))
	if err != nil || len(data) != 0 {
		t.Fatalf("checkpoint file: %q err=%v", data, err)
	}
	code, out, _ = f.run(t, "checkpoint", "--id", "job-1")
	if code != 0 || out != jobControlStatusLine {
		t.Fatalf("checkpoint again code=%d out=%q", code, out)
	}

	// Unknown ids are not_found for all four subcommands.
	for _, sub := range []string{"cancel", "checkpoint"} {
		code, out, errOut := f.run(t, sub, "--id", "nope")
		if code != 3 || out != `{"status":"not_found"}`+"\n" || errOut != "" {
			t.Fatalf("%s code=%d out=%q err=%q", sub, code, out, errOut)
		}
	}

	// A terminal job: amend and send refuse with exit 2, cancel and
	// checkpoint are no-ops with exit 0 and write nothing.
	f2 := newJobFix(t)
	co2 := f2.checkout(t, "example-org", "example-repo")
	f2.startJob(t, co2)
	f2.setStatus(t, co2, "job-1", "done")
	ctl2 := filepath.Join(co2, ".herdr-soho", "jobs", "job-1", "control")
	bodyFile := filepath.Join(t.TempDir(), "body.md")
	if err := os.WriteFile(bodyFile, []byte("amend body\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, out, errOut = f2.run(t, "amend", "--id", "job-1", bodyFile)
	if code != 2 || out != "" || !strings.Contains(errOut, "job is not active") {
		t.Fatalf("terminal amend code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = f2.run(t, "send", "--id", "job-1", bodyFile)
	if code != 2 || out != "" || !strings.Contains(errOut, "job is not active") {
		t.Fatalf("terminal send code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = f2.run(t, "cancel", "--id", "job-1", "--grace", "30")
	if code != 0 || out == "" || errOut != "" {
		t.Fatalf("terminal cancel code=%d out=%q err=%q", code, out, errOut)
	}
	code, out, errOut = f2.run(t, "checkpoint", "--id", "job-1")
	if code != 0 || out == "" || errOut != "" {
		t.Fatalf("terminal checkpoint code=%d out=%q err=%q", code, out, errOut)
	}
	if entries, err := os.ReadDir(ctl2); err != nil || len(entries) != 0 {
		t.Fatalf("terminal job wrote control files: %v err=%v", entries, err)
	}
}

func TestJobCapabilitiesLine(t *testing.T) {
	var out, errOut bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &out, &errOut
	code := Run([]string{"capabilities", "--json"}, platform.Env{})
	platform.Stdout, platform.Stderr = oldOut, oldErr
	want := `{"schema":1,"worker_collaboration":1,"ephemeral_job":1,"job_events":1}` + "\n"
	if code != 0 || out.String() != want || errOut.Len() != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}

func TestJobNowrite(t *testing.T) {
	f := newJobFix(t)
	co := f.checkout(t, "example-org", "example-repo")
	f.startJob(t, co)
	f.setStatus(t, co, "job-1", "done")
	env := platform.Env{}
	for name, value := range f.env {
		env[name] = value
	}
	env["HERDR_SOHO_NOWRITE"] = "1"

	tree := treeSnapshot(t, f.root)
	for _, args := range [][]string{
		{"status", "--id", "job-1"},
		{"wait", "--id", "job-1", "--timeout", "1000"},
		{"events", "--id", "job-1"},
		{"collect", "--id", "job-1"},
		{"list"},
	} {
		code, out, errOut := f.runEnv(t, env, args...)
		if code != 0 || errOut != "" {
			t.Fatalf("%v code=%d out=%q err=%q", args, code, out, errOut)
		}
	}
	assertJobTreeUnchanged(t, tree, treeSnapshot(t, f.root), "NOWRITE reads changed the tree")

	for _, sub := range []string{"start", "supervise", "amend", "send", "ack", "cancel", "close", "checkpoint", "note"} {
		code, out, errOut := f.runEnv(t, env, sub, "--id", "job-1")
		want := "herdr-soho: HERDR_SOHO_NOWRITE=1 is read-only: job " + sub + " writes; only job status, wait, events, collect and list run\n"
		if code != 2 || out != "" || errOut != want {
			t.Fatalf("%s code=%d out=%q err=%q", sub, code, out, errOut)
		}
	}
}

func TestJobUsage(t *testing.T) {
	if !strings.Contains(usage, "job status") {
		t.Fatal("usage missing the job status line")
	}
	if !strings.Contains(usage, "24 job close") {
		t.Fatal("usage missing the 24 job close exit code")
	}
	for _, banned := range []string{"job gate", "job_gate", " 23 "} {
		if strings.Contains(usage, banned) {
			t.Fatalf("usage contains %q", banned)
		}
	}
	if !strings.Contains(usage, "Only the `job` supervisor pushes the job branch and opens a draft pull request") {
		t.Fatal("usage opening not amended")
	}
	if strings.Contains(usage, "It never commits") {
		t.Fatal("old usage opening still present")
	}

	var out, errOut bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &out, &errOut
	code := Run([]string{"job", "--help"}, platform.Env{})
	platform.Stdout, platform.Stderr = oldOut, oldErr
	if code != 0 || errOut.Len() != 0 || !strings.Contains(out.String(), "herdr-soho job") {
		t.Fatalf("job --help code=%d out=%q err=%q", code, out.String(), errOut.String())
	}
}
