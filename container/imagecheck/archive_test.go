package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testEntry is one file (or directory, when Dir is set) of a synthetic
// layer.
type testEntry struct {
	Name    string
	Body    string
	Dir     bool
	Mode    int64
	ModTime int64
}

// layerTar builds one uncompressed layer tar.
func layerTar(t *testing.T, entries []testEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := &tar.Header{Name: e.Name, Mode: e.Mode, Format: tar.FormatPAX}
		if h.Mode == 0 {
			h.Mode = 0o644
		}
		if e.ModTime != 0 {
			h.ModTime = unixTime(e.ModTime)
		}
		if e.Dir {
			h.Typeflag = tar.TypeDir
		} else {
			h.Typeflag = tar.TypeReg
			h.Size = int64(len(e.Body))
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if !e.Dir {
			if _, err := io.WriteString(tw, e.Body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// sha256Hex is the lowercase hex sha256 of b.
func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// honestDiffIDs computes the rootfs.diff_ids of layers: the sha256 of
// each uncompressed layer tar.
func honestDiffIDs(t *testing.T, layers [][]testEntry) []string {
	t.Helper()
	ids := make([]string, len(layers))
	for i, l := range layers {
		ids[i] = "sha256:" + sha256Hex(layerTar(t, l))
	}
	return ids
}

// gzipBody returns the deterministic gzip bytes of b.
func gzipBody(t *testing.T, b []byte) []byte {
	t.Helper()
	var z bytes.Buffer
	zw := gzip.NewWriter(&z)
	if _, err := zw.Write(b); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return z.Bytes()
}

// baseOuterEntriesRaw builds the outer entries of a docker-save-style
// archive around the already-built config map: the config blob and every
// layer blob are named by the sha256 of their bytes (digest-named),
// followed by manifest.json and the extra outer entries (index.json,
// oci-layout, ...) in the given order. It returns the entries and the
// marshalled config bytes.
func baseOuterEntriesRaw(t *testing.T, cfgMap map[string]any, layers [][]testEntry, gzipLayers bool, extra []outerFile) ([]outerFile, []byte) {
	t.Helper()
	cfg, err := json.Marshal(cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := "blobs/sha256/" + sha256Hex(cfg)
	entries := []outerFile{{Name: cfgPath, Data: cfg}}
	var layerPaths []string
	for _, l := range layers {
		body := layerTar(t, l)
		if gzipLayers {
			body = gzipBody(t, body)
		}
		p := "blobs/sha256/" + sha256Hex(body)
		layerPaths = append(layerPaths, p)
		entries = append(entries, outerFile{Name: p, Data: body})
	}
	manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": layerPaths}})
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, outerFile{Name: "manifest.json", Data: manifest})
	entries = append(entries, extra...)
	return entries, cfg
}

// baseOuterEntries is baseOuterEntriesRaw with the honest rootfs set on
// a copy of config (a map[string]any): type "layers" and the sha256 of
// every uncompressed layer tar as diff_ids. It also returns the honest
// diff ids.
func baseOuterEntries(t *testing.T, config any, layers [][]testEntry, gzipLayers bool, extra []outerFile) ([]outerFile, []byte, []string) {
	t.Helper()
	diffIDs := honestDiffIDs(t, layers)
	cfgMap := map[string]any{}
	switch v := config.(type) {
	case map[string]any:
		for k, val := range v {
			cfgMap[k] = val
		}
	case nil:
	default:
		t.Fatalf("baseOuterEntries: unsupported config %T", config)
	}
	cfgMap["rootfs"] = map[string]any{"type": "layers", "diff_ids": diffIDs}
	entries, cfg := baseOuterEntriesRaw(t, cfgMap, layers, gzipLayers, extra)
	return entries, cfg, diffIDs
}

// writeOuterArchive writes one outer archive of the given regular-file
// entries, in the given order, to dir/name and returns its path. The
// low-level helper for broken archives: nothing here is verified.
func writeOuterArchive(t *testing.T, dir, name string, entries []outerFile) string {
	t.Helper()
	out := filepath.Join(dir, name)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	for _, e := range entries {
		if err := tw.WriteHeader(&tar.Header{Name: e.Name, Mode: 0o644, Size: int64(len(e.Data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

// writeSavedArchive writes an honest docker-save-style archive
// (manifest.json, the config blob and the layer blobs under
// blobs/sha256/, every blob named by the sha256 of its bytes) to
// dir/name and returns its path. config is marshalled as the image
// config; when it is a map[string]any, its rootfs key is always set to
// the honest value (type "layers" and the sha256 of every uncompressed
// layer tar as diff_ids) on the map itself as well as on the copy that
// is marshalled, so a later json.Marshal of the same map yields exactly
// the config bytes the archive holds. Negative tests that need a broken
// rootfs use the lower-level helper writeOuterArchive. When gzipLayers
// is set every layer blob is gzip-compressed, so the blob digest and
// the diff id then differ, as in real archives.
func writeSavedArchive(t *testing.T, dir, name string, config any, layers [][]testEntry, gzipLayers bool) string {
	t.Helper()
	if m, ok := config.(map[string]any); ok {
		m["rootfs"] = map[string]any{"type": "layers", "diff_ids": honestDiffIDs(t, layers)}
	}
	entries, _, _ := baseOuterEntries(t, config, layers, gzipLayers, nil)
	return writeOuterArchive(t, dir, name, entries)
}

// rawArchive builds an archive with the given raw config bytes (stored
// under their own digest name) and honest layer blobs for the given
// layers: it is honest around the config, which is what a negative test
// breaks.
func rawArchive(t *testing.T, dir, name string, cfg []byte, layers [][]testEntry) string {
	t.Helper()
	cfgPath := "blobs/sha256/" + sha256Hex(cfg)
	entries := []outerFile{{Name: cfgPath, Data: cfg}}
	var layerPaths []string
	for _, l := range layers {
		body := layerTar(t, l)
		p := "blobs/sha256/" + sha256Hex(body)
		layerPaths = append(layerPaths, p)
		entries = append(entries, outerFile{Name: p, Data: body})
	}
	manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": layerPaths}})
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, outerFile{Name: "manifest.json", Data: manifest})
	return writeOuterArchive(t, dir, name, entries)
}

// withOCIIndex appends to the outer entries of an honest archive the
// OCI image manifest blob and the index.json that an OCI-layout save
// writes for the same image: the manifest names the config and the
// layers that manifest.json names, by digest. mutate, when non-nil,
// edits the manifest map before it is marshalled and stored under the
// sha256 of its bytes. It returns the entries and the manifest digest.
func withOCIIndex(t *testing.T, entries []outerFile, mutate func(man map[string]any)) ([]outerFile, string) {
	t.Helper()
	byName := map[string][]byte{}
	var manifestJSON []byte
	for _, e := range entries {
		byName[e.Name] = e.Data
		if e.Name == "manifest.json" {
			manifestJSON = e.Data
		}
	}
	var m []struct {
		Config string
		Layers []string
	}
	if err := json.Unmarshal(manifestJSON, &m); err != nil || len(m) != 1 {
		t.Fatalf("withOCIIndex: manifest.json: %v", err)
	}
	var layers []any
	for _, l := range m[0].Layers {
		layers = append(layers, map[string]any{"mediaType": "application/vnd.oci.image.layer.v1.tar", "digest": "sha256:" + strings.TrimPrefix(l, "blobs/sha256/"), "size": len(byName[l])})
	}
	cfg := byName[m[0].Config]
	man := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": "sha256:" + sha256Hex(cfg), "size": len(cfg)},
		"layers":        layers,
	}
	if mutate != nil {
		mutate(man)
	}
	manBytes, err := json.Marshal(man)
	if err != nil {
		t.Fatal(err)
	}
	digest := "sha256:" + sha256Hex(manBytes)
	index, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.index.v1+json",
		"manifests":     []any{map[string]any{"mediaType": "application/vnd.oci.image.manifest.v1+json", "digest": digest, "size": len(manBytes)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := append([]outerFile{}, entries...)
	out = append(out, outerFile{Name: "blobs/sha256/" + sha256Hex(manBytes), Data: manBytes}, outerFile{Name: "index.json", Data: index})
	return out, digest
}

func TestOpenSavedAndWalkLayers(t *testing.T) {
	for _, gz := range []bool{false, true} {
		dir := t.TempDir()
		cfg := map[string]any{
			"config": map[string]any{"Env": []string{"PATH=/usr/bin"}, "User": "agent"},
			"rootfs": map[string]any{"diff_ids": []string{"sha256:a", "sha256:b"}},
		}
		layers := [][]testEntry{
			{{Name: "etc/", Dir: true}, {Name: "etc/hello", Body: "one"}},
			{{Name: "usr/bin/tool", Body: "two", Mode: 0o755}},
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, layers, gz)
		img, err := openSaved(archive)
		if err != nil {
			t.Fatal(err)
		}
		c, err := img.parseConfig()
		if err != nil {
			t.Fatal(err)
		}
		if c.Config.User != "agent" || len(c.RootFS.DiffIDs) != 2 {
			t.Fatalf("config = %+v", c)
		}
		// The helper overwrote the caller's fake rootfs with the honest
		// one; DiffIDs carries it through.
		want := honestDiffIDs(t, layers)
		if len(img.DiffIDs) != 2 || img.DiffIDs[0] != want[0] || img.DiffIDs[1] != want[1] {
			t.Fatalf("DiffIDs = %q, want %q", img.DiffIDs, want)
		}
		if len(img.Others) != 1 || img.Others[0].Name != "manifest.json" {
			t.Fatalf("Others = %+v", img.Others)
		}
		got := map[int][]string{}
		err = walkLayers(archive, img, func(i int, _ string, r io.Reader) error {
			return walkTar(r, func(h *tar.Header, content io.Reader) error {
				b, err := io.ReadAll(content)
				if err != nil {
					return err
				}
				got[i] = append(got[i], h.Name+"="+string(b))
				return nil
			})
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(got[0]) != 2 || got[0][1] != "etc/hello=one" || len(got[1]) != 1 || got[1][0] != "usr/bin/tool=two" {
			t.Fatalf("gzip=%v layers = %v", gz, got)
		}
	}
}

func TestOpenSavedRejectsNonArchive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.tar")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := openSaved(p); err == nil {
		t.Fatal("openSaved accepted an archive without manifest.json")
	}
}

func TestLayerStreamRefusesZstd(t *testing.T) {
	_, err := layerStream(bytes.NewReader([]byte{0x28, 0xb5, 0x2f, 0xfd, 0}))
	if err == nil {
		t.Fatal("zstd layer accepted")
	}
}

// TestSameConfigDifferentLayerFailsIdentical: two archives with the
// same config bytes but different layer bytes must not compare as
// identical: the layer no longer hashes to the diff id the shared
// config declares, so the verification fails with exit 4 before
// anything is printed.
func TestSameConfigDifferentLayerFailsIdentical(t *testing.T) {
	dir := t.TempDir()
	buildB := func(layersB [][]testEntry) string {
		layersA := [][]testEntry{{{Name: "etc/hello", Body: "one"}}}
		diffIDs := honestDiffIDs(t, layersA)
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": diffIDs}})
		if err != nil {
			t.Fatal(err)
		}
		cfgPath := "blobs/sha256/" + sha256Hex(cfg)
		bodyB := layerTar(t, layersB[0])
		manifestB, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{"blobs/sha256/" + sha256Hex(bodyB)}}})
		if err != nil {
			t.Fatal(err)
		}
		return writeOuterArchive(t, dir, "b.tar", []outerFile{
			{Name: cfgPath, Data: cfg},
			{Name: "blobs/sha256/" + sha256Hex(bodyB), Data: bodyB},
			{Name: "manifest.json", Data: manifestB},
		})
	}
	for _, c := range []struct {
		name    string
		layersB [][]testEntry
	}{
		{"content", [][]testEntry{{{Name: "etc/hello", Body: "two"}}}},
		{"mode", [][]testEntry{{{Name: "etc/hello", Body: "one", Mode: 0o600}}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Same config shape as buildB: byte-identical config bytes.
			a := writeSavedArchive(t, dir, "a.tar", map[string]any{"config": map[string]any{}}, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, false)
			b := buildB(c.layersB)
			var stdout, stderr bytes.Buffer
			code := run([]string{"compare", a, b}, &stdout, &stderr)
			if code != 4 {
				t.Fatalf("exit = %d, want 4; stderr = %q, stdout = %q", code, stderr.String(), stdout.String())
			}
			if stdout.Len() != 0 {
				t.Fatalf("nothing may be printed before the verification: stdout = %q", stdout.String())
			}
			if stderr.Len() == 0 {
				t.Fatal("stderr is empty, want the digest mismatch")
			}
		})
	}
}

// TestMissingLayerBlobFails: a layer blob absent from the archive fails
// both compare and image.
func TestMissingLayerBlobFails(t *testing.T) {
	dir := t.TempDir()
	entries, _, _ := baseOuterEntries(t, nil, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, false, nil)
	// entries: [config blob, layer blob, manifest.json]; drop the layer.
	p := writeOuterArchive(t, dir, "img.tar", []outerFile{entries[0], entries[2]})
	code := run([]string{"compare", p, p}, io.Discard, io.Discard)
	if code != 4 {
		t.Fatalf("compare exit = %d, want 4", code)
	}
	code = run([]string{"image", p}, io.Discard, io.Discard)
	if code != 4 {
		t.Fatalf("image exit = %d, want 4", code)
	}
}

// TestBlobDigestMismatchFails: a digest-named blob whose bytes do not
// hash to its name fails both compare and image, for the layer (raw
// check and decompressed check) and the config.
func TestBlobDigestMismatchFails(t *testing.T) {
	dir := t.TempDir()
	t.Run("layer", func(t *testing.T) {
		entries, _, _ := baseOuterEntries(t, nil, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, false, nil)
		// Corrupt the layer bytes; the digest-named blob no longer
		// hashes to its name.
		entries[1].Data = append([]byte{0xff}, entries[1].Data...)
		p := writeOuterArchive(t, dir, "img.tar", entries)
		for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
			code := run(args, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("%v exit = %d, want 4", args, code)
			}
		}
	})
	t.Run("layer-raw-only", func(t *testing.T) {
		// The blob keeps the digest name of one gzip stream but holds
		// the bytes of another, and the diff id is honest for the bytes
		// held: only the raw digest check can fail.
		l1 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "one"}})
		l2 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "two"}})
		g1, g2 := gzipBody(t, l1), gzipBody(t, l2)
		if bytes.Equal(g1, g2) {
			t.Fatal("test setup: gzip bodies must differ")
		}
		p := "blobs/sha256/" + sha256Hex(g1)
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + sha256Hex(l2)}}})
		if err != nil {
			t.Fatal(err)
		}
		cfgPath := "blobs/sha256/" + sha256Hex(cfg)
		manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{p}}})
		if err != nil {
			t.Fatal(err)
		}
		entries := []outerFile{
			{Name: cfgPath, Data: cfg},
			{Name: p, Data: g2},
			{Name: "manifest.json", Data: manifest},
		}
		code := run([]string{"compare", writeOuterArchive(t, dir, "img.tar", entries), writeOuterArchive(t, dir, "img2.tar", entries)}, io.Discard, io.Discard)
		if code != 4 {
			t.Fatalf("compare exit = %d, want 4", code)
		}
	})
	t.Run("config", func(t *testing.T) {
		// Flip one byte of a harmless field: the config stays a valid
		// object with the honest rootfs; only the digest check fails.
		entries, cfg, _ := baseOuterEntries(t, map[string]any{"architecture": "amd64"}, nil, false, nil)
		for i, b := range cfg {
			if b == 'a' {
				cfg[i] = 'b'
				break
			}
		}
		entries[0].Data = cfg
		p := writeOuterArchive(t, dir, "img.tar", entries)
		for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
			code := run(args, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("%v exit = %d, want 4", args, code)
			}
		}
	})
}

