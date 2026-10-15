// contract_limpeza_test.go pins the limpeza block of the contract's
// report.json example (docs/job-contract.md) to the fields the report
// actually carries: every key the example shows must be a json tag of the
// Limpeza type and a key of the limpeza object the real report write
// produces, and every key the type always carries (no omitempty) must
// appear in the example, so the example cannot drift back to a field the
// report does not produce (the panes_fechados of the old example).
package job

import (
	"encoding/json"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// contractDoc is the job contract the report.json example lives in,
// relative to the test's working directory (internal/job).
const contractDoc = "../../docs/job-contract.md"

// jsonFencedBlocks returns the body of every fenced code block tagged
// json, in document order.
func jsonFencedBlocks(doc string) []string {
	blocks := []string{}
	var body []string
	inFence := false
	for _, line := range strings.Split(doc, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "```") {
			if inFence {
				body = append(body, line)
			}
			continue
		}
		if inFence {
			blocks = append(blocks, strings.Join(body, "\n"))
			inFence = false
			continue
		}
		if strings.HasPrefix(strings.TrimPrefix(trimmed, "```"), "json") {
			inFence = true
			body = nil
		}
	}
	return blocks
}

// limpezaExampleKeys reads the contract, finds the report.json example —
// the only json block whose object carries a limpeza member — and returns
// that member's keys.
func limpezaExampleKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(contractDoc)
	if err != nil {
		t.Fatalf("read %s: %v", contractDoc, err)
	}
	var keys []string
	for _, block := range jsonFencedBlocks(string(raw)) {
		var doc map[string]any
		if err := json.Unmarshal([]byte(block), &doc); err != nil {
			continue // not a JSON object: not the report example
		}
		member, ok := doc["limpeza"].(map[string]any)
		if !ok {
			continue
		}
		if keys != nil {
			t.Fatalf("two report examples carry a limpeza member; the contract must have exactly one")
		}
		keys = make([]string, 0, len(member))
		for k := range member {
			keys = append(keys, k)
		}
	}
	if keys == nil {
		t.Fatalf("no json block with a limpeza member in %s", contractDoc)
	}
	return keys
}

// limpezaTypeKeys returns, from the Limpeza type's json tags, the set of
// all key names and the subset with no omitempty (always present in the
// report).
func limpezaTypeKeys(t *testing.T) (all, always map[string]bool) {
	t.Helper()
	typ := reflect.TypeOf(Limpeza{})
	all = map[string]bool{}
	always = map[string]bool{}
	for i := range typ.NumField() {
		f := typ.Field(i)
		tag := f.Tag.Get("json")
		name, rest, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			t.Fatalf("field %s has no json tag: %q", f.Name, tag)
		}
		all[name] = true
		optional := false
		for _, opt := range strings.Split(rest, ",") {
			if opt == "omitempty" {
				optional = true
			}
		}
		if !optional {
			always[name] = true
		}
	}
	return all, always
}

func sortedBoolKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestContractLimpezaExampleMatchesReport keeps the contract's report.json
// example in sync with the report the supervisor writes: the example's
// limpeza keys must be json tags of the Limpeza type that the real report
// produces, and the type's always-present keys must appear in the example.
// It fails on the old example, whose panes_fechados the report never
// produces (the terminal write records 0 and the tag omits it).
func TestContractLimpezaExampleMatchesReport(t *testing.T) {
	s, _ := startedJob(t)
	// The terminal write the supervisor's finalize hands to WriteReport
	// (supervise_git.go): the workspace outcome recorded, the panes count
	// zero, so panes_fechados' omitempty keeps the key out of the JSON the
	// dispatcher collects.
	rep, err := s.WriteReport("job-1", ReportFacts{
		Workspace:  "closed",
		Clean:      true,
		Head:       "abc123",
		HeadRemoto: "abc123",
	})
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw, err := json.Marshal(rep.Limpeza)
	if err != nil {
		t.Fatalf("marshal limpeza: %v", err)
	}
	var produced map[string]any
	if err := json.Unmarshal(raw, &produced); err != nil {
		t.Fatalf("unmarshal limpeza: %v", err)
	}
	producedSet := make(map[string]bool, len(produced))
	for k := range produced {
		producedSet[k] = true
	}
	typeAll, typeAlways := limpezaTypeKeys(t)
	example := limpezaExampleKeys(t)
	exampleSet := map[string]bool{}
	for _, k := range example {
		if exampleSet[k] {
			t.Fatalf("the contract example lists limpeza key %q twice", k)
		}
		exampleSet[k] = true
	}
	for _, k := range example {
		if !typeAll[k] {
			t.Errorf("the contract example shows limpeza key %q that is not a json tag of the Limpeza type: %v", k, sortedBoolKeys(typeAll))
		}
		if !producedSet[k] {
			t.Errorf("the contract example shows limpeza key %q that the report does not produce: real keys %v", k, sortedBoolKeys(producedSet))
		}
	}
	for k := range typeAlways {
		if !exampleSet[k] {
			t.Errorf("the contract example omits the always-present limpeza key %q", k)
		}
	}
}
