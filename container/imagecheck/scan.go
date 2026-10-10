package main

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Exit codes shared by the scan subcommands: 0 nothing found, 1
// findings present, 2 usage error, 4 I/O error.
const (
	exitOK       = 0
	exitFindings = 1
	exitUsage    = 2
	exitIO       = 4
)

// scanBlock is the streaming block size and scanOverlap the bytes of
// the previous block kept in front of the next one, so a match that
// crosses a block boundary stays visible.
const (
	scanBlock   = 1 << 20
	scanOverlap = 4 << 10
)

// scanRule is one named content rule: a rule name and a byte regex.
type scanRule struct {
	name    string
	pattern *regexp.Regexp
}

// scanRules are the built-in content rules. private-key requires a
// PEM body (optional header lines plus at least 64 base64 characters)
// so that bare format strings in binaries and docs do not count.
var scanRules = []scanRule{
	{name: "private-key", pattern: regexp.MustCompile(`-----BEGIN (RSA |EC |OPENSSH |DSA |ENCRYPTED )?PRIVATE KEY-----(?:\r?\n(?:[A-Za-z-]+: [^\r\n]*\r?\n)*(?:\r?\n)?[A-Za-z0-9+/=\r\n]{64,})`)},
	{name: "anthropic-key", pattern: regexp.MustCompile(`sk-ant-[a-z]{3}[0-9]{2}-[A-Za-z0-9_-]{80,}`)},
	{name: "openai-key", pattern: regexp.MustCompile(`sk-(proj|svcacct|admin)-[A-Za-z0-9_-]{40,}`)},
	{name: "xai-key", pattern: regexp.MustCompile(`xai-[A-Za-z0-9]{60,}`)},
	{name: "github-token", pattern: regexp.MustCompile(`(gh[pousr]_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{60,})`)},
	{name: "npm-token", pattern: regexp.MustCompile(`npm_[A-Za-z0-9]{36}`)},
	{name: "aws-access-key", pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
	{name: "slack-token", pattern: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
	{name: "google-api-key", pattern: regexp.MustCompile(`AIza[0-9A-Za-z_-]{35}`)},
}

// hostPathRules run only over the image config bytes. The Windows form
// matches the JSON-escaped backslashes of a path such as C:\Users\x.
var hostPathRules = []scanRule{
	{name: "host-path", pattern: regexp.MustCompile(`/Users/[^/\s"]+`)},
	{name: "host-path", pattern: regexp.MustCompile(`[A-Za-z]:\\\\Users\\\\`)},
	{name: "host-path", pattern: regexp.MustCompile(`/home/[^/\s"]+`)},
}

// awsDocsExample is the AWS documentation key; it is an example, not a
// credential, so it never counts as a finding. It is assembled from
// parts so the matchable literal does not live in the source.
const awsDocsExample = "AKIA" + "IOSFODNN7EXAMPLE"

// forbiddenComponents is the exact component table of the
// forbidden-name rule; every comparison is case-insensitive.
var forbiddenComponents = []string{
	".git", ".herdr-soho", ".herdr-agents", ".agents", ".worktrees",
	".claude", ".codex", ".grok", ".pi", ".ssh", ".aws", ".gnupg",
	".docker", ".npmrc", ".netrc", ".env", "auth.json",
	".credentials.json", "credentials.json", "credentials",
	".git-credentials",
}

// forbiddenPrefixes and forbiddenSuffixes are the partial component
// forms of forbidden-name.
var (
	forbiddenPrefixes = []string{".env.", "id_rsa", "id_ecdsa", "id_ed25519"}
	forbiddenSuffixes = []string{".pem", ".key", ".p12", ".pfx"}
)

// imageCredentialPaths is the remainder (after root/ or home/<user>/)
// that triggers credential-path in an image layer.
var imageCredentialPaths = []string{
	".claude/.credentials.json", ".claude.json", ".codex/auth.json",
	".grok/auth.json", ".pi/agent/auth.json", ".npmrc", ".netrc",
	".git-credentials", ".aws/credentials", ".docker/config.json",
	".config/gh/hosts.yml",
}

// imageSshPrefix: anything under it in the remainder triggers
// credential-path.
const imageSshPrefix = ".ssh/"

// envSecretKeywords: an image Env name whose upper-cased form contains
// any of these triggers config-env-secret.
var envSecretKeywords = []string{
	"TOKEN", "SECRET", "PASSWORD", "PASSWD", "API_KEY", "APIKEY",
	"ACCESS_KEY", "PRIVATE_KEY", "CREDENTIAL", "AUTH",
}

// denyToken holds one --deny-file token with the line number it came
// from. The token itself never leaves this process except as a search
// needle; the rule name is deny-token:<line>.
type denyToken struct {
	line   int
	needle []byte
}

// finding is one deduplicated result line.
type finding struct {
	rule   string
	path   string
	offset int64 // -1 for name rules, printed as "-"
	accept bool
}

// scanFindings collects the deduplicated findings and the summary
// counters of one subcommand run.
type scanFindings struct {
	seen  map[string]bool
	order []finding
	files int
	bytes int64
}

// newFindings starts an empty collector.
func newFindings() *scanFindings {
	return &scanFindings{seen: map[string]bool{}}
}

// add records one finding. Name rules pass offset -1. The first
// offset of each (rule, path) wins; later matches at other offsets of
// the same path are dropped.
func (f *scanFindings) add(rule, p string, offset int64) {
	key := rule + "\x00" + p
	if f.seen[key] {
		return
	}
	f.seen[key] = true
	f.order = append(f.order, finding{rule: rule, path: p, offset: offset})
}

// accept marks the finding (if any) of (p, rule) as accepted and
// returns whether it existed.
func (f *scanFindings) accept(p, rule string) bool {
	for i := range f.order {
		if f.order[i].rule == rule && f.order[i].path == p {
			f.order[i].accept = true
			return true
		}
	}
	return false
}

// counts splits the findings into the non-accepted and the accepted.
func (f *scanFindings) counts() (findings, accepted int) {
	for _, x := range f.order {
		if x.accept {
			accepted++
		} else {
			findings++
		}
	}
	return
}

// print writes one line per finding and the final summary line. It
// never prints the matched content: only the kind, the rule, the path
// and the byte offset.
func (f *scanFindings) print(w io.Writer) {
	nf, na := f.counts()
	for _, x := range f.order {
		kind := "finding"
		if x.accept {
			kind = "accepted"
		}
		off := "-"
		if x.offset >= 0 {
			off = strconv.FormatInt(x.offset, 10)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", kind, x.rule, x.path, off)
	}
	fmt.Fprintf(w, "summary\tfiles=%d\tbytes=%d\tfindings=%d\taccepted=%d\n", f.files, f.bytes, nf, na)
}

// scanOpts are the parsed flags of one subcommand run.
type scanOpts struct {
	allow      []string // context only; normalized without a trailing "/"
	denyFile   string
	acceptFile string // at most one
	accept     []string
}

// parseScanFlags splits the subcommand arguments into the positional
// args and the shared flags. Every flag may come before or after the
// positional argument. Unknown flags and malformed --accept specs are
// usage errors.
func parseScanFlags(args []string, opts *scanOpts) (pos []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		var v string
		switch {
		case a == "--allow":
			if i+1 >= len(args) {
				return nil, errors.New("--allow requires a value")
			}
			i++
			v = args[i]
			addAllow(opts, v)
		case strings.HasPrefix(a, "--allow="):
			addAllow(opts, a[len("--allow="):])
		case a == "--deny-file":
			if i+1 >= len(args) {
				return nil, errors.New("--deny-file requires a value")
			}
			i++
			opts.denyFile = args[i]
		case strings.HasPrefix(a, "--deny-file="):
			opts.denyFile = a[len("--deny-file="):]
		case a == "--accept-file":
			if i+1 >= len(args) {
				return nil, errors.New("--accept-file requires a value")
			}
			i++
			if opts.acceptFile != "" {
				return nil, errors.New("--accept-file may only be given once")
			}
			opts.acceptFile = args[i]
		case strings.HasPrefix(a, "--accept-file="):
			if opts.acceptFile != "" {
				return nil, errors.New("--accept-file may only be given once")
			}
			opts.acceptFile = a[len("--accept-file="):]
		case a == "--accept":
			if i+1 >= len(args) {
				return nil, errors.New("--accept requires a value")
			}
			i++
			v = args[i]
		case strings.HasPrefix(a, "--accept="):
			v = a[len("--accept="):]
		case strings.HasPrefix(a, "-") && a != "-":
			return nil, fmt.Errorf("unknown flag %q", a)
		default:
			pos = append(pos, a)
		}
		if a == "--accept" || strings.HasPrefix(a, "--accept=") {
			if err := checkAcceptSpec(v); err != nil {
				return nil, err
			}
			opts.accept = append(opts.accept, v)
		}
	}
	return pos, nil
}

// addAllow normalizes one --allow value and records it; a value that
// normalizes to empty (such as "") is not a prefix and is dropped.
func addAllow(opts *scanOpts, v string) {
	if v = normalizeAllow(v); v != "" {
		opts.allow = append(opts.allow, v)
	}
}

// normalizeAllow trims surrounding whitespace and the trailing "/" so
// a prefix covers the path equal to it and everything below it.
func normalizeAllow(v string) string {
	return strings.TrimSuffix(strings.TrimSpace(v), "/")
}

// checkAcceptSpec validates one --accept <path>=<rule> spec. The split
// happens at the last "=" because a rule name such as deny-token:12
// contains no "=" but a path may contain one.
func checkAcceptSpec(spec string) error {
	eq := strings.LastIndex(spec, "=")
	if eq <= 0 || eq == len(spec)-1 {
		return fmt.Errorf("malformed --accept %q: expected <path>=<rule>", spec)
	}
	return nil
}

// readAcceptFile loads a --accept-file: one <path>=<rule> entry per
// line, with the same shape as --accept (split at the last "="). Blank
// lines and lines starting with "#" are ignored; spaces at both ends
// are removed. A malformed line is a usage error and the message cites
// the line number only.
func readAcceptFile(p string) ([]string, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var specs []string
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if err := checkAcceptSpec(line); err != nil {
			return nil, fmt.Errorf("accept-file %s: line %d: malformed entry", p, i+1)
		}
		specs = append(specs, line)
	}
	return specs, nil
}

// readDenyFile loads the --deny-file tokens. Blank lines and lines
// starting with "#" are ignored; spaces at both ends are removed. A
// token shorter than 4 bytes is a usage error and the message cites
// the line number only, never the token.
func readDenyFile(p string) ([]denyToken, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var toks []denyToken
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(line) < 4 {
			return nil, fmt.Errorf("deny-file %s: line %d: token shorter than 4 bytes", p, i+1)
		}
		toks = append(toks, denyToken{line: i + 1, needle: []byte(line)})
	}
	return toks, nil
}

