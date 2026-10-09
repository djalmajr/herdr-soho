// report_fields_test.go proves that report.json carries every field of the
// contract report example, in the contract key order: the full and the empty
// report shapes, the report.md items/parciais/artefato parsing, the open
// questions after the last unblocked, the publico block, and the
// refreshReport round-trip that keeps the stored fields.
package job

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/djalmajr/herdr-soho/internal/reportscan"
)

// reportFieldOrder is the top-level key order of the contract report
// example: the schema through the publico block.
var reportFieldOrder = []string{
	"schema", "id", "status", "motivo", "resumo", "maquina", "repo", "base",
	"branch", "head", "head_remoto", "sincronizado", "commits", "pr",
	"itens", "parciais", "testes", "revisao", "artefatos", "blockers",
	"perguntas", "memoria", "equipe", "eventos", "custo", "logs",
	"limpeza", "publico",
}

// topKeys returns the top-level key order of one JSON object, as written.
func topKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("report is not a JSON object: %v %v", tok, err)
	}
	keys := []string{}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("key token: %v", err)
		}
		key, _ := tok.(string)
		keys = append(keys, key)
		skipValue(t, dec)
	}
	return keys
}

// skipValue consumes one JSON value so the next key can be read.
func skipValue(t *testing.T, dec *json.Decoder) {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("value token: %v", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok || (delim != '{' && delim != '[') {
		return
	}
	depth := 1
	for depth > 0 {
		tok, err := dec.Token()
		if err != nil {
			t.Fatalf("nested token: %v", err)
		}
		if nested, ok := tok.(json.Delim); ok {
			switch nested {
			case '{', '[':
				depth++
			case '}', ']':
				depth--
			}
		}
	}
}

func writeReportMD(t *testing.T, root, md string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, "jobs", "job-1", "report.md"), []byte(md), 0o600); err != nil {
		t.Fatal(err)
	}
}

func storedReport(t *testing.T, root string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "jobs", "job-1", "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func fullReportFacts() ReportFacts {
	return ReportFacts{
		Clean:         true,
		Head:          "abc123",
		HeadRemoto:    "abc123",
		Resumo:        "full",
		Commits:       []Commit{{SHA: "abc123", Titulo: "feat: thing", Push: "ok"}},
		PR:            &PullRequest{Numero: 12, URL: "https://github.com/example-org/example-repo/pull/12", Estado: "draft"},
		Testes:        []Teste{{Cmd: "go test ./internal/job", Resultado: "pass"}},
		Revisao:       &Revisao{Verdict: "pass", Findings: 1, Severity: "P0 0, P1 0, P2 1, P3 0"},
		Blockers:      []string{"quota"},
		Equipe:        &Equipe{Fonte: "teams/example-org/example-repo.conf", OverrideBrief: []string{"lane.review.effort"}},
		Custo:         &Custo{Inicio: "2026-01-01T10:00:00-03:00", Fim: "2026-01-01T10:45:10-03:00", DuracaoS: 2710, Workers: 3},
		Logs:          &Logs{ReportMD: "/abs/jobs/job-1/report.md", StateDir: "/abs/jobs/job-1", Friction: "/abs/jobs/job-1/friction.jsonl"},
		Workspace:     "closed",
		PanesFechados: 4,
	}
}

