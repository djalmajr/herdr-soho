// Package eval ports the observable prepare/probe/scope contracts of the
// legacy Node evaluation kit (evals/lib.mjs, evals/prepare.mjs,
// evals/run-probes.mjs) to the Go standard library. It prepares isolated
// worker copies from SHA-256-manifested fixtures and measures them with
// hidden Go tests; it never launches models or writes result records.
package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// manifestLineRE matches one MANIFEST.sha256 line: a 64-character lowercase
// hex digest, two spaces, then a relative path.
var manifestLineRE = regexp.MustCompile(`^([a-f0-9]{64})  ([^\r\n]+)$`)

// Fixture is a validated fixture directory: its resolved root path and the
// MANIFEST.sha256 path-to-hash map.
type Fixture struct {
	Root     string
	Manifest map[string]string
}

// WorkerEntry is one path observed inside a prepared worker destination.
// Type is "file", "symlink" or "other"; directories are visited but not
// recorded, mirroring the legacy workerFiles listing.
type WorkerEntry struct {
	Relative string
	Type     string
}

// isHiddenOrGenerated reports whether a fixture-relative path is evaluator-
// or tooling-generated content that must never be copied to a worker.
func isHiddenOrGenerated(relative string) bool {
	switch relative {
	case "MANIFEST.sha256", "ANSWER-KEY.md", ".eval-run.json", "probes":
		return true
	}
	return strings.HasPrefix(relative, "probes/")
}

// validateRelativePath reports whether a manifest or Owned-files path is a
// safe relative POSIX path: no backslash, not absolute, no empty, "." or
// ".." segments.
func validateRelativePath(relative string) bool {
	if relative == "" || strings.ContainsRune(relative, '\\') || strings.HasPrefix(relative, "/") {
		return false
	}
	for _, segment := range strings.Split(relative, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

// within reports whether candidate is inside, or identical to, parent,
// mirroring the legacy isWithin boundary predicate.
func within(parent, candidate string) bool {
	rel, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return false
	}
	return true
}

// physicalCandidate resolves the longest existing ancestor of candidate and
// re-attaches the missing trailing segments, mirroring lib.mjs
// physicalCandidate so a fresh destination whose parent already exists is
// still checked through any symlinked ancestor.
func physicalCandidate(candidate string) (string, error) {
	ancestor := candidate
	var suffix []string
	for {
		_, lstatErr := os.Lstat(ancestor)
		if lstatErr == nil {
			break
		}
		if !os.IsNotExist(lstatErr) {
			return "", lstatErr
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor {
			return "", lstatErr
		}
		suffix = append([]string{filepath.Base(ancestor)}, suffix...)
		ancestor = parent
	}
	real, err := filepath.EvalSymlinks(ancestor)
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{real}, suffix...)...), nil
}

// checkDestination enforces the legacy destination boundary rules: outside
// the repository both logically and physically (that is, not through a
// symlinked ancestor), fresh when mustExist is false, an existing directory
// when it is true.
func checkDestination(repoRoot, candidate string, mustExist bool) (string, error) {
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	physical, err := physicalCandidate(absolute)
	if err != nil {
		return "", err
	}
	if within(repoRoot, absolute) || within(repoRoot, physical) {
		return "", fmt.Errorf("destination must be outside the repository: %s", candidate)
	}
	if mustExist {
		st, err := os.Stat(absolute)
		if err != nil || !st.IsDir() {
			return "", fmt.Errorf("destination must be an existing directory: %s", candidate)
		}
		return absolute, nil
	}
	if _, err := os.Lstat(absolute); err == nil {
		return "", fmt.Errorf("destination already exists: %s", candidate)
	}
	return absolute, nil
}

