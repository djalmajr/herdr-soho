package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
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

// writeSavedArchive writes a docker-save-style archive (manifest.json,
// config blob and layer blobs under blobs/sha256/) to dir/name and
// returns its path. config is marshalled as the image config; when
// gzipLayers is set every layer blob is gzip-compressed.
func writeSavedArchive(t *testing.T, dir, name string, config any, layers [][]testEntry, gzipLayers bool) string {
	t.Helper()
	cfg, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	type blob struct {
		name string
		body []byte
	}
	blobs := []blob{{"blobs/sha256/config", cfg}}
	var layerPaths []string
	for i, l := range layers {
		body := layerTar(t, l)
		if gzipLayers {
			var z bytes.Buffer
			zw := gzip.NewWriter(&z)
			if _, err := zw.Write(body); err != nil {
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				t.Fatal(err)
			}
			body = z.Bytes()
		}
		p := "blobs/sha256/layer" + string(rune('a'+i))
		layerPaths = append(layerPaths, p)
		blobs = append(blobs, blob{p, body})
	}
	manifest, err := json.Marshal([]map[string]any{{"Config": "blobs/sha256/config", "Layers": layerPaths}})
	if err != nil {
		t.Fatal(err)
	}
	blobs = append(blobs, blob{"manifest.json", manifest})
	out := filepath.Join(dir, name)
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tw := tar.NewWriter(f)
	for _, b := range blobs {
		if err := tw.WriteHeader(&tar.Header{Name: b.name, Mode: 0o644, Size: int64(len(b.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(b.body); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOpenSavedAndWalkLayers(t *testing.T) {
	for _, gz := range []bool{false, true} {
		dir := t.TempDir()
		cfg := map[string]any{
			"config": map[string]any{"Env": []string{"PATH=/usr/bin"}, "User": "agent"},
			"rootfs": map[string]any{"diff_ids": []string{"sha256:a", "sha256:b"}},
		}
		archive := writeSavedArchive(t, dir, "img.tar", cfg, [][]testEntry{
			{{Name: "etc/", Dir: true}, {Name: "etc/hello", Body: "one"}},
			{{Name: "usr/bin/tool", Body: "two", Mode: 0o755}},
		}, gz)
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

func TestWithDecompressedRefusesZstd(t *testing.T) {
	err := withDecompressed(bytes.NewReader([]byte{0x28, 0xb5, 0x2f, 0xfd, 0}), func(io.Reader) error { return nil })
	if err == nil {
		t.Fatal("zstd layer accepted")
	}
}