func TestReportFieldsContractOrder(t *testing.T) {
	// The contract order includes the maquina and base keys, which the
	// existing omitempty tags drop when empty: the job brief carries both.
	root := t.TempDir()
	s := Open(root)
	brief := `{"schema":1,"id":"job-1","origem":{"tipo":"card","ref":"CARD-1"},"repo":"example-org/example-repo","base":"main","maquina":"machine-a","objetivo":"Ship the change.","aceite":[{"criterio":"tests pass","prova":"go test ./internal/job"}]}`
	if _, err := s.Start("job-1", []byte(brief)); err != nil {
		t.Fatal(err)
	}
	writeReportMD(t, root, "# report\n\n- [done] first item\n- [partial] second item\n")
	rep, err := s.WriteReport("job-1", fullReportFacts())
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw := storedReport(t, root)
	if keys := topKeys(t, raw); !reflect.DeepEqual(keys, reportFieldOrder) {
		t.Fatalf("top-level keys = %v, want the contract order\n%v", keys, reportFieldOrder)
	}
	// The new fields carry the facts.
	if len(rep.Commits) != 1 || rep.Commits[0].SHA != "abc123" || rep.Commits[0].Titulo != "feat: thing" || rep.Commits[0].Push != "ok" {
		t.Fatalf("commits = %+v", rep.Commits)
	}
	if rep.PR == nil || rep.PR.Numero != 12 || rep.PR.URL != "https://github.com/example-org/example-repo/pull/12" || rep.PR.Estado != "draft" {
		t.Fatalf("pr = %+v", rep.PR)
	}
	if len(rep.Itens) != 2 || rep.Itens[0].N != 1 || rep.Itens[0].Item != "[done] first item" || rep.Itens[0].Estado != "done" {
		t.Fatalf("itens = %+v", rep.Itens)
	}
	if rep.Parciais != 1 {
		t.Fatalf("parciais = %d, want 1", rep.Parciais)
	}
	if len(rep.Testes) != 1 || rep.Testes[0].Cmd != "go test ./internal/job" || rep.Testes[0].Resultado != "pass" {
		t.Fatalf("testes = %+v", rep.Testes)
	}
	if rep.Revisao == nil || rep.Revisao.Verdict != "pass" || rep.Revisao.Findings != 1 || rep.Revisao.Severity != "P0 0, P1 0, P2 1, P3 0" {
		t.Fatalf("revisao = %+v", rep.Revisao)
	}
	if len(rep.Blockers) != 1 || rep.Blockers[0] != "quota" {
		t.Fatalf("blockers = %+v", rep.Blockers)
	}
	if rep.Equipe == nil || rep.Equipe.Fonte != "teams/example-org/example-repo.conf" || len(rep.Equipe.OverrideBrief) != 1 || rep.Equipe.OverrideBrief[0] != "lane.review.effort" {
		t.Fatalf("equipe = %+v", rep.Equipe)
	}
	if rep.Custo == nil || rep.Custo.DuracaoS != 2710 || rep.Custo.Workers != 3 {
		t.Fatalf("custo = %+v", rep.Custo)
	}
	if rep.Logs == nil || rep.Logs.ReportMD != "/abs/jobs/job-1/report.md" {
		t.Fatalf("logs = %+v", rep.Logs)
	}
	if rep.Limpeza.Workspace != "closed" || rep.Limpeza.PanesFechados != 4 || rep.Limpeza.Worktree != "kept" || !rep.Limpeza.Removivel {
		t.Fatalf("limpeza = %+v", rep.Limpeza)
	}
	if rep.Maquina != "machine-a" || rep.Base != "main" {
		t.Fatalf("maquina/base = %q/%q", rep.Maquina, rep.Base)
	}
}

func TestReportFieldsEmpty(t *testing.T) {
	s, root := startedJob(t)
	rep, err := s.WriteReport("job-1", ReportFacts{})
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	raw := storedReport(t, root)
	for _, want := range []string{
		`"commits":[]`, `"itens":[]`, `"testes":[]`, `"artefatos":[]`,
		`"blockers":[]`, `"perguntas":[]`,
		`"revisao":null`, `"equipe":null`, `"custo":null`, `"logs":null`,
		`"pr":null`, `"motivo":"sem commits"`,
	} {
		if !bytes.Contains(raw, []byte(want)) {
			t.Fatalf("report lacks %s: %s", want, raw)
		}
	}
	if rep.Commits == nil || rep.Itens == nil || rep.Testes == nil || rep.Artefatos == nil || rep.Blockers == nil || rep.Perguntas == nil {
		t.Fatalf("empty report has nil slices: %+v", rep)
	}
	if rep.Revisao != nil || rep.Equipe != nil || rep.Custo != nil || rep.Logs != nil || rep.PR != nil {
		t.Fatalf("empty report has non-null pointers: %+v", rep)
	}
}

