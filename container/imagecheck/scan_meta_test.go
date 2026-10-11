package main

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// metaEntry is one arbitrary tar entry of a synthetic layer: the
// header and the body it carries (empty for directories and links).
type metaEntry struct {
	h    *tar.Header
	body []byte
}

// metaLayerTar builds one uncompressed layer tar from arbitrary
// headers and bodies.
func metaLayerTar(t *testing.T, entries []metaEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		h := *e.h
		h.Size = int64(len(e.body))
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write(e.body); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// metaArchive builds an honest docker-save-style archive from
// arbitrary layer entries: the config blob and every layer blob are
// named by the sha256 of their bytes, the config carries the true
// rootfs.diff_ids, the manifest object may carry extra fields and the
// extra outer entries follow it in archive order. It returns the
// archive path and the manifest bytes.
func metaArchive(t *testing.T, dir, name string, cfgInner map[string]any, layers [][]metaEntry, manifestExtra map[string]any, extraOuter []outerFile) (string, []byte) {
	t.Helper()
	var diffIDs []string
	var layerBlobs [][]byte
	for _, l := range layers {
		b := metaLayerTar(t, l)
		layerBlobs = append(layerBlobs, b)
		diffIDs = append(diffIDs, "sha256:"+sha256Hex(b))
	}
	cfgMap := map[string]any{
		"config": cfgInner,
		"rootfs": map[string]any{"type": "layers", "diff_ids": diffIDs},
	}
	cfg, err := json.Marshal(cfgMap)
	if err != nil {
		t.Fatal(err)
	}
	cfgPath := "blobs/sha256/" + sha256Hex(cfg)
	entries := []outerFile{{Name: cfgPath, Data: cfg}}
	var layerPaths []string
	for _, b := range layerBlobs {
		p := "blobs/sha256/" + sha256Hex(b)
		layerPaths = append(layerPaths, p)
		entries = append(entries, outerFile{Name: p, Data: b})
	}
	man := map[string]any{"Config": cfgPath, "Layers": layerPaths}
	for k, v := range manifestExtra {
		man[k] = v
	}
	manifest, err := json.Marshal([]map[string]any{man})
	if err != nil {
		t.Fatal(err)
	}
	entries = append(entries, outerFile{Name: "manifest.json", Data: manifest})
	entries = append(entries, extraOuter...)
	return writeOuterArchive(t, dir, name, entries), manifest
}

// assertNoLiteral fails when any of literals appears in out or errb in
// any ASCII case (the search is run on the lower-cased text).
func assertNoLiteral(t *testing.T, out, errb string, literals ...string) {
	t.Helper()
	for _, s := range []string{out, errb} {
		ls := lowerASCIIString(s)
		for _, lit := range literals {
			if strings.Contains(ls, lowerASCIIString(lit)) {
				t.Fatalf("literal %q appears in the output:\n%s", lit, s)
			}
		}
	}
}

// TestMetaContextDenyInPath: a deny literal held only by a directory
// name, a file name, a symlink name or a symlink target gives
// deny-token findings at @name / @link with the ordinal display, and
// the literal never reaches the output.
func TestMetaContextDenyInPath(t *testing.T) {
	token := "can" + "aryhost"
	t.Run("directory-name", func(t *testing.T) {
		auxDir := t.TempDir()
		deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
		xai := tokXAI()
		body := "k=" + xai + "\n"
		t.Run("with-deny", func(t *testing.T) {
			dir := t.TempDir()
			writeContextFile(t, dir, "src/canaryhost/inner.txt", body)
			code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			if !hasLine(out, "finding\tdeny-token:1\tentry#1\t@name") {
				t.Fatalf("directory @name finding missing in:\n%s", out)
			}
			if !hasLine(out, "finding\tdeny-token:1\tentry#2\t@name") {
				t.Fatalf("file @name finding missing in:\n%s", out)
			}
			if !hasLine(out, contentLine("finding", "xai-key", "entry#2", 2, len(xai), sumOf(body))) {
				t.Fatalf("redacted content finding missing in:\n%s", out)
			}
			assertNoLiteral(t, out, errb, token)
		})
		t.Run("without-deny", func(t *testing.T) {
			dir := t.TempDir()
			writeContextFile(t, dir, "src/canaryhost/inner.txt", body)
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			if lineWith(out, "finding\tdeny-token:") != "" {
				t.Fatalf("unexpected deny finding in:\n%s", out)
			}
			if !hasLine(out, contentLine("finding", "xai-key", "src/canaryhost/inner.txt", 2, len(xai), sumOf(body))) {
				t.Fatalf("real path missing in:\n%s", out)
			}
		})
	})
	t.Run("file-name", func(t *testing.T) {
		xai := tokXAI()
		body := "k=" + xai + "\n"
		t.Run("with-deny", func(t *testing.T) {
			auxDir := t.TempDir()
			deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
			dir := t.TempDir()
			writeContextFile(t, dir, "src/canaryhost.txt", body)
			code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			if !hasLine(out, "finding\tdeny-token:1\tentry#1\t@name") {
				t.Fatalf("file @name finding missing in:\n%s", out)
			}
			if !hasLine(out, contentLine("finding", "xai-key", "entry#1", 2, len(xai), sumOf(body))) {
				t.Fatalf("redacted content finding missing in:\n%s", out)
			}
			assertNoLiteral(t, out, errb, token)
		})
		t.Run("without-deny", func(t *testing.T) {
			dir := t.TempDir()
			writeContextFile(t, dir, "src/canaryhost.txt", body)
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			if lineWith(out, "finding\tdeny-token:") != "" {
				t.Fatalf("unexpected deny finding in:\n%s", out)
			}
			if !hasLine(out, contentLine("finding", "xai-key", "src/canaryhost.txt", 2, len(xai), sumOf(body))) {
				t.Fatalf("real path missing in:\n%s", out)
			}
		})
	})
	t.Run("symlink-name", func(t *testing.T) {
		auxDir := t.TempDir()
		deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
		xai := tokXAI()
		body := "k=" + xai + "\n"
		dir := t.TempDir()
		writeContextFile(t, dir, "src/real.txt", body)
		if err := os.Symlink("real.txt", filepath.Join(dir, "src", "canaryhost.lnk")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\tdeny-token:1\tentry#1\t@name") {
			t.Fatalf("symlink @name finding missing in:\n%s", out)
		}
		// The target keeps its real path: only the symlink's own
		// display is redacted.
		if !hasLine(out, contentLine("finding", "xai-key", "src/real.txt", 2, len(xai), sumOf(body))) {
			t.Fatalf("real target path missing in:\n%s", out)
		}
		assertNoLiteral(t, out, errb, token)
	})
	t.Run("symlink-target", func(t *testing.T) {
		auxDir := t.TempDir()
		deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
		dir := t.TempDir()
		if err := os.Symlink("canaryhost.txt", filepath.Join(dir, "src", "ptr")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		t.Run("with-deny", func(t *testing.T) {
			code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			if !hasLine(out, "finding\tdeny-token:1\tsrc/ptr\t@link") {
				t.Fatalf("symlink @link finding missing in:\n%s", out)
			}
			assertNoLiteral(t, out, errb, token)
		})
		t.Run("without-deny", func(t *testing.T) {
			code, out, errb := runContextCLI(t, dir, "--allow", "src")
			if code != 0 {
				t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
			}
			if lineWith(out, "finding\t") != "" {
				t.Fatalf("unexpected finding in:\n%s", out)
			}
		})
	})
}

// TestMetaContextSymlinkTargetRules: the content rules and the
// forbidden-name rule run on the symlink target text at @link; the
// link is never followed.
func TestMetaContextSymlinkTargetRules(t *testing.T) {
	aws := tokAWS()
	t.Run("aws-key-in-target", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("../creds/"+aws, filepath.Join(dir, "src", "ptr")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\taws-access-key\tsrc/ptr\t@link") {
			t.Fatalf("aws finding at @link missing in:\n%s", out)
		}
		assertNoLiteral(t, out, errb, aws)
	})
	t.Run("aws-key-control", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("../creds/notes.txt", filepath.Join(dir, "src", "ptr")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\t") != "" {
			t.Fatalf("unexpected finding in:\n%s", out)
		}
	})
	t.Run("forbidden-target", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("../home/someone/.ssh/id_rsa", filepath.Join(dir, "src", "sshlink")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\tforbidden-name\tsrc/sshlink\t@link") {
			t.Fatalf("forbidden finding at @link missing in:\n%s", out)
		}
	})
	t.Run("forbidden-target-control", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Symlink("../home/someone/notes.txt", filepath.Join(dir, "src", "sshlink")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src")
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\t") != "" {
			t.Fatalf("unexpected finding in:\n%s", out)
		}
	})
}