// forbiddenName reports whether any path component of rel triggers the
// forbidden-name rule. rel uses "/" separators.
func forbiddenName(rel string) bool {
	for _, c := range strings.Split(rel, "/") {
		if nameForbidden(c) {
			return true
		}
	}
	return false
}

// nameForbidden is the case-insensitive component test of
// forbidden-name.
func nameForbidden(c string) bool {
	lc := strings.ToLower(c)
	for _, x := range forbiddenComponents {
		if lc == x {
			return true
		}
	}
	for _, x := range forbiddenPrefixes {
		if strings.HasPrefix(lc, x) {
			return true
		}
	}
	for _, x := range forbiddenSuffixes {
		if strings.HasSuffix(lc, x) {
			return true
		}
	}
	return false
}

// notAllowlisted reports whether rel (a file or symlink path, "/"
// separators) is covered by no allow prefix. A prefix covers the path
// equal to it and everything below it.
func notAllowlisted(rel string, allow []string) bool {
	for _, p := range allow {
		if rel == p || strings.HasPrefix(rel, p+"/") {
			return false
		}
	}
	return true
}

// contentScanner streams r in scanBlock chunks with a scanOverlap tail
// of the previous chunk kept in front of the next one. Every rule and
// deny token is reported at most once, at the absolute offset where
// its first match starts; the overlap makes a match that crosses a
// block boundary visible and the first-match rule drops the duplicate
// report from the next window.
type contentScanner struct {
	r       io.Reader
	pos     int64 // absolute offset of the start of buf
	buf     []byte
	first   bool
	rules   []scanRule
	toks    []denyToken
	done    map[string]bool // rule names already reported
	overlap int             // tail kept between windows
	onMatch func(rule string, offset int64)
}

