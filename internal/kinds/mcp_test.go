package kinds

import (
	"strings"
	"testing"

	"github.com/djalmajr/soho/internal/platform"
)

func TestKindMcpArgsPerProcessTranslation(t *testing.T) {
	t.Run("mcp=none maps to the kind's per-process flag", func(t *testing.T) {
		cases := []struct{ kind, want string }{
			{"claude", "--strict-mcp-config"},
			{"codex", `-c mcp_servers={}`},
			{"pi", "--no-mcp"},
		}
		for _, tc := range cases {
			args, ok := KindMcpArgs(tc.kind, "none")
			if !ok {
				t.Fatalf("kind %s: the per-process flag was refused", tc.kind)
			}
			if got := strings.Join(args, " "); got != tc.want {
				t.Fatalf("kind %s args=%q, want %q", tc.kind, got, tc.want)
			}
		}
	})

	t.Run("codex carries the flag as two argv elements", func(t *testing.T) {
		args, ok := KindMcpArgs("codex", "none")
		if !ok || len(args) != 2 || args[0] != "-c" || args[1] != "mcp_servers={}" {
			t.Fatalf("codex args=%#v ok=%v, want [-c mcp_servers={}]", args, ok)
		}
	})

	t.Run("the kinds without a per-process flag report unsupported", func(t *testing.T) {
		for _, kind := range []string{"grok", "agy", "cursor", "opencode", "gemini"} {
			args, ok := KindMcpArgs(kind, "none")
			if ok || len(args) > 0 {
				t.Fatalf("kind %s: unsupported flag %q reported as supported", kind, args)
			}
		}
	})

	t.Run("inherit (and an absent value) changes nothing, even for supported kinds", func(t *testing.T) {
		for _, mcp := range []string{"", "inherit"} {
			for _, kind := range []string{"claude", "codex", "pi"} {
				args, ok := KindMcpArgs(kind, mcp)
				if !ok || len(args) > 0 {
					t.Fatalf("kind %s mcp=%q: args=%v ok=%v, want nothing", kind, mcp, args, ok)
				}
			}
		}
	})
}

func TestKindMcpOffColumnInKindsTable(t *testing.T) {
	// An empty PATH: every CLI is uninstalled, so INSTALLED is a constant
	// "no" and the only thing under test is the MCP column.
	env := platform.Env{"PATH": "/nonexistent"}
	oldOut := platform.Stdout
	var out strings.Builder
	platform.Stdout = &out
	defer func() { platform.Stdout = oldOut }()
	CmdKinds(env, "darwin")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != len(KnownKinds)+1 {
		t.Fatalf("table=%q", lines)
	}
	header := strings.Split(lines[0], " ")
	if header[len(header)-1] != "MCP" {
		t.Fatalf("header=%q, want the MCP column last", lines[0])
	}
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		kind := fields[0]
		mcp := fields[len(fields)-1]
		want := "no"
		if KindMcpOff(kind) {
			want = "yes"
		}
		if mcp != want {
			t.Fatalf("row %q MCP=%q, want %q", kind, mcp, want)
		}
	}
	// The supported set is exactly claude, codex and pi.
	if !KindMcpOff("claude") || !KindMcpOff("codex") || !KindMcpOff("pi") || KindMcpOff("grok") {
		t.Fatalf("KindMcpOff disagrees with the flag translation")
	}
}
