package jsonjs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type undefined struct{}

// Undefined represents JavaScript undefined. It is omitted in objects and encoded as null in arrays.
var Undefined any = undefined{}

// IsUndefined reports whether value is the Undefined sentinel.
func IsUndefined(value any) bool {
	_, ok := value.(undefined)
	return ok
}

// Object retains the order in which ordinary keys are inserted.
type Object struct {
	keys   []string
	values map[string]any
}

// O creates an insertion-ordered object from alternating string keys and values.
func O(pairs ...any) *Object {
	if len(pairs)%2 != 0 {
		panic("jsonjs: O requires alternating keys and values")
	}
	o := &Object{values: make(map[string]any, len(pairs)/2)}
	for i := 0; i < len(pairs); i += 2 {
		key, ok := pairs[i].(string)
		if !ok {
			panic("jsonjs: O keys must be strings")
		}
		o.Set(key, pairs[i+1])
	}
	return o
}

func (o *Object) Set(key string, value any) {
	if o.values == nil {
		o.values = make(map[string]any)
	}
	if _, exists := o.values[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.values[key] = value
}

func (o *Object) Get(key string) (any, bool) {
	if o == nil {
		return nil, false
	}
	v, ok := o.values[key]
	return v, ok
}

func (o *Object) Delete(key string) bool {
	if o == nil {
		return false
	}
	if _, ok := o.values[key]; !ok {
		return false
	}
	delete(o.values, key)
	for i, k := range o.keys {
		if k == key {
			o.keys = append(o.keys[:i], o.keys[i+1:]...)
			break
		}
	}
	return true
}

func (o *Object) Keys() []string {
	if o == nil {
		return nil
	}
	return append([]string(nil), o.keys...)
}

// Stringify encodes v compactly using JavaScript JSON string and number formatting.
func Stringify(v any) string {
	b, defined := appendValue(nil, v, 0, 0, false, false)
	if !defined {
		return ""
	}
	return string(b)
}

// StringifyIndent encodes v using JSON.stringify's numeric indentation width (clamped to 0..10).
func StringifyIndent(v any, indent int) string {
	if indent < 0 {
		indent = 0
	}
	if indent > 10 {
		indent = 10
	}
	b, defined := appendValue(nil, v, 0, indent, indent > 0, false)
	if !defined {
		return ""
	}
	return string(b)
}

func appendValue(dst []byte, value any, depth, width int, pretty, inArray bool) ([]byte, bool) {
	if value == nil {
		return append(dst, "null"...), true
	}
	if _, ok := value.(undefined); ok {
		if inArray {
			return append(dst, "null"...), true
		}
		return dst, false
	}
	switch v := value.(type) {
	case bool:
		return strconv.AppendBool(dst, v), true
	case string:
		return appendString(dst, v), true
	case float64:
		return append(dst, numberString(v)...), true
	case int:
		return append(dst, numberString(float64(v))...), true
	case int8:
		return append(dst, numberString(float64(v))...), true
	case int16:
		return append(dst, numberString(float64(v))...), true
	case int32:
		return append(dst, numberString(float64(v))...), true
	case int64:
		return append(dst, numberString(float64(v))...), true
	case uint:
		return append(dst, numberString(float64(v))...), true
	case uint8:
		return append(dst, numberString(float64(v))...), true
	case uint16:
		return append(dst, numberString(float64(v))...), true
	case uint32:
		return append(dst, numberString(float64(v))...), true
	case uint64:
		return append(dst, numberString(float64(v))...), true
	case []string:
		array := make([]any, len(v))
		for i := range v {
			array[i] = v[i]
		}
		return appendArray(dst, array, depth, width, pretty)
	case []any:
		return appendArray(dst, v, depth, width, pretty)
	case Object:
		return appendObject(dst, &v, depth, width, pretty)
	case *Object:
		if v == nil {
			return append(dst, "null"...), true
		}
		return appendObject(dst, v, depth, width, pretty)
	}
	if reflect.TypeOf(value).Kind() == reflect.Map {
		panic("jsonjs: map output is unsupported; use jsonjs.Object to preserve key order")
	}
	panic(fmt.Sprintf("jsonjs: unsupported value type %T", value))
}

func appendArray(dst []byte, values []any, depth, width int, pretty bool) ([]byte, bool) {
	if len(values) == 0 {
		return append(dst, '[', ']'), true
	}
	dst = append(dst, '[')
	for i, value := range values {
		if i > 0 {
			dst = append(dst, ',')
		}
		if pretty {
			dst = append(dst, '\n')
			dst = appendIndent(dst, (depth+1)*width)
		}
		var defined bool
		dst, defined = appendValue(dst, value, depth+1, width, pretty, true)
		if !defined {
			dst = append(dst, "null"...)
		}
	}
	if pretty {
		dst = append(dst, '\n')
		dst = appendIndent(dst, depth*width)
	}
	return append(dst, ']'), true
}

func appendObject(dst []byte, object *Object, depth, width int, pretty bool) ([]byte, bool) {
	keys := make([]string, 0, len(object.keys))
	for _, key := range jsKeyOrder(object.keys) {
		if _, ok := object.values[key].(undefined); !ok {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return append(dst, '{', '}'), true
	}
	dst = append(dst, '{')
	for i, key := range keys {
		if i > 0 {
			dst = append(dst, ',')
		}
		if pretty {
			dst = append(dst, '\n')
			dst = appendIndent(dst, (depth+1)*width)
		}
		dst = appendString(dst, key)
		dst = append(dst, ':')
		if pretty {
			dst = append(dst, ' ')
		}
		var defined bool
		dst, defined = appendValue(dst, object.values[key], depth+1, width, pretty, false)
		if !defined {
			panic("jsonjs: undefined object value was not omitted")
		}
	}
	if pretty {
		dst = append(dst, '\n')
		dst = appendIndent(dst, depth*width)
	}
	return append(dst, '}'), true
}

func appendIndent(dst []byte, count int) []byte {
	for i := 0; i < count; i++ {
		dst = append(dst, ' ')
	}
	return dst
}

func appendString(dst []byte, value string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(value); {
		r, size := utf8.DecodeRuneInString(value[i:])
		if r == utf8.RuneError && size == 1 && i+2 < len(value) && value[i] == 0xed && value[i+1] >= 0xa0 && value[i+1] <= 0xbf && value[i+2]&0xc0 == 0x80 {
			surrogate := rune(value[i]&0x0f)<<12 | rune(value[i+1]&0x3f)<<6 | rune(value[i+2]&0x3f)
			dst = append(dst, `\u`...)
			dst = appendHex4(dst, surrogate)
			i += 3
			continue
		}
		i += size
		switch r {
		case '"':
			dst = append(dst, `\"`...)
		case '\\':
			dst = append(dst, `\\`...)
		case '\b':
			dst = append(dst, `\b`...)
		case '\f':
			dst = append(dst, `\f`...)
		case '\n':
			dst = append(dst, `\n`...)
		case '\r':
			dst = append(dst, `\r`...)
		case '\t':
			dst = append(dst, `\t`...)
		default:
			if r < 0x20 {
				dst = append(dst, `\u00`...)
				dst = append(dst, "0123456789abcdef"[byte(r)>>4])
				dst = append(dst, "0123456789abcdef"[byte(r)&0xf])
			} else {
				dst = append(dst, string(r)...)
			}
		}
	}
	return append(dst, '"')
}

func appendHex4(dst []byte, value rune) []byte {
	const digits = "0123456789abcdef"
	return append(dst, digits[(value>>12)&0xf], digits[(value>>8)&0xf], digits[(value>>4)&0xf], digits[value&0xf])
}

func numberString(value float64) string {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return "null"
	}
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	scientific := strconv.FormatFloat(value, 'e', -1, 64)
	parts := strings.SplitN(scientific, "e", 2)
	exponent, _ := strconv.Atoi(parts[1])
	mantissa := strings.ReplaceAll(parts[0], ".", "")
	if exponent >= -6 && exponent < 21 {
		decimalAt := exponent + 1
		var plain string
		switch {
		case decimalAt <= 0:
			plain = "0." + strings.Repeat("0", -decimalAt) + mantissa
		case decimalAt >= len(mantissa):
			plain = mantissa + strings.Repeat("0", decimalAt-len(mantissa))
		default:
			plain = mantissa[:decimalAt] + "." + mantissa[decimalAt:]
		}
		if negative {
			return "-" + plain
		}
		return plain
	}
	if negative {
		parts[0] = "-" + parts[0]
	}
	sign := ""
	if exponent >= 0 {
		sign = "+"
	}
	return parts[0] + "e" + sign + strconv.Itoa(exponent)
}

// Parse parses JSON into ordered Objects, []any arrays, float64 numbers, and JSON scalar values.
func Parse(data []byte) (any, error) {
	prepared, surrogates, err := preserveLoneSurrogates(data)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(prepared))
	decoder.UseNumber()
	value, err := parseValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("jsonjs: trailing JSON data")
		}
		return nil, err
	}
	return restoreSurrogates(value, surrogates), nil
}

