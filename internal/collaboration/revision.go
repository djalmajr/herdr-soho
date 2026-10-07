package collaboration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ManifestEntry struct {
	Path string `json:"path"`
	Hash string `json:"hash"`
	Mode uint32 `json:"mode"`
}

type Revision struct {
	Fingerprint string          `json:"fingerprint"`
	Entries     []ManifestEntry `json:"entries"`
}

func Hash(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

func checkedPath(root, relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || strings.ContainsAny(relative, "\\\x00\r\n*?[]:") || relative != filepath.ToSlash(filepath.Clean(relative)) || relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", fmt.Errorf("invalid relative collaboration path %q", relative)
	}
	for _, part := range strings.Split(relative, "/") {
		if strings.EqualFold(part, ".git") {
			return "", errors.New("git metadata is outside the collaboration scope")
		}
	}
	full := filepath.Join(root, filepath.FromSlash(relative))
	for current := full; current != root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("symlink in collaboration scope: %s", relative)
		}
		if filepath.Dir(current) == current {
			return "", errors.New("path escapes collaboration root")
		}
	}
	return full, nil
}

// Fingerprint binds file names, presence, bytes and modes. Directories and
// globs are refused: the orchestrator declares concrete inputs explicitly.
func Fingerprint(root string, paths []string) (Revision, error) {
	if !filepath.IsAbs(root) || len(paths) == 0 {
		return Revision{}, errors.New("revision needs an absolute root and concrete paths")
	}
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		return Revision{}, err
	}
	ordered := append([]string(nil), paths...)
	sort.Strings(ordered)
	r := Revision{Entries: make([]ManifestEntry, 0, len(paths))}
	for i, relative := range ordered {
		if i > 0 && relative == ordered[i-1] {
			return r, errors.New("duplicate collaboration path")
		}
		full, err := checkedPath(root, relative)
		if err != nil {
			return r, err
		}
		info, err := os.Lstat(full)
		if errors.Is(err, os.ErrNotExist) {
			r.Entries = append(r.Entries, ManifestEntry{Path: relative, Hash: "missing"})
			continue
		}
		if err != nil {
			return r, err
		}
		if !info.Mode().IsRegular() {
			return r, fmt.Errorf("collaboration input is not a regular file: %s", relative)
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return r, err
		}
		r.Entries = append(r.Entries, ManifestEntry{Path: relative, Hash: Hash(raw), Mode: uint32(info.Mode().Perm())})
	}
	raw, err := json.Marshal(r.Entries)
	if err != nil {
		return r, err
	}
	r.Fingerprint = Hash(raw)
	return r, nil
}

// Snapshot refuses a concurrent change while copying and never replaces an
// existing snapshot. A failed attempt leaves no published review snapshot.
func Snapshot(root, destination string, revision Revision) error {
	if !filepath.IsAbs(destination) {
		return errors.New("snapshot destination must be absolute")
	}
	if err := os.Mkdir(destination, 0o700); err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = os.RemoveAll(destination)
		}
	}()
	for _, entry := range revision.Entries {
		if entry.Hash == "missing" {
			continue
		}
		full, err := checkedPath(root, entry.Path)
		if err != nil {
			return err
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return err
		}
		if Hash(raw) != entry.Hash {
			return errors.New("revision changed while creating snapshot")
		}
		target := filepath.Join(destination, "files", filepath.FromSlash(entry.Path))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(target, raw, os.FileMode(entry.Mode)&0o555); err != nil {
			return err
		}
	}
	paths := make([]string, len(revision.Entries))
	for i, e := range revision.Entries {
		paths[i] = e.Path
	}
	current, err := Fingerprint(root, paths)
	if err != nil || current.Fingerprint != revision.Fingerprint {
		return errors.New("revision changed while creating snapshot")
	}
	raw, err := json.Marshal(revision)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, "manifest.json"), raw, 0o400); err != nil {
		return err
	}
	ok = true
	return nil
}
