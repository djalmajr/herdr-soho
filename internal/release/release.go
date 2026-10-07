// Package release builds the six herdr-soho release binaries and publishes
// them with the native gh CLI, replacing the Node/Bash release workflow.
// Standard library only: go and gh are external Go CLIs invoked by argv,
// never through a shell.
package release

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// DevVersion is the build version for manual (workflow_dispatch) runs.
	DevVersion = "dev"
	// mainPackage is the package built into every release binary.
	mainPackage = "./cmd/herdr-soho"
	// versionLdflags is the exact ldflags format the release binaries get:
	// size stripping plus the version injected into the CLI.
	versionLdflags = "-s -w -X github.com/djalmajr/herdr-soho/internal/cli.version=%s"
	// sumsFileName is the checksum manifest name, matching the installer.
	sumsFileName = "SHA256SUMS"
	// stagingPattern prefixes the fresh sibling staging directory.
	stagingPattern = ".herdr-soho-release-"
)

// target is one release build target.
type target struct{ os, arch string }

// targets is the exact six release builds. This fixed order is also the
// lexicographic order of the artifact names, so the generated SHA256SUMS
// matches the old workflow's `sha256sum herdr-soho_*` output byte for byte.
var targets = [...]target{
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"linux", "amd64"}, {"linux", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

// ArtifactName keeps the installer asset naming: herdr-soho_<os>_<arch>,
// with the .exe suffix on windows.
func ArtifactName(osName, arch string) string {
	name := "herdr-soho_" + osName + "_" + arch
	if osName == "windows" {
		name += ".exe"
	}
	return name
}

// isTagChar accepts the safe ASCII release characters: letters, digits,
// dot, dash and underscore. Whitespace, quotes, slashes and everything
// else are rejected by validation.
func isTagChar(c byte) bool {
	return c >= 'a' && c <= 'z' ||
		c >= 'A' && c <= 'Z' ||
		c >= '0' && c <= '9' ||
		c == '.' || c == '-' || c == '_'
}

// ValidateVersion accepts DevVersion or a v-prefixed safe ASCII release
// tag: at least one tag character after the "v" prefix, only safe
// characters, no traversal (".."), no whitespace, no quotes.
func ValidateVersion(v string) error {
	if v == DevVersion {
		return nil
	}
	if !strings.HasPrefix(v, "v") || len(v) < 2 {
		return fmt.Errorf("release: version must be %q or a v-prefixed release tag, got %q", DevVersion, v)
	}
	if strings.Contains(v, "..") {
		return fmt.Errorf("release: version %q contains traversal", v)
	}
	for i := 1; i < len(v); i++ {
		if !isTagChar(v[i]) {
			return fmt.Errorf("release: version %q contains unsafe character %q", v, v[i])
		}
	}
	return nil
}

// ValidateRepo requires exactly one "/" and safe tag-character components:
// OWNER/REPO with no empty part, no traversal, no whitespace or quotes.
func ValidateRepo(repo string) error {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("release: repo must be OWNER/REPO, got %q", repo)
	}
	for _, part := range parts {
		if strings.Contains(part, "..") {
			return fmt.Errorf("release: repo %q contains traversal", repo)
		}
		for i := 0; i < len(part); i++ {
			if !isTagChar(part[i]) {
				return fmt.Errorf("release: repo %q contains unsafe character %q", repo, part[i])
			}
		}
	}
	return nil
}

// IsPrerelease reports whether the tag carries a hyphen and must be
// published with --prerelease.
func IsPrerelease(tag string) bool {
	return strings.Contains(tag, "-")
}

// ResolveBuildVersion resolves the version for ci-build: DevVersion for
// workflow_dispatch, otherwise a validated v-prefixed tag taken from
// GITHUB_REF_NAME (falling back to GITHUB_REF with the refs/tags/ prefix).
// When a nonempty GITHUB_REF is provided it must be the exact tag ref of
// the resolved version: a branch or a mismatched ref is refused even when
// GITHUB_REF_NAME alone would validate.
func ResolveBuildVersion(eventName, refName, ref string) (string, error) {
	if eventName == "workflow_dispatch" {
		return DevVersion, nil
	}
	v := refName
	if v == "" {
		v = strings.TrimPrefix(ref, "refs/tags/")
	}
	if err := ValidateVersion(v); err != nil {
		return "", fmt.Errorf("release: ci-build cannot resolve a version from GITHUB_REF_NAME/GITHUB_REF: %w", err)
	}
	if ref != "" && ref != "refs/tags/"+v {
		return "", fmt.Errorf("release: GITHUB_REF %q does not match version %q", ref, v)
	}
	return v, nil
}

