// Package install is the native Go core of the release installers
// (install.sh and install.ps1). It downloads the herdr-soho artifact for a
// requested platform from a release base URL, verifies it against
// SHA256SUMS, and replaces the destination file inside the requested
// directory using only the Go standard library.
//
// The core never invokes a shell, curl, awk, shasum, PowerShell, Node, or
// Bun, never changes PATH or the registry, and never executes the
// downloaded bytes. The root CLI owns platform defaults and environment
// resolution, PATH handling, and running the installed binary.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"
)

// defaultReleaseBase is the verified production release location. It stays
// HTTPS even though tests may point ReleaseBase at an httptest server.
const defaultReleaseBase = "https://github.com/djalmajr/herdr-soho/releases"

// defaultHTTPTimeout bounds the whole request (headers and body) when
// Options.Client is nil.
const defaultHTTPTimeout = 5 * time.Minute

const (
	binaryBaseName = "herdr-soho"
	sumsFileName   = "SHA256SUMS"
)

// Options configures a single install run.
//
// OS and Architecture default to runtime.GOOS and runtime.GOARCH when
// empty and must then name a supported platform. InstallDir is always
// explicit and nonempty: the core does not resolve platform defaults or
// environment variables. ReleaseBase defaults to defaultReleaseBase. An
// empty Version selects the latest release.
type Options struct {
	Architecture string
	Client       *http.Client
	InstallDir   string
	OS           string
	ReleaseBase  string
	Version      string
}

// Result describes a successful installation. Digest is the verified
// lowercase sha256 of the installed bytes. ReleaseURL is the download base
// (without the artifact or SHA256SUMS suffix) that was requested.
type Result struct {
	Artifact    string
	Destination string
	Digest      string
	ReleaseURL  string
}

// supportedOS mirrors the platforms the old installers accepted, in
// canonical Go names.
var supportedOS = map[string]bool{"darwin": true, "linux": true, "windows": true}

// Run downloads the release artifact for the requested platform, verifies
// it against SHA256SUMS, and replaces the destination file in
// Options.InstallDir. Every input is validated before any network or
// filesystem mutation. Exclusive temp files are created in the requested
// directory and removed on every failure and cancellation path, and the
// existing destination is preserved when the verification or the
// replacement fails.
func Run(ctx context.Context, opts Options) (Result, error) {
	osName, err := normalizeOS(opts.OS)
	if err != nil {
		return Result{}, err
	}
	arch, err := normalizeArch(opts.Architecture)
	if err != nil {
		return Result{}, err
	}
	if opts.InstallDir == "" {
		return Result{}, errors.New("install: InstallDir must be explicit and nonempty")
	}
	base, err := normalizeReleaseBase(opts.ReleaseBase)
	if err != nil {
		return Result{}, err
	}
	if err := validateVersion(opts.Version); err != nil {
		return Result{}, err
	}
	artifact := artifactName(osName, arch)
	destination := filepath.Join(opts.InstallDir, binaryBaseName)
	if osName == "windows" {
		destination = filepath.Join(opts.InstallDir, binaryBaseName+".exe")
	}
	releaseURL := base + "/latest/download"
	if opts.Version != "" {
		releaseURL = base + "/download/" + opts.Version
	}

	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: defaultHTTPTimeout}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, fmt.Errorf("install: %w", err)
	}

	if err := os.MkdirAll(opts.InstallDir, 0o755); err != nil {
		return Result{}, fmt.Errorf("install: creating install dir: %w", err)
	}
	binTmp, err := os.CreateTemp(opts.InstallDir, "."+binaryBaseName+".*.tmp")
	if err != nil {
		return Result{}, fmt.Errorf("install: creating temp file: %w", err)
	}
	sumsTmp, err := os.CreateTemp(opts.InstallDir, "."+binaryBaseName+".*.sums")
	if err != nil {
		_ = binTmp.Close()
		os.Remove(binTmp.Name())
		return Result{}, fmt.Errorf("install: creating temp file: %w", err)
	}
	binPath, sumsPath := binTmp.Name(), sumsTmp.Name()
	// removeTemp cleans both temp files on every path. After a successful
	// rename it is still correct: the missing binary temp fails silently
	// and the sums file is removed, mirroring the old installers.
	removeTemp := func() {
		_ = binTmp.Close()
		_ = os.Remove(binPath)
		_ = sumsTmp.Close()
		_ = os.Remove(sumsPath)
	}
	defer removeTemp()

	if err := download(ctx, client, releaseURL+"/"+artifact, binTmp); err != nil {
		return Result{}, fmt.Errorf("install: failed to download %s: %w", artifact, err)
	}
	if err := download(ctx, client, releaseURL+"/"+sumsFileName, sumsTmp); err != nil {
		return Result{}, fmt.Errorf("install: failed to download %s: %w", sumsFileName, err)
	}
	if err := binTmp.Close(); err != nil {
		return Result{}, fmt.Errorf("install: closing temp file: %w", err)
	}
	if err := sumsTmp.Close(); err != nil {
		return Result{}, fmt.Errorf("install: closing temp file: %w", err)
	}

	expected, err := expectedDigest(sumsPath, artifact)
	if err != nil {
		return Result{}, fmt.Errorf("install: %w", err)
	}
	digest, err := sha256File(binPath)
	if err != nil {
		return Result{}, fmt.Errorf("install: hashing %s: %w", artifact, err)
	}
	// Both sides are lowercase, so an uppercase SHA256SUMS line still
	// verifies.
	if digest != expected {
		return Result{}, fmt.Errorf("install: sha256 mismatch for %s", artifact)
	}

	// Executable mode on POSIX; the rename below carries the mode to the
	// destination. Windows needs no mode change.
	if runtime.GOOS != "windows" {
		if err := os.Chmod(binPath, 0o755); err != nil {
			return Result{}, fmt.Errorf("install: setting executable mode: %w", err)
		}
	}
	// Same-directory rename: one atomic operation on POSIX. On Windows
	// os.Rename is MoveFileEx with MOVEFILE_REPLACE_EXISTING (see
	// internal/syscall/windows.Rename), so it replaces a normal existing
	// destination. A destination held open by a running process (for
	// example, an executing copy of the installed binary) may make the
	// rename fail; the error is surfaced and the old destination is left
	// untouched, with the temp files cleaned up below.
	if err := os.Rename(binPath, destination); err != nil {
		return Result{}, fmt.Errorf("install: replacing %s: %w", destination, err)
	}
	return Result{
		Artifact:    artifact,
		Destination: destination,
		Digest:      digest,
		ReleaseURL:  releaseURL,
	}, nil
}

