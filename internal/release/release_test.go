// Tests for the release core. No real go build matrix, no real gh, no
// network, no shell: the running test binary is re-executed as a fake go
// build (argv + env + deterministic output bytes) and as a fake gh CLI
// (argv only — never the environment, so secrets cannot leak into logs).
package release

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain doubles as the re-executed helper. Launched as the go
// executable it sees os.Args[1] == "build"; as the gh CLI it sees
// "release". Any other invocation runs the real test binary.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "build" || os.Args[1] == "release") {
		os.Exit(runHelper())
	}
	os.Exit(m.Run())
}

// runHelper is the fake go build and the fake gh CLI. Evidence goes to the
// file named by RELEASE_TEST_LOG as JSON lines with the exact argv: each
// build records its argv plus the target environment; gh records its argv
// only (never the environment, so secrets cannot leak into logs).
func runHelper() int {
	logPath := os.Getenv("RELEASE_TEST_LOG")
	logLine := func(v any) {
		if logPath == "" {
			return
		}
		data, err := json.Marshal(v)
		if err != nil {
			os.Exit(3)
		}
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err == nil {
			f.Write(append(data, '\n'))
			f.Close()
		}
	}
	switch os.Args[1] {
	case "build":
		var ldflags, out string
		for i := 1; i+1 < len(os.Args); i++ {
			switch os.Args[i] {
			case "-ldflags":
				ldflags = os.Args[i+1]
				i++
			case "-o":
				out = os.Args[i+1]
				i++
			}
		}
		if os.Getenv("RELEASE_TEST_FAIL_GOOS") == os.Getenv("GOOS") {
			logLine(map[string]any{"kind": "build", "status": "FAIL", "argv": os.Args[1:]})
			fmt.Fprintln(os.Stderr, "fake go: injected build failure")
			return 1
		}
		if out == "" || ldflags == "" {
			logLine(map[string]any{"kind": "build", "status": "BADARGV", "argv": os.Args[1:]})
			fmt.Fprintln(os.Stderr, "fake go: missing -ldflags or -o")
			return 2
		}
		if os.Getenv("RELEASE_TEST_OUT_MODE") == "dir" {
			if err := os.MkdirAll(out, 0o755); err != nil {
				fmt.Fprintln(os.Stderr, "fake go:", err)
				return 3
			}
			logLine(map[string]any{"kind": "build", "status": "OK-DIR", "argv": os.Args[1:]})
			return 0
		}
		payload := fmt.Sprintf("fake artifact %s_%s ldflags=%s\n", os.Getenv("GOOS"), os.Getenv("GOARCH"), ldflags)
		if err := os.WriteFile(out, []byte(payload), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "fake go:", err)
			return 3
		}
		logLine(map[string]any{
			"kind":   "build",
			"status": "OK",
			"argv":   os.Args[1:],
			"env":    map[string]string{"GOOS": os.Getenv("GOOS"), "GOARCH": os.Getenv("GOARCH"), "CGO_ENABLED": os.Getenv("CGO_ENABLED")},
		})
		return 0
	case "release":
		logLine(map[string]any{"kind": "gh", "argv": os.Args[1:]})
		if os.Getenv("RELEASE_TEST_GH_FAIL") == "1" {
			fmt.Fprintln(os.Stderr, "fake gh: injected publish failure")
			return 1
		}
		return 0
	}
	return 2
}

// helperPath returns the running test binary path (the fake go/gh).
func helperPath(t *testing.T) string {
	t.Helper()
	p, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// setBuildLog points the fake go at a fresh log file.
func setBuildLog(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "build.log")
	t.Setenv("RELEASE_TEST_LOG", log)
	return log
}

// setGHLog points the fake gh at a fresh log file.
func setGHLog(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "gh.log")
	t.Setenv("RELEASE_TEST_LOG", log)
	return log
}

// readLog returns the log lines, or nil if the file was never created.
func readLog(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
}

// countStaging counts leftover staging directories in dir.
func countStaging(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".herdr-soho-release-") {
			n++
		}
	}
	return n
}

// expectedPayload is the exact bytes the fake go writes for a target.
func expectedPayload(osName, arch, version string) string {
	return fmt.Sprintf("fake artifact %s_%s ldflags=%s\n", osName, arch, fmt.Sprintf(versionLdflags, version))
}