func preserveLoneSurrogates(data []byte) ([]byte, map[string]string, error) {
	lower := strings.ToLower(string(data))
	markers := make(map[uint16]string)
	replacements := make(map[string]string)
	marker := uint16(0xe000)
	var out []byte
	inString := false
	for i := 0; i < len(data); {
		if !inString {
			out = append(out, data[i])
			if data[i] == '"' {
				inString = true
			}
			i++
			continue
		}
		if data[i] == '"' {
			out = append(out, data[i])
			inString = false
			i++
			continue
		}
		if data[i] != '\\' {
			out = append(out, data[i])
			i++
			continue
		}
		if i+1 >= len(data) {
			out = append(out, data[i])
			i++
			continue
		}
		if data[i+1] != 'u' || i+6 > len(data) {
			out = append(out, data[i:i+2]...)
			i += 2
			continue
		}
		unitValue, err := strconv.ParseUint(string(data[i+2:i+6]), 16, 16)
		if err != nil {
			out = append(out, data[i:i+6]...)
			i += 6
			continue
		}
		unit := uint16(unitValue)
		if 0xd800 <= unit && unit <= 0xdbff && i+12 <= len(data) && data[i+6] == '\\' && data[i+7] == 'u' {
			lowValue, lowErr := strconv.ParseUint(string(data[i+8:i+12]), 16, 16)
			if lowErr == nil && 0xdc00 <= lowValue && lowValue <= 0xdfff {
				out = append(out, data[i:i+12]...)
				i += 12
				continue
			}
		}
		if 0xd800 <= unit && unit <= 0xdfff {
			markerText, exists := markers[unit]
			if !exists {
				for ; marker <= 0xf8ff; marker++ {
					candidate := fmt.Sprintf("\\u%04x", marker)
					literal := string(rune(marker))
					if !strings.Contains(lower, strings.ToLower(candidate)) && !strings.Contains(string(data), literal) {
						break
					}
				}
				if marker > 0xf8ff {
					return nil, nil, fmt.Errorf("jsonjs: could not reserve a surrogate placeholder")
				}
				markerText = string(rune(marker))
				markers[unit] = markerText
				w := uint16(unit)
				replacements[markerText] = string([]byte{0xe0 | byte(w>>12), 0x80 | byte((w>>6)&0x3f), 0x80 | byte(w&0x3f)})
				marker++
			}
			out = append(out, []byte(fmt.Sprintf("\\u%04x", []rune(markerText)[0]))...)
			i += 6
			continue
		}
		out = append(out, data[i:i+6]...)
		i += 6
	}
	return out, replacements, nil
}

