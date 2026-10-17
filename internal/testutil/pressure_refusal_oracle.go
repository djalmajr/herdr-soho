// Oracle for the DJA-392 pressure refusal keys over the frozen legacy
// parity goldens.
//
// The frozen goldens (internal/testdata/legacy: parity-config.json) predate
// the two pressure refusal keys: their expected `soho config` output lists
// the scalar keys ending at pressure_swap_percent. The implementation now
// lists pressure_refuse_disk_free_percent and pressure_refuse_swap_percent
// right after it, so this exact-string adaptation inserts the two new
// listing lines after the frozen anchor line in every expected output.
// Every other frozen expectation passes through byte-identical.
package testutil

import "strings"

// pressureRefusalAnchor is the exact frozen config listing line the two new
// lines follow in every expected output (key padded to 18, value padded to
// 30, single-space columns). Frozen as inert test data.
const pressureRefusalAnchor = "pressure_swap_percent 80                             defaults"

// pressureRefusalInsert is the anchor plus the two exact listing lines the
// current `soho config` prints right after it. If the row format or the
// defaults drift, the parity comparison fails on purpose.
const pressureRefusalInsert = pressureRefusalAnchor + "\n" +
	"pressure_refuse_disk_free_percent 5                              defaults\n" +
	"pressure_refuse_swap_percent 95                             defaults"

// DJA-392 intentional contract adaptation: soho config lists the two
// pressure refusal keys after pressure_swap_percent; the frozen bytes are
// unchanged.
//
// RewritePressureRefusal applies the adaptation to one string: after every
// exact occurrence of the frozen anchor line it inserts the two new
// listing lines. A string without the anchor passes through byte-identical,
// so an expected output the adaptation was meant for fails the comparison
// instead of being silently adapted.
func RewritePressureRefusal(s string) string {
	return strings.ReplaceAll(s, pressureRefusalAnchor, pressureRefusalInsert)
}

// ApplyPressureRefusalOracle recursively walks a decoded JSON value tree
// (objects, arrays and scalar strings) and rewrites every string through
// RewritePressureRefusal. Non-string values and strings without the anchor
// pass through unchanged.
func ApplyPressureRefusalOracle(value any) any {
	switch v := value.(type) {
	case map[string]any:
		for key, item := range v {
			v[key] = ApplyPressureRefusalOracle(item)
		}
		return v
	case []any:
		for i := range v {
			v[i] = ApplyPressureRefusalOracle(v[i])
		}
		return v
	case string:
		return RewritePressureRefusal(v)
	default:
		return value
	}
}