// TestIndexJSONValidation: a malformed index.json beside a valid
// manifest fails closed; a valid index.json passes.
func TestIndexJSONValidation(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		name string
		body string
	}{
		{"not-json", `{not-json`},
		{"array", `[]`},
		{"missing-schema-version", `{"manifests":[]}`},
		{"schema-version-string", `{"schemaVersion":"2","manifests":[]}`},
		{"manifests-object", `{"schemaVersion":2,"manifests":{}}`},
	} {
		t.Run("compare/"+c.name, func(t *testing.T) {
			entries, _, _ := baseOuterEntries(t, nil, nil, false, []outerFile{{Name: "index.json", Data: []byte(c.body)}})
			p := writeOuterArchive(t, dir, c.name+".tar", entries)
			code := run([]string{"compare", p, p}, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("compare exit = %d, want 4", code)
			}
		})
		t.Run("image/"+c.name, func(t *testing.T) {
			entries, _, _ := baseOuterEntries(t, nil, nil, false, []outerFile{{Name: "index.json", Data: []byte(c.body)}})
			p := writeOuterArchive(t, dir, c.name+".tar", entries)
			code := run([]string{"image", p}, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("image exit = %d, want 4", code)
			}
		})
	}
	t.Run("valid", func(t *testing.T) {
		base, _, _ := baseOuterEntries(t, nil, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, true, nil)
		entries, _ := withOCIIndex(t, base, nil)
		p := writeOuterArchive(t, dir, "valid.tar", entries)
		var stdout, stderr bytes.Buffer
		if code := run([]string{"compare", p, p}, &stdout, &stderr); code != 0 {
			t.Fatalf("compare exit = %d, stderr = %q", code, stderr.String())
		}
		stdout.Reset()
		stderr.Reset()
		if code := run([]string{"image", p}, &stdout, &stderr); code != 0 {
			t.Fatalf("image exit = %d, stderr = %q", code, stderr.String())
		}
	})
}