// newContentScanner wraps r; report receives (rule, absolute offset)
// for the first match of each rule and deny token.
func newContentScanner(r io.Reader, rules []scanRule, toks []denyToken, report func(rule string, offset int64)) *contentScanner {
	done := map[string]bool{}
	for _, t := range toks {
		done["deny-token:"+strconv.Itoa(t.line)] = false
	}
	// The tail must hold all but the last byte of the longest deny token,
	// so a literal longer than scanOverlap that crosses a block boundary
	// still lies whole inside one window.
	overlap := scanOverlap
	for _, t := range toks {
		if n := len(t.needle) - 1; n > overlap {
			overlap = n
		}
	}
	return &contentScanner{r: r, first: true, rules: rules, toks: toks, done: done, overlap: overlap, onMatch: report}
}

// scan runs the stream to the end and returns how many new bytes it
// read (the overlap kept between windows is not counted twice).
func (s *contentScanner) scan() (int64, error) {
	var total int64
	for {
		var chunk, nb []byte
		var err error
		if s.first {
			nb, err = readBlock(s.r)
			chunk = nb
		} else {
			tail := s.buf
			if len(tail) > s.overlap {
				tail = tail[len(tail)-s.overlap:]
			}
			nb, err = readBlock(s.r)
			if len(nb) == 0 {
				if err != nil && err != io.EOF && !errors.Is(err, io.ErrUnexpectedEOF) {
					return total, err
				}
				break // nothing beyond the overlap
			}
			if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
				return total, err
			}
			// The new window starts at the start of the overlap:
			// the previous window minus the bytes before the tail.
			s.pos += int64(len(s.buf) - len(tail))
			chunk = append(append([]byte{}, tail...), nb...)
		}
		s.first = false
		s.buf = chunk
		total += int64(len(nb))
		s.scanBuf()
		if err == nil {
			continue
		}
		if err == io.EOF || errors.Is(err, io.ErrUnexpectedEOF) {
			return total, nil
		}
		return total, err
	}
	return total, nil
}

