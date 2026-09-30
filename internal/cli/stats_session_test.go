package cli

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func (f *statsFixture) acceptedSession(kind, model, effort, session string) string {
	f.t.Helper()
	if session == "" {
		return f.accepted(kind, model, effort)
	}
	return fmt.Sprintf(`{"version":1,"submission":"accepted","kind":%q,"model":%q,"effort":%q,"session":%q}`, kind, model, effort, session)
}

func TestStatsSessionReuse(t *testing.T) {
	t.Run("a second non-amendment brief in the same session is a reuse", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T085143"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T085143"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":1,"briefs":2,"amendments":0,"reuses":1`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("the same agent name in a different session counts two tasks", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T085143"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T091000"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":2,"briefs":2,"amendments":0,"reuses":0`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("without a session a model change counts two tasks", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.accepted("opencode", "m2", "high"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":2,"briefs":2,"amendments":0,"reuses":0`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("without a session a kind change alone counts two tasks", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.accepted("opencode", "m1", "high"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":2,"briefs":2,"amendments":0,"reuses":0`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("without a session the same kind and model counts one task and one reuse", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":1,"briefs":2,"amendments":0,"reuses":1`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("a legacy sidecar and a session sidecar fall back to kind and model", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T091000"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":1,"briefs":2,"amendments":0,"reuses":1`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("a legacy sidecar and a different-kind session sidecar count two tasks", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.accepted("pi", "m1", "high"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.acceptedSession("opencode", "m2", "high", "20260930T091000"))
		out, _, code := f.invoke("stats", "--json")
		if code != 0 || !strings.Contains(out, `"tasker":{"tasks":2,"briefs":2,"amendments":0,"reuses":0`) {
			t.Fatalf("code=%d out=%s", code, out)
		}
	})
	t.Run("briefs appears after tasks in the text table and in the JSON", func(t *testing.T) {
		f := newCommandFixture(t)
		f.pair("build", "20260930T090000", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T085143"))
		f.pair("build", "20260930T090500", "tasker", "done\n", 0, f.acceptedSession("pi", "m1", "high", "20260930T085143"))
		textOut, _, textCode := f.invoke("stats")
		if textCode != 0 {
			t.Fatalf("text code=%d out=%q", textCode, textOut)
		}
		if !strings.Contains(textOut, "tasks  briefs  amendments  reuses  no-report (pending/lost)") {
			t.Fatalf("text header=%q", textOut)
		}
		if !regexp.MustCompile(`(?m)^tasker +1 +2 +0 +1`).MatchString(textOut) {
			t.Fatalf("text row=%q", textOut)
		}
		jsonOut, _, jsonCode := f.invoke("stats", "--json")
		if jsonCode != 0 || !strings.Contains(jsonOut, `"tasker":{"tasks":1,"briefs":2`) {
			t.Fatalf("json code=%d out=%s", jsonCode, jsonOut)
		}
	})
}