// TestConfigShapeValidation: non-object configs and configs whose
// rootfs does not match the manifest fail closed.
func TestConfigShapeValidation(t *testing.T) {
	dir := t.TempDir()
	// Non-object configs (null, array, string, number), digest-named so
	// only the object check can fail.
	for _, body := range []string{`null`, `[]`, `"x"`, `1`} {
		t.Run("shape "+body, func(t *testing.T) {
			p := rawArchive(t, dir, "img.tar", []byte(body), nil)
			for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
				code := run(args, io.Discard, io.Discard)
				if code != 4 {
					t.Fatalf("%v exit = %d, want 4", args, code)
				}
			}
		})
	}
	// Object configs with a broken rootfs, around true digests.
	layers := [][]testEntry{{{Name: "etc/hello", Body: "one"}}}
	diffIDs := honestDiffIDs(t, layers)
	two := append(append([]string{}, diffIDs...), diffIDs[0])
	for _, c := range []struct {
		name   string
		rootfs map[string]any
	}{
		{"wrong-type", map[string]any{"type": "chunks", "diff_ids": diffIDs}},
		{"missing-type", map[string]any{"diff_ids": diffIDs}},
		{"count-mismatch", map[string]any{"type": "layers", "diff_ids": two}},
		{"malformed-uppercase", map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + strings.Repeat("A", 64)}}},
		{"malformed-short", map[string]any{"type": "layers", "diff_ids": []string{"sha256:" + strings.Repeat("a", 63)}}},
		{"malformed-algorithm", map[string]any{"type": "layers", "diff_ids": []string{"md5:" + strings.Repeat("a", 64)}}},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": c.rootfs})
			if err != nil {
				t.Fatal(err)
			}
			p := rawArchive(t, dir, c.name+".tar", cfg, layers)
			code := run([]string{"compare", p, p}, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("compare exit = %d, want 4", code)
			}
			code = run([]string{"image", p}, io.Discard, io.Discard)
			if code != 4 {
				t.Fatalf("image exit = %d, want 4", code)
			}
		})
	}
}