// ResolvePublishRef validates the ref for ci-publish and returns the tag:
// only a push event on an exact refs/tags/<v-tag> ref publishes. Manual
// runs, branches, and non-v tag names (including dev) are refused.
func ResolvePublishRef(eventName, ref string) (string, error) {
	if eventName != "push" {
		return "", fmt.Errorf("release: ci-publish only runs on push events, got %q", eventName)
	}
	const prefix = "refs/tags/"
	if !strings.HasPrefix(ref, prefix) {
		return "", fmt.Errorf("release: ci-publish requires an exact refs/tags/<tag> ref, got %q", ref)
	}
	tag := strings.TrimPrefix(ref, prefix)
	if tag == DevVersion {
		return "", fmt.Errorf("release: ci-publish tag %q is not a release tag", tag)
	}
	if err := ValidateVersion(tag); err != nil {
		return "", fmt.Errorf("release: ci-publish tag %q is not a valid release tag", tag)
	}
	return tag, nil
}

// BuildConfig controls a release build.
type BuildConfig struct {
	// Version is DevVersion or a validated v-prefixed release tag.
	Version string
	// RepoDir is the repository root that contains mainPackage.
	// Default ".".
	RepoDir string
	// Dest is the output directory. Default "dist".
	Dest string
	// GoExecutable is a trusted absolute path to the go binary. When
	// empty, "go" is resolved from PATH.
	GoExecutable string
}

// BuildResult reports what a successful build wrote.
type BuildResult struct {
	// Version is the version injected into the binaries.
	Version string
	// Dest is the final artifact directory.
	Dest string
	// Artifacts lists the artifact file names in Dest, in target order.
	Artifacts []string
	// Sums is the path of the written SHA256SUMS file.
	Sums string
}

// Build stages the six exact release assets plus SHA256SUMS into a fresh
// sibling directory and publishes the whole completed directory with one
// atomic rename into Dest. RepoDir and Dest are resolved to absolute paths
// anchored to the invoking cwd before any effect, so a relative --repo
// (which changes the build subprocess cwd) cannot redirect staging or the
// destination. Dest must not exist at all — file, empty directory, or
// symlink, checked with Lstat — and the check runs before staging and
// builds, so prior content is never touched. Only the owned staging
// directory is cleaned up. Build never publishes.
func Build(ctx context.Context, cfg BuildConfig) (BuildResult, error) {
	if err := ValidateVersion(cfg.Version); err != nil {
		return BuildResult{}, err
	}
	repoDir := cfg.RepoDir
	if repoDir == "" {
		repoDir = "."
	}
	dest := cfg.Dest
	if dest == "" {
		dest = "dist"
	}
	// Anchor both paths to the invoking cwd before any effect: a relative
	// --repo changes the build subprocess cwd (cmd.Dir), which must not
	// change the meaning of the staging or destination paths.
	repoDir, err := filepath.Abs(repoDir)
	if err != nil {
		return BuildResult{}, fmt.Errorf("release: resolving repo root: %w", err)
	}
	dest, err = filepath.Abs(dest)
	if err != nil {
		return BuildResult{}, fmt.Errorf("release: resolving destination: %w", err)
	}
	if info, err := os.Stat(repoDir); err != nil || !info.IsDir() {
		return BuildResult{}, fmt.Errorf("release: repo root %s is not a directory", repoDir)
	}
	// The destination must not exist at all: Lstat does not follow links,
	// so a dangling symlink is seen and refused like any other existing
	// entry; any lookup error other than not-exist is refused too, because
	// an unknown state must not be renamed over. This runs before staging
	// and before any build.
	if _, err := os.Lstat(dest); err == nil {
		return BuildResult{}, fmt.Errorf("release: refusing to build over existing destination %s", dest)
	} else if !os.IsNotExist(err) {
		return BuildResult{}, fmt.Errorf("release: checking destination %s: %v", dest, err)
	}
	goExe, err := resolveExecutable(cfg.GoExecutable, "go")
	if err != nil {
		return BuildResult{}, err
	}
	// Fresh sibling staging directory: on the same filesystem as Dest, so
	// the final moves are atomic renames. Only this owned directory is
	// ever cleaned up.
	staging, err := os.MkdirTemp(filepath.Dir(dest), stagingPattern)
	if err != nil {
		return BuildResult{}, fmt.Errorf("release: staging: %w", err)
	}
	defer os.RemoveAll(staging)

	// Bounded sequential builds: one target at a time, no shell.
	var staged []string
	for _, t := range targets {
		name := ArtifactName(t.os, t.arch)
		out := filepath.Join(staging, name)
		cmd := exec.CommandContext(ctx, goExe, "build", "-trimpath", "-buildvcs=false",
			"-ldflags", fmt.Sprintf(versionLdflags, cfg.Version), "-o", out, mainPackage)
		cmd.Dir = repoDir
		cmd.Env = targetEnv(t)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return BuildResult{}, fmt.Errorf("release: building %s: %v: %s", name, err, tail(stderr.String(), 400))
		}
		if info, err := os.Stat(out); err != nil || !info.Mode().IsRegular() {
			return BuildResult{}, fmt.Errorf("release: %s is not a regular file", name)
		}
		staged = append(staged, out)
	}

	// Deterministic SHA256SUMS: sha256sum layout, target (lexicographic)
	// order, one entry per artifact.
	var sums strings.Builder
	for i, t := range targets {
		digest, err := fileDigest(staged[i])
		if err != nil {
			return BuildResult{}, err
		}
		fmt.Fprintf(&sums, "%s  %s\n", digest, ArtifactName(t.os, t.arch))
	}

	sumsPath := filepath.Join(staging, sumsFileName)
	if err := os.WriteFile(sumsPath, []byte(sums.String()), 0o644); err != nil {
		return BuildResult{}, fmt.Errorf("release: writing %s: %w", sumsFileName, err)
	}

	// Publish the whole completed staging directory with one atomic rename
	// into the fresh (Lstat-verified absent) destination: no partial moves,
	// and the destination appears only fully populated. Chmod first so the
	// published directory keeps the 0755 mode the old workflow produced.
	if err := os.Chmod(staging, 0o755); err != nil {
		return BuildResult{}, fmt.Errorf("release: staging: %w", err)
	}
	if err := os.Rename(staging, dest); err != nil {
		return BuildResult{}, fmt.Errorf("release: moving staging into %s: %w", dest, err)
	}
	names := make([]string, len(targets))
	for i, t := range targets {
		names[i] = ArtifactName(t.os, t.arch)
	}
	return BuildResult{
		Version:   cfg.Version,
		Dest:      dest,
		Artifacts: names,
		Sums:      filepath.Join(dest, sumsFileName),
	}, nil
}

