//go:build !windows

// The install.sh tests need a POSIX sh; install.ps1 has its own tests in
// install_windows_test.go.

package install

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestInstallSHappyPathAndInstallDir(t *testing.T) {
	// Mutation captured: installing outside HERDR_SOHO_INSTALL_DIR breaks the installed-path contract.
	result := runInstaller(t, fixtureOptions{})
	if result.code != 0 || !strings.Contains(result.stdout, "herdr-soho test-version") || !strings.Contains(result.stdout, "Add "+result.installDir+" to your PATH") {
		t.Fatalf("installer status=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	installed, err := os.ReadFile(filepath.Join(result.installDir, "herdr-soho"))
	if err != nil || string(installed) != fakeBinary {
		t.Fatalf("installed binary=%q err=%v", installed, err)
	}
	if !strings.Contains(strings.Join(result.paths, "\n"), "/latest/download/"+assetName()) {
		t.Fatalf("download paths=%v", result.paths)
	}
}

func TestInstallSExplicitVersionUsesMatchingRelease(t *testing.T) {
	// Mutation captured: routing --version to latest downloads the wrong release path.
	result := runInstaller(t, fixtureOptions{version: "v1.2.3"})
	if result.code != 0 || !strings.Contains(strings.Join(result.paths, "\n"), "/download/v1.2.3/"+assetName()) {
		t.Fatalf("installer status=%d stdout=%q stderr=%q paths=%v", result.code, result.stdout, result.stderr, result.paths)
	}
}

func TestInstallSRejectsWrongShaWithoutReplacingExistingBinary(t *testing.T) {
	// Mutation captured: skipping the digest comparison replaces a working install with untrusted bytes.
	dir := t.TempDir()
	old := []byte("existing binary")
	if err := os.WriteFile(filepath.Join(dir, "herdr-soho"), old, 0o700); err != nil {
		t.Fatal(err)
	}
	result := runInstaller(t, fixtureOptions{installDir: dir, badChecksum: true})
	got, err := os.ReadFile(filepath.Join(dir, "herdr-soho"))
	if result.code == 0 || !strings.Contains(result.stderr, "sha256 mismatch") || err != nil || string(got) != string(old) {
		t.Fatalf("installer status=%d stderr=%q existing=%q err=%v", result.code, result.stderr, got, err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".herdr-soho.") {
			t.Fatalf("installer left temporary file %q", entry.Name())
		}
	}
}

func TestInstallSRejectsMissingArtifactAndMissingChecksumEntry(t *testing.T) {
	// Mutation captured: accepting a missing asset or checksum row creates a false successful install.
	for _, test := range []struct {
		name    string
		options fixtureOptions
		want    string
	}{
		{name: "asset missing", options: fixtureOptions{missingAsset: true}, want: "failed to download"},
		{name: "checksum entry missing", options: fixtureOptions{missingEntry: true}, want: "no unique entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runInstaller(t, test.options)
			if result.code == 0 || !strings.Contains(result.stderr, test.want) {
				t.Fatalf("installer status=%d stderr=%q", result.code, result.stderr)
			}
			if _, err := os.Stat(filepath.Join(result.installDir, "herdr-soho")); !os.IsNotExist(err) {
				t.Fatalf("failed install left destination: %v", err)
			}
		})
	}
}

const fakeBinary = "#!/bin/sh\n[ \"${1:-}\" = \"--version\" ] || exit 2\nprintf '%s\\n' 'herdr-soho test-version'\n"

type fixtureOptions struct {
	version      string
	installDir   string
	badChecksum  bool
	missingAsset bool
	missingEntry bool
	// upperChecksum writes the digest in uppercase; baseSuffix is appended to
	// the release base URL.
	upperChecksum bool
	baseSuffix    string
}

type installerResult struct {
	code       int
	stdout     string
	stderr     string
	installDir string
	paths      []string
}

func assetName() string {
	os := runtime.GOOS
	if os == "windows" {
		os = "windows"
	}
	arch := runtime.GOARCH
	if arch != "amd64" && arch != "arm64" {
		panic("unsupported test host architecture: " + arch)
	}
	suffix := ""
	if os == "windows" {
		suffix = ".exe"
	}
	return fmt.Sprintf("herdr-soho_%s_%s%s", os, arch, suffix)
}

func runInstaller(t *testing.T, options fixtureOptions) installerResult {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	installDir := options.installDir
	if installDir == "" {
		installDir = filepath.Join(t.TempDir(), "custom-bin")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(fakeBinary)))
	if options.badChecksum {
		digest = strings.Repeat("0", 64)
	}
	if options.missingEntry {
		digest = strings.Repeat("0", 64)
	}
	if options.upperChecksum {
		digest = strings.ToUpper(digest)
	}
	var paths []string
	var pathsMu sync.Mutex
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, r.URL.Path)
		pathsMu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS") {
			entry := ""
			if !options.missingEntry {
				entry = digest + "  " + assetName() + "\n"
			}
			_, _ = w.Write([]byte(entry))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/"+assetName()) && !options.missingAsset {
			_, _ = w.Write([]byte(fakeBinary))
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cmd := exec.Command("sh", filepath.Join(root, "install.sh"))
	if options.version != "" {
		cmd.Args = append(cmd.Args, "--version", options.version)
	}
	cmd.Env = append(os.Environ(), "HERDR_SOHO_RELEASE_BASE="+server.URL+options.baseSuffix, "HERDR_SOHO_INSTALL_DIR="+installDir, "HOME="+t.TempDir(), "PATH=/usr/bin:/bin")
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("start install.sh: %v", err)
		}
	}
	pathsMu.Lock()
	defer pathsMu.Unlock()
	return installerResult{code: code, stdout: stdout.String(), stderr: stderr.String(), installDir: installDir, paths: paths}
}

func TestInstallSAcceptsAnUppercaseChecksum(t *testing.T) {
	// Mutation captured: comparing the digest case-sensitively rejects a valid
	// SHA256SUMS written in uppercase (sha256sum and shasum print lowercase).
	result := runInstaller(t, fixtureOptions{upperChecksum: true})
	if result.code != 0 || !strings.Contains(result.stdout, "herdr-soho test-version") {
		t.Fatalf("installer status=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
}

func TestInstallSRejectsWhitespaceInTheReleaseBase(t *testing.T) {
	// Mutation captured: a guard that matches a backslash-n instead of a real
	// newline lets a newline or tab reach mkdir and curl.
	for name, suffix := range map[string]string{"space": "/a b", "newline": "/a\nb", "tab": "/a\tb"} {
		t.Run(name, func(t *testing.T) {
			result := runInstaller(t, fixtureOptions{baseSuffix: suffix})
			if result.code != 2 || !strings.Contains(result.stderr, "release base URL contains whitespace") {
				t.Fatalf("installer status=%d stderr=%q", result.code, result.stderr)
			}
			if _, err := os.Stat(result.installDir); !os.IsNotExist(err) {
				t.Fatalf("install dir was created before the guard: %v", err)
			}
		})
	}
}
