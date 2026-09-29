//go:build windows

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

func TestInstallPS1HappyPathVersionAndInstallDir(t *testing.T) {
	// Mutation captured: ignoring -InstallDir installs outside the requested user directory.
	result := runPowerShellInstaller(t, psFixtureOptions{})
	if result.code != 0 || !strings.Contains(result.stdout, "herdr-soho test-version") || !strings.Contains(result.stdout, "Add "+result.installDir+" to your user PATH") {
		t.Fatalf("installer status=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
	}
	if _, err := os.Stat(filepath.Join(result.installDir, "herdr-soho.exe")); err != nil {
		t.Fatalf("installed executable missing: %v", err)
	}
}

func TestInstallPS1ExplicitVersionAndRejectsBadDownloads(t *testing.T) {
	// Mutation captured: skipping version routing or checksum validation accepts the wrong release bytes.
	result := runPowerShellInstaller(t, psFixtureOptions{version: "v1.2.3"})
	if result.code != 0 || !strings.Contains(strings.Join(result.paths, "\n"), "/download/v1.2.3/"+psAssetName()) {
		t.Fatalf("installer status=%d stdout=%q stderr=%q paths=%v", result.code, result.stdout, result.stderr, result.paths)
	}
	for _, test := range []struct {
		name    string
		options psFixtureOptions
		want    string
	}{
		{name: "wrong checksum", options: psFixtureOptions{badChecksum: true}, want: "sha256 mismatch"},
		{name: "missing artifact", options: psFixtureOptions{missingAsset: true}, want: "404"},
		{name: "missing checksum entry", options: psFixtureOptions{missingEntry: true}, want: "no unique entry"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runPowerShellInstaller(t, test.options)
			if result.code == 0 || !strings.Contains(strings.ToLower(result.stderr), strings.ToLower(test.want)) {
				t.Fatalf("installer status=%d stdout=%q stderr=%q", result.code, result.stdout, result.stderr)
			}
			if _, err := os.Stat(filepath.Join(result.installDir, "herdr-soho.exe")); !os.IsNotExist(err) {
				t.Fatalf("failed install left executable: %v", err)
			}
		})
	}
}

type psFixtureOptions struct {
	version      string
	badChecksum  bool
	missingAsset bool
	missingEntry bool
}

type psInstallerResult struct {
	code       int
	stdout     string
	stderr     string
	installDir string
	paths      []string
}

func psAssetName() string {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		panic("unsupported test host architecture: " + runtime.GOARCH)
	}
	return fmt.Sprintf("herdr-soho_windows_%s.exe", runtime.GOARCH)
}

func runPowerShellInstaller(t *testing.T, options psFixtureOptions) psInstallerResult {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	installDir := filepath.Join(t.TempDir(), "custom bin")
	goSource := filepath.Join(t.TempDir(), "fake.go")
	goBinary := filepath.Join(t.TempDir(), "herdr-soho.exe")
	source := `package main
import ("fmt"; "os")
func main() { if len(os.Args) != 2 || os.Args[1] != "--version" { os.Exit(2) }; fmt.Println("herdr-soho test-version") }
`
	if err := os.WriteFile(goSource, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", goBinary, goSource)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build fake executable: %v\n%s", err, output)
	}
	binary, err := os.ReadFile(goBinary)
	if err != nil {
		t.Fatal(err)
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(binary))
	if options.badChecksum {
		digest = strings.Repeat("0", 64)
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
				entry = digest + "  " + psAssetName() + "\n"
			}
			_, _ = w.Write([]byte(entry))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/"+psAssetName()) && !options.missingAsset {
			_, _ = w.Write(binary)
			return
		}
		http.NotFound(w, r)
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cmd := exec.Command("powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", filepath.Join(root, "install.ps1"), "-InstallDir", installDir)
	if options.version != "" {
		cmd.Args = append(cmd.Args, "-Version", options.version)
	}
	cmd.Env = append(os.Environ(), "HERDR_SOHO_RELEASE_BASE="+server.URL)
	var stdout, stderr strings.Builder
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("start install.ps1: %v", err)
		}
	}
	pathsMu.Lock()
	defer pathsMu.Unlock()
	return psInstallerResult{code: code, stdout: stdout.String(), stderr: stderr.String(), installDir: installDir, paths: paths}
}
