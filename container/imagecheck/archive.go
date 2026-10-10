package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
)

// savedImage is the single image of a `docker save` archive: the raw
// config JSON and the archive paths of its layers, in manifest order.
// Both the legacy layout (manifest.json at the root) and the OCI layout
// that newer Docker writes alongside it carry manifest.json, which is the
// only index read here.
type savedImage struct {
	ConfigPath string
	Config     []byte
	Layers     []string
}

// imageConfig is the subset of the image config this tool reads.
type imageConfig struct {
	Config struct {
		Env        []string          `json:"Env"`
		Cmd        []string          `json:"Cmd"`
		Entrypoint []string          `json:"Entrypoint"`
		Labels     map[string]string `json:"Labels"`
		User       string            `json:"User"`
		WorkingDir string            `json:"WorkingDir"`
	} `json:"config"`
	RootFS struct {
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	History []struct {
		CreatedBy  string `json:"created_by"`
		Comment    string `json:"comment"`
		EmptyLayer bool   `json:"empty_layer"`
	} `json:"history"`
}

// maxIndexFile bounds manifest.json and the config blob: both are small
// JSON documents, and a bound keeps a hostile archive from exhausting
// memory.
const maxIndexFile = 16 << 20

// openSaved reads manifest.json and the config of the archive's only
// image. An archive holding zero or several images is refused: every
// check here is about one build.
func openSaved(archive string) (*savedImage, error) {
	var manifestJSON []byte
	if err := walkOuter(archive, func(name string, r io.Reader) error {
		if path.Clean(name) == "manifest.json" {
			b, err := readBounded(r, maxIndexFile)
			if err != nil {
				return fmt.Errorf("manifest.json: %w", err)
			}
			manifestJSON = b
			return errStopWalk
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if manifestJSON == nil {
		return nil, fmt.Errorf("%s: no manifest.json (not a docker save archive)", archive)
	}
	var entries []struct {
		Config string   `json:"Config"`
		Layers []string `json:"Layers"`
	}
	if err := json.Unmarshal(manifestJSON, &entries); err != nil {
		return nil, fmt.Errorf("%s: manifest.json: %w", archive, err)
	}
	if len(entries) != 1 {
		return nil, fmt.Errorf("%s: manifest.json lists %d images; save exactly one", archive, len(entries))
	}
	img := &savedImage{ConfigPath: path.Clean(entries[0].Config)}
	for _, l := range entries[0].Layers {
		img.Layers = append(img.Layers, path.Clean(l))
	}
	if err := walkOuter(archive, func(name string, r io.Reader) error {
		if path.Clean(name) == img.ConfigPath {
			b, err := readBounded(r, maxIndexFile)
			if err != nil {
				return fmt.Errorf("config %s: %w", img.ConfigPath, err)
			}
			img.Config = b
			return errStopWalk
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if img.Config == nil {
		return nil, fmt.Errorf("%s: config %s is missing", archive, img.ConfigPath)
	}
	return img, nil
}

// parseConfig decodes the fields of the image config this tool reads.
func (img *savedImage) parseConfig() (*imageConfig, error) {
	var c imageConfig
	if err := json.Unmarshal(img.Config, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", img.ConfigPath, err)
	}
	return &c, nil
}

// walkLayers streams every layer of img in archive order, calling fn once
// per manifest position that names the blob (a blob listed twice is
// visited for each position). The reader is the uncompressed layer tar.
func walkLayers(archive string, img *savedImage, fn func(index int, blob string, layer io.Reader) error) error {
	positions := map[string][]int{}
	for i, l := range img.Layers {
		positions[l] = append(positions[l], i)
	}
	seen := map[string]bool{}
	err := walkOuter(archive, func(name string, r io.Reader) error {
		name = path.Clean(name)
		idx, ok := positions[name]
		if !ok || seen[name] {
			return nil
		}
		seen[name] = true
		if len(idx) == 1 {
			return withDecompressed(r, func(lr io.Reader) error { return fn(idx[0], name, lr) })
		}
		// A repeated blob is buffered to a temporary file so every
		// position gets a full stream.
		tmp, err := os.CreateTemp("", "imagecheck-layer-*")
		if err != nil {
			return err
		}
		defer os.Remove(tmp.Name())
		defer tmp.Close()
		if _, err := io.Copy(tmp, r); err != nil {
			return err
		}
		for _, i := range idx {
			if _, err := tmp.Seek(0, io.SeekStart); err != nil {
				return err
			}
			if err := withDecompressed(tmp, func(lr io.Reader) error { return fn(i, name, lr) }); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, l := range img.Layers {
		if !seen[l] {
			return fmt.Errorf("%s: layer %s is missing from the archive", archive, l)
		}
	}
	return nil
}

// walkTar calls fn for every entry of an (uncompressed) tar stream. The
// content reader is only valid during the call.
func walkTar(r io.Reader, fn func(h *tar.Header, content io.Reader) error) error {
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(h, tr); err != nil {
			return err
		}
	}
}

// errStopWalk ends walkOuter early without an error.
var errStopWalk = errors.New("stop walk")

// walkOuter streams the regular-file entries of the docker save archive.
func walkOuter(archive string, fn func(name string, r io.Reader) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	err = walkTar(bufio.NewReaderSize(f, 1<<20), func(h *tar.Header, r io.Reader) error {
		if h.Typeflag != tar.TypeReg {
			return nil
		}
		return fn(h.Name, r)
	})
	if errors.Is(err, errStopWalk) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("%s: %w", archive, err)
	}
	return nil
}

// withDecompressed detects the layer compression by its magic bytes: gzip
// is decoded, an uncompressed tar is passed through, and zstd (outside the
// standard library) is refused with a clear message.
func withDecompressed(r io.Reader, fn func(io.Reader) error) error {
	br := bufio.NewReaderSize(r, 1<<20)
	magic, err := br.Peek(4)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		zr, err := gzip.NewReader(br)
		if err != nil {
			return err
		}
		defer zr.Close()
		return fn(zr)
	case bytes.HasPrefix(magic, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return errors.New("zstd-compressed layer: save the image with gzip or uncompressed layers")
	default:
		return fn(br)
	}
}

// readBounded reads at most limit bytes and fails when r holds more.
func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("larger than %d bytes", limit)
	}
	return b, nil
}
