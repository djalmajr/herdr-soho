package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

const briefFull = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","contexto":"The layout is frozen.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}],"decisoes":["keep the flag named --yes","a hash conflict exits 20"],"restricoes":["do not touch internal/plugin/**"],"nao_objetivos":["no push wiring"],"idioma":"en"}`

const briefWithSlot = `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","objetivo":"Ship the change.","contexto":"Fill the MY_SLOT marker before dispatch.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`

func TestBriefRenderFields(t *testing.T) {
	root := t.TempDir()
	s := Open(root)
	if _, err := s.Start("job-1", []byte(briefFull)); err != nil {
		t.Fatal(err)
	}
	md, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(md)
	for _, want := range []string{
		"## Goal",
		"Ship the change.",
		"Context: The layout is frozen.",
		"## Decisions already made",
		"keep the flag named --yes",
		"a hash conflict exits 20",
		"## Acceptance criteria",
		"go test ./internal/job",
		"## Forbidden",
		"do not touch internal/plugin/**",
		"workers do not commit or push",
		"## Non-goals",
		"no push wiring",
		"Report language: en",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("brief.md missing %q:\n%s", want, body)
		}
	}

	root2 := t.TempDir()
	s2 := Open(root2)
	if _, err := s2.Start("job-1", []byte(briefA)); err != nil {
		t.Fatal(err)
	}
	md2, err := os.ReadFile(filepath.Join(root2, "jobs", "job-1", "brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	body2 := string(md2)
	for _, absent := range []string{"## Decisions already made", "## Non-goals", "Report language:", "Context:"} {
		if strings.Contains(body2, absent) {
			t.Errorf("brief.md has an empty section %q:\n%s", absent, body2)
		}
	}
	if missing := dispatch.BriefMissingSections(body2, false, nil); missing != "" {
		t.Fatalf("minimal brief lint gaps:%s", missing)
	}
}

func TestLintBrief(t *testing.T) {
	t.Run("strict rejects a configured placeholder", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefWithSlot)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "jobs", "job-1", "brief.md")
		strict := &core.Config{Entries: map[string]core.ConfigEntry{
			"brief_lint":              {Value: "strict"},
			"brief_lint_placeholders": {Value: "MY_SLOT"},
		}}
		err := LintBrief(path, strict, platform.Env{})
		if code := exitCode(t, err); code != ExitUsage {
			t.Fatalf("strict lint: code %d err %v", code, err)
		}
	})

	t.Run("warn and off return nil", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefWithSlot)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "jobs", "job-1", "brief.md")
		warn := &core.Config{Entries: map[string]core.ConfigEntry{"brief_lint": {Value: "warn"}}}
		if err := LintBrief(path, warn, platform.Env{}); err != nil {
			t.Fatalf("warn lint: %v", err)
		}
		off := &core.Config{Entries: map[string]core.ConfigEntry{"brief_lint": {Value: "off"}}}
		if err := LintBrief(path, off, platform.Env{}); err != nil {
			t.Fatalf("off lint: %v", err)
		}
	})

	t.Run("strict accepts a clean brief", func(t *testing.T) {
		root := t.TempDir()
		s := Open(root)
		if _, err := s.Start("job-1", []byte(briefFull)); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "jobs", "job-1", "brief.md")
		strict := &core.Config{Entries: map[string]core.ConfigEntry{"brief_lint": {Value: "strict"}}}
		if err := LintBrief(path, strict, platform.Env{}); err != nil {
			t.Fatalf("strict clean lint: %v", err)
		}
	})
}