// expectedSums is the exact SHA256SUMS content for a version.
func expectedSums(version string) string {
	var b strings.Builder
	for _, tg := range targets {
		sum := sha256.Sum256([]byte(expectedPayload(tg.os, tg.arch, version)))
		fmt.Fprintf(&b, "%x  %s\n", sum, ArtifactName(tg.os, tg.arch))
	}
	return b.String()
}

// helperBuildLine is one fake-go build record.
type helperBuildLine struct {
	Kind   string            `json:"kind"`
	Status string            `json:"status"`
	Argv   []string          `json:"argv"`
	Env    map[string]string `json:"env"`
}

// helperGHLine is the fake-gh record (argv only).
type helperGHLine struct {
	Kind string   `json:"kind"`
	Argv []string `json:"argv"`
}

// parseBuildLog decodes the fake-go JSON log lines.
func parseBuildLog(t *testing.T, path string) []helperBuildLine {
	t.Helper()
	lines := readLog(t, path)
	if lines == nil {
		return nil
	}
	out := make([]helperBuildLine, 0, len(lines))
	for _, line := range lines {
		var l helperBuildLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("unparseable build log line %q: %v", line, err)
		}
		out = append(out, l)
	}
	return out
}

// buildValidDist builds a complete, correct dist directory with the fake
// go and returns it.
func buildValidDist(t *testing.T, version string) string {
	t.Helper()
	base := t.TempDir()
	dest := filepath.Join(base, "dist")
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	setBuildLog(t)
	if _, err := Build(context.Background(), BuildConfig{
		Version:      version,
		RepoDir:      repo,
		Dest:         dest,
		GoExecutable: helperPath(t),
	}); err != nil {
		t.Fatalf("valid build: %v", err)
	}
	return dest
}

// artifactNames lists the six names in target order.
func artifactNames() []string {
	names := make([]string, len(targets))
	for i, tg := range targets {
		names[i] = ArtifactName(tg.os, tg.arch)
	}
	return names
}

