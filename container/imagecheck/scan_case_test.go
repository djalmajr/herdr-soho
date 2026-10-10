package main

import (
	"archive/tar"
	"strings"
	"testing"
)

// testCaseReg is one regular layer entry with the harmless body "x\n".
func testCaseReg(name string) metaEntry {
	return metaEntry{h: &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte("x\n")}
}

// TestCaseImageCredentialAndGitDirMixedCase: the git-dir and
// credential-path rules match ASCII case-insensitively, on the
// normalized member name and on symlink and hard link targets; the
// printed display keeps the original case of the member name.
func TestCaseImageCredentialAndGitDirMixedCase(t *testing.T) {
	entries := []metaEntry{
		testCaseReg("src/.Git/config"),
		testCaseReg("src/.GIT/HEAD"),
		{h: &tar.Header{Name: "a/.gIt/", Typeflag: tar.TypeDir, Mode: 0o755}},
		testCaseReg("root/.AWS/credentials"),
		testCaseReg("/Root/.aws/credentials"),
		testCaseReg("./ROOT/.Aws/Credentials"),
		testCaseReg("../root/.aws/CREDENTIALS"),
		testCaseReg("home/agent/.SSH/id_rsa"),
		testCaseReg("HOME/Agent/.ssh/config"),
		testCaseReg("/Home/u/.Codex/Auth.json"),
		testCaseReg("home/u/.CONFIG/GH/hosts.yml"),
		{h: &tar.Header{Name: "etc/awsl", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "/root/.Aws/credentials"}},
		{h: &tar.Header{Name: "opt/awl", Typeflag: tar.TypeLink, Mode: 0o644, Linkname: "/root/.Aws/credentials"}},
		{h: &tar.Header{Name: "etc/gitl", Typeflag: tar.TypeSymlink, Mode: 0o777, Linkname: "x/.GIT/config"}},
	}
	dir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 1 {
		t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
	}
	// The exact lines pin the rule, the display (original case, with
	// leading "/", "./" and ".." normalized away) and the @link marker.
	for _, w := range []string{
		"finding\tgit-dir\tlayer0:src/.Git/config\t-",
		"finding\tgit-dir\tlayer0:src/.GIT/HEAD\t-",
		"finding\tgit-dir\tlayer0:a/.gIt\t-",
		"finding\tcredential-path\tlayer0:root/.AWS/credentials\t-",
		"finding\tcredential-path\tlayer0:Root/.aws/credentials\t-",
		"finding\tcredential-path\tlayer0:ROOT/.Aws/Credentials\t-",
		"finding\tcredential-path\tlayer0:root/.aws/CREDENTIALS\t-",
		"finding\tcredential-path\tlayer0:home/agent/.SSH/id_rsa\t-",
		"finding\tcredential-path\tlayer0:HOME/Agent/.ssh/config\t-",
		"finding\tcredential-path\tlayer0:Home/u/.Codex/Auth.json\t-",
		"finding\tcredential-path\tlayer0:home/u/.CONFIG/GH/hosts.yml\t-",
		"finding\tcredential-path\tlayer0:etc/awsl\t@link",
		"finding\tcredential-path\tlayer0:opt/awl\t@link",
		"finding\tgit-dir\tlayer0:etc/gitl\t@link",
	} {
		if !hasLine(out, w) {
			t.Fatalf("missing %q in:\n%s", w, out)
		}
	}
	if n := strings.Count(out, "finding\t"); n != 14 {
		t.Fatalf("findings = %d, want 14:\n%s", n, out)
	}
}

// TestCaseImageCredentialAndGitDirNegatives: look-alike names that
// only become credential or git paths after a different (or
// case-fold-only) reading stay clean.
func TestCaseImageCredentialAndGitDirNegatives(t *testing.T) {
	entries := []metaEntry{
		testCaseReg("src/.gitignore"),
		testCaseReg("src/.github/x"),
		testCaseReg("src/.gitkeep"),
		testCaseReg("root/.aws/config"),
		testCaseReg("rooted/.aws/credentials"),
		testCaseReg("homes/u/.ssh/id_rsa"),
		testCaseReg("home/.ssh"),
		testCaseReg("home/u/.sshx/key"),
		testCaseReg("root/.awscredentials"),
		testCaseReg("opt/root/.aws/credentials"),
	}
	dir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if lineWith(out, "finding\t") != "" {
		t.Fatalf("unexpected finding in:\n%s", out)
	}
}

