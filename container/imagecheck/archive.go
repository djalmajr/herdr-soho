package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strings"
)

// outerFile is a regular file of the outer docker-save archive that is
// neither the config blob nor one of the manifest's layer blobs, kept in
// archive order for later consumers.
type outerFile struct {
	Name string
	Data []byte
}

// savedImage is the single image of a `docker save` archive: the raw
// config JSON and the archive paths of its layers, in manifest order.
// Both the legacy layout (manifest.json at the root) and the OCI layout
// that newer Docker writes alongside it carry manifest.json, which is the
// only index read here. DiffIDs are the validated rootfs.diff_ids of the
// config and Others the archive's remaining outer files.
type savedImage struct {
	ConfigPath string
	Config     []byte
	Layers     []string
	DiffIDs    []string
	Others     []outerFile
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
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
	History []struct {
		CreatedBy  string `json:"created_by"`
		Comment    string `json:"comment"`
		EmptyLayer bool   `json:"empty_layer"`
	} `json:"history"`
}

// maxIndexFile bounds manifest.json, the config blob and every other
// outer file (index.json, oci-layout, repositories, ...): they are small
// JSON documents, and a bound keeps a hostile archive from exhausting
// memory. Layer blobs are layer-sized and are streamed, not buffered.
const maxIndexFile = 16 << 20

// maxOthersTotal bounds the sum of every outer file openSaved keeps in
// memory (manifest.json, index.json, the OCI manifest blob, ...): the
// per-file bound alone lets an archive of many such files hold many
// times maxIndexFile. A variable so tests can lower it.
var maxOthersTotal int64 = 64 << 20

