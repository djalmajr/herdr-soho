package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Options selects the inputs and boundaries of one prepare or probes run.
type Options struct {
	// Destination is the prepared worker copy (prepare) or the completed
	// worker destination to measure (probes).
	Destination string
	// Fixture is the fixture directory holding MANIFEST.sha256, brief.md and
	// the hidden probes.
	Fixture string
	// GoExecutable is an optional trusted absolute path to the Go toolchain
	// used for the hidden probe run; empty resolves 'go' from PATH.
	GoExecutable string
	// Repository is the repository boundary that destinations must stay
	// outside of; empty defaults to the invoking working directory.
	Repository string
	// TempRoot is the optional directory in which the probes runner creates
	// its owned temporary copy; empty uses the process temporary directory.
	TempRoot string
}

// PrepareResult is the prepare result object; its JSON fields mirror the
// legacy prepare.mjs stdout and .eval-run.json payloads.
type PrepareResult struct {
	Fixture     string `json:"fixture"`
	Destination string `json:"destination"`
	PreparedAt  string `json:"prepared_at"`
	ManifestOK  bool   `json:"manifest_ok"`
}

// runMetadata is the .eval-run.json marker written into a prepared
// destination; its field order mirrors the legacy payload.
type runMetadata struct {
	Fixture    string `json:"fixture"`
	PreparedAt string `json:"prepared_at"`
	ManifestOK bool   `json:"manifest_ok"`
}

// Prepare validates the fixture manifest, then copies exactly the
// manifest-listed regular files into a fresh destination outside the
// repository and writes the exclusive .eval-run.json marker. On any failure
// after the destination directory was created, only that newly created
// destination is removed.
func Prepare(ctx context.Context, o Options) (PrepareResult, error) {
	if err := ctx.Err(); err != nil {
		return PrepareResult{}, err
	}
	fixture, err := readFixtureManifest(o.Fixture)
	if err != nil {
		return PrepareResult{}, err
	}
	repository, err := resolveRepository(o.Repository)
	if err != nil {
		return PrepareResult{}, err
	}
	dest, err := checkDestination(repository, o.Destination, false)
	if err != nil {
		return PrepareResult{}, err
	}
	// Check the inside-fixture boundary physically as well: on platforms
	// where the destination parent is a symlink (for example macOS /var),
	// the logical path and the fixture's real root share no prefix.
	physicalDest, err := physicalCandidate(dest)
	if err != nil {
		return PrepareResult{}, err
	}
	if within(fixture.Root, dest) || within(fixture.Root, physicalDest) {
		return PrepareResult{}, fmt.Errorf("destination must not be inside the fixture")
	}
	if err := ctx.Err(); err != nil {
		return PrepareResult{}, err
	}
	parent := filepath.Dir(dest)
	parentStat, err := os.Stat(parent)
	if err != nil || !parentStat.IsDir() {
		return PrepareResult{}, fmt.Errorf("destination parent is not a directory: %s", parent)
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		return PrepareResult{}, err
	}
	prepared, err := copyPreparedFixture(ctx, fixture, dest)
	if err != nil {
		// Remove only the destination this call created, as in the legacy kit.
		_ = os.RemoveAll(dest)
		return PrepareResult{}, err
	}
	return prepared, nil
}

// copyPreparedFixture copies the manifest-listed files with exclusive-
// create semantics and writes the exclusive .eval-run.json marker.
func copyPreparedFixture(ctx context.Context, fixture Fixture, dest string) (PrepareResult, error) {
	relatives := make([]string, 0, len(fixture.Manifest))
	for relative := range fixture.Manifest {
		relatives = append(relatives, relative)
	}
	sort.Strings(relatives)
	for _, relative := range relatives {
		if err := ctx.Err(); err != nil {
			return PrepareResult{}, err
		}
		target := filepath.Join(dest, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return PrepareResult{}, err
		}
		if err := copyFileExclusive(filepath.Join(fixture.Root, filepath.FromSlash(relative)), target); err != nil {
			return PrepareResult{}, err
		}
	}
	preparedAt := isoTimestamp(time.Now())
	payload, err := json.Marshal(runMetadata{
		Fixture:    filepath.Base(fixture.Root),
		PreparedAt: preparedAt,
		ManifestOK: true,
	})
	if err != nil {
		return PrepareResult{}, err
	}
	if err := writeExclusive(filepath.Join(dest, ".eval-run.json"), append(payload, '\n')); err != nil {
		return PrepareResult{}, err
	}
	return PrepareResult{
		Fixture:     filepath.Base(fixture.Root),
		Destination: dest,
		PreparedAt:  preparedAt,
		ManifestOK:  true,
	}, nil
}

// resolveRepository resolves the explicit repository boundary:
// Options.Repository when set, otherwise the invoking working directory,
// always as a real path so symlinked checkouts cannot hide inside it.
func resolveRepository(repository string) (string, error) {
	root := repository
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		root = cwd
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	real, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("repository is not an existing directory: %s", repository)
	}
	return real, nil
}

// isoTimestamp renders a time like JavaScript Date.toISOString: ISO-8601 UTC
// with exactly three fractional digits, so prepared_at keeps the legacy form.
func isoTimestamp(t time.Time) string {
	t = t.UTC()
	return fmt.Sprintf("%s.%03dZ", t.Format("2006-01-02T15:04:05"), t.Nanosecond()/1e6)
}

// copyFileExclusive copies src to dst only if dst does not exist yet
// (COPYFILE_EXCL semantics), preserving the source mode bits.
func copyFileExclusive(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	sourceStat, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, sourceStat.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// writeExclusive creates the file with O_EXCL and writes data to it.
func writeExclusive(path string, data []byte) error {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := out.Write(data); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
