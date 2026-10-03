package platform

import (
	"path/filepath"
	"testing"
)

func TestParseSwapSysctl(t *testing.T) {
	t.Run("the example line reads 96 percent used", func(t *testing.T) {
		used, total, ok := parseSwapSysctl("total = 47104.00M  used = 45517.00M  free = 1587.00M  (encrypted)\n")
		if !ok {
			t.Fatal("parse failed")
		}
		wantTotal := int64(47104) * 1024 * 1024
		wantUsed := int64(45517) * 1024 * 1024
		if total != wantTotal || used != wantUsed {
			t.Fatalf("total=%d used=%d, want %d/%d", total, used, wantTotal, wantUsed)
		}
		if pct := used * 100 / total; pct != 96 {
			t.Fatalf("used percent=%d, want 96", pct)
		}
	})

	t.Run("gigabyte units", func(t *testing.T) {
		used, total, ok := parseSwapSysctl("total = 2.00G  used = 1.50G  free = 0.50G  (encrypted)")
		if !ok {
			t.Fatal("parse failed")
		}
		if total != int64(2)*1024*1024*1024 || used != int64(1536)*1024*1024 {
			t.Fatalf("total=%d used=%d", total, used)
		}
	})

	t.Run("a zero-total machine parses as ok with no swap", func(t *testing.T) {
		used, total, ok := parseSwapSysctl("total = 0.00M  used = 0.00M  free = 0.00M")
		if !ok || used != 0 || total != 0 {
			t.Fatalf("ok=%v used=%d total=%d, want ok 0/0", ok, used, total)
		}
	})

	t.Run("a missing used field fails", func(t *testing.T) {
		if _, _, ok := parseSwapSysctl("total = 47104.00M  free = 1587.00M  (encrypted)"); ok {
			t.Fatal("parsed without a used field")
		}
		if _, _, ok := parseSwapSysctl(""); ok {
			t.Fatal("parsed the empty output")
		}
	})
}

func TestParseMeminfoSwap(t *testing.T) {
	t.Run("the sample file reads 96 percent used", func(t *testing.T) {
		text := "MemTotal:       16384000 kB\n" +
			"MemFree:         4096000 kB\n" +
			"Buffers:           51200 kB\n" +
			"SwapTotal:        4096000 kB\n" +
			"SwapFree:          163840 kB\n"
		used, total, ok := parseMeminfoSwap(text)
		if !ok {
			t.Fatal("parse failed")
		}
		wantTotal := int64(4096000) * 1024
		wantUsed := int64(4096000-163840) * 1024
		if total != wantTotal || used != wantUsed {
			t.Fatalf("total=%d used=%d, want %d/%d", total, used, wantTotal, wantUsed)
		}
		if pct := used * 100 / total; pct != 96 {
			t.Fatalf("used percent=%d, want 96", pct)
		}
	})

	t.Run("a zero-total meminfo parses as ok with no swap", func(t *testing.T) {
		used, total, ok := parseMeminfoSwap("SwapTotal:              0 kB\nSwapFree:              0 kB\n")
		if !ok || used != 0 || total != 0 {
			t.Fatalf("ok=%v used=%d total=%d, want ok 0/0", ok, used, total)
		}
	})

	t.Run("a missing SwapTotal fails", func(t *testing.T) {
		if _, _, ok := parseMeminfoSwap("MemTotal:       16384000 kB\nSwapFree:              0 kB\n"); ok {
			t.Fatal("parsed without SwapTotal")
		}
	})
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1 KB"},
		{900 * 1024 * 1024, "900 MB"},
		{1024 * 1024 * 1024, "1 GB"},
		{10 * 1024 * 1024 * 1024, "10 GB"},
		{1677721600, "1.6 GB"},
		{int64(47104) * 1024 * 1024, "46 GB"},
		{int64(45517) * 1024 * 1024, "44.5 GB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.bytes); got != c.want {
			t.Errorf("HumanSize(%d)=%q, want %q", c.bytes, got, c.want)
		}
	}
}

func TestDiskFreeMeasuresExistingAndWalksUp(t *testing.T) {
	tmp := t.TempDir()
	free, total, ok := DiskFree(tmp)
	if !ok || total <= 0 || free < 0 {
		t.Fatalf("DiskFree(%s): ok=%v free=%d total=%d", tmp, ok, free, total)
	}
	// A real volume holds data, so the free space is below the total. With
	// the Windows outputs out of the API order both read as free space.
	if free >= total {
		t.Fatalf("DiskFree(%s): free=%d is not below total=%d", tmp, free, total)
	}
	// A state dir that does not exist yet still measures through its
	// existing ancestor.
	missing := filepath.Join(tmp, "state", "ws")
	if _, _, ok := DiskFree(missing); !ok {
		t.Fatalf("DiskFree(%s) did not walk up to %s", missing, tmp)
	}
}
