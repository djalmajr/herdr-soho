package wait

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

// Fifteen roster columns like spawn writes: name, pane, kind, role, family,
// created_pane, cwd, started, model, approvals, roles, lane, burst, native,
// effort.
const metricsImplementerRoster = "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\nworker\tp0a\tclaude\timplementer\tanthropic\t\t\t20260930T210000\tclaude-opus\task\timplementer\tbuild\t0\t0\thigh\n"
const metricsReviewerRoster = "# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\nworker\tp0a\tcodex\treviewer\topenai\t\t\t20260930T210000\tgpt-5\task\treviewer\treview\t0\t0\tmax\n"

const metricsImplementerReport = "# Report\n\nSECRET_REPORT_TOKEN_123\n\n| item | state |\n| --- | --- |\n| slice a | [done] |\n| slice b | [done] |\n| slice c | [partial] |\n| slice d | [skipped] |\n"

const metricsBriefBackend = "# Brief — metrics probe\n\nType: backend\n\nSECRET_BRIEF_TOKEN_123 must never reach metrics.jsonl.\n"

const metricsSidecarQueued = `{"version":1,"kind":"claude","model":"claude-opus","effort":"high","submission":"accepted","arrival":"queued","session":"20260930T210000"}`

// metricsFixture holds a settled-report fixture like waitVerdictRun, with a
// brief and a dispatch sidecar beside the report; run() drives WaitFor once.
type metricsFixture struct {
	sd     string
	ctx    *core.Config
	env    platform.Env
	base   string
	report string
}

func newMetricsFixture(t *testing.T, metricsValue, roster, reportBody, brief, sidecar string) *metricsFixture {
	t.Helper()
	f := newQueuedProbeFixture(t, "idle", "5", "done screen\n", nil)
	if err := os.WriteFile(filepath.Join(f.sd, "agents.tsv"), []byte(roster), 0o600); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(filepath.Dir(f.sd))
	report := filepath.Join(f.sd, "reports", "worker.md")
	if err := os.MkdirAll(filepath.Dir(report), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(report, []byte(reportBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.sd, "last-report-worker"), []byte(report+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if brief != "" {
		if err := os.WriteFile(filepath.Join(filepath.Dir(report), "worker.brief.md"), []byte(brief), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if sidecar != "" {
		if err := os.WriteFile(filepath.Join(filepath.Dir(report), "worker.dispatch.json"), []byte(sidecar+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if metricsValue != "" {
		f.ctx.Entries["metrics"] = core.ConfigEntry{Value: metricsValue}
	}
	f.env["HERDR_WORKSPACE_ID"] = "ws"
	return &metricsFixture{sd: f.sd, ctx: f.ctx, env: f.env, base: base, report: report}
}

func (m *metricsFixture) run(t *testing.T) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	oldOut, oldErr := platform.Stdout, platform.Stderr
	platform.Stdout, platform.Stderr = &stdout, &stderr
	code := WaitFor([]string{"worker"}, m.sd, m.ctx, m.env, 5000, false, "")
	platform.Stdout, platform.Stderr = oldOut, oldErr
	return stdout.String(), stderr.String(), code
}

func (m *metricsFixture) lines(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(m.sd, "metrics.jsonl"))
	if err != nil {
		t.Fatalf("metrics.jsonl: %v", err)
	}
	return strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
}

func parseMetricsLine(t *testing.T, raw string) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal([]byte(raw), &value); err != nil {
		t.Fatalf("metrics line is not JSON: %v: %q", err, raw)
	}
	return value
}

func TestMetricsOptIn(t *testing.T) { // mutation: dropping the metrics=on gate records with metrics off
	t.Run("wait: with metrics off (default) the wait settles the report and records nothing", func(t *testing.T) {
		m := newMetricsFixture(t, "", metricsImplementerRoster, metricsImplementerReport, metricsBriefBackend, metricsSidecarQueued)
		out, _, code := m.run(t)
		if code != 0 || !strings.Contains(out, `"status":"done"`) {
			t.Fatalf("code=%d out=%q", code, out)
		}
		if _, err := os.Stat(filepath.Join(m.sd, "metrics.jsonl")); !os.IsNotExist(err) {
			t.Fatalf("metrics.jsonl exists with metrics off: err=%v", err)
		}
	})
}

func TestMetricsLineContent(t *testing.T) { // mutation: recording a path (or brief/report content) leaks it into the line
	t.Run("wait: an implementer report with Type backend records one line with the facts and no path", func(t *testing.T) {
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, metricsBriefBackend, metricsSidecarQueued)
		_, _, code := m.run(t)
		if code != 0 {
			t.Fatalf("code=%d", code)
		}
		lines := m.lines(t)
		if len(lines) != 1 {
			t.Fatalf("want exactly 1 line, got %d: %q", len(lines), lines)
		}
		raw := lines[0]
		// No path of any kind: not the temp dir the fixture lives in, not the
		// state dir, not the report path.
		for _, probe := range []string{m.base, m.sd, m.report} {
			if strings.Contains(raw, probe) {
				t.Fatalf("metrics line contains a path %q: %q", probe, raw)
			}
		}
		if strings.Contains(raw, "SECRET_REPORT_TOKEN_123") || strings.Contains(raw, "SECRET_BRIEF_TOKEN_123") {
			t.Fatalf("metrics line carries report or brief content: %q", raw)
		}
		line := parseMetricsLine(t, raw)
		ts, _ := line["ts"].(string)
		if _, err := time.Parse(time.RFC3339, ts); err != nil || !strings.HasSuffix(ts, "Z") {
			t.Fatalf("ts is not ISO 8601 UTC: %q", ts)
		}
		if line["agent"] != "worker" || line["role"] != "implementer" || line["kind"] != "claude" ||
			line["model"] != "claude-opus" || line["effort"] != "high" || line["family"] != "anthropic" {
			t.Fatalf("roster facts wrong: %q", raw)
		}
		if line["type"] != "backend" {
			t.Fatalf("type wrong: %q", raw)
		}
		if _, ok := line["duration_s"].(float64); !ok {
			t.Fatalf("duration_s missing or not a number: %q", raw)
		}
		if line["session"] != "20260930T210000" || line["arrival"] != "queued" {
			t.Fatalf("sidecar fields wrong: %q", raw)
		}
		items, _ := line["items"].(map[string]any)
		if items == nil || items["done"] != 2.0 || items["partial"] != 1.0 || items["skipped"] != 1.0 {
			t.Fatalf("items wrong: %q", raw)
		}
		for _, key := range []string{"verdict", "verdict_effective", "findings", "severity", "amendment", "partial"} {
			if _, ok := line[key]; ok {
				t.Fatalf("field %q must not appear on an implementer report: %q", key, raw)
			}
		}
	})
}

func TestMetricsReviewLine(t *testing.T) {
	t.Run("wait: a review report records verdict, findings, severity and verdict_effective", func(t *testing.T) {
		m := newMetricsFixture(t, "on", metricsReviewerRoster, reviewReportBody(0, 0, 1, 0, "pass"), metricsBriefBackend, "")
		_, _, code := m.run(t)
		if code != 0 {
			t.Fatalf("code=%d", code)
		}
		lines := m.lines(t)
		if len(lines) != 1 {
			t.Fatalf("want exactly 1 line, got %d", len(lines))
		}
		line := parseMetricsLine(t, lines[0])
		if line["verdict"] != "pass" || line["findings"] != 1.0 || line["verdict_effective"] != "fail" {
			t.Fatalf("review fields wrong: %q", lines[0])
		}
		severity, _ := line["severity"].(map[string]any)
		if severity == nil || severity["P0"] != 0.0 || severity["P1"] != 0.0 || severity["P2"] != 1.0 || severity["P3"] != 0.0 {
			t.Fatalf("severity wrong: %q", lines[0])
		}
		if line["role"] != "reviewer" {
			t.Fatalf("role wrong: %q", lines[0])
		}
	})
}

func TestMetricsOncePerReport(t *testing.T) { // mutation: dropping the metrics-recorded marker records the second wait again
	t.Run("wait: a second wait on the same report keeps one line", func(t *testing.T) {
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, metricsBriefBackend, metricsSidecarQueued)
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("first wait code=%d", code)
		}
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("second wait code=%d", code)
		}
		if lines := m.lines(t); len(lines) != 1 {
			t.Fatalf("want exactly 1 line after two waits, got %d: %q", len(lines), lines)
		}
	})
}

func TestMetricsTypeValue(t *testing.T) { // mutation: accepting any Type: value records unknown slice types
	t.Run("wait: an unknown Type value keeps the type field out", func(t *testing.T) {
		brief := strings.Replace(metricsBriefBackend, "Type: backend", "Type: quantum", 1)
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, brief, "")
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("code=%d", code)
		}
		lines := m.lines(t)
		if len(lines) != 1 {
			t.Fatalf("want exactly 1 line, got %d", len(lines))
		}
		line := parseMetricsLine(t, lines[0])
		if _, ok := line["type"]; ok {
			t.Fatalf("type must be absent for an unknown value: %q", lines[0])
		}
	})
	t.Run("wait: a brief without a Type line has no type field", func(t *testing.T) {
		brief := strings.Replace(metricsBriefBackend, "Type: backend\n\n", "", 1)
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, brief, "")
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("code=%d", code)
		}
		line := parseMetricsLine(t, m.lines(t)[0])
		if _, ok := line["type"]; ok {
			t.Fatalf("type must be absent without a Type line: %q", m.lines(t)[0])
		}
	})
}