// TestGzipLayerDigests: for a gzip layer the diff id is the digest of
// the decompressed bytes and the blob name the digest of the compressed
// bytes; the honest archive passes.
func TestGzipLayerDigests(t *testing.T) {
	dir := t.TempDir()
	layers := [][]testEntry{{{Name: "etc/data", Body: strings.Repeat("z", 4096)}}}
	p := writeSavedArchive(t, dir, "img.tar", map[string]any{"config": map[string]any{"Env": []string{"PATH=/usr/bin"}}}, layers, true)
	img, err := openSaved(p)
	if err != nil {
		t.Fatal(err)
	}
	plain := layerTar(t, layers[0])
	if img.DiffIDs[0] != "sha256:"+sha256Hex(plain) {
		t.Fatalf("DiffIDs = %q, want the decompressed digest", img.DiffIDs)
	}
	if img.Layers[0] != "blobs/sha256/"+sha256Hex(gzipBody(t, plain)) {
		t.Fatalf("layer path = %q, want the compressed digest", img.Layers)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"compare", p, p}, &stdout, &stderr); code != 0 {
		t.Fatalf("compare exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"image", p}, &stdout, &stderr); code != 0 {
		t.Fatalf("image exit = %d, stderr = %q", code, stderr.String())
	}
}