func TestReportFieldsMarkdown(t *testing.T) {
	t.Run("items are numbered in order with the cleaned text", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, strings.Join([]string{
			"# report",
			"",
			"- [done] alpha",
			"* [partial] beta",
			"1. [done] gamma",
			"| [done] delta | extra",
			"[skipped] epsilon",
			"",
		}, "\n"))
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		want := []Item{
			{N: 1, Item: "[done] alpha", Estado: "done"},
			{N: 2, Item: "[partial] beta", Estado: "partial"},
			{N: 3, Item: "[done] gamma", Estado: "done"},
			{N: 4, Item: "[done] delta | extra", Estado: "done"},
			{N: 5, Item: "[skipped] epsilon", Estado: "skipped"},
		}
		if !reflect.DeepEqual(rep.Itens, want) {
			t.Fatalf("itens = %+v, want %+v", rep.Itens, want)
		}
	})

	t.Run("markers inside fenced blocks are ignored", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, strings.Join([]string{
			"```",
			"[done] in a plain fence",
			"[partial] also fenced",
			"```",
			"```go",
			"[done] tagged fence",
			"```",
			"~~~",
			"[partial] tilde fence",
			"~~~",
			"",
			"[done] outside",
			"",
		}, "\n"))
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		want := []Item{{N: 1, Item: "[done] outside", Estado: "done"}}
		if !reflect.DeepEqual(rep.Itens, want) {
			t.Fatalf("itens = %+v, want %+v", rep.Itens, want)
		}
		if rep.Parciais != 0 {
			t.Fatalf("parciais = %d, want 0 (the fenced markers do not count)", rep.Parciais)
		}
	})

	t.Run("a line with two markers is skipped", func(t *testing.T) {
		s, root := startedJob(t)
		writeReportMD(t, root, strings.Join([]string{
			"[done] then [partial]",
			"* [partial] [done]",
			"- [partial] only partial",
			"- [done] doubled [done]",
			"",
		}, "\n"))
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		want := []Item{{N: 1, Item: "[partial] only partial", Estado: "partial"}}
		if !reflect.DeepEqual(rep.Itens, want) {
			t.Fatalf("itens = %+v, want %+v", rep.Itens, want)
		}
		if rep.Parciais != 2 {
			t.Fatalf("parciais = %d, want 2 (the two-marker lines count once each)", rep.Parciais)
		}
	})

	t.Run("a long item is cut to 280 code points", func(t *testing.T) {
		s, root := startedJob(t)
		long := strings.Repeat("é", 300) // 300 non-ASCII code points
		writeReportMD(t, root, "[done] "+long+"\n")
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if len(rep.Itens) != 1 {
			t.Fatalf("itens = %+v", rep.Itens)
		}
		if got := len([]rune(rep.Itens[0].Item)); got != 280 {
			t.Fatalf("item length = %d code points, want 280", got)
		}
		if !strings.HasPrefix(rep.Itens[0].Item, "[done] ") {
			t.Fatalf("item = %q, want the marker kept at the start", rep.Itens[0].Item)
		}
	})

	t.Run("parciais equals reportscan.PartialCount", func(t *testing.T) {
		s, root := startedJob(t)
		md := "- [done] a\n- [partial] b\n- [partial] c\n\n- [done] d\n"
		writeReportMD(t, root, md)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if want := reportscan.PartialCount(md); rep.Parciais != want || want != 2 {
			t.Fatalf("parciais = %d, want %d", rep.Parciais, want)
		}
	})

	t.Run("artefatos hashes the report.md bytes", func(t *testing.T) {
		s, root := startedJob(t)
		md := "# report\n- [done] x\n"
		writeReportMD(t, root, md)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		sum := sha256.Sum256([]byte(md))
		want := []Artefato{{Path: "report.md", SHA256: hex.EncodeToString(sum[:])}}
		if !reflect.DeepEqual(rep.Artefatos, want) {
			t.Fatalf("artefatos = %+v, want %+v", rep.Artefatos, want)
		}
	})

	t.Run("a missing report.md leaves items, parciais and artefatos empty", func(t *testing.T) {
		s, _ := startedJob(t)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if len(rep.Itens) != 0 || rep.Parciais != 0 || len(rep.Artefatos) != 0 {
			t.Fatalf("no report.md: itens=%+v parciais=%d artefatos=%+v", rep.Itens, rep.Parciais, rep.Artefatos)
		}
	})
}

