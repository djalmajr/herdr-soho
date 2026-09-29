package text

import (
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

var jsDecimalNumberRE = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// ParseJSNumber applies the string-to-number forms accepted by JavaScript Number.
func ParseJSNumber(value string) (float64, bool) {
	value = trimJSWhitespace(value)
	if value == "" {
		return 0, true
	}
	switch value {
	case "Infinity", "+Infinity":
		return math.Inf(1), true
	case "-Infinity":
		return math.Inf(-1), true
	case "NaN", "+NaN", "-NaN":
		return math.NaN(), true
	}
	for _, prefix := range []struct {
		prefix string
		base   int
	}{{"0x", 16}, {"0X", 16}, {"0b", 2}, {"0B", 2}, {"0o", 8}, {"0O", 8}} {
		if strings.HasPrefix(value, prefix.prefix) {
			digits := value[2:]
			if digits == "" {
				return 0, false
			}
			integer, ok := new(big.Int).SetString(digits, prefix.base)
			if !ok {
				return 0, false
			}
			number, _ := new(big.Float).SetInt(integer).Float64()
			return number, true
		}
	}
	if !jsDecimalNumberRE.MatchString(value) {
		return 0, false
	}
	number, err := strconv.ParseFloat(value, 64)
	return number, err == nil
}

func trimJSWhitespace(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		return r == 0x0009 || r == 0x000a || r == 0x000b || r == 0x000c || r == 0x000d || r == 0x0020 || r == 0x00a0 || r == 0x1680 || r >= 0x2000 && r <= 0x200a || r == 0x2028 || r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000 || r == 0xfeff
	})
}
