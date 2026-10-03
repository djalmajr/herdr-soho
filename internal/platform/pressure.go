package platform

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Resource pressure measurement for the `spawn`, `wait`, and `status`
// warning. The measurement functions are variables so the tests can fake
// the machine; every measurement failure is silent (ok=false), which means
// no warning and no exit-code change.

// DiskFree reports the user-free bytes and the total bytes of the
// filesystem containing path. A path that does not exist yet walks up to
// the first existing ancestor, so a fresh state dir still measures.
var DiskFree = diskFreeReal

// SwapUsage reports the used and total swap bytes. ok=false when swap is
// not measurable (Windows); a machine without swap answers ok with
// total==0, which must not warn.
var SwapUsage = swapUsageReal

func diskFreeReal(path string) (free, total int64, ok bool) {
	for p := path; ; {
		if free, total, ok := statfsFree(p); ok {
			return free, total, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return 0, 0, false
		}
		p = parent
	}
}

var swapUnit = map[byte]int64{'K': 1024, 'M': 1024 * 1024, 'G': 1024 * 1024 * 1024, 'T': 1024 * 1024 * 1024 * 1024}

var swapSysctlRE = regexp.MustCompile(`total\s*=\s*([0-9]+(?:\.[0-9]+)?)\s*([KMGT])?[^=]*used\s*=\s*([0-9]+(?:\.[0-9]+)?)\s*([KMGT])?`)

var meminfoSwapRE = regexp.MustCompile(`(?m)^Swap(Total|Free):\s+([0-9]+)\s*kB\s*$`)

// parseSwapSysctl parses `sysctl -n vm.swapusage` output, e.g.
// `total = 47104.00M  used = 45517.00M  free = 1587.00M  (encrypted)`.
func parseSwapSysctl(text string) (used, total int64, ok bool) {
	m := swapSysctlRE.FindStringSubmatch(text)
	if m == nil {
		return 0, 0, false
	}
	t, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, 0, false
	}
	u, err := strconv.ParseFloat(m[3], 64)
	if err != nil {
		return 0, 0, false
	}
	return int64(u * float64(swapUnitValue(m[4]))), int64(t * float64(swapUnitValue(m[2]))), true
}

func swapUnitValue(unit string) int64 {
	if unit == "" {
		return 1
	}
	return swapUnit[unit[0]]
}

// parseMeminfoSwap parses the SwapTotal/SwapFree lines of /proc/meminfo
// (kilobytes); used = total - free.
func parseMeminfoSwap(text string) (used, total int64, ok bool) {
	var swapTotal, swapFree int64
	var haveTotal, haveFree bool
	for _, m := range meminfoSwapRE.FindAllStringSubmatch(text, -1) {
		value, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return 0, 0, false
		}
		switch m[1] {
		case "Total":
			swapTotal, haveTotal = value, true
		case "Free":
			swapFree, haveFree = value, true
		}
	}
	if !haveTotal || !haveFree {
		return 0, 0, false
	}
	total = swapTotal * 1024
	return (swapTotal - swapFree) * 1024, total, true
}

// HumanSize renders a byte count in base-1024 units with one decimal,
// trimmed when whole: `512 B`, `900 MB`, `1.6 GB`, `46 GB`.
func HumanSize(bytes int64) string {
	if bytes < 1024 {
		return strconv.FormatInt(bytes, 10) + " B"
	}
	value := float64(bytes)
	index := 0
	for value >= 1024 && index < 4 {
		value /= 1024
		index++
	}
	units := []string{"", "KB", "MB", "GB", "TB"}
	text := strconv.FormatFloat(value, 'f', 1, 64)
	if strings.HasSuffix(text, ".0") {
		text = text[:len(text)-2]
	}
	return text + " " + units[index]
}