// TestMetaImageDenyInFields: a deny literal held only in a directory
// member name, a file member name, a symlink Linkname, a hard link
// Linkname, Uname, Gname, a PAX record value or a PAX record key is
// found at the right @<field>, redacted only where the member name
// holds it.
func TestMetaImageDenyInFields(t *testing.T) {
	token := "can" + "aryhost"
	slackBody := "k=" + tokSlack() + "\n"
	npmBody := "k=" + tokNPM() + "\n"
	entries := []metaEntry{
		{h: &tar.Header{Name: "home/agent/canaryhost/", Typeflag: tar.TypeDir, Mode: 0o755}},
		{h: &tar.Header{Name: "opt/canaryhost.conf", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte(slackBody)},
		{h: &tar.Header{Name: "opt/symlink", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: token + ".txt"}},
		{h: &tar.Header{Name: "opt/hardlink", Typeflag: tar.TypeLink, Mode: 0o644, Linkname: token + ".bin"}},
		{h: &tar.Header{Name: "opt/uname.txt", Typeflag: tar.TypeReg, Mode: 0o644, Uname: token}, body: []byte(npmBody)},
		{h: &tar.Header{Name: "opt/gname.txt", Typeflag: tar.TypeReg, Mode: 0o644, Gname: token}, body: []byte("ok2\n")},
		{h: &tar.Header{Name: "opt/paxval.txt", Typeflag: tar.TypeReg, Mode: 0o644, Format: tar.FormatPAX, PAXRecords: map[string]string{"SCHILY.xattr.user.note": "owned-by-" + token}}, body: []byte("ok3\n")},
		{h: &tar.Header{Name: "opt/paxkey.txt", Typeflag: tar.TypeReg, Mode: 0o644, Format: tar.FormatPAX, PAXRecords: map[string]string{token + ".note": "x"}}, body: []byte("ok4\n")},
	}
	dir := t.TempDir()
	auxDir := t.TempDir()
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	t.Run("with-deny", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		for _, w := range []string{
			"finding\tdeny-token:1\tlayer0:entry#0\t@name",
			"finding\tdeny-token:1\tlayer0:entry#1\t@name",
			"finding\tdeny-token:1\tlayer0:opt/symlink\t@link",
			"finding\tdeny-token:1\tlayer0:opt/hardlink\t@link",
			"finding\tdeny-token:1\tlayer0:opt/uname.txt\t@uname",
			"finding\tdeny-token:1\tlayer0:opt/gname.txt\t@gname",
			"finding\tdeny-token:1\tlayer0:opt/paxval.txt\t@pax",
			"finding\tdeny-token:1\tlayer0:opt/paxkey.txt\t@pax",
		} {
			if !hasLine(out, w) {
				t.Fatalf("missing %q in:\n%s", w, out)
			}
		}
		// The member names holding the literal are redacted everywhere;
		// the other entries keep their real names.
		if !hasLine(out, contentLine("finding", "slack-token", "layer0:entry#1", 2, len(tokSlack()), sumOf(slackBody))) {
			t.Fatalf("redacted slack finding missing in:\n%s", out)
		}
		if !hasLine(out, contentLine("finding", "npm-token", "layer0:opt/uname.txt", 2, len(tokNPM()), sumOf(npmBody))) {
			t.Fatalf("real-name npm finding missing in:\n%s", out)
		}
		assertNoLiteral(t, out, errb, token)
	})
	t.Run("without-deny", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\tdeny-token:") != "" {
			t.Fatalf("unexpected deny finding in:\n%s", out)
		}
		// Without the deny file the file member name is printed under
		// its real display.
		if !hasLine(out, contentLine("finding", "slack-token", "layer0:opt/canaryhost.conf", 2, len(tokSlack()), sumOf(slackBody))) {
			t.Fatalf("real member name missing in:\n%s", out)
		}
	})
}

