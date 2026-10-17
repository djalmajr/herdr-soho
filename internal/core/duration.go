package core

import (
	"math"
	"strconv"
)

// ParseTimeoutMS parses a --timeout value that is in milliseconds: an
// unsigned integer (milliseconds, as before) or an integer followed by
// exactly one lower-case unit ms, s, m or h (500ms, 30s, 90m, 2h). It
// refuses decimals, signs, whitespace, upper-case units, combinations such
// as 1h30m, a unit without a number, a number without digits and int64
// overflow. It does not decide whether zero is valid: each command keeps
// its own bound.
func ParseTimeoutMS(raw string) (int64, bool) {
	if raw == "" {
		return 0, false
	}
	digits, factor := raw, int64(1)
	if len(raw) >= 2 && raw[len(raw)-2:] == "ms" {
		digits = raw[:len(raw)-2]
	} else {
		switch raw[len(raw)-1] {
		case 's':
			digits = raw[:len(raw)-1]
			factor = 1000
		case 'm':
			digits = raw[:len(raw)-1]
			factor = 60000
		case 'h':
			digits = raw[:len(raw)-1]
			factor = 3600000
		}
	}
	if !isAllDigits(digits) {
		return 0, false
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return 0, false
	}
	if n > 0 && n > math.MaxInt64/factor {
		return 0, false
	}
	return n * factor, true
}

// FormatTimeoutMS renders a millisecond count in the largest unit that
// divides it exactly, in the order h, m, s, and ms when none divides:
// 10800000 -> 3h, 5400000 -> 90m, 10800 -> 10800ms, 0 -> 0ms.
func FormatTimeoutMS(ms int64) string {
	for _, unit := range []struct {
		divisor int64
		suffix  string
	}{{3600000, "h"}, {60000, "m"}, {1000, "s"}} {
		if ms > 0 && ms%unit.divisor == 0 {
			return strconv.FormatInt(ms/unit.divisor, 10) + unit.suffix
		}
	}
	return strconv.FormatInt(ms, 10) + "ms"
}

// IsPlainTimeoutMS reports whether raw is a plain unsigned integer of
// milliseconds with no unit. The short-timeout warning (WarnShortTimeout)
// only applies to such values: a unit names the intent, so 500ms is never
// a seconds-typed-where-milliseconds-are mistake.
func IsPlainTimeoutMS(raw string) bool {
	if raw == "" {
		return false
	}
	return isAllDigits(raw)
}

func isAllDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}