func restoreSurrogates(value any, replacements map[string]string) any {
	switch item := value.(type) {
	case string:
		for marker, surrogate := range replacements {
			item = strings.ReplaceAll(item, marker, surrogate)
		}
		return item
	case []any:
		for i := range item {
			item[i] = restoreSurrogates(item[i], replacements)
		}
		return item
	case *Object:
		out := O()
		for _, key := range item.keys {
			out.Set(restoreSurrogates(key, replacements).(string), restoreSurrogates(item.values[key], replacements))
		}
		return out
	default:
		return value
	}
}

func parseValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := O()
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("jsonjs: object key is not a string")
				}
				item, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				object.Set(key, item)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			object.keys = jsKeyOrder(object.keys)
			return object, nil
		case '[':
			array := []any{}
			for decoder.More() {
				item, err := parseValue(decoder)
				if err != nil {
					return nil, err
				}
				array = append(array, item)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, err
			}
			return array, nil
		}
	case json.Number:
		parsed, err := strconv.ParseFloat(string(value), 64)
		if numberError, ok := err.(*strconv.NumError); ok && numberError.Err == strconv.ErrRange {
			return parsed, nil
		}
		return parsed, err
	case string, bool, nil:
		return value, nil
	}
	return nil, fmt.Errorf("jsonjs: unexpected token %v", token)
}

func jsKeyOrder(keys []string) []string {
	indices := make([]string, 0, len(keys))
	ordinary := make([]string, 0, len(keys))
	for _, key := range keys {
		if isArrayIndex(key) {
			indices = append(indices, key)
		} else {
			ordinary = append(ordinary, key)
		}
	}
	sort.Slice(indices, func(i, j int) bool {
		a, _ := strconv.ParseUint(indices[i], 10, 32)
		b, _ := strconv.ParseUint(indices[j], 10, 32)
		return a < b
	})
	return append(indices, ordinary...)
}

func isArrayIndex(key string) bool {
	if key == "" || (len(key) > 1 && key[0] == '0') {
		return false
	}
	value, err := strconv.ParseUint(key, 10, 32)
	return err == nil && value < math.MaxUint32 && strconv.FormatUint(value, 10) == key
}