// TestMetaImageAbsoluteAndEscapingNames: absolute and .. member names
// are normalized before the credential-path rule, so they cannot
// bypass it.
func TestMetaImageAbsoluteAndEscapingNames(t *testing.T) {
	dir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{
		{
			{h: &tar.Header{Name: "/root/.aws/credentials", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("a\n")},
			{h: &tar.Header{Name: "/home/agent/.ssh/id_rsa", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("b\n")},
			{h: &tar.Header{Name: "../root/.ssh/k", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("c\n")},
			{h: &tar.Header{Name: "/etc/passwd", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("d\n")},
		},
	}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	for _, w := range []string{
		"finding\tcredential-path\tlayer0:root/.aws/credentials\t-",
		"finding\tcredential-path\tlayer0:home/agent/.ssh/id_rsa\t-",
		"finding\tcredential-path\tlayer0:root/.ssh/k\t-",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	if strings.Contains(out, "layer0:etc/passwd") {
		t.Fatalf("/etc/passwd must not be flagged:\n%s", out)
	}
	// Control: an absolute name that is not a credential path gives no
	// finding at all.
	control, _ := metaArchive(t, dir, "ctrl.tar", map[string]any{}, [][]metaEntry{
		{{h: &tar.Header{Name: "/etc/motd", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("m\n")}},
	}, nil, nil)
	code, out, errb = runImageCLI(t, control)
	if code != 0 {
		t.Fatalf("control code = %d, want 0; stderr=%s", code, errb)
	}
	if lineWith(out, "finding\t") != "" {
		t.Fatalf("unexpected finding in:\n%s", out)
	}
}

// TestMetaImageLinkTargets: absolute and relative symlink targets and
// hard link targets get the credential-path rule and the content
// rules at @link.
func TestMetaImageLinkTargets(t *testing.T) {
	dir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{
		{
			{h: &tar.Header{Name: "etc/credlink", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "/home/agent/.ssh/id_rsa"}},
			{h: &tar.Header{Name: "etc/rel", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "../home/agent/.ssh/id_rsa"}},
			{h: &tar.Header{Name: "etc/keylink", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "data/" + tokAWS()}},
			{h: &tar.Header{Name: "opt/hard", Typeflag: tar.TypeLink, Mode: 0o644, Linkname: "root/.aws/credentials"}},
		},
	}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	for _, w := range []string{
		"finding\tcredential-path\tlayer0:etc/credlink\t@link",
		"finding\tcredential-path\tlayer0:etc/rel\t@link",
		"finding\taws-access-key\tlayer0:etc/keylink\t@link",
		"finding\tcredential-path\tlayer0:opt/hard\t@link",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	assertNoLiteral(t, out, errb, tokAWS())
	// Control: a relative target that is not a credential path gives no
	// finding.
	control, _ := metaArchive(t, dir, "ctrl.tar", map[string]any{}, [][]metaEntry{
		{{h: &tar.Header{Name: "etc/safe", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "../home/agent/notes.txt"}}},
	}, nil, nil)
	code, out, errb = runImageCLI(t, control)
	if code != 0 {
		t.Fatalf("control code = %d, want 0; stderr=%s", code, errb)
	}
	if lineWith(out, "finding\t") != "" {
		t.Fatalf("unexpected finding in:\n%s", out)
	}
}

// TestMetaImageWhiteouts: a genuine whiteout marker gives no finding;
// a .wh. entry that is not a marker gets the content scan; a zero-size
// .wh. member whose name holds a deny literal gives deny-token at
// @name.
func TestMetaImageWhiteouts(t *testing.T) {
	token := "can" + "aryhost"
	whBody := "k=" + tokNPM() + "\n"
	entries := []metaEntry{
		{h: &tar.Header{Name: "etc/.wh.secret", Typeflag: tar.TypeReg, Mode: 0o644}},
		{h: &tar.Header{Name: "etc/.wh.secret2", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte(whBody)},
		{h: &tar.Header{Name: "root/.wh.credentials", Typeflag: tar.TypeReg, Mode: 0o644}},
		{h: &tar.Header{Name: "etc/.wh.canaryhost", Typeflag: tar.TypeReg, Mode: 0o644}},
	}
	dir := t.TempDir()
	auxDir := t.TempDir()
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	t.Run("with-deny", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\tdeny-token:1\tlayer0:entry#3\t@name") {
			t.Fatalf("zero-size .wh. @name finding missing in:\n%s", out)
		}
		// A non-empty .wh. file is not a marker: its content is scanned
		// under its real name.
		if !hasLine(out, contentLine("finding", "npm-token", "layer0:etc/.wh.secret2", 2, len(tokNPM()), sumOf(whBody))) {
			t.Fatalf("non-marker .wh. content finding missing in:\n%s", out)
		}
		// Genuine markers: the zero-size names are not credential or
		// git material, and the other zero-size marker has no literal.
		if strings.Contains(out, "layer0:etc/.wh.secret\t") || strings.Contains(out, "layer0:root/.wh.credentials") || strings.Contains(out, "layer0:entry#0") || strings.Contains(out, "layer0:entry#2") {
			t.Fatalf("genuine whiteout markers must give no finding:\n%s", out)
		}
		if c := strings.Count(out, "finding\t"); c != 2 {
			t.Fatalf("findings = %d, want 2:\n%s", c, out)
		}
		assertNoLiteral(t, out, errb, token, tokNPM())
	})
	t.Run("without-deny", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\tdeny-token:") != "" {
			t.Fatalf("unexpected deny finding in:\n%s", out)
		}
		if c := strings.Count(out, "finding\t"); c != 1 {
			t.Fatalf("findings = %d, want 1:\n%s", c, out)
		}
		if !hasLine(out, contentLine("finding", "npm-token", "layer0:etc/.wh.secret2", 2, len(tokNPM()), sumOf(whBody))) {
			t.Fatalf("non-marker .wh. content finding missing in:\n%s", out)
		}
	})
}

// TestMetaContextErrorRedaction: an I/O error that embeds a path
// holding a deny literal is printed redacted.
func TestMetaContextErrorRedaction(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 does not prevent reading on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 000 does not prevent reading")
	}
	token := "can" + "aryhost"
	auxDir := t.TempDir()
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	dir := t.TempDir()
	name := "src/canaryhost/x.txt"
	writeContextFile(t, dir, name, "x\n")
	if err := os.Chmod(filepath.Join(dir, filepath.FromSlash(name)), 0); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
	if code != 4 {
		t.Fatalf("code = %d, want 4; stderr=%s", code, errb)
	}
	if out != "" {
		t.Fatalf("stdout should be empty, got:\n%s", out)
	}
	if !strings.Contains(errb, "[deny-token:") {
		t.Fatalf("stderr must carry the redaction marker:\n%s", errb)
	}
	assertNoLiteral(t, out, errb, token)
}

// TestMetaArchiveIndex: the archive's index files are scanned for
// content rules and deny tokens under the archive: display, and an
// outer name holding a deny literal is redacted.
func TestMetaArchiveIndex(t *testing.T) {
	token := "can" + "aryhost"
	dir := t.TempDir()
	auxDir := t.TempDir()
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	layers := [][]metaEntry{{{h: &tar.Header{Name: "etc/a.txt", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("ok\n")}}}
	t.Run("manifest-repotags", func(t *testing.T) {
		archive, manifest := metaArchive(t, dir, "img.tar", map[string]any{}, layers,
			map[string]any{"RepoTags": []string{"example/" + token + ":latest"}}, nil)
		off := strings.Index(string(manifest), token)
		if off < 0 {
			t.Fatalf("fixture: the token must be in the manifest")
		}
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, denyLine("deny-token:1", "archive:manifest.json", off)) {
			t.Fatalf("manifest deny finding missing in:\n%s", out)
		}
		assertNoLiteral(t, out, errb, token)
	})
	t.Run("manifest-repotags-control", func(t *testing.T) {
		archive, _ := metaArchive(t, dir, "ctrl.tar", map[string]any{}, layers, nil, nil)
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\tdeny-token:") != "" {
			t.Fatalf("unexpected deny finding in:\n%s", out)
		}
	})
	t.Run("outer-name-redacted", func(t *testing.T) {
		archive, _ := metaArchive(t, dir, "named.tar", map[string]any{}, layers, nil,
			[]outerFile{{Name: token + ".json", Data: []byte("{}")}})
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\tdeny-token:1\tarchive:entry#1\t@name") {
			t.Fatalf("outer @name finding missing in:\n%s", out)
		}
		assertNoLiteral(t, out, errb, token)
	})
	t.Run("outer-name-control", func(t *testing.T) {
		archive, _ := metaArchive(t, dir, "clean.tar", map[string]any{}, layers, nil,
			[]outerFile{{Name: "clean.json", Data: []byte("{}")}})
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 0 {
			t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
		}
		if lineWith(out, "finding\t") != "" {
			t.Fatalf("unexpected finding in:\n%s", out)
		}
	})
}

// TestMetaAcceptCannotSuppressMetadata: an accept entry built from the
// content match of one entry leaves the metadata finding of the same
// entry active.
func TestMetaAcceptCannotSuppressMetadata(t *testing.T) {
	dir := t.TempDir()
	auxDir := t.TempDir()
	body := "t=" + tokNPM() + "\n"
	entries := []metaEntry{
		{h: &tar.Header{Name: "etc/data", Typeflag: tar.TypeReg, Mode: 0o644, Uname: tokAWS()}, body: []byte(body)},
	}
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	off := strings.Index(body, tokNPM())
	accept := writeAuxFile(t, auxDir, "accept.txt", entryOf("npm-token", "etc/data", body, tokNPM())+"\n")
	t.Run("with-accept", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive, "--accept-file", accept)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, contentLine("accepted", "npm-token", "layer0:etc/data", off, len(tokNPM()), sumOf(body))) {
			t.Fatalf("accepted content line missing in:\n%s", out)
		}
		if !hasLine(out, "finding\taws-access-key\tlayer0:etc/data\t@uname") {
			t.Fatalf("metadata finding must stay active:\n%s", out)
		}
		assertNoLiteral(t, out, errb, tokNPM(), tokAWS())
	})
	t.Run("without-accept", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, contentLine("finding", "npm-token", "layer0:etc/data", off, len(tokNPM()), sumOf(body))) {
			t.Fatalf("content finding missing in:\n%s", out)
		}
		if !hasLine(out, "finding\taws-access-key\tlayer0:etc/data\t@uname") {
			t.Fatalf("metadata finding missing in:\n%s", out)
		}
	})
}