// TestUncompressedPaddingDigest: an uncompressed layer with extra zero
// padding after the tar end-of-archive marker passes only when the
// declared diff id covers the padding.
func TestUncompressedPaddingDigest(t *testing.T) {
	dir := t.TempDir()
	plain := layerTar(t, []testEntry{{Name: "etc/hello", Body: "one"}})
	padded := append(append([]byte{}, plain...), make([]byte, 8192)...)
	build := func(diffID, name string) string {
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}}})
		if err != nil {
			t.Fatal(err)
		}
		cfgPath := "blobs/sha256/" + sha256Hex(cfg)
		manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{"blobs/sha256/" + sha256Hex(padded)}}})
		if err != nil {
			t.Fatal(err)
		}
		entries := []outerFile{
			{Name: cfgPath, Data: cfg},
			{Name: "blobs/sha256/" + sha256Hex(padded), Data: padded},
			{Name: "manifest.json", Data: manifest},
		}
		return writeOuterArchive(t, dir, name, entries)
	}
	ok := build("sha256:"+sha256Hex(padded), "padded.tar")
	var stdout, stderr bytes.Buffer
	if code := run([]string{"compare", ok, ok}, &stdout, &stderr); code != 0 {
		t.Fatalf("padded compare exit = %d, stderr = %q", code, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if code := run([]string{"image", ok}, &stdout, &stderr); code != 0 {
		t.Fatalf("padded image exit = %d, stderr = %q", code, stderr.String())
	}
	// The same archive with a diff id that stops at the tar end: the
	// padding bytes make the decompressed stream mismatch.
	bad := build("sha256:"+sha256Hex(plain), "unpadded.tar")
	code := run([]string{"compare", bad, bad}, io.Discard, &stderr)
	if code != 4 {
		t.Fatalf("unpadded compare exit = %d, want 4; stderr = %q", code, stderr.String())
	}
	code = run([]string{"image", bad}, io.Discard, io.Discard)
	if code != 4 {
		t.Fatalf("unpadded image exit = %d, want 4", code)
	}
}

// TestHonestArchiveOthers: Others holds manifest.json (and index.json
// when written), in archive order, with the file bytes.
func TestHonestArchiveOthers(t *testing.T) {
	dir := t.TempDir()
	layers := [][]testEntry{{{Name: "etc/hello", Body: "one"}}}
	plain := writeSavedArchive(t, dir, "plain.tar", nil, layers, false)
	img, err := openSaved(plain)
	if err != nil {
		t.Fatal(err)
	}
	if len(img.DiffIDs) != 1 {
		t.Fatalf("DiffIDs = %v", img.DiffIDs)
	}
	if len(img.Others) != 1 || img.Others[0].Name != "manifest.json" {
		t.Fatalf("Others = %+v", img.Others)
	}
	base, _, _ := baseOuterEntries(t, nil, layers, false, nil)
	entries, digest := withOCIIndex(t, base, nil)
	withIndex := writeOuterArchive(t, dir, "idx.tar", entries)
	img2, err := openSaved(withIndex)
	if err != nil {
		t.Fatal(err)
	}
	manPath := "blobs/sha256/" + strings.TrimPrefix(digest, "sha256:")
	if len(img2.Others) != 3 {
		t.Fatalf("Others = %+v", img2.Others)
	}
	if img2.Others[0].Name != "manifest.json" || img2.Others[1].Name != manPath || img2.Others[2].Name != "index.json" {
		t.Fatalf("Others order = %+v", img2.Others)
	}
	for i, e := range entries[len(entries)-3:] {
		if !bytes.Equal(img2.Others[i].Data, e.Data) {
			t.Fatalf("Others[%d] data = %q, want %q", i, img2.Others[i].Data, e.Data)
		}
	}
}

// TestRepeatedBlobPositions: a blob listed at two manifest positions is
// verified and walked at both positions.
func TestRepeatedBlobPositions(t *testing.T) {
	dir := t.TempDir()
	l0 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "one"}})
	l1 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "two"}})
	d0, d1 := "sha256:"+sha256Hex(l0), "sha256:"+sha256Hex(l1)
	// Legacy-style layer name: not digest-named, so each position is
	// judged by the decompressed digest.
	ln := "sub/layer.tar"
	build := func(id1, name string) string {
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{d0, id1}}})
		if err != nil {
			t.Fatal(err)
		}
		cfgPath := "blobs/sha256/" + sha256Hex(cfg)
		manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{ln, ln}}})
		if err != nil {
			t.Fatal(err)
		}
		entries := []outerFile{
			{Name: cfgPath, Data: cfg},
			{Name: ln, Data: l0},
			{Name: "manifest.json", Data: manifest},
		}
		return writeOuterArchive(t, dir, name, entries)
	}
	// Honest: both positions carry the same diff id. Both positions are
	// verified and walked, and the archive compares as identical.
	ok := build(d0, "ok.tar")
	img, err := openSaved(ok)
	if err != nil {
		t.Fatal(err)
	}
	visited := map[int]bool{}
	err = walkLayers(ok, img, func(i int, _ string, _ io.Reader) error {
		visited[i] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !visited[0] || !visited[1] {
		t.Fatalf("positions visited = %v, want both", visited)
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"compare", ok, ok}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit = %d, want 0; stderr = %q", code, stderr.String())
	}
	// Position 1's diff id does not match the blob: the verification
	// must reach position 1 (exit 0 would mean it was skipped).
	bad := build(d1, "bad.tar")
	code = run([]string{"compare", bad, bad}, io.Discard, &stderr)
	if code != 4 {
		t.Fatalf("exit = %d, want 4 (position 1 must be verified); stderr = %q", code, stderr.String())
	}
}