func TestMetricsAmendment(t *testing.T) {
	t.Run("wait: a report the pointer lists after a history records amendment true", func(t *testing.T) {
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, "", "")
		// The pointer holds real paths; JSON-escape them, because a Windows
		// path embeds backslashes that would break a hand-joined document.
		taskJSON, err := json.Marshal(m.report + ".current.md")
		if err != nil {
			t.Fatal(err)
		}
		currentJSON, err := json.Marshal(m.report)
		if err != nil {
			t.Fatal(err)
		}
		historyJSON, err := json.Marshal(filepath.Join(m.sd, "reports", "worker-earlier.md"))
		if err != nil {
			t.Fatal(err)
		}
		pointer := `{"version":1,"task_report":` + string(taskJSON) + `,"current":` + string(currentJSON) + `,"history":[` + string(historyJSON) + `]}`
		if err := os.WriteFile(filepath.Join(m.sd, "task-report-worker.json"), []byte(pointer+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("code=%d", code)
		}
		line := parseMetricsLine(t, m.lines(t)[0])
		if line["amendment"] != true {
			t.Fatalf("amendment must be true: %q", m.lines(t)[0])
		}
	})
	t.Run("wait: a report without a pointer history has no amendment field", func(t *testing.T) {
		m := newMetricsFixture(t, "on", metricsImplementerRoster, metricsImplementerReport, "", "")
		if _, _, code := m.run(t); code != 0 {
			t.Fatalf("code=%d", code)
		}
		line := parseMetricsLine(t, m.lines(t)[0])
		if _, ok := line["amendment"]; ok {
			t.Fatalf("amendment must be absent: %q", m.lines(t)[0])
		}
	})
}