var (
	// blobHexRe matches one sha256 digest in lowercase hex.
	blobHexRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// diffIDRe matches one rootfs.diff_ids entry.
	diffIDRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// openSaved reads manifest.json and the config of the archive's only
// image. An archive holding zero or several images is refused: every
// check here is about one build. The config, manifest.json and every
// other outer file (index.json, oci-layout, repositories, ...) are read
// bounded by maxIndexFile and the archive fails closed on anything
// malformed: every digest-named blob must hash to its name, the config
// must be a JSON object whose layers rootfs matches the manifest, the
// optional index.json, oci-layout and repositories files must decode
// with their expected shapes, and index.json must describe the same
// config and layers as manifest.json (checkIndexMatches). Layer blobs
// are not read here; they are layer-sized and are streamed by
// walkLayers.
func openSaved(archive string) (*savedImage, error) {
	// First, fail closed on ambiguous outer structure: two regular
	// entries whose cleaned names are equal are different copies to
	// different readers, and a name the manifest may use as the config
	// or a layer must not appear only as a non-regular entry.
	seenRegular := make(map[string]bool)
	nonRegular := make(map[string]bool)
	if err := walkOuterEntries(archive, func(name string, regular bool) error {
		if !regular {
			nonRegular[name] = true
			return nil
		}
		if seenRegular[name] {
			return fmt.Errorf("%s: duplicate outer entry %s", archive, name)
		}
		seenRegular[name] = true
		return nil
	}); err != nil {
		return nil, err
	}
	var manifestJSON []byte
	if err := walkOuter(archive, func(name string, r io.Reader) error {
		if path.Clean(name) != "manifest.json" {
			return nil
		}
		b, err := readBounded(r, maxIndexFile)
		if err != nil {
			return fmt.Errorf("manifest.json: %w", err)
		}
		manifestJSON = b
		return errStopWalk
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
	layerSet := make(map[string]bool, len(img.Layers))
	for _, l := range img.Layers {
		layerSet[l] = true
	}
	var others []outerFile
	othersTotal := int64(len(manifestJSON))
	haveConfig := false
	err := walkOuter(archive, func(name string, r io.Reader) error {
		name = path.Clean(name)
		switch {
		case name == "manifest.json":
			// Already read; keep it in Others at its archive position
			// without reading it again.
			others = append(others, outerFile{Name: name, Data: manifestJSON})
		case name == img.ConfigPath:
			b, err := readBounded(r, maxIndexFile)
			if err != nil {
				return fmt.Errorf("config %s: %w", name, err)
			}
			img.Config = b
			haveConfig = true
		case layerSet[name]:
			// Layer blobs are streamed by walkLayers, not read here.
		default:
			b, err := readBounded(r, maxIndexFile)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			if othersTotal += int64(len(b)); othersTotal > maxOthersTotal {
				return fmt.Errorf("outer files other than the config and layers total more than %d bytes", maxOthersTotal)
			}
			// Every digest-named blob must hash to its name, whether
			// or not manifest.json names it: an OCI reader loads the
			// image manifest blob that index.json names.
			if hexPart, ok := digestBlobName(name); ok {
				sum := sha256.Sum256(b)
				if actual := hex.EncodeToString(sum[:]); actual != hexPart {
					return fmt.Errorf("blob %s digest mismatch: declared sha256:%s, actual sha256:%s", name, hexPart, actual)
				}
			}
			others = append(others, outerFile{Name: name, Data: b})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if nonRegular[img.ConfigPath] {
		return nil, fmt.Errorf("%s: config %s is not a regular file", archive, img.ConfigPath)
	}
	for _, l := range img.Layers {
		if nonRegular[l] {
			return nil, fmt.Errorf("%s: layer %s is not a regular file", archive, l)
		}
	}
	if !haveConfig {
		return nil, fmt.Errorf("%s: config %s is missing", archive, img.ConfigPath)
	}
	if err := checkConfigObject(img.Config); err != nil {
		return nil, fmt.Errorf("%s: config %s: %v", archive, img.ConfigPath, err)
	}
	var doc imageConfig
	if err := json.Unmarshal(img.Config, &doc); err != nil {
		return nil, fmt.Errorf("%s: config %s: %w", archive, img.ConfigPath, err)
	}
	if doc.RootFS.Type != "layers" {
		return nil, fmt.Errorf("%s: config %s: rootfs.type must be \"layers\"", archive, img.ConfigPath)
	}
	if len(doc.RootFS.DiffIDs) != len(img.Layers) {
		return nil, fmt.Errorf("%s: config %s: rootfs.diff_ids lists %d entries, the manifest lists %d layers", archive, img.ConfigPath, len(doc.RootFS.DiffIDs), len(img.Layers))
	}
	for _, d := range doc.RootFS.DiffIDs {
		if !diffIDRe.MatchString(d) {
			return nil, fmt.Errorf("%s: config %s: rootfs.diff_ids entry is not sha256 plus 64 lowercase hex characters", archive, img.ConfigPath)
		}
	}
	img.DiffIDs = doc.RootFS.DiffIDs
	if hexPart, ok := digestBlobName(img.ConfigPath); ok {
		sum := sha256.Sum256(img.Config)
		if actual := hex.EncodeToString(sum[:]); actual != hexPart {
			return nil, fmt.Errorf("%s: config %s digest mismatch: declared sha256:%s, actual sha256:%s", archive, img.ConfigPath, hexPart, actual)
		}
	}
	byName := make(map[string]*outerFile, len(others))
	for i := range others {
		byName[others[i].Name] = &others[i]
	}
	if i := byName["index.json"]; i != nil {
		if err := checkIndexFile(i.Data); err != nil {
			return nil, fmt.Errorf("%s: index.json: %v", archive, err)
		}
		if err := checkIndexMatches(i.Data, byName, img); err != nil {
			return nil, fmt.Errorf("%s: index.json: %v", archive, err)
		}
	}
	if o := byName["oci-layout"]; o != nil {
		if err := checkOCILayout(o.Data); err != nil {
			return nil, fmt.Errorf("%s: oci-layout: %v", archive, err)
		}
	}
	if rp := byName["repositories"]; rp != nil {
		var v any
		if err := json.Unmarshal(rp.Data, &v); err != nil {
			return nil, fmt.Errorf("%s: repositories: %w", archive, err)
		}
		if _, ok := v.(map[string]any); !ok {
			return nil, fmt.Errorf("%s: repositories: not a JSON object", archive)
		}
	}
	img.Others = others
	return img, nil
}

// checkConfigObject rejects a config that is not a JSON object: null,
// arrays, strings and numbers are not image configs.
func checkConfigObject(b []byte) error {
	for i := 0; i < len(b); i++ {
		switch b[i] {
		case ' ', '\t', '\r', '\n':
			continue
		case '{':
			return nil
		default:
			return errors.New("not a JSON object")
		}
	}
	return errors.New("empty config")
}

// checkIndexFile validates an OCI index.json: a JSON object with a
// numeric schemaVersion and a manifests array.
func checkIndexFile(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("not a JSON object")
	}
	_, present := obj["schemaVersion"]
	if _, ok := obj["schemaVersion"].(float64); !ok {
		if !present {
			return errors.New("missing numeric schemaVersion")
		}
		return errors.New("schemaVersion is not numeric")
	}
	if _, ok := obj["manifests"].([]any); !ok {
		return errors.New("manifests is not an array")
	}
	return nil
}

// Image manifest media types index.json may name; anything else (a
// nested index, an artifact) is refused.
var imageManifestTypes = map[string]bool{
	"application/vnd.oci.image.manifest.v1+json":           true,
	"application/vnd.docker.distribution.manifest.v2+json": true,
}

// checkIndexMatches requires index.json to describe the same image as
// manifest.json, so an OCI reader and a docker-save reader load the
// same bytes: at least one manifest, each one an image manifest stored
// in the archive as blobs/sha256/<hex> (already verified against its
// name), whose config digest is the sha256 of the config manifest.json
// names and whose layer digests are, in order, the digest names of the
// manifest.json layers (verified against their bytes by walkLayers).
func checkIndexMatches(index []byte, byName map[string]*outerFile, img *savedImage) error {
	var idx struct {
		Manifests []struct {
			MediaType string `json:"mediaType"`
			Digest    string `json:"digest"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(index, &idx); err != nil {
		return err
	}
	if len(idx.Manifests) == 0 {
		return errors.New("lists no manifests")
	}
	cfgSum := sha256.Sum256(img.Config)
	cfgDigest := "sha256:" + hex.EncodeToString(cfgSum[:])
	var layerDigests []string
	for _, l := range img.Layers {
		hexPart, ok := digestBlobName(l)
		if !ok {
			return fmt.Errorf("layer %s is not digest-named", l)
		}
		layerDigests = append(layerDigests, "sha256:"+hexPart)
	}
	for i, m := range idx.Manifests {
		if !imageManifestTypes[m.MediaType] {
			return fmt.Errorf("manifests[%d] is not an image manifest", i)
		}
		if !diffIDRe.MatchString(m.Digest) {
			return fmt.Errorf("manifests[%d] digest is not sha256 plus 64 lowercase hex characters", i)
		}
		blob := byName["blobs/sha256/"+strings.TrimPrefix(m.Digest, "sha256:")]
		if blob == nil {
			return fmt.Errorf("manifests[%d] blob %s is missing", i, m.Digest)
		}
		if err := checkConfigObject(blob.Data); err != nil {
			return fmt.Errorf("manifest %s: %v", m.Digest, err)
		}
		var man struct {
			Config struct {
				Digest string `json:"digest"`
			} `json:"config"`
			Layers []struct {
				Digest string `json:"digest"`
			} `json:"layers"`
		}
		if err := json.Unmarshal(blob.Data, &man); err != nil {
			return fmt.Errorf("manifest %s: %w", m.Digest, err)
		}
		if man.Config.Digest != cfgDigest {
			return fmt.Errorf("manifest %s config digest is not the manifest.json config %s", m.Digest, cfgDigest)
		}
		if len(man.Layers) != len(layerDigests) {
			return fmt.Errorf("manifest %s lists %d layers, manifest.json lists %d", m.Digest, len(man.Layers), len(layerDigests))
		}
		for j, l := range man.Layers {
			if l.Digest != layerDigests[j] {
				return fmt.Errorf("manifest %s layer %d digest is not the manifest.json layer %s", m.Digest, j, layerDigests[j])
			}
		}
	}
	return nil
}

// checkOCILayout validates an OCI oci-layout: a JSON object with a
// string imageLayoutVersion.
func checkOCILayout(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return errors.New("not a JSON object")
	}
	_, present := obj["imageLayoutVersion"]
	if _, ok := obj["imageLayoutVersion"].(string); !ok {
		if !present {
			return errors.New("missing string imageLayoutVersion")
		}
		return errors.New("imageLayoutVersion is not a string")
	}
	return nil
}

// parseConfig decodes the fields of the image config this tool reads;
// openSaved already validated the object shape and the rootfs.
func (img *savedImage) parseConfig() (*imageConfig, error) {
	var c imageConfig
	if err := json.Unmarshal(img.Config, &c); err != nil {
		return nil, fmt.Errorf("config %s: %w", img.ConfigPath, err)
	}
	return &c, nil
}

// digestBlobName reports whether p names a blob by its sha256 digest and
// returns the 64 lowercase hex characters: the OCI form
// blobs/sha256/<hex> and the legacy docker save form <hex>.json.
func digestBlobName(p string) (string, bool) {
	if rest, ok := strings.CutPrefix(p, "blobs/sha256/"); ok && blobHexRe.MatchString(rest) {
		return rest, true
	}
	if rest, ok := strings.CutSuffix(p, ".json"); ok && blobHexRe.MatchString(rest) {
		return rest, true
	}
	return "", false
}

// walkLayers streams every layer of img in archive order, calling fn
// once per manifest position whose blob occurrence is visited (a blob
// listed twice and stored twice is visited at both positions). Every
// visited occurrence is verified before fn runs: a digest-named blob
// must hash to its name and the decompressed stream, hashed to the very
// end, must hash to the layer's diff id. The reader is the decompressed
// layer tar and is only valid during the call.
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
			return processLayer(archive, name, idx[0], img.DiffIDs, r, fn)
		}
		// A blob listed at several manifest positions is buffered to a
		// temporary file so every position gets the full stream.
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
			if err := processLayer(archive, name, i, img.DiffIDs, tmp, fn); err != nil {
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

// processLayer verifies one occurrence of the layer blob at r and hands
// its decompressed stream to fn for the manifest position index: the raw
// bytes must hash to the blob's digest name (when it has one) and the
// decompressed stream, hashed to the very end, must hash to the layer's
// diff id. A mismatch is an error naming the layer index, the declared
// and the actual digest.
func processLayer(archive, name string, index int, diffIDs []string, r io.Reader, fn func(index int, blob string, layer io.Reader) error) error {
	raw := sha256.New()
	rawtee := io.TeeReader(r, raw)
	dec, err := layerStream(rawtee)
	if err != nil {
		return fmt.Errorf("%s: layer %d blob %s: %w", archive, index, name, err)
	}
	hash := sha256.New()
	drained := io.TeeReader(dec, hash)
	if err := fn(index, name, drained); err != nil {
		return fmt.Errorf("%s: layer %d blob %s: %w", archive, index, name, err)
	}
	// Hash the decompressed stream to the very end: drain whatever fn
	// did not read (tar end-of-archive padding counts).
	if _, err := io.Copy(io.Discard, drained); err != nil {
		return fmt.Errorf("%s: layer %d blob %s: %w", archive, index, name, err)
	}
	// Hash the raw blob over every stored byte: after the decompressed
	// stream is drained, drain the raw tee as well so bytes the decoder
	// did not pull (e.g. after the end of a gzip stream) are hashed too.
	if _, err := io.Copy(io.Discard, rawtee); err != nil {
		return fmt.Errorf("%s: layer %d blob %s: %w", archive, index, name, err)
	}
	if hexPart, ok := digestBlobName(name); ok {
		if actual := hex.EncodeToString(raw.Sum(nil)); actual != hexPart {
			return fmt.Errorf("%s: layer %d blob %s digest mismatch: declared sha256:%s, actual sha256:%s", archive, index, name, hexPart, actual)
		}
	}
	if actual := "sha256:" + hex.EncodeToString(hash.Sum(nil)); actual != diffIDs[index] {
		return fmt.Errorf("%s: layer %d digest mismatch: declared %s, actual %s", archive, index, diffIDs[index], actual)
	}
	return nil
}

// verifyLayers walks every layer of img with a no-op callback so that
// only the digest checks run.
func verifyLayers(archive string, img *savedImage) error {
	return walkLayers(archive, img, func(int, string, io.Reader) error { return nil })
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

// walkOuterEntries visits every entry in the archive, of any type,
// reporting its cleaned name and whether the entry is a regular file.
// A non-regular entry that carries data (a contiguous file, a sparse
// file, an unknown type) is an error: docker save writes none, readers
// disagree on its bytes, and walkOuter would skip them unscanned. It
// reads no entry data; the underlying file is an io.Seeker, so skipping
// entry data is a seek, not a read.
func walkOuterEntries(archive string, fn func(name string, regular bool) error) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	tr := tar.NewReader(f)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("%s: %w", archive, err)
		}
		if hdr.Typeflag != tar.TypeReg && hdr.Size > 0 {
			return fmt.Errorf("%s: outer entry %s is not a regular file and carries %d bytes", archive, path.Clean(hdr.Name), hdr.Size)
		}
		if err := fn(path.Clean(hdr.Name), hdr.Typeflag == tar.TypeReg); err != nil {
			return err
		}
	}
	return nil
}

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

// layerStream detects the layer compression by its magic bytes: gzip is
// decoded, an uncompressed tar is passed through, and zstd (outside the
// standard library) is refused with a clear message. The returned reader
// must be read to EOF for a checksummed stream.
func layerStream(r io.Reader) (io.Reader, error) {
	br := bufio.NewReaderSize(r, 1<<20)
	magic, err := br.Peek(4)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	switch {
	case bytes.HasPrefix(magic, []byte{0x1f, 0x8b}):
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, err
		}
		return zr, nil
	case bytes.HasPrefix(magic, []byte{0x28, 0xb5, 0x2f, 0xfd}):
		return nil, errors.New("zstd-compressed layer: save the image with gzip or uncompressed layers")
	default:
		return br, nil
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