// PublishConfig controls a release publish.
type PublishConfig struct {
	// Version is the v-prefixed release tag to publish (dev is refused).
	Version string
	// Repo is the OWNER/REPO passed explicitly to gh as --repo.
	Repo string
	// Dest is the artifact directory. Default "dist".
	Dest string
	// GHExecutable is a trusted absolute path to the gh CLI. When empty,
	// "gh" is resolved from PATH.
	GHExecutable string
}

// PublishResult reports what a successful publish passed to gh.
type PublishResult struct {
	// Tag is the release tag.
	Tag string
	// Repo is the repository gh was told to use.
	Repo string
	// Files is the exact argv file list (six artifacts plus SHA256SUMS)
	// in deterministic order.
	Files []string
	// Prerelease reports whether --prerelease was passed.
	Prerelease bool
}

// Publish validates the tag, the repo, and the exact six artifacts plus
// SHA256SUMS (bytes, digests, unique entries) before executing the native
// gh CLI: `gh release create TAG FILES --repo OWNER/REPO --generate-notes`
// plus --prerelease only on hyphen tags. No shell is involved, and no
// credential is invented, printed, or passed on the command line: gh
// reads GH_TOKEN from the inherited environment.
func Publish(ctx context.Context, cfg PublishConfig) (PublishResult, error) {
	if !strings.HasPrefix(cfg.Version, "v") {
		return PublishResult{}, fmt.Errorf("release: publish requires a v-prefixed release tag, got %q", cfg.Version)
	}
	if err := ValidateVersion(cfg.Version); err != nil {
		return PublishResult{}, err
	}
	if err := ValidateRepo(cfg.Repo); err != nil {
		return PublishResult{}, err
	}
	dest := cfg.Dest
	if dest == "" {
		dest = "dist"
	}
	ghExe, err := resolveExecutable(cfg.GHExecutable, "gh")
	if err != nil {
		return PublishResult{}, err
	}

	// The exact six artifacts, all required regular files.
	paths := make([]string, len(targets))
	for i, t := range targets {
		paths[i] = filepath.Join(dest, ArtifactName(t.os, t.arch))
	}
	for _, p := range append(append([]string{}, paths...), filepath.Join(dest, sumsFileName)) {
		if err := requireRegularFile(p); err != nil {
			return PublishResult{}, err
		}
	}

	// SHA256SUMS: unique, well-formed entries for exactly the six names,
	// and every digest must match the file bytes.
	entries, err := parseSumsFile(filepath.Join(dest, sumsFileName))
	if err != nil {
		return PublishResult{}, err
	}
	for i, t := range targets {
		name := ArtifactName(t.os, t.arch)
		actual, err := fileDigest(paths[i])
		if err != nil {
			return PublishResult{}, err
		}
		if entries[name] != actual {
			return PublishResult{}, fmt.Errorf("release: SHA256SUMS entry for %s does not match file bytes", name)
		}
	}

	// Deterministic argv: tag, the six artifacts, SHA256SUMS, then the
	// explicit repo and notes flags; --prerelease only on hyphen tags.
	files := append(append([]string{}, paths...), filepath.Join(dest, sumsFileName))
	prerelease := IsPrerelease(cfg.Version)
	argv := append([]string{"release", "create", cfg.Version}, files...)
	argv = append(argv, "--repo", cfg.Repo, "--generate-notes")
	if prerelease {
		argv = append(argv, "--prerelease")
	}
	cmd := exec.CommandContext(ctx, ghExe, argv...)
	var stderr, stdout bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		return PublishResult{}, fmt.Errorf("release: gh release create: %v: %s", err, tail(stderr.String(), 400))
	}
	return PublishResult{Tag: cfg.Version, Repo: cfg.Repo, Files: files, Prerelease: prerelease}, nil
}

