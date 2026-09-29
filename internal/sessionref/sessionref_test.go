package sessionref

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestSessionRefCases(t *testing.T) {
	t.Run("parseRef: a bare pane id is local; a machine prefix is kept", func(t *testing.T) { // JS: "parseRef: a bare pane id is local; a machine prefix is kept"
		for _, input := range []string{"w12:p1", "  windows/w3:p1\n", "local/w14:pR", "hetzner.eu-1/w3:p2", "\u00a0w1:p2\u2028"} {
			if ParseRef(input) == nil {
				t.Errorf("ParseRef(%q) = nil", input)
			}
		}
		if got := ParseRef("w12:p1"); got == nil || *got != (Ref{Machine: LocalMachine, PaneID: "w12:p1"}) {
			t.Fatalf("bare = %#v", got)
		}
	})
	t.Run("parseRef: anything that is not a reference is null", func(t *testing.T) { // JS: "parseRef: anything that is not a reference is null"
		for _, bad := range []any{"", "soho-s1", "w12", "w12:", ":p1", "p1", "w1:p", "w12:p1:extra", "a/b/w12:p1", "/w12:p1", "win dows/w3:p1", "-x/w3:p1", "windows/", "C:/Users/x", "w1:p1\rx", "w1:p1\x1b", "w1:p1😀", nil, 42} {
			if got := ParseRef(bad); got != nil {
				t.Errorf("ParseRef(%#v) = %#v", bad, got)
			}
		}
	})
	t.Run("formatRef: always names the machine; round-trips with parseRef", func(t *testing.T) { // JS: "formatRef: always names the machine; round-trips with parseRef"
		for _, row := range []struct{ machine, pane, want string }{{"windows", "w3:p1", "windows/w3:p1"}, {"", "w12:p1", "local/w12:p1"}} {
			if got := FormatRef(Ref{Machine: row.machine, PaneID: row.pane}); got != row.want {
				t.Errorf("FormatRef() = %q", got)
			}
		}
		for _, ref := range []string{"local/w12:p1", "windows/w3:p1"} {
			parsed := ParseRef(ref)
			if parsed == nil || FormatRef(*parsed) != ref {
				t.Errorf("round trip %q: %#v", ref, parsed)
			}
		}
	})
	t.Run("herdrMachineArgs: nothing for the local server, --machine otherwise", func(t *testing.T) { // JS: "herdrMachineArgs: nothing for the local server, --machine otherwise"
		for _, row := range []struct {
			machine string
			want    []string
		}{{LocalMachine, []string{}}, {"", []string{}}, {"windows", []string{"--machine", "windows"}}} {
			if got := HerdrMachineArgs(row.machine); !reflect.DeepEqual(got, row.want) {
				t.Errorf("HerdrMachineArgs(%q) = %#v", row.machine, got)
			}
		}
	})
}

func TestSessionRefDifferential(t *testing.T) {
	data, err := os.ReadFile("testdata/sessionref.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		Kind     string          `json:"kind"`
		Input    any             `json:"input"`
		Expected json.RawMessage `json:"expected"`
		Machine  string          `json:"machine"`
		Pane     string          `json:"pane"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	for i, row := range rows {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			if row.Kind == "" {
				t.Fatalf("sessionref.json row %d has empty kind", i)
			}
			switch row.Kind {
			case "parse":
				got := ParseRef(row.Input)
				var want *Ref
				if err := json.Unmarshal(row.Expected, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("ParseRef(%#v) = %#v, JS %s", row.Input, got, row.Expected)
				}
			case "format":
				var want string
				if err := json.Unmarshal(row.Expected, &want); err != nil {
					t.Fatal(err)
				}
				if got := FormatRef(Ref{Machine: row.Machine, PaneID: row.Pane}); got != want {
					t.Fatalf("FormatRef() = %q, JS %q", got, want)
				}
			case "args":
				got := HerdrMachineArgs(row.Machine)
				var want []string
				if err := json.Unmarshal(row.Expected, &want); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("HerdrMachineArgs() = %#v, JS %s", got, row.Expected)
				}
			default:
				t.Fatalf("sessionref.json row %d has unknown kind %q", i, row.Kind)
			}
		})
	}
}