// scanBuf reports the first match of every pending rule and deny
// token inside the current window.
func (s *contentScanner) scanBuf() {
	for _, r := range s.rules {
		if s.done[r.name] {
			continue
		}
		for _, m := range r.pattern.FindAllIndex(s.buf, -1) {
			seg := s.buf[m[0]:m[1]]
			if r.name == "aws-access-key" && string(seg) == awsDocsExample {
				continue // the documentation example is not a finding
			}
			if r.name == "host-path" && string(seg) == "/home/agent" {
				continue // the image's own home is not a host path
			}
			s.done[r.name] = true
			s.onMatch(r.name, s.pos+int64(m[0]))
			break
		}
	}
	for _, t := range s.toks {
		name := "deny-token:" + strconv.Itoa(t.line)
		if s.done[name] {
			continue
		}
		if i := bytes.Index(s.buf, t.needle); i >= 0 {
			s.done[name] = true
			s.onMatch(name, s.pos+int64(i))
		}
	}
}

// readBlock reads up to scanBlock bytes; a short read at the end of
// the stream is io.ErrUnexpectedEOF and a clean end is io.EOF.
func readBlock(r io.Reader) ([]byte, error) {
	buf := make([]byte, scanBlock)
	n, err := io.ReadFull(r, buf)
	if n == 0 && err == nil {
		err = io.EOF
	}
	return buf[:n], err
}

// applyAccept marks every --accept <path>=<rule> pair; a spec that
// matches no finding is not an error (it is a forward-looking
// allowlist). The split is at the last "=".
func applyAccept(f *scanFindings, specs []string) {
	for _, spec := range specs {
		eq := strings.LastIndex(spec, "=")
		f.accept(spec[:eq], spec[eq+1:])
	}
}

// reportLoadError maps a --deny-file or --accept-file load error to an
// exit code: a missing or unreadable file is an I/O error, a short
// token or a malformed entry is a usage error.
func reportLoadError(w io.Writer, err error) int {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		fmt.Fprintf(w, "imagecheck: %v\n", err)
		return exitIO
	}
	fmt.Fprintf(w, "imagecheck: %v\n", err)
	return exitUsage
}