// parseSumsFile reads a SHA256SUMS manifest and returns its digests keyed
// by artifact name. Every non-blank line must be a valid
// `<64-hex-digest>  <name>` entry (optional '*' marker) naming one of the
// six release artifacts; malformed lines, unknown names, duplicates, and a
// wrong entry count are refused.
func parseSumsFile(path string) (map[string]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("release: reading %s: %v", sumsFileName, err)
	}
	known := map[string]bool{}
	for _, t := range targets {
		known[ArtifactName(t.os, t.arch)] = true
	}
	entries := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || !isHex64(fields[0]) {
			return nil, fmt.Errorf("release: malformed %s line: %q", sumsFileName, line)
		}
		name := fields[1]
		if strings.HasPrefix(name, "*") {
			name = name[1:]
		}
		if !known[name] {
			return nil, fmt.Errorf("release: %s has an unexpected entry for %q", sumsFileName, name)
		}
		if _, dup := entries[name]; dup {
			return nil, fmt.Errorf("release: %s has duplicate entries for %s", sumsFileName, name)
		}
		entries[name] = strings.ToLower(fields[0])
	}
	if len(entries) != len(targets) {
		return nil, fmt.Errorf("release: %s must list exactly %d entries, got %d", sumsFileName, len(targets), len(entries))
	}
	return entries, nil
}

// requireRegularFile verifies that path exists and is a regular file.
func requireRegularFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("release: %s: %v", path, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("release: %s is not a regular file", path)
	}
	return nil
}

// resolveExecutable returns a trusted absolute executable path. When the
// explicit path is empty the name is resolved from PATH; a relative
// explicit path is refused.
func resolveExecutable(explicit, name string) (string, error) {
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return "", fmt.Errorf("release: %s executable %q must be an absolute path", name, explicit)
		}
		return explicit, nil
	}
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("release: %s executable not found in PATH", name)
	}
	return p, nil
}

// targetEnv derives the per-target build environment from the parent
// environment, replacing GOOS/GOARCH/CGO_ENABLED in place so a duplicate
// parent variable can never shadow the target values (the C environment
// honors the first match).
func targetEnv(t target) []string {
	values := map[string]string{"GOOS": t.os, "GOARCH": t.arch, "CGO_ENABLED": "0"}
	env := os.Environ()
	out := make([]string, 0, len(env)+len(values))
	replaced := map[string]bool{}
	for _, kv := range env {
		name := kv
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			name = kv[:eq]
		}
		if want, ok := values[name]; ok {
			if !replaced[name] {
				out = append(out, name+"="+want)
				replaced[name] = true
			}
			continue
		}
		out = append(out, kv)
	}
	for _, name := range []string{"GOOS", "GOARCH", "CGO_ENABLED"} {
		if !replaced[name] {
			out = append(out, name+"="+values[name])
		}
	}
	return out
}

// fileDigest returns the lowercase hex SHA-256 of the file at path.
func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isHex64 reports whether s is exactly 64 hexadecimal characters.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// tail trims s to its last n bytes for bounded error messages.
func tail(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
