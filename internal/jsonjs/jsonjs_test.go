package jsonjs

import (
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestJSONJSObjectContract(t *testing.T) {
	t.Run("Object preserves insertion order while stringify follows JavaScript index-key order", func(t *testing.T) { // Mutation captured: emitting insertion order for integer property names diverges from JSON.stringify.
		o := O("later", 1, "10", "ten", "2", "two", "first", true)
		if got, want := o.Keys(), []string{"later", "10", "2", "first"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("Keys() = %#v", got)
		}
		if got, want := Stringify(o), `{"2":"two","10":"ten","later":1,"first":true}`; got != want {
			t.Fatalf("Stringify() = %s", got)
		}
		o.Set("later", 2)
		o.Delete("10")
		o.Set("10", "again")
		if got, want := o.Keys(), []string{"later", "2", "first", "10"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("keys after mutation = %#v", got)
		}
		if value, ok := o.Get("later"); !ok || value != 2 {
			t.Fatalf("Get = %#v, %t", value, ok)
		}
	})
	t.Run("Undefined is omitted from objects and null in arrays", func(t *testing.T) { // Mutation captured: keeping undefined properties or emitting undefined array values changes JSON.stringify output.
		if got := Stringify(O("a", Undefined, "b", []any{Undefined})); got != `{"b":[null]}` {
			t.Fatalf("Stringify() = %s", got)
		}
		if got := Stringify(Undefined); got != "" {
			t.Fatalf("top-level undefined = %q", got)
		}
	})
	t.Run("strings escape only the JavaScript JSON controls", func(t *testing.T) { // Mutation captured: HTML escaping or uppercase control hex changes the byte output.
		input := "\"\\\b\f\n\r\t\x00\x1f <>&\u2028\u2029😀"
		want := `"\"\\\b\f\n\r\t\u0000\u001f <>&` + "\u2028\u2029😀" + `"`
		if got := Stringify(input); got != want {
			t.Fatalf("Stringify() = %q, want %q", got, want)
		}
		if got := Stringify(string([]byte{0xed, 0xa0, 0x80})); got != `"\ud800"` {
			t.Fatalf("Stringify(lone surrogate) = %q", got)
		}
	})
	t.Run("numbers use JavaScript thresholds and special values", func(t *testing.T) { // Mutation captured: Go exponent thresholds or negative-zero rendering differ from JSON.stringify.
		cases := []struct {
			value float64
			want  string
		}{{-0.0, "0"}, {1e21, "1e+21"}, {1e20, "100000000000000000000"}, {1e-7, "1e-7"}, {1e-6, "0.000001"}, {math.Inf(1), "null"}, {math.NaN(), "null"}}
		for _, c := range cases {
			if got := Stringify(c.value); got != c.want {
				t.Errorf("Stringify(%v)=%s want %s", c.value, got, c.want)
			}
		}
	})
	t.Run("maps fail with a clear programming error", func(t *testing.T) { // Mutation captured: silently serializing unordered maps loses the output-order contract.
		defer func() {
			if value := recover(); value == nil {
				t.Fatal("Stringify(map) did not panic")
			}
		}()
		Stringify(map[string]int{"x": 1})
	})
	t.Run("Parse returns ordered objects, float numbers and arrays", func(t *testing.T) { // Mutation captured: decoding into a map or retaining numeric lexemes breaks JavaScript ordering and number behavior.
		v, err := Parse([]byte(`{"b":1,"10":2,"2":3,"a":[true,null,1.5]}`))
		if err != nil {
			t.Fatal(err)
		}
		o, ok := v.(*Object)
		if !ok {
			t.Fatalf("Parse() = %T", v)
		}
		if got, want := o.Keys(), []string{"2", "10", "b", "a"}; !reflect.DeepEqual(got, want) {
			t.Fatalf("Keys() = %#v", got)
		}
		if got := Stringify(v); got != `{"2":3,"10":2,"b":1,"a":[true,null,1.5]}` {
			t.Fatalf("round trip = %s", got)
		}
		for _, test := range []struct{ raw, want string }{{`"\ud800"`, `"\ud800"`}, {`"\udc00"`, `"\udc00"`}, {`"\ud83d\ude00"`, `"😀"`}} {
			parsed, err := Parse([]byte(test.raw))
			if err != nil {
				t.Fatal(err)
			}
			if got := Stringify(parsed); got != test.want {
				t.Errorf("surrogate Parse/Stringify(%s) = %s, want %s", test.raw, got, test.want)
			}
		}
		if _, err := Parse([]byte(`{} garbage`)); err == nil {
			t.Fatal("trailing data accepted")
		}
	})
}

func TestJSONJSDifferential(t *testing.T) {
	type row struct {
		Kind    string          `json:"kind"`
		Value   json.RawMessage `json:"value"`
		Source  string          `json:"source"`
		Compact string          `json:"compact"`
		Indent  string          `json:"indent"`
		Keys    []string        `json:"keys"`
	}
	data, err := os.ReadFile("testdata/jsonjs.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []row
	if err = json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, r := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if r.Kind == "" {
				t.Fatalf("jsonjs.json row %d has empty kind", i)
			}
			switch r.Kind {
			case "stringify":
				var d struct {
					Type    string            `json:"type"`
					Special string            `json:"special"`
					S       string            `json:"s"`
					N       float64           `json:"n"`
					Values  []json.RawMessage `json:"values"`
					Pairs   []json.RawMessage `json:"pairs"`
				}
				if err := json.Unmarshal(r.Value, &d); err != nil {
					t.Fatal(err)
				}
				var v any
				switch d.Type {
				case "string":
					v = d.S
				case "number":
					if d.Special == "NaN" {
						v = math.NaN()
					} else if d.Special == "Infinity" {
						v = math.Inf(1)
					} else {
						v = d.N
					}
				case "undefined":
					v = Undefined
				case "array":
					var values []any
					for _, raw := range d.Values {
						var x any
						if string(raw) == `"__UNDEFINED__"` {
							x = Undefined
						} else if err := json.Unmarshal(raw, &x); err != nil {
							t.Fatal(err)
						}
						values = append(values, x)
					}
					v = values
				case "object":
					pairs := []any{}
					for _, raw := range d.Pairs {
						var p []json.RawMessage
						if err := json.Unmarshal(raw, &p); err != nil {
							t.Fatal(err)
						}
						var k string
						var x any
						if len(p) != 2 {
							t.Fatalf("object pair has %d values", len(p))
						}
						if err := json.Unmarshal(p[0], &k); err != nil {
							t.Fatal(err)
						}
						if string(p[1]) == `"__UNDEFINED__"` {
							x = Undefined
						} else {
							if err := json.Unmarshal(p[1], &x); err != nil {
								t.Fatal(err)
							}
						}
						pairs = append(pairs, k, x)
					}
					v = O(pairs...)
				default:
					t.Fatalf("jsonjs stringify row %d has unknown value type %q", i, d.Type)
				}
				if got := Stringify(v); got != r.Compact {
					t.Fatalf("Stringify() = %q JS=%q", got, r.Compact)
				}
				if got := StringifyIndent(v, 2); got != r.Indent {
					t.Fatalf("StringifyIndent() = %q JS=%q", got, r.Indent)
				}
			case "parse":
				v, err := Parse([]byte(r.Source))
				if err != nil {
					t.Fatal(err)
				}
				if got := Stringify(v); got != r.Compact {
					t.Fatalf("Parse/Stringify = %q JS=%q", got, r.Compact)
				}
				if o, ok := v.(*Object); ok && !reflect.DeepEqual(o.Keys(), r.Keys) {
					t.Fatalf("keys=%#v JS=%#v", o.Keys(), r.Keys)
				}
			default:
				t.Fatalf("jsonjs.json row %d has unknown kind %q", i, r.Kind)
			}
		})
	}
}

func TestStringifyIndentClampsAtTen(t *testing.T) {
	value := O("outer", O("inner", 1))
	if got, want := StringifyIndent(value, 11), StringifyIndent(value, 10); got != want {
		t.Fatalf("indent 11 differs from JS clamp at 10:\n%s\nwant:\n%s", got, want)
	}
}
