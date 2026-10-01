package doctor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
	"github.com/djalmajr/herdr-soho/internal/testutil/fakecli"
)

// insideSkillFixture builds a hermetic env whose skill dir (SKILL.md only) is
// the project root the state would resolve to: with cwd inside the skill and
// no HERDR_SOHO_DIR the state root is <skill>/.herdr-soho.
func insideSkillFixture(t *testing.T) (root, skill string, env platform.Env) {
	t.Helper()
	root = t.TempDir()
	skill = filepath.Join(root, "skill")
	home := filepath.Join(root, "home")
	bin := filepath.Join(root, "bin")
	for _, d := range []string{skill, home, bin} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("# herdr-soho\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fakecli.Install(t, bin, "herdr", []fakecli.Rule{}); err != nil {
		t.Fatal(err)
	}
	env = platform.Env{
		"HOME": home, "USERPROFILE": home, "PATH": bin,
		"HERDR_ENV": "1", "HERDR_SOHO_FAKECLI_CONFIG": bin,
		"HERDR_SOHO_SKILL_DIR": skill,
	}
	return root, skill, env
}

func TestDoctorWarnsWhenTheStateDirIsInsideTheSkill(t *testing.T) {
	_, skill, env := insideSkillFixture(t)
	ctx := core.Config{Entries: map[string]core.ConfigEntry{}}
	var out strings.Builder
	DoctorCheck(&ctx, env, skill, &out) // cwd = the skill; must finish
	got := out.String()
	rootPath := filepath.Join(skill, ".herdr-soho")
	want := "warn   state dir: '" + rootPath + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory\n"
	if !strings.Contains(got, want) {
		t.Fatalf("doctor output lacks the warn line:\n%s", got)
	}
	if strings.Contains(got, "state dir writable") {
		t.Fatalf("the state write check still ran inside the skill:\n%s", got)
	}
	if _, err := os.Stat(rootPath); !os.IsNotExist(err) {
		t.Fatalf("state dir created inside the skill: %v", err)
	}
	entries, err := os.ReadDir(skill)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("skill dir changed: %v", entries)
	}
	// The rest of the doctor follows the warn line.
	if i := strings.Index(got, want); i < 0 || !strings.Contains(got[i:], "project config:") {
		t.Fatalf("the doctor stopped at the state line:\n%s", got)
	}

	// Control: a cwd outside the skill keeps the writable line and creates
	// the state dir as before.
	proj := filepath.Join(filepath.Dir(skill), "project")
	if err := os.MkdirAll(proj, 0o700); err != nil {
		t.Fatal(err)
	}
	env["HERDR_SOHO_DIR"] = filepath.Join(filepath.Dir(skill), "state")
	var out2 strings.Builder
	DoctorCheck(&ctx, env, proj, &out2)
	if !strings.Contains(out2.String(), "state dir writable: "+filepath.Join(filepath.Dir(skill), "state")+"\n") {
		t.Fatalf("control lost the writable line:\n%s", out2.String())
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(skill), "state")); err != nil {
		t.Fatalf("control did not create the state dir: %v", err)
	}
}

func TestInitRefusesBeforeDoctorCheckWhenInsideTheSkill(t *testing.T) {
	_, skill, env := insideSkillFixture(t)
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var outBuf, errBuf strings.Builder
	platform.Stdout, platform.Stderr = &outBuf, &errBuf
	var recovered any
	func() {
		defer func() { recovered = recover() }()
		CmdInit(&core.Config{Entries: map[string]core.ConfigEntry{}}, env, skill)
	}()
	platform.Stdout, platform.Stderr = oldOut, oldErr
	exitErr, ok := recovered.(*platform.ExitError)
	if !ok {
		t.Fatalf("CmdInit did not die (recovered=%#v)", recovered)
	}
	if exitErr.Code != 2 {
		t.Fatalf("exit=%d msg=%q", exitErr.Code, exitErr.Msg)
	}
	want := "the state dir '" + filepath.Join(skill, ".herdr-soho") + "' would be inside the herdr-soho skill ('" + skill + "'); run herdr-soho from the project's directory (nothing was written)"
	if exitErr.Msg != want {
		t.Fatalf("msg=%q want %q", exitErr.Msg, want)
	}
	// DoctorCheck never ran: it would have written its report before the
	// refusal.
	if outBuf.String() != "" || errBuf.String() != "" {
		t.Fatalf("DoctorCheck ran before the refusal (stdout=%q stderr=%q)", outBuf.String(), errBuf.String())
	}
	// Nothing was created: DoctorCheck (with its MkdirAll) never ran.
	if _, err := os.Stat(filepath.Join(skill, ".herdr-soho")); !os.IsNotExist(err) {
		t.Fatalf("state dir created before the refusal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(skill, ".gitignore")); !os.IsNotExist(err) {
		t.Fatalf(".gitignore created before the refusal: %v", err)
	}
	entries, err := os.ReadDir(skill)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "SKILL.md" {
		t.Fatalf("skill dir changed: %v", entries)
	}
}
