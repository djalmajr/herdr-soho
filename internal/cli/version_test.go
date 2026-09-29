package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/platform"
)

func TestVersionCommandPrintsBuildVersion(t *testing.T) {
	// Mutation captured: dropping the ldflags version must change the observable CLI output.
	oldVersion := version
	oldOut := platform.Stdout
	defer func() {
		version = oldVersion
		platform.Stdout = oldOut
	}()

	version = "v1.2.3"
	var out bytes.Buffer
	platform.Stdout = &out
	if code := Run([]string{"--version"}, platform.Env{}); code != 0 || out.String() != "herdr-soho v1.2.3\n" {
		t.Fatalf("--version returned %d and %q", code, out.String())
	}
}

func TestVersionTextUsesDevelopmentAndRevision(t *testing.T) {
	// Mutation captured: ignoring vcs.revision must omit the revision from dev output.
	if got := versionText("dev", "abcdef"); got != "herdr-soho dev+abcdef\n" {
		t.Fatalf("versionText() = %q", got)
	}
	if got := versionText("", ""); got != "herdr-soho dev\n" {
		t.Fatalf("empty build metadata versionText() = %q", got)
	}
	if strings.Contains(versionText("v1.2.3", "abcdef"), "abcdef") {
		t.Fatal("release version unexpectedly included the VCS revision")
	}
}
