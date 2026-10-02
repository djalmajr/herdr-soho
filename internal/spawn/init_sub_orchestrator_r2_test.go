package spawn
import("os";"path/filepath";"testing";"github.com/djalmajr/herdr-soho/internal/testutil/fakecli")
func TestInitSubOrchestratorR2RosterLookupReadOnly(t *testing.T) {
 f := newSpawnFixture(t,nil)
 f.env["HERDR_SOHO_DIR"] = filepath.Join(f.cwd,".herdr-soho")
 _,err:=fakecli.Install(t,f.bin,"git",[]fakecli.Rule{
  {Argv:[]string{"rev-parse","--show-toplevel"},Stdout:f.cwd+"\n"},
  {Argv:[]string{"rev-parse","--git-common-dir"},Stdout:filepath.Join(f.cwd,".git")+"\n"},
  {Argv:[]string{"-C",f.cwd,"rev-parse","--is-inside-work-tree"},Stdout:"true\n"},
  {Argv:[]string{"-C",f.cwd,"check-ignore","-q",".herdr-soho"},Code:1},
 })
 if err!=nil {t.Fatal(err)}
 _,_,found:=RosterCaller(f.ctx,f.env,f.cwd,"p-sub")
 if found {t.Fatal("unexpected roster")}
 if b,err:=os.ReadFile(filepath.Join(f.cwd,".gitignore")); err==nil {t.Fatalf("roster lookup created .gitignore: %q",b)}
 if _,err:=os.Stat(f.env["HERDR_SOHO_DIR"]); !os.IsNotExist(err) {t.Fatalf("lookup created state: %v",err)}
}
func TestInitSubOrchestratorR2RosterLookupInsideSkillDoesNotCreateState(t *testing.T) {
 f := newSpawnFixture(t,nil)
 f.env["HERDR_SOHO_DIR"] = filepath.Join(f.env["HERDR_SOHO_SKILL_DIR"],".herdr-soho")
 RosterCaller(f.ctx,f.env,f.cwd,"p-sub")
 if _,err:=os.Stat(f.env["HERDR_SOHO_DIR"]); !os.IsNotExist(err) {t.Fatalf("lookup created state inside skill: %v",err)}
}
