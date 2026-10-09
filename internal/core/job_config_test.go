package core

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestReadConfigFilePairs pins the shared single-file parser adapter: one
// pair per key in first-occurrence order, the normalized key, the raw
// (whitespace-trimmed) spelling of the last occurrence, last value wins,
// and os.ErrNotExist for an absent file.
func TestReadConfigFilePairs(t *testing.T) {
	write := func(t *testing.T, body string) string {
		t.Helper()
		file := filepath.Join(t.TempDir(), "conf")
		if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return file
	}

	t.Run("normalized key, raw original, last value wins, file order", func(t *testing.T) {
		file := write(t, "# note\npanes=2\npanes=3\nlane.build.roles = implementer # trailing\n")
		pairs, err := ReadConfigFilePairs(file)
		if err != nil {
			t.Fatal(err)
		}
		want := []ConfigPair{
			{Key: "panes", Original: "panes", Value: "3"},
			{Key: "lane_build_roles", Original: "lane.build.roles", Value: "implementer"},
		}
		if !reflect.DeepEqual(pairs, want) {
			t.Fatalf("pairs = %#v", pairs)
		}
	})

	t.Run("bom is whitespace on key and value", func(t *testing.T) {
		file := write(t, "\uFEFFpanes=2\uFEFF\n")
		pairs, err := ReadConfigFilePairs(file)
		if err != nil {
			t.Fatal(err)
		}
		want := []ConfigPair{{Key: "panes", Original: "panes", Value: "2"}}
		if !reflect.DeepEqual(pairs, want) {
			t.Fatalf("pairs = %#v", pairs)
		}
	})

	t.Run("quoted value and crlf", func(t *testing.T) {
		file := write(t, "lanes=\"on\"\r\n")
		pairs, err := ReadConfigFilePairs(file)
		if err != nil {
			t.Fatal(err)
		}
		want := []ConfigPair{{Key: "lanes", Original: "lanes", Value: "on"}}
		if !reflect.DeepEqual(pairs, want) {
			t.Fatalf("pairs = %#v", pairs)
		}
	})

	t.Run("absent file is os.ErrNotExist", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "absent")
		_, err := ReadConfigFilePairs(file)
		if err == nil {
			t.Fatal("err = nil, want os.ErrNotExist")
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("err = %v, want os.ErrNotExist", err)
		}
	})

	t.Run("empty file has no pairs", func(t *testing.T) {
		file := write(t, "# only a comment\n")
		pairs, err := ReadConfigFilePairs(file)
		if err != nil {
			t.Fatal(err)
		}
		if len(pairs) != 0 {
			t.Fatalf("pairs = %#v", pairs)
		}
	})
}
