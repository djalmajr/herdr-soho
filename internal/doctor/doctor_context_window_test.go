package doctor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func contextWindowDoctorFixture(t *testing.T) (platform.Env, string, *core.Config) {
	t.Helper()
	root := t.TempDir()
	env := platform.Env{
		"HOME": root, "USERPROFILE": root, "PATH": filepath.Join(root, "bin"),
		"HERDR_SOHO_DIR":   filepath.Join(root, "state"),
		"HERDR_SOHO_LANES": "off", "HERDR_SOHO_SKILL_DIR": root,
	}
	ctx := &core.Config{Entries: map[string]core.ConfigEntry{}}
	return env, root, ctx
}

func doctorContextWindowOut(t *testing.T, env platform.Env, cwd string, ctx *core.Config) string {
	t.Helper()
	var out strings.Builder
	DoctorCheck(ctx, env, cwd, &out)
	return out.String()
}

func TestDoctorWarnsContextWindowForNonGrokKind(t *testing.T) {
	env, cwd, ctx := contextWindowDoctorFixture(t)
	ctx.Entries["context_window_claude"] = core.ConfigEntry{Value: "500k", Source: "project"}
	out := doctorContextWindowOut(t, env, cwd, ctx)
	want := "warn   config: context_window.claude=500k has no effect (only grok uses context_window.<kind>)"
	if !strings.Contains(out, want) {
		t.Fatalf("doctor output missing %q:\n%s", want, out)
	}
}

func TestDoctorWarnsContextWindowForInvalidGrokValue(t *testing.T) {
	env, cwd, ctx := contextWindowDoctorFixture(t)
	ctx.Entries["context_window_grok"] = core.ConfigEntry{Value: "abc", Source: "project"}
	out := doctorContextWindowOut(t, env, cwd, ctx)
	want := "warn   config: context_window.grok='abc' is not a token count like 500k or 256000 (ignored)"
	if !strings.Contains(out, want) {
		t.Fatalf("doctor output missing %q:\n%s", want, out)
	}
}

func TestDoctorContextWindowFromEnvironment(t *testing.T) {
	t.Run("invalid grok value", func(t *testing.T) {
		env, cwd, ctx := contextWindowDoctorFixture(t)
		env["HERDR_SOHO_CONTEXT_WINDOW_GROK"] = "abc"
		out := doctorContextWindowOut(t, env, cwd, ctx)
		want := "warn   config: context_window.grok='abc' is not a token count like 500k or 256000 (ignored)"
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	})
	t.Run("non-grok kind", func(t *testing.T) {
		env, cwd, ctx := contextWindowDoctorFixture(t)
		env["HERDR_SOHO_CONTEXT_WINDOW_CODEX"] = "500k"
		out := doctorContextWindowOut(t, env, cwd, ctx)
		want := "warn   config: context_window.codex=500k has no effect (only grok uses context_window.<kind>)"
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	})
	t.Run("the environment value wins over the file", func(t *testing.T) {
		env, cwd, ctx := contextWindowDoctorFixture(t)
		ctx.Entries["context_window_grok"] = core.ConfigEntry{Value: "500k", Source: "project"}
		env["HERDR_SOHO_CONTEXT_WINDOW_GROK"] = "abc"
		out := doctorContextWindowOut(t, env, cwd, ctx)
		if !strings.Contains(out, "warn   config: context_window.grok='abc' is not a token count like 500k or 256000 (ignored)") {
			t.Fatalf("doctor output missing the env-wins warning:\n%s", out)
		}
	})
}

func TestDoctorQuietOnValidContextWindowGrok(t *testing.T) {
	env, cwd, ctx := contextWindowDoctorFixture(t)
	ctx.Entries["context_window_grok"] = core.ConfigEntry{Value: "500k", Source: "project"}
	out := doctorContextWindowOut(t, env, cwd, ctx)
	if strings.Contains(out, "context_window") {
		t.Fatalf("a valid grok value produced a context_window line:\n%s", out)
	}
}
