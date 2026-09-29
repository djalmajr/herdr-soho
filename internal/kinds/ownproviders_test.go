package kinds

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestPiOwnModelsExposeIdsAndEffortOnly(t *testing.T) {
	home := t.TempDir()
	file := filepath.Join(home, ".pi", "agent", "models.json")
	if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
		t.Fatal(err)
	}
	content := `{"providers":{"vendor":{"apiKey":"sk-live-DO_NOT_COPY","models":[{"id":"m","thinkingLevelMap":{"low":"low","max":"max"}}]}}}`
	if err := os.WriteFile(file, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got := PiOwnModels(platform.Env{"HOME": home, "USERPROFILE": home})
	if len(got) != 1 || got[0].ID != "vendor/m" || got[0].MaxEffort != "max" {
		t.Fatalf("got %#v", got)
	}
	if strings.Contains(strings.Join(got[0].Warnings, " "), "DO_NOT_COPY") {
		t.Fatal("credential leaked")
	}
}