// runContext scans a build context directory for secrets, credential
// files and denied tokens. It prints one line per finding and a
// summary line; the matched content is never printed.
func runContext(args []string, stdout, stderr io.Writer) int {
	var opts scanOpts
	pos, err := parseScanFlags(args, &opts)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		usage(stderr)
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "imagecheck: context needs exactly one <dir> argument\n")
		usage(stderr)
		return exitUsage
	}
	dir := pos[0]
	if len(opts.allow) == 0 {
		fmt.Fprintln(stderr, "imagecheck: context needs at least one --allow <prefix>")
		usage(stderr)
		return exitUsage
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		if err == nil {
			err = fmt.Errorf("%s: not a directory", dir)
		}
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return exitIO
	}
	var toks []denyToken
	if opts.denyFile != "" {
		toks, err = readDenyFile(opts.denyFile)
		if err != nil {
			return reportLoadError(stderr, err)
		}
	}
	var fileAccepts []string
	if opts.acceptFile != "" {
		fileAccepts, err = readAcceptFile(opts.acceptFile)
		if err != nil {
			return reportLoadError(stderr, err)
		}
	}
	f := newFindings()
	if err := walkContext(dir, opts.allow, toks, f); err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return exitIO
	}
	applyAccept(f, append(fileAccepts, opts.accept...))
	f.print(stdout)
	if nf, _ := f.counts(); nf > 0 {
		return exitFindings
	}
	return exitOK
}

// walkContext walks dir in lexical order. Every entry gets the
// forbidden-name check; files and symlinks also get not-allowlisted;
// only regular files are content-scanned (symlinks are never read,
// avoiding cycles and duplicate content).
func walkContext(dir string, allow []string, toks []denyToken, f *scanFindings) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if forbiddenName(rel) {
				f.add("forbidden-name", rel, -1)
			}
			return nil
		}
		f.files++
		if forbiddenName(rel) {
			f.add("forbidden-name", rel, -1)
		}
		if notAllowlisted(rel, allow) {
			f.add("not-allowlisted", rel, -1)
		}
		if !d.Type().IsRegular() {
			// Symlinks and other entries: name rules only, the
			// content is never read.
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Size() == 0 {
			return nil
		}
		file, err := os.Open(p)
		if err != nil {
			return err
		}
		defer file.Close()
		s := newContentScanner(file, scanRules, toks, func(rule string, off int64) {
			f.add(rule, rel, off)
		})
		n, err := s.scan()
		if err != nil {
			return err
		}
		f.bytes += n
		return nil
	})
}

// runImage scans a docker save archive: every layer entry and the
// image config for secrets, credential paths, denied tokens and host
// paths. The matched content is never printed.
func runImage(args []string, stdout, stderr io.Writer) int {
	var opts scanOpts
	pos, err := parseScanFlags(args, &opts)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		usage(stderr)
		return exitUsage
	}
	if len(pos) != 1 {
		fmt.Fprintf(stderr, "imagecheck: image needs exactly one <archive> argument\n")
		usage(stderr)
		return exitUsage
	}
	archive := pos[0]
	if len(opts.allow) > 0 {
		fmt.Fprintln(stderr, "imagecheck: image does not take --allow")
		usage(stderr)
		return exitUsage
	}
	if info, err := os.Stat(archive); err != nil || !info.Mode().IsRegular() {
		if err == nil {
			err = fmt.Errorf("%s: not a regular file", archive)
		}
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return exitIO
	}
	var toks []denyToken
	if opts.denyFile != "" {
		toks, err = readDenyFile(opts.denyFile)
		if err != nil {
			return reportLoadError(stderr, err)
		}
	}
	var fileAccepts []string
	if opts.acceptFile != "" {
		fileAccepts, err = readAcceptFile(opts.acceptFile)
		if err != nil {
			return reportLoadError(stderr, err)
		}
	}
	img, err := openSaved(archive)
	if err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return exitIO
	}
	f := newFindings()
	walkErr := walkLayers(archive, img, func(index int, _ string, layer io.Reader) error {
		return scanLayer(layer, index, toks, f)
	})
	if walkErr != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", walkErr)
		return exitIO
	}
	// The config is scanned after the layers; its content rules also
	// cover history/created_by, plus the config-only rules.
	if err := scanImageConfig(img, toks, f); err != nil {
		fmt.Fprintf(stderr, "imagecheck: %v\n", err)
		return exitIO
	}
	applyAccept(f, append(fileAccepts, opts.accept...))
	f.print(stdout)
	if nf, _ := f.counts(); nf > 0 {
		return exitFindings
	}
	return exitOK
}

