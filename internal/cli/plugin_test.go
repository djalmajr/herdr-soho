package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestPluginHelpersRemainHiddenAndRouteInternally(t *testing.T) {
	// Mutation captured: exposing plugin helpers in public help or dropping the internal route changes this output/code contract.
	oldOut, oldErr := platform.Stdout, platform.Stderr
	var out, errOut bytes.Buffer
	platform.Stdout, platform.Stderr = &out, &errOut
	t.Cleanup(func() { platform.Stdout, platform.Stderr = oldOut, oldErr })
	if code := Run([]string{"help"}, platform.Env{}); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	if strings.Contains(out.String(), "plugin bridge") || strings.Contains(out.String(), "plugin clipboard") {
		t.Fatalf("plugin helpers leaked into public help: %s", out.String())
	}
	out.Reset()
	if code := Run([]string{"plugin", "bridge", "unknown"}, platform.Env{}); code != 2 || !strings.Contains(errOut.String(), "unknown subcommand 'unknown'") {
		t.Fatalf("plugin bridge route: code=%d stderr=%q", code, errOut.String())
	}
}
