package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

// maxEntryLines caps the per-layer entry diff so a heavily different layer
// cannot flood the report; the remainder is summarised in an entry-limit
// line instead.
const maxEntryLines = 500

// runCompare compares two `docker save` archives of the same image, built
// twice, and reports layer by layer whether the rebuild is reproducible.
// It prints one tab-separated line per finding on stdout: a config verdict
// line, one line per differing top-level config key, one line per layer
// and, for each layer whose diff id differs, one line per differing entry.
// Exit codes: 0 identical, 1 the configs differ, 2 usage, 4 I/O error.
func runCompare(args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: imagecheck compare <a.tar> <b.tar>")
		return 2
	}
	imgA, err := openSaved(args[0])
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	imgB, err := openSaved(args[1])
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	cfgA, err := imgA.parseConfig()
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	cfgB, err := imgB.parseConfig()
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return 4
	}
	// The image id is the sha256 of the raw config blob; equal ids mean the
	// rebuild is byte-identical at the config level.
	idA, idB := imageID(imgA.Config), imageID(imgB.Config)
	identical := idA == idB
	verdict := "different"
	if identical {
		verdict = "identical"
	}
	fmt.Fprintf(stdout, "config\t%s\t%s\t%s\n", idA, idB, verdict)
	if !identical {
		keysA, err := topLevelKeys(imgA.Config)
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %v\n", err)
			return 4
		}
		keysB, err := topLevelKeys(imgB.Config)
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %v\n", err)
			return 4
		}
		// Union of the top-level keys, alphabetical; a key whose raw value
		// differs (or exists on one side only) is reported.
		seen := map[string]bool{}
		var keys []string
		for k := range keysA {
			seen[k] = true
		}
		for k := range keysB {
			seen[k] = true
		}
		for k := range seen {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			va, oka := keysA[k]
			vb, okb := keysB[k]
			if !oka || !okb || !bytes.Equal(va, vb) {
				fmt.Fprintf(stdout, "config-field\t%s\n", k)
			}
		}
	}
	diffsA, diffsB := cfgA.RootFS.DiffIDs, cfgB.RootFS.DiffIDs
	if len(diffsA) != len(diffsB) {
		fmt.Fprintf(stdout, "layers\t%d\t%d\n", len(diffsA), len(diffsB))
	}
	min := len(diffsA)
	if len(diffsB) < min {
		min = len(diffsB)
	}
	for i := 0; i < min; i++ {
		same := diffsA[i] == diffsB[i]
		v := "different"
		if same {
			v = "same"
		}
		fmt.Fprintf(stdout, "layer\t%d\t%s\t%s\t%s\n", i, diffsA[i], diffsB[i], v)
		if same {
			continue
		}
		entriesA, err := layerEntries(args[0], imgA, i)
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %v\n", err)
			return 4
		}
		entriesB, err := layerEntries(args[1], imgB, i)
		if err != nil {
			fmt.Fprintf(stderr, "imagecheck: %v\n", err)
			return 4
		}
		compareEntries(stdout, i, entriesA, entriesB)
	}
	if identical {
		return 0
	}
	return 1
}

// imageID is "sha256:" plus the hex sha256 of the raw config bytes.
func imageID(cfg []byte) string {
	sum := sha256.Sum256(cfg)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// topLevelKeys decodes the top-level JSON keys of a config blob with their
// raw values, so differences are detected byte-exactly, including keys this
// tool does not otherwise interpret.
func topLevelKeys(cfg []byte) (map[string]json.RawMessage, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &m); err != nil {
		return nil, fmt.Errorf("config: %v", err)
	}
	return m, nil
}

// layerEntry is one tar entry of a layer, keyed by its cleaned path.
type layerEntry struct {
	header *tar.Header
	sha    string
}

// layerEntries streams the layer at manifest index i once and collects its
// entries by cleaned path (a "./" prefix or inner "." segments are gone).
// The content sha256 of regular files is hashed in streaming; nothing is
// kept in memory. A path listed twice keeps its last occurrence.
func layerEntries(archive string, img *savedImage, index int) (map[string]*layerEntry, error) {
	entries := map[string]*layerEntry{}
	err := walkLayers(archive, img, func(i int, _ string, r io.Reader) error {
		if i != index {
			return nil
		}
		return walkTar(r, func(h *tar.Header, content io.Reader) error {
			e := &layerEntry{header: h}
			if h.Typeflag == tar.TypeReg && h.Linkname == "" {
				digest := sha256.New()
				if _, err := io.Copy(digest, content); err != nil {
					return err
				}
				e.sha = hex.EncodeToString(digest.Sum(nil))
			}
			entries[path.Clean(h.Name)] = e
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// compareEntries prints, for one differing layer, one line per entry whose
// presence or attributes differ, sorted by path: added (only in b),
// removed (only in a) or changed with the differing attributes in the
// fixed order type,mode,uid,gid,uname,gname,size,linkname,mtime,xattrs,
// content. At most maxEntryLines lines are printed; the remainder becomes
// an entry-limit line.
func compareEntries(w io.Writer, index int, a, b map[string]*layerEntry) {
	type diff struct {
		path string
		kind string
	}
	var diffs []diff
	seen := map[string]bool{}
	for p := range a {
		seen[p] = true
	}
	for p := range b {
		seen[p] = true
	}
	for p := range seen {
		ea, oka := a[p]
		eb, okb := b[p]
		switch {
		case !oka:
			diffs = append(diffs, diff{p, "added"})
		case !okb:
			diffs = append(diffs, diff{p, "removed"})
		default:
			attrs := diffAttrs(ea.header, eb.header, ea.sha, eb.sha)
			if len(attrs) > 0 {
				diffs = append(diffs, diff{p, "changed:" + strings.Join(attrs, ",")})
			}
		}
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].path < diffs[j].path })
	for n, d := range diffs {
		if n == maxEntryLines {
			fmt.Fprintf(w, "entry-limit\t%d\t%d\n", index, len(diffs)-maxEntryLines)
			return
		}
		fmt.Fprintf(w, "entry\t%d\t%s\t%s\n", index, d.path, d.kind)
	}
}

// diffAttrs lists, in the fixed order type,mode,uid,gid,uname,gname,size,
// linkname,mtime,xattrs,content, the attributes that differ between two
// entries present on both sides. content is the streamed content sha256;
// xattrs is the PAX records map.
func diffAttrs(a, b *tar.Header, shaA, shaB string) []string {
	var attrs []string
	if a.Typeflag != b.Typeflag {
		attrs = append(attrs, "type")
	}
	if a.Mode != b.Mode {
		attrs = append(attrs, "mode")
	}
	if a.Uid != b.Uid {
		attrs = append(attrs, "uid")
	}
	if a.Gid != b.Gid {
		attrs = append(attrs, "gid")
	}
	if a.Uname != b.Uname {
		attrs = append(attrs, "uname")
	}
	if a.Gname != b.Gname {
		attrs = append(attrs, "gname")
	}
	if a.Size != b.Size {
		attrs = append(attrs, "size")
	}
	if a.Linkname != b.Linkname {
		attrs = append(attrs, "linkname")
	}
	if !a.ModTime.Equal(b.ModTime) {
		attrs = append(attrs, "mtime")
	}
	if !samePAX(a.PAXRecords, b.PAXRecords) {
		attrs = append(attrs, "xattrs")
	}
	if shaA != shaB {
		attrs = append(attrs, "content")
	}
	return attrs
}

// samePAX reports whether two PAX records maps are equal.
func samePAX(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || bv != v {
			return false
		}
	}
	return true
}