// entryName normalizes a tar entry name: the leading "./" is removed
// and the path cleaned (a directory's trailing "/" included).
func entryName(h *tar.Header) string {
	n := strings.TrimPrefix(h.Name, "./")
	if n == "" {
		n = "/"
	}
	return path.Clean(n)
}

// scanLayer scans one layer tar: every entry gets the image name rules
// (whiteouts are ignored) and regular files are content-scanned.
func scanLayer(layer io.Reader, index int, toks []denyToken, f *scanFindings) error {
	return walkTar(layer, func(h *tar.Header, content io.Reader) error {
		name := entryName(h)
		if strings.HasPrefix(path.Base(name), ".wh.") {
			// Whiteout markers carry no content and no meaning.
			return nil
		}
		display := "layer" + strconv.Itoa(index) + ":" + name
		if name != "/" {
			if credentialPath(name) {
				f.add("credential-path", display, -1)
			}
			if gitDir(name) {
				f.add("git-dir", display, -1)
			}
		}
		if h.Typeflag != tar.TypeReg {
			// Non-regular entries (dirs, symlinks, links): name rules
			// only, no content.
			return nil
		}
		f.files++
		if h.Size == 0 {
			return nil
		}
		s := newContentScanner(content, scanRules, toks, func(rule string, off int64) {
			f.add(rule, display, off)
		})
		n, err := s.scan()
		if err != nil {
			return err
		}
		f.bytes += n
		return nil
	})
}

// credentialPath reports whether the remainder of name below root/ or
// home/<user>/ is a known credential path or anything under .ssh/.
func credentialPath(name string) bool {
	rest, ok := credentialRemainder(name)
	if !ok {
		return false
	}
	if strings.HasPrefix(rest, imageSshPrefix) {
		return true
	}
	for _, p := range imageCredentialPaths {
		if rest == p {
			return true
		}
	}
	return false
}

// credentialRemainder strips the root/ or home/<user>/ prefix and
// returns the remainder below it.
func credentialRemainder(name string) (string, bool) {
	rest := strings.TrimPrefix(name, "root/")
	if rest != name {
		return rest, true
	}
	if !strings.HasPrefix(name, "home/") {
		return "", false
	}
	rest = name[len("home/"):]
	i := strings.IndexByte(rest, '/')
	if i < 0 {
		return "", false
	}
	return rest[i+1:], true
}

// gitDir reports whether any component of name is .git.
func gitDir(name string) bool {
	for _, c := range strings.Split(name, "/") {
		if c == ".git" {
			return true
		}
	}
	return false
}

// scanImageConfig scans the config: the Env names, then the content
// rules (which cover history/created_by), the deny tokens and the
// host-path rule. The matched content is never printed.
func scanImageConfig(img *savedImage, toks []denyToken, f *scanFindings) error {
	f.files++
	f.bytes += int64(len(img.Config))
	rules := make([]scanRule, 0, len(scanRules)+len(hostPathRules))
	rules = append(rules, scanRules...)
	rules = append(rules, hostPathRules...)
	s := newContentScanner(bytes.NewReader(img.Config), rules, toks, func(rule string, off int64) {
		f.add(rule, "config", off)
	})
	if _, err := s.scan(); err != nil {
		return err
	}
	// A config that does not parse cannot be checked for secret Env
	// entries, so it fails the scan instead of passing unread.
	cfg, err := img.parseConfig()
	if err != nil {
		return err
	}
	for _, e := range cfg.Config.Env {
		name, value, ok := strings.Cut(e, "=")
		if !ok || value == "" {
			continue
		}
		if envSecretName(name) {
			f.add("config-env-secret:"+name, "config", -1)
		}
	}
	return nil
}

// envSecretName reports whether the upper-cased Env name contains one
// of the secret keywords.
func envSecretName(name string) bool {
	up := strings.ToUpper(name)
	for _, k := range envSecretKeywords {
		if strings.Contains(up, k) {
			return true
		}
	}
	return false
}
