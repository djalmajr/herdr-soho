package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestContextWindowValueOk(t *testing.T) {
	ok := []string{"500k", "256000", "1k", "9"}
	bad := []string{"", "abc", "0", "0k", "-5k", "500.5k", "50 k", "500kk", "5.0k", "k500", "256000 ", "500K"}
	for _, value := range ok {
		if !ContextWindowValueOk(value) {
			t.Fatalf("ContextWindowValueOk(%q)=false, want true", value)
		}
	}
	for _, value := range bad {
		if ContextWindowValueOk(value) {
			t.Fatalf("ContextWindowValueOk(%q)=true, want false", value)
		}
	}
}

func TestContextWindowKeyAndEnv(t *testing.T) {
	env, cwd := fixture(t)
	project := filepath.Join(cwd, ".agents", "herdr-soho.conf")
	write(t, project, "context_window.grok=256000\n")
	ctx := LoadConfig(env, cwd)
	if got := Cfg(&ctx, ContextWindowKey("grok"), "", env); got != "256000" {
		t.Fatalf("cfg=%q, want 256000 (file layer)", got)
	}
	env["HERDR_SOHO_CONTEXT_WINDOW_GROK"] = "500k"
	if got := Cfg(&ctx, ContextWindowKey("grok"), "", env); got != "500k" {
		t.Fatalf("cfg=%q, want 500k (env wins)", got)
	}
	if got := CfgSource(&ctx, ContextWindowKey("grok"), env); got != "env" {
		t.Fatalf("source=%q, want env", got)
	}
	entries := ContextWindowEntries(&ctx, env)
	if len(entries) != 1 || entries["grok"].Value != "500k" || entries["grok"].Source != "env" {
		t.Fatalf("entries=%#v", entries)
	}
	// Legacy env spelling lands on the same key, like the other keys.
	legacy := platform.Env{}
	legacy["HERDR_AGENTS_CONTEXT_WINDOW_GROK"] = "500k"
	ApplyLegacyEnv(legacy)
	if got := legacy.Get("HERDR_SOHO_CONTEXT_WINDOW_GROK"); got != "500k" {
		t.Fatalf("legacy env=%q", got)
	}
}

func TestContextWindowConfigKeyOkAndDottedName(t *testing.T) {
	for _, key := range []string{"context_window.grok", "context_window.codex"} {
		if !ConfigKeyOk(key) {
			t.Fatalf("ConfigKeyOk(%q)=false, want true", key)
		}
	}
	for _, key := range []string{"context_window", "context_window.", "context_windowx.grok", "Context_Window.grok"} {
		if ConfigKeyOk(key) {
			t.Fatalf("ConfigKeyOk(%q)=true, want false", key)
		}
	}
	ctx := &Config{Entries: map[string]ConfigEntry{}}
	env := platform.Env{}
	if got := DottedKeyName("context_window_grok", ctx, env, ""); got != "context_window.grok" {
		t.Fatalf("DottedKeyName=%q, want context_window.grok", got)
	}
	// config set accepts the value like effort.<kind> (doctor owns the
	// format warning); the key is unknown-shaped only without a kind.
	env, cwd := fixture(t)
	if !ConfigValueOk("context_window.grok", "abc", env, cwd) {
		t.Fatal("ConfigValueOk rejected a one-line value (doctor warns instead)")
	}
	_, errOut := capture(t, func() {
		CmdConfigSet([]string{"context_window.grok=500k"}, nil, env, cwd)
	})
	if errOut != "" {
		t.Fatalf("config set stderr=%q", errOut)
	}
	data, err := os.ReadFile(filepath.Join(cwd, ".agents", "herdr-soho.conf"))
	if err != nil || string(data) != "context_window.grok=500k\n" {
		t.Fatalf("config file=%q err=%v", data, err)
	}
}

func TestConfigListsContextWindowLikeTheOtherDottedKeys(t *testing.T) {
	env, cwd := fixture(t)
	project := filepath.Join(cwd, ".agents", "herdr-soho.conf")
	write(t, project, "effort.grok=xhigh\ncontext_window.grok=500k\n")
	ctx := LoadConfig(env, cwd)
	out, _ := capture(t, func() { CmdConfig(&ctx, env, cwd) })
	effortLine := ""
	windowLine := ""
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "effort.grok") {
			effortLine = line
		}
		if strings.HasPrefix(line, "context_window.grok") {
			windowLine = line
		}
	}
	if effortLine == "" || !strings.Contains(effortLine, "xhigh") || !strings.Contains(effortLine, "project") {
		t.Fatalf("effort line=%q", effortLine)
	}
	if windowLine == "" || !strings.Contains(windowLine, "500k") || !strings.Contains(windowLine, "project") {
		t.Fatalf("context_window line=%q (want value + source like the effort line)", windowLine)
	}
	// A key that only exists in the environment is listed under its rebuilt
	// dotted name, as effort.* does.
	env["HERDR_SOHO_CONTEXT_WINDOW_CODEX"] = "256000"
	out, _ = capture(t, func() { CmdConfig(&ctx, env, cwd) })
	found := false
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "context_window.codex") && strings.Contains(line, "256000") && strings.Contains(line, "env") {
			found = true
		}
	}
	if !found {
		t.Fatalf("env-only key not listed under its dotted name:\n%s", out)
	}
}