// TestTrailingBytesAfterGzipStream: bytes following the gzip stream of a
// layer blob are covered by the fail-closed checks. Named by the sha256
// of the stream without the trailer, the archive must exit 4 (the
// decoder errors on the trailing bytes, or the raw digest mismatches);
// named by the sha256 of the bytes including a second valid gzip member
// that the decoder accepts, the raw digest matches the name and the
// result is whatever the diff id check says: exit 4 when the declared
// diff id covers only the first member, exit 0 when it covers both.
func TestTrailingBytesAfterGzipStream(t *testing.T) {
	dir := t.TempDir()
	l1 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "one"}})
	g1 := gzipBody(t, l1)
	g2 := gzipBody(t, []byte("trailing-member\n"))
	withGarbage := append(append([]byte{}, g1...), []byte("\x00\x01\x02\x03trailer-xxxx")...)
	withMember := append(append([]byte{}, g1...), g2...)
	build := func(name string, blobName string, blob []byte, diffID string) string {
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}}})
		if err != nil {
			t.Fatal(err)
		}
		cfgPath := "blobs/sha256/" + sha256Hex(cfg)
		manifest, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{blobName}}})
		if err != nil {
			t.Fatal(err)
		}
		return writeOuterArchive(t, dir, name, []outerFile{
			{Name: cfgPath, Data: cfg},
			{Name: blobName, Data: blob},
			{Name: "manifest.json", Data: manifest},
		})
	}
	diffID := "sha256:" + sha256Hex(l1)
	t.Run("named-without-trailer", func(t *testing.T) {
		p := build("trailer.tar", "blobs/sha256/"+sha256Hex(g1), withGarbage, diffID)
		code := run([]string{"compare", p, p}, io.Discard, io.Discard)
		if code != 4 {
			t.Fatalf("compare exit = %d, want 4 (bytes after the gzip stream)", code)
		}
		code = run([]string{"image", p}, io.Discard, io.Discard)
		if code != 4 {
			t.Fatalf("image exit = %d, want 4", code)
		}
	})
	t.Run("named-with-trailer-second-member", func(t *testing.T) {
		// The decoder accepts the second gzip member and the raw digest
		// matches the name; the diff id covers only the first member, so
		// the decompressed digest mismatches.
		p := build("member.tar", "blobs/sha256/"+sha256Hex(withMember), withMember, diffID)
		code := run([]string{"compare", p, p}, io.Discard, io.Discard)
		if code != 4 {
			t.Fatalf("compare exit = %d, want 4 (the decompressed stream covers both members)", code)
		}
		code = run([]string{"image", p}, io.Discard, io.Discard)
		if code != 4 {
			t.Fatalf("image exit = %d, want 4", code)
		}
	})
	t.Run("diff-id-covers-both-members", func(t *testing.T) {
		// A diff id covering both decompressed members passes: a
		// multi-member stream is not rejected for that reason alone.
		both := append(append([]byte{}, l1...), []byte("trailing-member\n")...)
		p := build("members-ok.tar", "blobs/sha256/"+sha256Hex(withMember), withMember, "sha256:"+sha256Hex(both))
		code := run([]string{"compare", p, p}, io.Discard, io.Discard)
		if code != 0 {
			t.Fatalf("compare exit = %d, want 0 (the diff id covers both members)", code)
		}
	})
}