// TestCaseImageWhiteoutMixedCase: a genuine whiteout marker with a
// mixed-case credential path (zero-size regular file, ".wh." base)
// records a deletion and is not reported, as today for lower case.
func TestCaseImageWhiteoutMixedCase(t *testing.T) {
	dir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{
		{{h: &tar.Header{Name: "root/.AWS/.wh.credentials", Typeflag: tar.TypeReg, Mode: 0o644}}},
	}, nil, nil)
	code, out, errb := runImageCLI(t, archive)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%s", code, errb)
	}
	if strings.Contains(out, "layer0:root/.AWS/.wh.credentials") {
		t.Fatalf("genuine whiteout marker must give no finding:\n%s", out)
	}
	if lineWith(out, "finding\t") != "" {
		t.Fatalf("unexpected finding in:\n%s", out)
	}
}

// TestCaseAcceptCannotSuppressNameRules: an accept entry binds to a
// content match only; the credential-path and git-dir findings of the
// same mixed-case entries stay active and the run fails.
func TestCaseAcceptCannotSuppressNameRules(t *testing.T) {
	token := tokNPM()
	body := "k=" + token + "\n"
	entries := []metaEntry{
		{h: &tar.Header{Name: "root/.AWS/credentials", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte(body)},
		{h: &tar.Header{Name: "a/.gIt/notes.txt", Typeflag: tar.TypeReg, Mode: 0o644}, body: []byte(body)},
	}
	dir := t.TempDir()
	auxDir := t.TempDir()
	archive, _ := metaArchive(t, dir, "img.tar", map[string]any{}, [][]metaEntry{entries}, nil, nil)
	accept := writeAuxFile(t, auxDir, "accept.txt",
		entryOf("npm-token", "root/.AWS/credentials", body, token)+"\n"+
			entryOf("npm-token", "a/.gIt/notes.txt", body, token)+"\n")
	t.Run("with-accept", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive, "--accept-file", accept)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		for _, w := range []string{
			contentLine("accepted", "npm-token", "layer0:root/.AWS/credentials", 2, len(token), sumOf(body)),
			contentLine("accepted", "npm-token", "layer0:a/.gIt/notes.txt", 2, len(token), sumOf(body)),
			"finding\tcredential-path\tlayer0:root/.AWS/credentials\t-",
			"finding\tgit-dir\tlayer0:a/.gIt/notes.txt\t-",
		} {
			if !hasLine(out, w) {
				t.Fatalf("missing %q in:\n%s", w, out)
			}
		}
		assertNoLiteral(t, out, errb, token)
	})
	t.Run("without-accept", func(t *testing.T) {
		code, out, errb := runImageCLI(t, archive)
		if code != 1 {
			t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
		}
		for _, w := range []string{
			contentLine("finding", "npm-token", "layer0:root/.AWS/credentials", 2, len(token), sumOf(body)),
			contentLine("finding", "npm-token", "layer0:a/.gIt/notes.txt", 2, len(token), sumOf(body)),
			"finding\tcredential-path\tlayer0:root/.AWS/credentials\t-",
			"finding\tgit-dir\tlayer0:a/.gIt/notes.txt\t-",
		} {
			if !hasLine(out, w) {
				t.Fatalf("missing %q in:\n%s", w, out)
			}
		}
		assertNoLiteral(t, out, errb, token)
	})
}

// TestImageContextForbiddenNameMixedCase: the context command already
// reports forbidden-name for mixed-case .git, .aws and .ssh
// components (nameForbidden folds the components before the table
// lookup); this pins that behaviour.
func TestImageContextForbiddenNameMixedCase(t *testing.T) {
	cases := []struct{ rel string }{
		{".Git/config"},
		{"x/.AWS/credentials"},
		{"x/.SSH/id_rsa"},
	}
	for _, c := range cases {
		t.Run(c.rel, func(t *testing.T) {
			dir := t.TempDir()
			writeContextFile(t, dir, c.rel, "x\n")
			allow := strings.Split(c.rel, "/")[0]
			code, out, errb := runContextCLI(t, dir, "--allow", allow)
			if code != 1 {
				t.Fatalf("code = %d, want 1; stderr=%s", code, errb)
			}
			want := "finding\tforbidden-name\t" + c.rel + "\t-"
			if !hasLine(out, want) {
				t.Fatalf("missing %q in:\n%s", want, out)
			}
		})
	}
}
