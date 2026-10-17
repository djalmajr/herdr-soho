package job

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/djalmajr/soho/internal/platform"
)

// The spawn step of the default adapter waits spawn_timeout on top of the
// team-ops bound (DJA-395): the spawn itself waits that long for the agent
// to start, so a fixed 120 s cut a slow machine's spawn short.
func TestSelfCLISpawnTimeoutFollowsSpawnTimeout(t *testing.T) {
	skill, err := filepath.Abs(filepath.Join("..", "..", "skills", "soho"))
	if err != nil {
		t.Fatal(err)
	}
	env := func(spawn string) platform.Env {
		e := platform.Env{"HOME": t.TempDir(), "XDG_CONFIG_HOME": t.TempDir(), "SOHO_SKILL_DIR": skill}
		if spawn != "" {
			e["SOHO_SPAWN_TIMEOUT"] = spawn
		}
		return e
	}
	cases := []struct {
		name  string
		spawn string
		want  time.Duration
	}{
		{"default spawn_timeout", "", defaultTeamOpsTimeout + 60*time.Second},
		{"milliseconds", "90000", defaultTeamOpsTimeout + 90*time.Second},
		{"duration with a unit", "5m", defaultTeamOpsTimeout + 5*time.Minute},
		{"unreadable value keeps the team-ops bound", "soon", defaultTeamOpsTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selfCLI{Exe: "x", Dir: t.TempDir(), Env: env(tc.spawn)}.spawnTimeout()
			if got != tc.want {
				t.Fatalf("spawnTimeout = %s, want %s", got, tc.want)
			}
		})
	}
	t.Run("a configuration that cannot load keeps the team-ops bound", func(t *testing.T) {
		// No skill directory and no home to find or build one in: the
		// loader dies, and the bound stays.
		e := platform.Env{"SOHO_SPAWN_TIMEOUT": "90000"}
		if got := (selfCLI{Exe: "x", Dir: t.TempDir(), Env: e}).spawnTimeout(); got != defaultTeamOpsTimeout {
			t.Fatalf("spawnTimeout = %s, want %s", got, defaultTeamOpsTimeout)
		}
	})
	t.Run("an explicit Timeout stays the bound", func(t *testing.T) {
		c := selfCLI{Exe: "x", Dir: t.TempDir(), Env: env("90000"), Timeout: 30 * time.Second}
		if got := c.spawnTimeout(); got != 30*time.Second {
			t.Fatalf("spawnTimeout = %s, want 30s", got)
		}
		bounded := selfCLI{Exe: "x", Dir: t.TempDir(), Env: env("90000")}.bounded(7 * time.Second).(selfCLI)
		if got := bounded.spawnTimeout(); got != 7*time.Second {
			t.Fatalf("bounded spawnTimeout = %s, want 7s", got)
		}
	})
}