// TestDuplicateOuterEntries: an outer tar holding two regular entries
// whose cleaned names are equal is ambiguous (different readers pick
// different copies) and fails closed, whether or not the bytes match;
// an outer entry the manifest names as the config or a layer must be a
// regular file.
func TestDuplicateOuterEntries(t *testing.T) {
	dir := t.TempDir()
	l0 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "one"}})
	l1 := layerTar(t, []testEntry{{Name: "etc/hello", Body: "two"}})
	d0 := "sha256:" + sha256Hex(l0)
	cfgFor := func(diffID string) (string, []byte) {
		cfg, err := json.Marshal(map[string]any{"config": map[string]any{}, "rootfs": map[string]any{"type": "layers", "diff_ids": []string{diffID}}})
		if err != nil {
			t.Fatal(err)
		}
		return "blobs/sha256/" + sha256Hex(cfg), cfg
	}
	manifestFor := func(cfgPath, layerPath string) []byte {
		m, err := json.Marshal([]map[string]any{{"Config": cfgPath, "Layers": []string{layerPath}}})
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	failBoth := func(p, what string) {
		var stderr bytes.Buffer
		code := run([]string{"compare", p, p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("%s: compare exit = %d, want 4; stderr = %q", what, code, stderr.String())
		}
		code = run([]string{"image", p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("%s: image exit = %d, want 4; stderr = %q", what, code, stderr.String())
		}
		return
	}
	t.Run("duplicate-layer-blob-different-bytes", func(t *testing.T) {
		cp, cfg := cfgFor(d0)
		n := "blobs/sha256/" + sha256Hex(l0)
		m := manifestFor(cp, n)
		p := writeOuterArchive(t, dir, "dup-layer.tar", []outerFile{
			{Name: cp, Data: cfg},
			{Name: n, Data: l0},
			{Name: "manifest.json", Data: m},
			{Name: n, Data: l1},
		})
		var stderr bytes.Buffer
		code := run([]string{"compare", p, p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("compare exit = %d, want 4; stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "duplicate outer entry") {
			t.Fatalf("stderr = %q, want the duplicate entry error", stderr.String())
		}
		code = run([]string{"image", p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("image exit = %d, want 4; stderr = %q", code, stderr.String())
		}
	})
	t.Run("duplicate-layer-blob-identical-bytes", func(t *testing.T) {
		// Identical copies: every digest check would pass, so only the
		// duplicate-name rule can make this fail.
		cp, cfg := cfgFor(d0)
		n := "blobs/sha256/" + sha256Hex(l0)
		m := manifestFor(cp, n)
		p := writeOuterArchive(t, dir, "dup-same.tar", []outerFile{
			{Name: cp, Data: cfg},
			{Name: n, Data: l0},
			{Name: "manifest.json", Data: m},
			{Name: n, Data: l0},
		})
		failBoth(p, "identical-bytes duplicate layer")
	})
	t.Run("duplicate-manifest", func(t *testing.T) {
		cp, cfg := cfgFor(d0)
		n := "blobs/sha256/" + sha256Hex(l0)
		m := manifestFor(cp, n)
		p := writeOuterArchive(t, dir, "dup-manifest.tar", []outerFile{
			{Name: cp, Data: cfg},
			{Name: n, Data: l0},
			{Name: "manifest.json", Data: m},
			{Name: "manifest.json", Data: append([]byte{}, m...)[1:]},
		})
		failBoth(p, "duplicate manifest.json")
	})
	t.Run("dot-slash-manifest", func(t *testing.T) {
		cp, cfg := cfgFor(d0)
		n := "blobs/sha256/" + sha256Hex(l0)
		m := manifestFor(cp, n)
		p := writeOuterArchive(t, dir, "dotslash.tar", []outerFile{
			{Name: cp, Data: cfg},
			{Name: n, Data: l0},
			{Name: "manifest.json", Data: m},
			{Name: "./manifest.json", Data: m},
		})
		failBoth(p, "./manifest.json plus manifest.json")
	})
	t.Run("layer-only-symlink", func(t *testing.T) {
		cp, cfg := cfgFor(d0)
		ln := "sub/layer.tar"
		m := manifestFor(cp, ln)
		writeSymlinkArchive(t, dir, "symlink-layer.tar", []outerFile{{Name: cp, Data: cfg}}, ln, "blobs/sha256/"+sha256Hex(l0), []outerFile{{Name: "manifest.json", Data: m}})
		p := filepath.Join(dir, "symlink-layer.tar")
		var stderr bytes.Buffer
		code := run([]string{"compare", p, p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("compare exit = %d, want 4; stderr = %q", code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "not a regular file") {
			t.Fatalf("stderr = %q, want the not-a-regular-file error", stderr.String())
		}
		code = run([]string{"image", p}, io.Discard, &stderr)
		if code != 4 {
			t.Fatalf("image exit = %d, want 4; stderr = %q", code, stderr.String())
		}
	})
	t.Run("config-only-symlink", func(t *testing.T) {
		cp, _ := cfgFor(d0)
		n := "blobs/sha256/" + sha256Hex(l0)
		m := manifestFor(cp, n)
		writeSymlinkArchive(t, dir, "symlink-config.tar", []outerFile{{Name: n, Data: l0}}, cp, n, []outerFile{{Name: "manifest.json", Data: m}})
		p := filepath.Join(dir, "symlink-config.tar")
		failBoth(p, "symlink config")
	})
}

// writeSymlinkArchive writes one outer archive of the given regular
// entries around a symlink entry named linkName (target linkTarget).
func writeSymlinkArchive(t *testing.T, dir, name string, before []outerFile, linkName, linkTarget string, after []outerFile) string {
	t.Helper()
	out := filepath.Join(dir, name)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	write := func(e outerFile) {
		if err := tw.WriteHeader(&tar.Header{Name: e.Name, Mode: 0o644, Size: int64(len(e.Data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(e.Data); err != nil {
			t.Fatal(err)
		}
	}
	for _, e := range before {
		write(e)
	}
	if err := tw.WriteHeader(&tar.Header{Name: linkName, Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: linkTarget}); err != nil {
		t.Fatal(err)
	}
	for _, e := range after {
		write(e)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestOCIManifestBlobAndIndex: a digest-named blob that is neither the
// config nor a layer must hash to its name, and index.json must name
// an image manifest blob stored in the archive whose config and layers
// are those of manifest.json; anything else exits 4 from both compare
// and image, and the error never echoes text from the manifest blob.
func TestOCIManifestBlobAndIndex(t *testing.T) {
	dir := t.TempDir()
	layers := [][]testEntry{{{Name: "etc/hello", Body: "one"}}, {{Name: "etc/other", Body: "two"}}}
	base, _, _ := baseOuterEntries(t, nil, layers, true, nil)
	planted := "AKIA" + "ABCDEFGHIJKLMNOP"
	replaceIndex := func(entries []outerFile, index string) []outerFile {
		out := append([]outerFile{}, entries...)
		out[len(out)-1] = outerFile{Name: "index.json", Data: []byte(index)}
		return out
	}
	cases := []struct {
		name    string
		entries func(t *testing.T) []outerFile
	}{
		{"unreferenced-blob-mismatch", func(t *testing.T) []outerFile {
			body := []byte(`{"note":"extra"}`)
			name := "blobs/sha256/" + sha256Hex(body)
			return append(append([]outerFile{}, base...), outerFile{Name: name, Data: []byte(`{"note":"extrA"}`)})
		}},
		{"manifest-blob-flipped", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, nil)
			blob := entries[len(entries)-2]
			flipped := append([]byte{}, blob.Data...)
			flipped[len(flipped)/2] ^= 0x01
			entries[len(entries)-2] = outerFile{Name: blob.Name, Data: flipped}
			return entries
		}},
		{"manifest-blob-missing", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, nil)
			return append(append([]outerFile{}, entries[:len(entries)-2]...), entries[len(entries)-1])
		}},
		{"manifest-other-config", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, func(man map[string]any) {
				man["config"].(map[string]any)["digest"] = "sha256:" + planted
			})
			return entries
		}},
		{"manifest-config-of-other-bytes", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, func(man map[string]any) {
				man["config"].(map[string]any)["digest"] = "sha256:" + sha256Hex([]byte("{}"))
			})
			return entries
		}},
		{"manifest-drops-layer", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, func(man map[string]any) {
				man["layers"] = man["layers"].([]any)[:1]
			})
			return entries
		}},
		{"manifest-swaps-layers", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, func(man map[string]any) {
				l := man["layers"].([]any)
				man["layers"] = []any{l[1], l[0]}
			})
			return entries
		}},
		{"manifest-null", func(t *testing.T) []outerFile {
			body := []byte("null")
			entries, _ := withOCIIndex(t, base, nil)
			entries[len(entries)-2] = outerFile{Name: "blobs/sha256/" + sha256Hex(body), Data: body}
			return replaceIndex(entries, `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:`+sha256Hex(body)+`"}]}`)
		}},
		{"nested-index", func(t *testing.T) []outerFile {
			entries, digest := withOCIIndex(t, base, nil)
			return replaceIndex(entries, `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.index.v1+json","digest":"`+digest+`"}]}`)
		}},
		{"no-manifests", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, nil)
			return replaceIndex(entries, `{"schemaVersion":2,"manifests":[]}`)
		}},
		{"bad-digest-form", func(t *testing.T) []outerFile {
			entries, _ := withOCIIndex(t, base, nil)
			return replaceIndex(entries, `{"schemaVersion":2,"manifests":[{"mediaType":"application/vnd.oci.image.manifest.v1+json","digest":"sha256:`+planted+`"}]}`)
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := writeOuterArchive(t, dir, c.name+".tar", c.entries(t))
			for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
				var stdout, stderr bytes.Buffer
				if code := run(args, &stdout, &stderr); code != 4 {
					t.Fatalf("%v exit = %d, want 4; stdout = %q, stderr = %q", args, code, stdout.String(), stderr.String())
				}
				if strings.Contains(stdout.String()+stderr.String(), planted) {
					t.Fatalf("%v output echoes the manifest text: %q", args, stderr.String())
				}
			}
		})
	}
	t.Run("honest", func(t *testing.T) {
		entries, _ := withOCIIndex(t, base, nil)
		p := writeOuterArchive(t, dir, "honest.tar", entries)
		for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != 0 {
				t.Fatalf("%v exit = %d, stderr = %q", args, code, stderr.String())
			}
		}
	})
}