// sha256File returns the hex SHA-256 digest of a file's bytes.
func sha256File(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// slashRel is filepath.Rel with forward slashes, the fixture-relative path
// form used by manifests and probe listings.
func slashRel(root, absolute string) string {
	rel, err := filepath.Rel(root, absolute)
	if err != nil {
		return absolute
	}
	return filepath.ToSlash(rel)
}

// walkTree visits every entry of the tree rooted at dir. fn runs on every
// entry unless visitDirs is false, in which case directories are recursed
// into but not visited; symlinked directories are never descended into.
func walkTree(root, dir string, visitDirs bool, fn func(absolute, relative string, entry os.DirEntry) error) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		absolute := filepath.Join(dir, entry.Name())
		relative := slashRel(root, absolute)
		isDir := entry.IsDir() && entry.Type()&os.ModeSymlink == 0
		if visitDirs || !isDir {
			if err := fn(absolute, relative, entry); err != nil {
				return err
			}
		}
		if isDir {
			if err := walkTree(root, absolute, visitDirs, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// listFixtureFiles recursively lists the worker-visible regular files of a
// fixture, skipping hidden/generated paths and refusing symlinks or special
// files, mirroring lib.mjs listFixtureFiles.
func listFixtureFiles(root string) ([]string, error) {
	files := []string{}
	err := walkTree(root, root, true, func(_ string, relative string, entry os.DirEntry) error {
		if isHiddenOrGenerated(relative) {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("fixture contains a symlink: %s", relative)
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			if !entry.IsDir() {
				return fmt.Errorf("fixture contains a non-regular file: %s", relative)
			}
			return nil
		}
		files = append(files, relative)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

// readFixtureManifest validates a fixture directory and its MANIFEST.sha256:
// line shape and path safety, no hidden/generated or duplicate paths, no
// symlink traversal, regular files with matching digests, and an exact
// file-set match against the worker-visible fixture contents.
func readFixtureManifest(fixture string) (Fixture, error) {
	absolute, err := filepath.Abs(fixture)
	if err != nil {
		return Fixture{}, err
	}
	st, err := os.Stat(absolute)
	if err != nil || !st.IsDir() {
		return Fixture{}, fmt.Errorf("fixture is not a directory: %s", fixture)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return Fixture{}, err
	}
	manifestPath := filepath.Join(root, "MANIFEST.sha256")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return Fixture{}, fmt.Errorf("invalid manifest: %v", err)
	}
	manifest := map[string]string{}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if line == "" && len(manifest) > 0 {
			continue
		}
		match := manifestLineRE.FindStringSubmatch(line)
		if match == nil || !validateRelativePath(match[2]) {
			return Fixture{}, fmt.Errorf("invalid manifest line: %s", line)
		}
		relative, expected := match[2], match[1]
		if isHiddenOrGenerated(relative) {
			return Fixture{}, fmt.Errorf("manifest must not include hidden or generated path: %s", relative)
		}
		if _, duplicate := manifest[relative]; duplicate {
			return Fixture{}, fmt.Errorf("duplicate manifest path: %s", relative)
		}
		cursor := root
		for _, segment := range strings.Split(relative, "/") {
			cursor = filepath.Join(cursor, segment)
			cursorStat, err := os.Lstat(cursor)
			if err != nil {
				break
			}
			if cursorStat.Mode()&os.ModeSymlink != 0 {
				return Fixture{}, fmt.Errorf("manifest path traverses a symlink: %s", relative)
			}
		}
		filePath := filepath.Join(root, relative)
		fileStat, err := os.Stat(filePath)
		if err != nil || !fileStat.Mode().IsRegular() {
			return Fixture{}, fmt.Errorf("manifest path is not a regular file: %s", relative)
		}
		actual, err := sha256File(filePath)
		if err != nil {
			return Fixture{}, err
		}
		if actual != expected {
			return Fixture{}, fmt.Errorf("manifest checksum mismatch: %s", relative)
		}
		manifest[relative] = expected
	}
	if len(manifest) == 0 {
		return Fixture{}, fmt.Errorf("manifest is empty")
	}
	actualFiles, err := listFixtureFiles(root)
	if err != nil {
		return Fixture{}, err
	}
	listed := make(map[string]bool, len(manifest))
	for relative := range manifest {
		listed[relative] = true
	}
	unlisted := []string{}
	present := make(map[string]bool, len(actualFiles))
	for _, file := range actualFiles {
		present[file] = true
		if !listed[file] {
			unlisted = append(unlisted, file)
		}
	}
	missing := []string{}
	for relative := range manifest {
		if !present[relative] {
			missing = append(missing, relative)
		}
	}
	sort.Strings(missing)
	if len(actualFiles) != len(manifest) || len(unlisted) > 0 || len(missing) > 0 {
		return Fixture{}, fmt.Errorf("manifest file set mismatch (unlisted: %s; missing: %s)", joinOrNone(unlisted), joinOrNone(missing))
	}
	return Fixture{Root: root, Manifest: manifest}, nil
}

func joinOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

// workerFiles lists every entry of a prepared destination (skipping the
// generated .eval-run.json), mirroring lib.mjs workerFiles.
func workerFiles(directory string) ([]WorkerEntry, error) {
	entries := []WorkerEntry{}
	err := walkTree(directory, directory, true, func(_ string, relative string, entry os.DirEntry) error {
		if relative == ".eval-run.json" {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			entries = append(entries, WorkerEntry{Relative: relative, Type: "symlink"})
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		kind := "other"
		if entry.Type().IsRegular() {
			kind = "file"
		}
		entries = append(entries, WorkerEntry{Relative: relative, Type: kind})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Relative < entries[j].Relative })
	return entries, nil
}

var (
	ownedHeadingRE = regexp.MustCompile(`(?m)^## Owned files\s*$`)
	nextHeadingRE  = regexp.MustCompile(`(?m)^## `)
	ownedItemRE    = regexp.MustCompile(`^- ` + "`([^`]+)`" + `(?:\s+—.*)?$`)
)

// ownedPaths parses the brief's "## Owned files" section into the set of
// worker-owned relative paths, mirroring lib.mjs ownedPaths.
func ownedPaths(briefPath string) (map[string]bool, error) {
	text, err := os.ReadFile(briefPath)
	if err != nil {
		return nil, err
	}
	heading := ownedHeadingRE.FindIndex(text)
	if heading == nil {
		return nil, fmt.Errorf("brief is missing an Owned files section")
	}
	rest := string(text)[heading[1]:]
	next := nextHeadingRE.FindIndex([]byte(rest))
	section := rest
	if next != nil {
		section = rest[:next[0]]
	}
	paths := map[string]bool{}
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		item := ownedItemRE.FindStringSubmatch(strings.TrimSpace(line))
		if item == nil || !validateRelativePath(item[1]) {
			return nil, fmt.Errorf("invalid Owned files entry: %s", line)
		}
		paths[item[1]] = true
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("brief Owned files section is empty")
	}
	return paths, nil
}

// sortKeys returns the sorted keys of a set.
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
