package cli

import (
	"strings"
	"testing"
)

func TestPromotePublicCommand(t *testing.T) {
	t.Run("help needs no Herdr session", func(t *testing.T) {
		env, cwd := commandFixture(t)
		delete(env, "HERDR_ENV")
		code, out, stderr := runIn(t, []string{"promote", "--help"}, env, cwd)
		if code != 0 || stderr != "" || !strings.Contains(out, "herdr-soho promote") || !strings.Contains(out, "keeps its pane") {
			t.Fatalf("help: code=%d out=%q stderr=%q", code, out, stderr)
		}
	})

	t.Run("working caller uses the guarded handler", func(t *testing.T) {
		f, title := promoteFixture(t, "sub-orch", "working", promoteVerbRules()...)
		sd := f.stateDir(t)
		promoteRoster(t, sd, "sub-orch")
		code, out, stderr := runIn(t, []string{"promote"}, f.env, f.cwd)
		result := parsePromoteOut(t, out)
		if code != 0 || stderr != "" || result.Name != "orchestrator" || result.Role != "orchestrator" || result.RosterRow != "removed" || result.Title != title {
			t.Fatalf("public promotion: code=%d result=%+v stderr=%q", code, result, stderr)
		}
		if raw := rosterRaw(t, sd); strings.Contains(raw, "sub-orch\t"+promotePane) || !strings.Contains(raw, "impl-1\tws:p1") {
			t.Fatalf("unexpected ownership after promotion: %q", raw)
		}
		assertOnlyScopeCalls(t, promoteCalls(t, f.env))
	})

	t.Run("plugin read-only mode rejects before Herdr calls", func(t *testing.T) {
		f, _ := promoteFixture(t, "sub-orch", "working", promoteVerbRules()...)
		sd := f.stateDir(t)
		promoteRoster(t, sd, "sub-orch")
		before := rosterRaw(t, sd)
		env := f.env.Clone()
		env["HERDR_SOHO_NOWRITE"] = "1"
		code, out, stderr := runIn(t, []string{"promote"}, env, f.cwd)
		if code != 2 || out != "" || !strings.Contains(stderr, "read-only") || rosterRaw(t, sd) != before || len(promoteCalls(t, f.env)) != 0 {
			t.Fatalf("read-only refusal: code=%d out=%q stderr=%q calls=%v", code, out, stderr, promoteCalls(t, f.env))
		}
	})

	t.Run("outside Herdr rejects without touching roster", func(t *testing.T) {
		f, _ := promoteFixture(t, "sub-orch", "working", promoteVerbRules()...)
		sd := f.stateDir(t)
		promoteRoster(t, sd, "sub-orch")
		before := rosterRaw(t, sd)
		env := f.env.Clone()
		delete(env, "HERDR_ENV")
		code, out, stderr := runIn(t, []string{"promote"}, env, f.cwd)
		if code != 2 || out != "" || stderr == "" || rosterRaw(t, sd) != before {
			t.Fatalf("outside-Herdr refusal: code=%d out=%q stderr=%q calls=%v", code, out, stderr, promoteCalls(t, f.env))
		}
		// The ancestry diagnostic may read the server snapshot to explain
		// a stripped Herdr environment; it must not control any actor.
		for _, call := range promoteCalls(t, f.env) {
			if strings.Join(call.Argv, " ") != "api snapshot" {
				t.Fatalf("outside-Herdr diagnostic controlled an actor: %v", call.Argv)
			}
		}
	})
}