// TestOthersTotalBounded: the outer files kept in memory are bounded in
// total, not only per file; an archive past the bound exits 4.
func TestOthersTotalBounded(t *testing.T) {
	old := maxOthersTotal
	defer func() { maxOthersTotal = old }()
	dir := t.TempDir()
	base, _, _ := baseOuterEntries(t, nil, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, false, nil)
	entries := append([]outerFile{}, base...)
	for i := 0; i < 4; i++ {
		entries = append(entries, outerFile{Name: fmt.Sprintf("extra-%d", i), Data: bytes.Repeat([]byte{'x'}, 1024)})
	}
	p := writeOuterArchive(t, dir, "many.tar", entries)
	maxOthersTotal = 3 * 1024
	for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
		if code := run(args, io.Discard, io.Discard); code != 4 {
			t.Fatalf("%v exit = %d, want 4", args, code)
		}
	}
	maxOthersTotal = 8 * 1024
	if code := run([]string{"compare", p, p}, io.Discard, io.Discard); code != 0 {
		t.Fatalf("compare under the bound exit = %d, want 0", code)
	}
}

// TestOuterNonRegularWithData: an outer member that is not a regular
// file but carries data (a contiguous file, an unknown type flag) is
// skipped by every regular-file walk, so its bytes would never be
// verified or scanned; the archive fails closed with exit 4. A
// directory entry (no data) is fine.
func TestOuterNonRegularWithData(t *testing.T) {
	dir := t.TempDir()
	base, _, _ := baseOuterEntries(t, nil, [][]testEntry{{{Name: "etc/hello", Body: "one"}}}, false, nil)
	write := func(name string, extra *tar.Header, body []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		tw := tar.NewWriter(f)
		for _, e := range base {
			if err := tw.WriteHeader(&tar.Header{Name: e.Name, Mode: 0o644, Size: int64(len(e.Data)), Typeflag: tar.TypeReg}); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(e.Data); err != nil {
				t.Fatal(err)
			}
		}
		if extra != nil {
			extra.Size = int64(len(body))
			if err := tw.WriteHeader(extra); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(body); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	body := []byte("k=" + tokNPM() + "\n")
	for _, c := range []struct {
		name string
		h    *tar.Header
	}{
		{"contiguous", &tar.Header{Name: "extra.bin", Typeflag: tar.TypeCont, Mode: 0o644}},
		{"unknown-type", &tar.Header{Name: "extra.bin", Typeflag: 'Z', Mode: 0o644}},
	} {
		t.Run(c.name, func(t *testing.T) {
			p := write(c.name+".tar", c.h, body)
			for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
				var stdout, stderr bytes.Buffer
				if code := run(args, &stdout, &stderr); code != 4 {
					t.Fatalf("%v exit = %d, want 4; stdout=%q stderr=%q", args, code, stdout.String(), stderr.String())
				}
				if strings.Contains(stdout.String()+stderr.String(), tokNPM()) {
					t.Fatalf("%v output echoes the body", args)
				}
			}
		})
	}
	t.Run("directory-control", func(t *testing.T) {
		p := write("dir.tar", &tar.Header{Name: "blobs/", Typeflag: tar.TypeDir, Mode: 0o755}, nil)
		for _, args := range [][]string{{"compare", p, p}, {"image", p}} {
			if code := run(args, io.Discard, io.Discard); code != 0 {
				t.Fatalf("%v exit = %d, want 0", args, code)
			}
		}
	})
}