// TestMetaImageDataInNonRegularTypes: an entry that carries data is
// content-scanned whatever its type flag, so a contiguous file ('7')
// cannot hide a key; a symlink without data stays unread (control).
func TestMetaImageDataInNonRegularTypes(t *testing.T) {
	dir := t.TempDir()
	key := tokAWS()
	body := []byte("k=" + key + "\n")
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{{
		{h: &tar.Header{Name: "opt/cont", Typeflag: tar.TypeCont, Mode: 0o644}, body: body},
		{h: &tar.Header{Name: "opt/link", Typeflag: tar.TypeSymlink, Linkname: "cont", Mode: 0o777}},
	}}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stdout=%s stderr=%s", code, out, errb)
	}
	if !hasLine(out, contentLine("finding", "aws-access-key", "layer0:opt/cont", 2, len(key), sumOf(string(body)))) {
		t.Fatalf("contiguous-file content not scanned:\n%s", out)
	}
	if strings.Contains(out, "layer0:opt/link") {
		t.Fatalf("symlink without data reported:\n%s", out)
	}
	assertNoLiteral(t, out, errb, key)
}

// TestMetaArchiveIndexAcceptKey: a match in an archive index file is
// accepted only by an entry whose path is the printed archive:<name>,
// never by the bare outer name.
func TestMetaArchiveIndexAcceptKey(t *testing.T) {
	dir := t.TempDir()
	key := tokNPM()
	archive, manifest := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{{
		{h: &tar.Header{Name: "etc/ok", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("ok\n")},
	}}, map[string]any{"Note": key}, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	off := bytes.Index(manifest, []byte(key))
	line := contentLine("finding", "npm-token", "archive:manifest.json", off, len(key), sumOf(string(manifest)))
	if !hasLine(out, line) {
		t.Fatalf("index finding missing, want %q in:\n%s", line, out)
	}
	sum := sumOf(string(manifest))
	entry := func(p string) string {
		return "npm-token " + strconv.Itoa(off) + " " + strconv.Itoa(len(key)) + " sha256:" + sum + " " + p
	}
	code, out, errb = runImageCLI(t, archive, "--accept", entry("archive:manifest.json"))
	if code != 0 || !strings.Contains(out, "accepted\tnpm-token\tarchive:manifest.json\t") {
		t.Fatalf("archive: key not accepted: code=%d\n%s%s", code, out, errb)
	}
	code, out, errb = runImageCLI(t, archive, "--accept", entry("manifest.json"))
	if code != 1 || !hasLine(out, "unmatched-accept\taccept\t1") || !hasLine(out, line) {
		t.Fatalf("bare outer name must not accept: code=%d\n%s%s", code, out, errb)
	}
	assertNoLiteral(t, out, errb, key)
}

// TestMetaDenyOnJoinedNamesAndTargets: a deny literal that the raw
// member name or link target splits ("dir/./x", "dir//x", "dir/y/../x")
// but that a reader extracts as one path is found on the normalized
// name (@name, redacted display) and on the resolved link target
// (@link), in layers and in the context; a member that does not
// resolve to the literal is not reported.
func TestMetaDenyOnJoinedNamesAndTargets(t *testing.T) {
	token := "dir/can" + "aryx"
	auxDir := t.TempDir()
	deny := writeAuxFile(t, auxDir, "deny.txt", token+"\n")
	t.Run("image", func(t *testing.T) {
		entries := []metaEntry{
			{h: &tar.Header{Name: "dir/./canaryx", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("a\n")},
			{h: &tar.Header{Name: "dir//canaryx", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("b\n")},
			{h: &tar.Header{Name: "dir/y/../canaryx", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("c\n")},
			{h: &tar.Header{Name: "opt/link", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "../dir/./canaryx"}},
			{h: &tar.Header{Name: "opt/hard", Typeflag: tar.TypeLink, Mode: 0o644, Linkname: "dir//canaryx"}},
			{h: &tar.Header{Name: "dir/other", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("d\n")},
			{h: &tar.Header{Name: "opt/otherlink", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "../dir/./other"}},
		}
		archive, _ := metaArchive(t, t.TempDir(), "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
		code, out, errb := runImageCLI(t, archive, "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		want := []string{
			"finding\tdeny-token:1\tlayer0:entry#0\t@name",
			"finding\tdeny-token:1\tlayer0:entry#1\t@name",
			"finding\tdeny-token:1\tlayer0:entry#2\t@name",
			"finding\tdeny-token:1\tlayer0:opt/link\t@link",
			"finding\tdeny-token:1\tlayer0:opt/hard\t@link",
		}
		for _, w := range want {
			if !hasLine(out, w) {
				t.Fatalf("missing %q in:\n%s", w, out)
			}
		}
		if n := strings.Count(out, "finding\t"); n != len(want) {
			t.Fatalf("findings = %d, want %d:\n%s", n, len(want), out)
		}
		assertNoLiteral(t, out, errb, token)
		code, out, errb = runImageCLI(t, archive)
		if code != 0 || strings.Contains(out, "finding\t") {
			t.Fatalf("control: code = %d, stdout=%s stderr=%s", code, out, errb)
		}
	})
	t.Run("context", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "src/keep.txt", "ok\n")
		if err := os.Symlink("../dir/./canaryx", filepath.Join(dir, "src", "lnk")); err != nil {
			t.Skipf("os.Symlink failed: %v", err)
		}
		if err := os.Symlink("../dir/./other", filepath.Join(dir, "src", "otherlnk")); err != nil {
			t.Fatal(err)
		}
		code, out, errb := runContextCLI(t, dir, "--allow", "src", "--deny-file", deny)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		if !hasLine(out, "finding\tdeny-token:1\tsrc/lnk\t@link") {
			t.Fatalf("resolved-target finding missing in:\n%s", out)
		}
		if n := strings.Count(out, "finding\t"); n != 1 {
			t.Fatalf("findings = %d, want 1:\n%s", n, out)
		}
		assertNoLiteral(t, out, errb, token)
		code, out, errb = runContextCLI(t, dir, "--allow", "src")
		if code != 0 || strings.Contains(out, "finding\t") {
			t.Fatalf("control: code = %d, stdout=%s stderr=%s", code, out, errb)
		}
	})
}