func TestReportFieldsQuestionsAndPublico(t *testing.T) {
	t.Run("perguntas lists only questions after the last unblocked", func(t *testing.T) {
		s, _ := startedJob(t)
		if _, err := s.Note("job-1", EventIn{Tipo: "question", Resumo: "first question"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Append("job-1", EventIn{Tipo: "unblocked", Resumo: "back to running"}); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Note("job-1", EventIn{Tipo: "question", Resumo: "second question"}); err != nil {
			t.Fatal(err)
		}
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if want := []string{"second question"}; !reflect.DeepEqual(rep.Perguntas, want) {
			t.Fatalf("perguntas = %+v, want %+v", rep.Perguntas, want)
		}
	})

	t.Run("without an unblocked event every question is open", func(t *testing.T) {
		s, _ := startedJob(t)
		for _, resumo := range []string{"q one", "q two"} {
			if _, err := s.Note("job-1", EventIn{Tipo: "question", Resumo: resumo}); err != nil {
				t.Fatal(err)
			}
		}
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if want := []string{"q one", "q two"}; !reflect.DeepEqual(rep.Perguntas, want) {
			t.Fatalf("perguntas = %+v, want %+v", rep.Perguntas, want)
		}
	})

	t.Run("no questions is an empty list", func(t *testing.T) {
		s, root := startedJob(t)
		rep, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if len(rep.Perguntas) != 0 || rep.Perguntas == nil {
			t.Fatalf("perguntas = %+v, want empty non-nil", rep.Perguntas)
		}
		raw := storedReport(t, root)
		if !bytes.Contains(raw, []byte(`"perguntas":[]`)) {
			t.Fatalf("report lacks perguntas []: %s", raw)
		}
	})

	t.Run("publico gains the PR id and follows the review verdict", func(t *testing.T) {
		s, _ := startedJob(t)
		rep, err := s.WriteReport("job-1", fullReportFacts())
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		// briefA carries origem.ref CARD-1; the PR adds PR#12.
		if want := []string{"CARD-1", "PR#12"}; !reflect.DeepEqual(rep.Publico.IDs, want) {
			t.Fatalf("publico ids = %+v, want %+v", rep.Publico.IDs, want)
		}
		if rep.Publico.Verdict != "pass" {
			t.Fatalf("publico verdict = %q, want pass", rep.Publico.Verdict)
		}
		if want := []string{"quota"}; !reflect.DeepEqual(rep.Publico.Blockers, want) {
			t.Fatalf("publico blockers = %+v, want %+v", rep.Publico.Blockers, want)
		}

		// Without a review or a PR the verdict is empty and the ids stop at
		// the origem ref.
		rep2, err := s.WriteReport("job-1", ReportFacts{})
		if err != nil {
			t.Fatalf("WriteReport: %v", err)
		}
		if want := []string{"CARD-1"}; !reflect.DeepEqual(rep2.Publico.IDs, want) {
			t.Fatalf("publico ids = %+v, want %+v", rep2.Publico.IDs, want)
		}
		if rep2.Publico.Verdict != "" || rep2.Publico.Blockers == nil || len(rep2.Publico.Blockers) != 0 {
			t.Fatalf("publico = %+v", rep2.Publico)
		}
	})
}

func TestReportFieldsRefresh(t *testing.T) {
	s, root := startedJob(t)
	md := "# report\n\n- [done] kept item\n- [partial] partial item\n"
	writeReportMD(t, root, md)
	sum := sha256.Sum256([]byte(md))
	facts := fullReportFacts()

	first, err := s.WriteReport("job-1", facts)
	if err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	// A decision the ack will acknowledge, so the watermark moves.
	decision, err := s.Note("job-1", EventIn{Tipo: "decision", Resumo: "local decision", Escopo: "projeto", Motivo: "why"})
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []string{StatusPreparing, StatusRunning, StatusFinishing} {
		if _, err := s.Transition("job-1", status); err != nil {
			t.Fatalf("to %s: %v", status, err)
		}
	}
	if _, err := s.Transition("job-1", StatusDone); err != nil {
		t.Fatalf("to done: %v", err)
	}
	if _, err := s.Ack("job-1", decision.Seq); err != nil {
		t.Fatalf("Ack: %v", err)
	}
	rep, err := readReport(filepath.Join(root, "jobs", "job-1"))
	if err != nil {
		t.Fatalf("readReport: %v", err)
	}
	// Every stored field the refresh does not recompute is kept.
	if !reflect.DeepEqual(rep.Commits, first.Commits) {
		t.Fatalf("commits = %+v, want %+v", rep.Commits, first.Commits)
	}
	if !reflect.DeepEqual(rep.Itens, first.Itens) || rep.Parciais != first.Parciais {
		t.Fatalf("itens/parciais = %+v/%d, want %+v/%d", rep.Itens, rep.Parciais, first.Itens, first.Parciais)
	}
	if !reflect.DeepEqual(rep.Testes, first.Testes) {
		t.Fatalf("testes = %+v, want %+v", rep.Testes, first.Testes)
	}
	if !reflect.DeepEqual(rep.Revisao, first.Revisao) {
		t.Fatalf("revisao = %+v, want %+v", rep.Revisao, first.Revisao)
	}
	if !reflect.DeepEqual(rep.Artefatos, first.Artefatos) {
		t.Fatalf("artefatos = %+v, want %+v", rep.Artefatos, first.Artefatos)
	}
	if len(rep.Artefatos) != 1 || rep.Artefatos[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("artefato hash = %+v", rep.Artefatos)
	}
	if !reflect.DeepEqual(rep.Blockers, first.Blockers) || !reflect.DeepEqual(rep.Perguntas, first.Perguntas) {
		t.Fatalf("blockers/perguntas = %+v/%+v", rep.Blockers, rep.Perguntas)
	}
	if !reflect.DeepEqual(rep.Equipe, first.Equipe) || !reflect.DeepEqual(rep.Custo, first.Custo) || !reflect.DeepEqual(rep.Logs, first.Logs) {
		t.Fatalf("equipe/custo/logs = %+v %+v %+v", rep.Equipe, rep.Custo, rep.Logs)
	}
	if rep.Limpeza.Workspace != "closed" || rep.Limpeza.PanesFechados != 4 || rep.Limpeza.Worktree != "kept" || !rep.Limpeza.Removivel {
		t.Fatalf("limpeza = %+v", rep.Limpeza)
	}
	if rep.Publico.Verdict != "pass" {
		t.Fatalf("publico verdict = %q, want pass", rep.Publico.Verdict)
	}
	if want := []string{"CARD-1", "PR#12"}; !reflect.DeepEqual(rep.Publico.IDs, want) {
		t.Fatalf("publico ids = %+v, want %+v", rep.Publico.IDs, want)
	}
	if rep.Status != StatusDone || rep.Memoria.DecisionsAckedSeq != decision.Seq {
		t.Fatalf("status/watermark = %q/%d, want %s/%d", rep.Status, rep.Memoria.DecisionsAckedSeq, StatusDone, decision.Seq)
	}
}
