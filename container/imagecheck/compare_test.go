package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"
)

// compareTestConfig builds an image config with the given layer diff ids
// and extra top-level keys.
func compareTestConfig(diffIDs []string, extra map[string]any) map[string]any {
	cfg := map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"config":       map[string]any{"Env": []string{"PATH=/usr/bin"}, "User": "agent"},
		"rootfs":       map[string]any{"diff_ids": diffIDs},
		"version":      "1.0",
	}
	for k, v := range extra {
		cfg[k] = v
	}
	return cfg
}

// configID is the image id the tool derives from the raw config bytes:
// the same marshalling writeSavedArchive uses.
func configID(t *testing.T, cfg any) string {
	t.Helper()
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// runCompareLines runs compare on two archives and returns the exit code,
// the stdout lines and stderr.
func runCompareLines(t *testing.T, a, b string) (int, []string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := runCompare([]string{a, b}, &stdout, &stderr)
	var lines []string
	for _, l := range bytes.Split(stdout.Bytes(), []byte("\n")) {
		lines = append(lines, string(l))
	}
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return code, lines, stderr.String()
}

func TestCompareIdentical(t *testing.T) {
	dir := t.TempDir()
	layers := [][]testEntry{
		{{Name: "etc/", Dir: true}, {Name: "etc/hello", Body: "one"}},
		{{Name: "usr/bin/tool", Body: "two", Mode: 0o755}},
	}
	diffIDs := honestDiffIDs(t, layers)
	cfg := compareTestConfig(diffIDs, nil)
	// Same content, different layer compression: the verdict must not
	// depend on how the layer blobs are stored.
	a := writeSavedArchive(t, dir, "a.tar", cfg, layers, false)
	b := writeSavedArchive(t, dir, "b.tar", cfg, layers, true)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q", stderr)
	}
	id := configID(t, cfg)
	want := []string{
		"config\t" + id + "\t" + id + "\tidentical",
		"layer\t0\t" + diffIDs[0] + "\t" + diffIDs[0] + "\tsame",
		"layer\t1\t" + diffIDs[1] + "\t" + diffIDs[1] + "\tsame",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareLayerMtimeDiffers(t *testing.T) {
	dir := t.TempDir()
	layersA := [][]testEntry{
		{{Name: "etc/hello", Body: "one", ModTime: 1000}},
	}
	layersB := [][]testEntry{
		{{Name: "etc/hello", Body: "one", ModTime: 2000}},
	}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\trootfs",
		"layer\t0\t" + diffA[0] + "\t" + diffB[0] + "\tdifferent",
		"entry\t0\tetc/hello\tchanged:mtime",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareContentDiffersSameSize(t *testing.T) {
	dir := t.TempDir()
	layersA := [][]testEntry{
		{{Name: "etc/data", Body: "abcd"}},
	}
	layersB := [][]testEntry{
		{{Name: "etc/data", Body: "abce"}},
	}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\trootfs",
		"layer\t0\t" + diffA[0] + "\t" + diffB[0] + "\tdifferent",
		"entry\t0\tetc/data\tchanged:content",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareEntryAddedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	layersA := [][]testEntry{
		{{Name: "etc/keep", Body: "k"}, {Name: "etc/only-a", Body: "x"}},
	}
	layersB := [][]testEntry{
		{{Name: "etc/keep", Body: "k"}, {Name: "etc/only-b", Body: "y"}},
	}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	// The unchanged "etc/keep" entry is not reported; the differing
	// entries come out sorted by path.
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\trootfs",
		"layer\t0\t" + diffA[0] + "\t" + diffB[0] + "\tdifferent",
		"entry\t0\tetc/only-a\tremoved",
		"entry\t0\tetc/only-b\tadded",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareConfigFieldDiffs(t *testing.T) {
	dir := t.TempDir()
	// Same layers and diff ids; the configs differ in the nested "config"
	// object, a key present only in a ("author") and the "history" value:
	// the config-field lines must be alphabetical.
	layers := [][]testEntry{{{Name: "etc/keep", Body: "k"}}}
	diffID := honestDiffIDs(t, layers)[0]
	cfgA := compareTestConfig([]string{diffID}, map[string]any{
		"author":  "alice",
		"history": []any{map[string]any{"created_by": "a"}},
	})
	cfgB := compareTestConfig([]string{diffID}, map[string]any{
		"history": []any{map[string]any{"created_by": "b"}},
	})
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layers, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layers, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\tauthor",
		"config-field\thistory",
		"layer\t0\t" + diffID + "\t" + diffID + "\tsame",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareLayerCountDiffers(t *testing.T) {
	dir := t.TempDir()
	layersA := [][]testEntry{
		{{Name: "etc/a", Body: "1"}},
		{{Name: "etc/b", Body: "2"}},
	}
	layersB := [][]testEntry{
		{{Name: "etc/a", Body: "1"}},
	}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\trootfs",
		"layers\t2\t1",
		"layer\t0\t" + diffA[0] + "\t" + diffB[0] + "\tsame",
	}
	if len(lines) != len(want) {
		t.Fatalf("lines = %q, want %q", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareChangedAttributeOrder(t *testing.T) {
	dir := t.TempDir()
	layersA := [][]testEntry{
		{{Name: "bin/tool", Body: "aaaa", Mode: 0o755}},
	}
	layersB := [][]testEntry{
		{{Name: "bin/tool", Body: "bbbb", Mode: 0o644}},
	}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	last := lines[len(lines)-1]
	if last != "entry\t0\tbin/tool\tchanged:mode,content" {
		t.Fatalf("last line = %q, want changed:mode,content", last)
	}
}

func TestCompareEntryLimit(t *testing.T) {
	dir := t.TempDir()
	const n = 503
	// All n entries differ in content only: equal 4-byte bodies keep the
	// size attribute out of the diff.
	var entriesA, entriesB []testEntry
	for i := 0; i < n; i++ {
		p := fmt.Sprintf("f/%04d.txt", i)
		entriesA = append(entriesA, testEntry{Name: p, Body: "base"})
		entriesB = append(entriesB, testEntry{Name: p, Body: fmt.Sprintf("v%03d", i)})
	}
	layersA := [][]testEntry{entriesA}
	layersB := [][]testEntry{entriesB}
	diffA, diffB := honestDiffIDs(t, layersA), honestDiffIDs(t, layersB)
	cfgA := compareTestConfig(diffA, nil)
	cfgB := compareTestConfig(diffB, nil)
	a := writeSavedArchive(t, dir, "a.tar", cfgA, layersA, false)
	b := writeSavedArchive(t, dir, "b.tar", cfgB, layersB, false)
	code, lines, stderr := runCompareLines(t, a, b)
	if code != 1 || stderr != "" {
		t.Fatalf("exit = %d, stderr = %q", code, stderr)
	}
	want := []string{
		"config\t" + configID(t, cfgA) + "\t" + configID(t, cfgB) + "\tdifferent",
		"config-field\trootfs",
		"layer\t0\t" + diffA[0] + "\t" + diffB[0] + "\tdifferent",
	}
	for i := 0; i < maxEntryLines; i++ {
		want = append(want, fmt.Sprintf("entry\t0\tf/%04d.txt\tchanged:content", i))
	}
	want = append(want, "entry-limit\t0\t3")
	if len(lines) != len(want) {
		t.Fatalf("lines = %d, want %d (last = %q)", len(lines), len(want), lines[len(lines)-1])
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

func TestCompareMissingFile(t *testing.T) {
	dir := t.TempDir()
	var stdout, stderr bytes.Buffer
	code := runCompare([]string{filepath.Join(dir, "nope.tar"), filepath.Join(dir, "nada.tar")}, &stdout, &stderr)
	if code != 4 {
		t.Fatalf("exit = %d, want 4", code)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() == 0 {
		t.Fatal("stderr is empty, want an I/O error message")
	}
}

func TestCompareUsage(t *testing.T) {
	for _, args := range [][]string{nil, {"one"}, {"a", "b", "c"}} {
		var stdout, stderr bytes.Buffer
		code := runCompare(args, &stdout, &stderr)
		if code != 2 {
			t.Fatalf("args %v: exit = %d, want 2", args, code)
		}
		if stdout.Len() != 0 {
			t.Fatalf("args %v: stdout = %q", args, stdout.String())
		}
		if stderr.Len() == 0 {
			t.Fatalf("args %v: stderr is empty", args)
		}
	}
}