// normalizeOS applies the host default and the supported-platform rule.
func normalizeOS(osName string) (string, error) {
	if osName == "" {
		osName = runtime.GOOS
	}
	if !supportedOS[osName] {
		return "", fmt.Errorf("install: unsupported operating system: %s", osName)
	}
	return osName, nil
}

// normalizeArch applies the host default and the supported-architecture
// rule.
func normalizeArch(arch string) (string, error) {
	if arch == "" {
		arch = runtime.GOARCH
	}
	if arch != "amd64" && arch != "arm64" {
		return "", fmt.Errorf("install: unsupported architecture: %s", arch)
	}
	return arch, nil
}

// artifactName keeps the old installer asset naming:
// herdr-soho_<os>_<arch>, with the .exe suffix on windows.
func artifactName(osName, arch string) string {
	name := binaryBaseName + "_" + osName + "_" + arch
	if osName == "windows" {
		name += ".exe"
	}
	return name
}

// normalizeReleaseBase applies the default, trims a trailing slash, and
// validates the base before any network or filesystem mutation: whitespace
// anywhere, credentials, a query, or a fragment are rejected, and only
// http/https with a host is accepted.
func normalizeReleaseBase(base string) (string, error) {
	if base == "" {
		base = defaultReleaseBase
	}
	// The raw string must not carry whitespace before it is parsed: the old
	// installers rejected space, newline, and tab because such bases made
	// the shell and PowerShell URL handling unsafe.
	if idx := strings.IndexFunc(base, unicode.IsSpace); idx >= 0 {
		return "", errors.New("install: release base URL contains whitespace")
	}
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("install: invalid release base URL: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", errors.New("install: release base URL must be http or https")
	}
	if u.Host == "" {
		return "", errors.New("install: release base URL has no host")
	}
	if u.User != nil {
		return "", errors.New("install: release base URL contains credentials")
	}
	if u.RawQuery != "" {
		return "", errors.New("install: release base URL contains a query")
	}
	if u.Fragment != "" {
		return "", errors.New("install: release base URL contains a fragment")
	}
	return base, nil
}

// versionChars is the release character set the old install.sh accepted:
// v, digits, letters, dot, underscore, and dash.
const versionChars = "v0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz._-"

// validateVersion keeps the old tag rule: it must begin with v and use
// only release characters, and it must not contain ".." so a tag can never
// escape the /download/ path. Empty means the latest release and is valid.
func validateVersion(version string) error {
	if version == "" {
		return nil
	}
	if !strings.HasPrefix(version, "v") {
		return fmt.Errorf("install: version %q must begin with v", version)
	}
	for i := 0; i < len(version); i++ {
		if !strings.ContainsRune(versionChars, rune(version[i])) {
			return fmt.Errorf("install: version %q contains invalid characters", version)
		}
	}
	if strings.Contains(version, "..") {
		return fmt.Errorf("install: version %q must not contain %q", version, "..")
	}
	return nil
}

// download stores the response body in dst. Non-2xx statuses are failures,
// mirroring curl -f and Invoke-WebRequest.
func download(ctx context.Context, client *http.Client, rawURL string, dst *os.File) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		status := resp.Status
		if status == "" {
			status = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("unexpected status %s", status)
	}
	_, err = io.Copy(dst, resp.Body)
	return err
}

// expectedDigest reads SHA256SUMS and requires exactly one valid
// 64-hex-digest row for the artifact name. The optional '*' marker is
// accepted, lines for other files are ignored, and the returned digest is
// lowercase.
func expectedDigest(sumsPath, artifact string) (string, error) {
	data, err := os.ReadFile(sumsPath)
	if err != nil {
		return "", fmt.Errorf("reading SHA256SUMS: %w", err)
	}
	var found int
	var digest string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum, name := fields[0], fields[1]
		if strings.HasPrefix(name, "*") {
			name = name[1:]
		}
		if name != artifact || !isHex64(sum) {
			continue
		}
		found++
		if found == 1 {
			digest = strings.ToLower(sum)
		}
	}
	if found != 1 {
		return "", fmt.Errorf("SHA256SUMS has no unique entry for %s", artifact)
	}
	return digest, nil
}

// isHex64 reports whether s is exactly 64 hexadecimal characters.
func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// sha256File returns the lowercase hex sha256 of the file contents.
func sha256File(path string) (string, error) {
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