func TestBuildProducesSixArtifactsAndExactSums(t *testing.T) {
	for _, version := range []string{"v1.2.3", DevVersion} {
		t.Run(version, func(t *testing.T) {
			base := t.TempDir()
			dest := filepath.Join(base, "dist")
			repo := filepath.Join(base, "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			log := setBuildLog(t)
			res, err := Build(context.Background(), BuildConfig{
				Version:      version,
				RepoDir:      repo,
				Dest:         dest,
				GoExecutable: helperPath(t),
			})
			if err != nil {
				t.Fatalf("build: %v", err)
			}
			if res.Version != version || res.Dest != dest || res.Sums != filepath.Join(dest, sumsFileName) {
				t.Fatalf("result = %+v", res)
			}
			wantNames := artifactNames()
			if strings.Join(res.Artifacts, ",") != strings.Join(wantNames, ",") {
				t.Fatalf("artifacts = %v, want %v", res.Artifacts, wantNames)
			}
			// Exact output bytes per target.
			for i, tg := range targets {
				got, err := os.ReadFile(filepath.Join(dest, wantNames[i]))
				if err != nil {
					t.Fatalf("reading %s: %v", wantNames[i], err)
				}
				want := []byte(expectedPayload(tg.os, tg.arch, version))
				if string(got) != string(want) {
					t.Fatalf("%s bytes = %q, want %q", wantNames[i], got, want)
				}
				info, err := os.Stat(filepath.Join(dest, wantNames[i]))
				if err != nil || !info.Mode().IsRegular() {
					t.Fatalf("%s is not a regular file: %v", wantNames[i], err)
				}
			}
			// Deterministic SHA256SUMS content.
			sums, err := os.ReadFile(filepath.Join(dest, sumsFileName))
			if err != nil {
				t.Fatal(err)
			}
			if string(sums) != expectedSums(version) {
				t.Fatalf("SHA256SUMS = %q, want %q", sums, expectedSums(version))
			}
			// All six target argvs (exact, JSON-parsed), sequential order,
			// exact env.
			bl := parseBuildLog(t, log)
			if len(bl) != len(targets) {
				t.Fatalf("want %d build log lines, got %d", len(targets), len(bl))
			}
			ldflags := fmt.Sprintf(versionLdflags, version)
			for i, tg := range targets {
				line := bl[i]
				if line.Kind != "build" || line.Status != "OK" {
					t.Fatalf("line %d = %+v", i, line)
				}
				argv := line.Argv
				// build -trimpath -buildvcs=false -ldflags LF -o OUT ./cmd/herdr-soho
				if len(argv) != 8 ||
					argv[0] != "build" || argv[1] != "-trimpath" || argv[2] != "-buildvcs=false" ||
					argv[3] != "-ldflags" || argv[4] != ldflags ||
					argv[5] != "-o" || filepath.Base(argv[6]) != wantNames[i] ||
					!strings.Contains(argv[6], ".herdr-soho-release-") ||
					argv[7] != mainPackage {
					t.Fatalf("line %d argv = %v", i, argv)
				}
				wantEnv := map[string]string{"GOOS": tg.os, "GOARCH": tg.arch, "CGO_ENABLED": "0"}
				if len(line.Env) != 3 || line.Env["GOOS"] != wantEnv["GOOS"] || line.Env["GOARCH"] != wantEnv["GOARCH"] || line.Env["CGO_ENABLED"] != "0" {
					t.Fatalf("line %d env = %v, want %v", i, line.Env, wantEnv)
				}
			}
			// Only owned staging is cleaned up: nothing left behind.
			if n := countStaging(t, base); n != 0 {
				t.Fatalf("%d staging directories left behind", n)
			}
		})
	}
}

func TestBuildRejectsInvalidVersionsBeforeExecution(t *testing.T) {
	cases := []string{
		"", "1.2.3", "v", "v ", "v a", "v\"a\"", "../v", "v..1", "dev ",
		"V1.2.3", "v1.2.3/x", "v\t1",
	}
	for _, version := range cases {
		t.Run(fmt.Sprintf("%q", version), func(t *testing.T) {
			base := t.TempDir()
			log := setBuildLog(t)
			repo := filepath.Join(base, "repo")
			if err := os.MkdirAll(repo, 0o755); err != nil {
				t.Fatal(err)
			}
			_, err := Build(context.Background(), BuildConfig{
				Version:      version,
				RepoDir:      repo,
				Dest:         filepath.Join(base, "dist"),
				GoExecutable: helperPath(t),
			})
			if err == nil {
				t.Fatal("invalid version accepted")
			}
			if lines := readLog(t, log); lines != nil {
				t.Fatalf("fake go ran for invalid version: %v", lines)
			}
			if n := countStaging(t, base); n != 0 {
				t.Fatalf("%d staging directories for invalid version", n)
			}
		})
	}
	// A relative explicit go executable is refused before execution.
	base := t.TempDir()
	log := setBuildLog(t)
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := Build(context.Background(), BuildConfig{
		Version:      DevVersion,
		RepoDir:      repo,
		Dest:         filepath.Join(base, "dist"),
		GoExecutable: "relative/go",
	})
	if err == nil || !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("relative go executable: err = %v", err)
	}
	if lines := readLog(t, log); lines != nil {
		t.Fatalf("fake go ran: %v", lines)
	}
}

// A failed build with a relative --dest and a different relative --repo
// must leave no destination, no staging, and nothing inside the repo
// directory: paths are anchored to the invoking cwd, and only the owned
// staging directory is ever created or cleaned.
func TestBuildFailureCleansStagingAndLeavesNoDest(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	if err := os.MkdirAll("repo", 0o755); err != nil {
		t.Fatal(err)
	}
	log := setBuildLog(t)
	t.Setenv("RELEASE_TEST_FAIL_GOOS", "windows")
	_, err := Build(context.Background(), BuildConfig{
		Version:      "v1.2.3",
		RepoDir:      "repo",
		Dest:         "dist",
		GoExecutable: helperPath(t),
	})
	if err == nil || !strings.Contains(err.Error(), "herdr-soho_windows_amd64") {
		t.Fatalf("failure error = %v, want it naming the failed target", err)
	}
	if _, err := os.Lstat("dist"); !os.IsNotExist(err) {
		t.Fatalf("dist created despite the failed build: %v", err)
	}
	if n := countStaging(t, base); n != 0 {
		t.Fatalf("%d staging directories left behind in the base directory", n)
	}
	repoDir := filepath.Join(base, "repo")
	if n := countStaging(t, repoDir); n != 0 {
		t.Fatalf("%d staging directories leaked into the repo directory", n)
	}
	entries, err := os.ReadDir("repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("repo directory not empty after the failed build: %v", entries)
	}
	// The sequential run stops at the first windows target, so exactly the
	// lines up to (and including) that target are recorded.
	wantLines := len(targets)
	for i, tg := range targets {
		if tg.os == "windows" {
			wantLines = i + 1
			break
		}
	}
	bl := parseBuildLog(t, log)
	if len(bl) != wantLines {
		t.Fatalf("want %d build log lines, got %d", wantLines, len(bl))
	}
	for i, line := range bl {
		wantStatus := "OK"
		if targets[i].os == "windows" {
			wantStatus = "FAIL"
		}
		if line.Status != wantStatus {
			t.Fatalf("line %d status = %q, want %q", i, line.Status, wantStatus)
		}
	}
}

// An existing destination is refused before staging and before any build
// run: prior content is preserved untouched (old files, empty directory,
// dangling symlink), and a lookup error other than not-exist is refused
// instead of guessed at.
func TestBuildRejectsExistingDestBeforeStaging(t *testing.T) {
	run := func(t *testing.T, base, dest string) error {
		t.Helper()
		log := setBuildLog(t)
		repo := filepath.Join(base, "repo")
		if err := os.MkdirAll(repo, 0o755); err != nil {
			t.Fatal(err)
		}
		_, err := Build(context.Background(), BuildConfig{
			Version:      "v1.2.3",
			RepoDir:      repo,
			Dest:         dest,
			GoExecutable: helperPath(t),
		})
		if lines := readLog(t, log); lines != nil {
			t.Fatalf("fake go ran before the refusal: %v", lines)
		}
		return err
	}

	t.Run("existing files", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "dist")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, name := range append(append([]string{}, artifactNames()...), sumsFileName) {
			if err := os.WriteFile(filepath.Join(dest, name), []byte("old "+name+"\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		err := run(t, base, dest)
		if err == nil || !strings.Contains(err.Error(), "refusing to build over existing destination") {
			t.Fatalf("err = %v", err)
		}
		for _, name := range append(append([]string{}, artifactNames()...), sumsFileName) {
			got, readErr := os.ReadFile(filepath.Join(dest, name))
			if readErr != nil || string(got) != "old "+name+"\n" {
				t.Fatalf("%s changed: %q err=%v", name, got, readErr)
			}
		}
		if n := countStaging(t, base); n != 0 {
			t.Fatalf("%d staging directories created", n)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "dist")
		if err := os.MkdirAll(dest, 0o755); err != nil {
			t.Fatal(err)
		}
		err := run(t, base, dest)
		if err == nil || !strings.Contains(err.Error(), "refusing to build over existing destination") {
			t.Fatalf("err = %v", err)
		}
		entries, err := os.ReadDir(dest)
		if err != nil || len(entries) != 0 {
			t.Fatalf("dest content changed: %v err=%v", entries, err)
		}
		if n := countStaging(t, base); n != 0 {
			t.Fatalf("%d staging directories created", n)
		}
	})

	t.Run("dangling symlink", func(t *testing.T) {
		base := t.TempDir()
		dest := filepath.Join(base, "dist")
		if err := os.Symlink(filepath.Join(base, "gone"), dest); err != nil {
			t.Fatal(err)
		}
		err := run(t, base, dest)
		if err == nil || !strings.Contains(err.Error(), "refusing to build over existing destination") {
			t.Fatalf("err = %v", err)
		}
		// The symlink itself is untouched (Lstat, not Stat).
		target, err := os.Readlink(dest)
		if err != nil || target != filepath.Join(base, "gone") {
			t.Fatalf("symlink changed: %q err=%v", target, err)
		}
		if n := countStaging(t, base); n != 0 {
			t.Fatalf("%d staging directories created", n)
		}
	})

	t.Run("lookup error", func(t *testing.T) {
		if os.Getuid() == 0 {
			t.Skip("root ignores permission bits")
		}
		base := t.TempDir()
		locked := filepath.Join(base, "locked")
		if err := os.MkdirAll(locked, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			os.Chmod(locked, 0o755)
		})
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		err := run(t, base, filepath.Join(locked, "dist"))
		if err == nil || !strings.Contains(err.Error(), "checking destination") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestBuildRejectsNonRegularStagedOutput(t *testing.T) {
	base := t.TempDir()
	dest := filepath.Join(base, "dist")
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	setBuildLog(t)
	t.Setenv("RELEASE_TEST_OUT_MODE", "dir")
	_, err := Build(context.Background(), BuildConfig{
		Version:      "v1.2.3",
		RepoDir:      repo,
		Dest:         dest,
		GoExecutable: helperPath(t),
	})
	if err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("error = %v, want non-regular-file refusal", err)
	}
	if n := countStaging(t, base); n != 0 {
		t.Fatalf("%d staging directories left behind", n)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("dest created despite the failure: %v", err)
	}
}

// The real relative-path consumer: a relative --dest and a different
// relative --repo, run from a fresh cwd. The destination must be
// cwd-anchored with exact bytes, the build argv must be absolute, and
// nothing may land under the repo directory.
func TestBuildRelativeDestAndDifferentRepo(t *testing.T) {
	base := t.TempDir()
	t.Chdir(base)
	if err := os.MkdirAll("repo", 0o755); err != nil {
		t.Fatal(err)
	}
	log := setBuildLog(t)
	res, err := Build(context.Background(), BuildConfig{
		Version:      "v1.2.3",
		RepoDir:      "repo",
		Dest:         "dist",
		GoExecutable: helperPath(t),
	})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if res.Dest != filepath.Join(base, "dist") {
		t.Fatalf("dest = %q, want the cwd-anchored path", res.Dest)
	}
	for _, tg := range targets {
		name := ArtifactName(tg.os, tg.arch)
		got, err := os.ReadFile(filepath.Join("dist", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		want := []byte(expectedPayload(tg.os, tg.arch, "v1.2.3"))
		if string(got) != string(want) {
			t.Fatalf("%s bytes = %q, want %q", name, got, want)
		}
	}
	sums, err := os.ReadFile(filepath.Join("dist", sumsFileName))
	if err != nil || string(sums) != expectedSums("v1.2.3") {
		t.Fatalf("SHA256SUMS = %q err=%v", sums, err)
	}
	// Nothing leaked into the repo directory.
	repoEntries, err := os.ReadDir("repo")
	if err != nil {
		t.Fatal(err)
	}
	if len(repoEntries) != 0 {
		t.Fatalf("repo directory not empty: %v", repoEntries)
	}
	// The build argv is absolute and never under the repo directory.
	bl := parseBuildLog(t, log)
	if len(bl) != len(targets) {
		t.Fatalf("want %d build log lines, got %d", len(targets), len(bl))
	}
	for i, line := range bl {
		if !filepath.IsAbs(line.Argv[6]) {
			t.Fatalf("line %d -o not absolute: %v", i, line.Argv)
		}
		if strings.Contains(line.Argv[6], string(os.PathSeparator)+"repo"+string(os.PathSeparator)) {
			t.Fatalf("line %d staging under the repo directory: %v", i, line.Argv)
		}
	}
	if n := countStaging(t, base); n != 0 {
		t.Fatalf("%d staging directories left behind", n)
	}
}

func TestBuildGoExecutablePathLookup(t *testing.T) {
	// A restricted PATH with no explicit go executable fails before any
	// execution; the explicit trusted absolute path works under the same
	// PATH.
	empty := t.TempDir()
	t.Setenv("PATH", empty)
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	log := setBuildLog(t)
	_, err := Build(context.Background(), BuildConfig{
		Version: "v1.2.3",
		RepoDir: repo,
		Dest:    filepath.Join(base, "dist"),
	})
	if err == nil || !strings.Contains(err.Error(), "not found in PATH") {
		t.Fatalf("restricted PATH without explicit go: err = %v", err)
	}
	if lines := readLog(t, log); lines != nil {
		t.Fatalf("fake go ran: %v", lines)
	}

	base2 := t.TempDir()
	repo2 := filepath.Join(base2, "repo")
	if err := os.MkdirAll(repo2, 0o755); err != nil {
		t.Fatal(err)
	}
	setBuildLog(t)
	if _, err := Build(context.Background(), BuildConfig{
		Version:      "v1.2.3",
		RepoDir:      repo2,
		Dest:         filepath.Join(base2, "dist"),
		GoExecutable: helperPath(t),
	}); err != nil {
		t.Fatalf("restricted PATH with explicit go: %v", err)
	}
}

func TestPublishRunsGhWithExactArgv(t *testing.T) {
	dest := buildValidDist(t, "v1.2.3")
	log := setGHLog(t)
	secret := "super-secret-token-value"
	t.Setenv("GH_TOKEN", secret)
	res, err := Publish(context.Background(), PublishConfig{
		Version:      "v1.2.3",
		Repo:         "djalmajr/herdr-soho",
		Dest:         dest,
		GHExecutable: helperPath(t),
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if res.Tag != "v1.2.3" || res.Repo != "djalmajr/herdr-soho" || res.Prerelease {
		t.Fatalf("result = %+v", res)
	}
	fields := ghFields(t, log)
	// release create TAG <7 files> --repo REPO --generate-notes
	if len(fields) != 3+7+3 {
		t.Fatalf("gh argv = %v", fields)
	}
	if fields[0] != "release" || fields[1] != "create" || fields[2] != "v1.2.3" {
		t.Fatalf("gh argv prefix = %v", fields[:3])
	}
	names := append(append([]string{}, artifactNames()...), sumsFileName)
	wantFiles := make([]string, len(names))
	for i, name := range names {
		wantFiles[i] = filepath.Join(dest, name)
		if fields[3+i] != wantFiles[i] {
			t.Fatalf("gh argv file %d = %q, want %q", i, fields[3+i], wantFiles[i])
		}
	}
	if fields[10] != "--repo" || fields[11] != "djalmajr/herdr-soho" || fields[12] != "--generate-notes" {
		t.Fatalf("gh argv tail = %v", fields[10:13])
	}
	for _, f := range fields {
		if f == "--prerelease" {
			t.Fatal("--prerelease passed for a non-hyphen tag")
		}
	}
	// res.Files must match the exact argv file list.
	if strings.Join(res.Files, "\n") != strings.Join(wantFiles, "\n") {
		t.Fatalf("res.Files = %v", res.Files)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), secret) {
		t.Fatal("gh log exposed the token")
	}
}

// ghFields parses the single recorded gh argv (JSON, no prefix).
func ghFields(t *testing.T, log string) []string {
	t.Helper()
	lines := readLog(t, log)
	if len(lines) != 1 {
		t.Fatalf("gh log: want one line, got %v", lines)
	}
	var l helperGHLine
	if err := json.Unmarshal([]byte(lines[0]), &l); err != nil {
		t.Fatalf("unparseable gh log line %q: %v", lines[0], err)
	}
	if l.Kind != "gh" {
		t.Fatalf("gh log kind = %q", l.Kind)
	}
	return l.Argv
}

func TestPublishPrereleaseOnlyForHyphenTags(t *testing.T) {
	for version, wantPre := range map[string]bool{"v0.9.0-rc.1": true, "v1.0.0-beta.2": true, "v2.0.0": false} {
		t.Run(version, func(t *testing.T) {
			dest := buildValidDist(t, version)
			log := setGHLog(t)
			res, err := Publish(context.Background(), PublishConfig{
				Version:      version,
				Repo:         "djalmajr/herdr-soho",
				Dest:         dest,
				GHExecutable: helperPath(t),
			})
			if err != nil {
				t.Fatalf("publish: %v", err)
			}
			if res.Prerelease != wantPre {
				t.Fatalf("prerelease = %v, want %v", res.Prerelease, wantPre)
			}
			fields := ghFields(t, log)
			if wantPre {
				if len(fields) != 14 || fields[13] != "--prerelease" {
					t.Fatalf("gh argv = %v", fields)
				}
			} else if len(fields) != 13 {
				t.Fatalf("gh argv = %v", fields)
			}
		})
	}
}

func TestPublishRefusesBadInputsBeforeGh(t *testing.T) {
	mutate := func(t *testing.T, dest string) func() {
		t.Helper()
		orig, err := os.ReadFile(filepath.Join(dest, sumsFileName))
		if err != nil {
			t.Fatal(err)
		}
		return func() {
			if err := os.WriteFile(filepath.Join(dest, sumsFileName), orig, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	run := func(t *testing.T, version, repo, dest string) error {
		t.Helper()
		log := setGHLog(t)
		_, err := Publish(context.Background(), PublishConfig{
			Version:      version,
			Repo:         repo,
			Dest:         dest,
			GHExecutable: helperPath(t),
		})
		if lines := readLog(t, log); lines != nil {
			t.Fatalf("gh ran before validation: %v", lines)
		}
		return err
	}

	t.Run("corrupt digest", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		restore := mutate(t, dest)
		defer restore()
		data, _ := os.ReadFile(filepath.Join(dest, sumsFileName))
		lines := strings.Split(string(data), "\n")
		repl := byte('0')
		if lines[0][0] == '0' {
			repl = '1'
		}
		lines[0] = string(repl) + lines[0][1:] // break the first hex char
		if err := os.WriteFile(filepath.Join(dest, sumsFileName), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "does not match file bytes") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("duplicate entry", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		restore := mutate(t, dest)
		defer restore()
		data, _ := os.ReadFile(filepath.Join(dest, sumsFileName))
		lines := strings.Split(string(data), "\n")
		if err := os.WriteFile(filepath.Join(dest, sumsFileName), []byte(strings.Join(append(lines, lines[0]), "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "duplicate") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("missing artifact", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		if err := os.Remove(filepath.Join(dest, "herdr-soho_linux_arm64")); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "herdr-soho_linux_arm64") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unexpected entry", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		restore := mutate(t, dest)
		defer restore()
		data, _ := os.ReadFile(filepath.Join(dest, sumsFileName))
		extra := fmt.Sprintf("%x  herdr-soho_extra\n", sha256.Sum256([]byte("x")))
		if err := os.WriteFile(filepath.Join(dest, sumsFileName), append(data, []byte(extra)...), 0o644); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "unexpected entry") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("missing entry", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		restore := mutate(t, dest)
		defer restore()
		data, _ := os.ReadFile(filepath.Join(dest, sumsFileName))
		lines := strings.Split(string(data), "\n")
		if err := os.WriteFile(filepath.Join(dest, sumsFileName), []byte(strings.Join(append(lines[1:], ""), "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "exactly 6 entries") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("malformed line", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		restore := mutate(t, dest)
		defer restore()
		data, _ := os.ReadFile(filepath.Join(dest, sumsFileName))
		lines := strings.Split(string(data), "\n")
		lines[0] = "nothex  herdr-soho_darwin_amd64"
		if err := os.WriteFile(filepath.Join(dest, sumsFileName), []byte(strings.Join(lines, "\n")), 0o644); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "malformed") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("missing sums file", func(t *testing.T) {
		dest := buildValidDist(t, "v1.2.3")
		if err := os.Remove(filepath.Join(dest, sumsFileName)); err != nil {
			t.Fatal(err)
		}
		err := run(t, "v1.2.3", "djalmajr/herdr-soho", dest)
		if err == nil || !strings.Contains(err.Error(), "SHA256SUMS") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("dev version", func(t *testing.T) {
		err := run(t, "dev", "djalmajr/herdr-soho", "does-not-matter")
		if err == nil || !strings.Contains(err.Error(), "v-prefixed release tag") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("bad repo", func(t *testing.T) {
		for _, repo := range []string{"djalmajr", "a/b/c", "../repo", "a b/repo", `own"er/repo`} {
			err := run(t, "v1.2.3", repo, "does-not-matter")
			if err == nil {
				t.Fatalf("repo %q accepted", repo)
			}
		}
	})
}

func TestPublishSurfacesGhFailureWithoutSecrets(t *testing.T) {
	dest := buildValidDist(t, "v1.2.3")
	setGHLog(t)
	secret := "another-secret-token"
	t.Setenv("GH_TOKEN", secret)
	t.Setenv("RELEASE_TEST_GH_FAIL", "1")
	_, err := Publish(context.Background(), PublishConfig{
		Version:      "v1.2.3",
		Repo:         "djalmajr/herdr-soho",
		Dest:         dest,
		GHExecutable: helperPath(t),
	})
	if err == nil || !strings.Contains(err.Error(), "fake gh: injected publish failure") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatal("error exposed the token")
	}
}

func TestResolveBuildVersion(t *testing.T) {
	cases := []struct {
		event, refName, ref string
		want                string
		err                 bool
	}{
		{"workflow_dispatch", "main", "refs/heads/main", DevVersion, false},
		{"workflow_dispatch", "v9.9.9", "refs/tags/v9.9.9", DevVersion, false},
		{"push", "v1.2.3", "refs/tags/v1.2.3", "v1.2.3", false},
		{"push", "", "refs/tags/v2.0.0", "v2.0.0", false},
		{"push", "main", "refs/heads/main", "", true},
		{"push", "v1.2.3 ", "refs/tags/v1.2.3 ", "", true},
		{"push", "v1.2.3", "refs/heads/v1.2.3", "", true},
		{"push", "v1.2.3", "refs/tags/v9.9.9", "", true},
		{"push", "v1.2.3", "", "v1.2.3", false},
		{"push", "../v", "refs/tags/../v", "", true},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			got, err := ResolveBuildVersion(c.event, c.refName, c.ref)
			if c.err {
				if err == nil {
					t.Fatalf("got %q, want error", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q err=%v, want %q", got, err, c.want)
			}
		})
	}
}

func TestResolvePublishRef(t *testing.T) {
	cases := []struct {
		event, ref string
		want       string
		err        bool
	}{
		{"push", "refs/tags/v1.2.3", "v1.2.3", false},
		{"push", "refs/heads/v1.2.3", "", true},
		{"push", "refs/tags/main", "", true},
		{"push", "refs/tags/v..1", "", true},
		{"push", "refs/tags/dev", "", true},
		{"push", "refs/tags/v1.2.3/extra", "", true},
		{"workflow_dispatch", "refs/tags/v1.2.3", "", true},
		{"push", "refs/tags/v 1", "", true},
	}
	for i, c := range cases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			got, err := ResolvePublishRef(c.event, c.ref)
			if c.err {
				if err == nil {
					t.Fatalf("got %q, want error", got)
				}
				return
			}
			if err != nil || got != c.want {
				t.Fatalf("got %q err=%v, want %q", got, err, c.want)
			}
		})
	}
}

func TestValidateVersion(t *testing.T) {
	valid := []string{DevVersion, "v1", "v1.2.3", "v0.9.0-rc.1", "v2_beta.1"}
	for _, v := range valid {
		if err := ValidateVersion(v); err != nil {
			t.Fatalf("ValidateVersion(%q) = %v, want nil", v, err)
		}
	}
	invalid := []string{"", "1.2.3", "v", "v ", "v a", `v"a'`, "../v", "v..", "v/x", "V1", "v\t1"}
	for _, v := range invalid {
		if err := ValidateVersion(v); err == nil {
			t.Fatalf("ValidateVersion(%q) accepted, want error", v)
		}
	}
}

func TestValidateRepo(t *testing.T) {
	valid := []string{"djalmajr/herdr-soho", "a-b/c_d", "Owner/Repo.v2"}
	for _, r := range valid {
		if err := ValidateRepo(r); err != nil {
			t.Fatalf("ValidateRepo(%q) = %v, want nil", r, err)
		}
	}
	invalid := []string{"", "owner", "a/b/c", "/repo", "owner/", "../repo", "a/../b", "a b/c", `o"n/repo`}
	for _, r := range invalid {
		if err := ValidateRepo(r); err == nil {
			t.Fatalf("ValidateRepo(%q) accepted, want error", r)
		}
	}
}

func TestArtifactName(t *testing.T) {
	cases := map[string]string{
		"darwin:amd64":  "herdr-soho_darwin_amd64",
		"linux:arm64":   "herdr-soho_linux_arm64",
		"windows:amd64": "herdr-soho_windows_amd64.exe",
		"windows:arm64": "herdr-soho_windows_arm64.exe",
	}
	for key, want := range cases {
		osName, arch, ok := strings.Cut(key, ":")
		if !ok {
			t.Fatalf("bad test key %q", key)
		}
		if got := ArtifactName(osName, arch); got != want {
			t.Fatalf("ArtifactName(%q, %q) = %q, want %q", osName, arch, got, want)
		}
	}
}

func TestIsPrerelease(t *testing.T) {
	if !IsPrerelease("v0.9.0-rc.1") || !IsPrerelease("v1.0.0-beta.2") {
		t.Fatal("hyphen tags must be prereleases")
	}
	if IsPrerelease("v1.0.0") || IsPrerelease(DevVersion) {
		t.Fatal("non-hyphen tags are not prereleases")
	}
}
