package job

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/reportscan"
)

// ReportFacts is the worktree and job snapshot the supervisor passes in to
// fill the report: the clean and head facts behind limpeza.removivel, the
// commits, the draft pull request, the acceptance checks, the review, the
// blockers, the team, the cost, the logs and the workspace release.
type ReportFacts struct {
	Clean         bool
	Head          string
	HeadRemoto    string
	Resumo        string
	Commits       []Commit
	PR            *PullRequest
	Testes        []Teste
	Revisao       *Revisao
	Blockers      []string
	Equipe        *Equipe
	Custo         *Custo
	Logs          *Logs
	Workspace     string
	PanesFechados int
}

// PullRequest is the draft pull request recorded by a later slice.
type PullRequest struct {
	Numero int    `json:"numero,omitempty"`
	URL    string `json:"url,omitempty"`
	Estado string `json:"estado,omitempty"`
}

// Report is the supervisor report.json document. The field order is the
// key order of the contract report example. It has no retencao_ate key.
type Report struct {
	Schema       int          `json:"schema"`
	ID           string       `json:"id"`
	Status       string       `json:"status"`
	Motivo       *string      `json:"motivo"`
	Resumo       string       `json:"resumo"`
	Maquina      string       `json:"maquina,omitempty"`
	Repo         string       `json:"repo,omitempty"`
	Base         string       `json:"base,omitempty"`
	Branch       string       `json:"branch,omitempty"`
	Head         string       `json:"head,omitempty"`
	HeadRemoto   string       `json:"head_remoto,omitempty"`
	Sincronizado bool         `json:"sincronizado"`
	Commits      []Commit     `json:"commits"`
	PR           *PullRequest `json:"pr"`
	Itens        []Item       `json:"itens"`
	Parciais     int          `json:"parciais"`
	Testes       []Teste      `json:"testes"`
	Revisao      *Revisao     `json:"revisao"`
	Artefatos    []Artefato   `json:"artefatos"`
	Blockers     []string     `json:"blockers"`
	Perguntas    []string     `json:"perguntas"`
	Memoria      Memoria      `json:"memoria"`
	Equipe       *Equipe      `json:"equipe"`
	Eventos      Eventos      `json:"eventos"`
	Custo        *Custo       `json:"custo"`
	Logs         *Logs        `json:"logs"`
	Limpeza      Limpeza      `json:"limpeza"`
	Publico      Publico      `json:"publico"`
}

// Item is one orchestrator report line carrying a completion marker, in
// file order.
type Item struct {
	N      int    `json:"n"`
	Item   string `json:"item"`
	Estado string `json:"estado"`
}

// Teste is one acceptance check the supervisor ran.
type Teste struct {
	Cmd       string `json:"cmd"`
	Resultado string `json:"resultado"`
}

// Revisao is the review verdict of another model family. Severity is
// formatted exactly "P0 a, P1 b, P2 c, P3 d".
type Revisao struct {
	Verdict  string `json:"verdict"`
	Findings int    `json:"findings"`
	Severity string `json:"severity"`
}

// Artefato is a job artefact the supervisor hashed; the path is relative to
// the job dir, which job collect --verify resolves.
type Artefato struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Equipe is the team resolution the job ran with.
type Equipe struct {
	Fonte         string   `json:"fonte"`
	OverrideBrief []string `json:"override_brief"`
}

// Custo is the job duration and worker count.
type Custo struct {
	Inicio   string `json:"inicio"`
	Fim      string `json:"fim"`
	DuracaoS int    `json:"duracao_s"`
	Workers  int    `json:"workers"`
}

// Logs points at the job logs.
type Logs struct {
	ReportMD string `json:"report_md"`
	StateDir string `json:"state_dir"`
	Friction string `json:"friction"`
}

// MemItem is one decision suggestion for the dispatcher to route.
type MemItem struct {
	Tipo   string            `json:"tipo"`
	Seq    int               `json:"seq"`
	Resumo string            `json:"resumo"`
	Motivo string            `json:"motivo"`
	Refs   map[string]string `json:"refs"`
}

// MemRead is a memory scope the orchestrator read. This slice does not read one.
type MemRead struct {
	Escopo  string   `json:"escopo"`
	Paginas []string `json:"paginas"`
}

// Memoria aggregates decision events. The job does not write them anywhere else.
type Memoria struct {
	Lida              []MemRead `json:"lida"`
	Global            []MemItem `json:"global"`
	Projeto           []MemItem `json:"projeto"`
	DecisionsTotal    int       `json:"decisions_total"`
	DecisionsAckedSeq int       `json:"decisions_acked_seq"`
}

// Eventos points at the job event log.
type Eventos struct {
	Total     int    `json:"total"`
	UltimoSeq int    `json:"ultimo_seq"`
	Arquivo   string `json:"arquivo"`
}

// Limpeza records process release. The worktree is always kept.
// Removivel is informational and never causes removal.
type Limpeza struct {
	Workspace     string `json:"workspace,omitempty"`
	Worktree      string `json:"worktree"`
	PanesFechados int    `json:"panes_fechados,omitempty"`
	Removivel     bool   `json:"removivel"`
}

// Publico is the allowlisted block that may appear on a public forge.
type Publico struct {
	Verdict  string   `json:"verdict"`
	Blockers []string `json:"blockers"`
	IDs      []string `json:"ids"`
}

// WriteReport rebuilds report.json from the job files and the worktree facts.
func (s *Store) WriteReport(id string, facts ReportFacts) (Report, error) {
	var rep Report
	err := s.withExisting(id, func(dir string) error {
		st, err := readState(dir)
		if err != nil {
			return err
		}
		events, err := readEvents(dir)
		if err != nil {
			return err
		}
		rep, err = buildReport(dir, st, events, facts)
		if err != nil {
			return err
		}
		return saveReport(dir, rep)
	})
	return rep, err
}

func buildReport(dir string, st State, events []Event, facts ReportFacts) (Report, error) {
	fields, err := readBriefFields(dir)
	if err != nil {
		return Report{}, err
	}
	global, projeto, decisions := decisionAggregates(events)
	items, parciais, artefatos := readReportMD(dir)
	// The report is built only from known facts: with no commits known (no
	// head), the pull request is null and the motivo says so; the state
	// motivo wins when set.
	motivo := st.Motivo
	if motivo == nil && facts.Head == "" {
		none := "sem commits"
		motivo = &none
	}
	ids := []string{}
	if origem, ok := fields["origem"].(map[string]any); ok {
		if ref, ok := origem["ref"].(string); ok && ref != "" {
			ids = append(ids, ref)
		}
	}
	if facts.PR != nil {
		ids = append(ids, "PR#"+strconv.Itoa(facts.PR.Numero))
	}
	inSync := facts.Head != "" && facts.Head == facts.HeadRemoto
	verdict := ""
	if facts.Revisao != nil {
		verdict = facts.Revisao.Verdict
	}
	commits := facts.Commits
	if commits == nil {
		commits = []Commit{}
	}
	testes := facts.Testes
	if testes == nil {
		testes = []Teste{}
	}
	blockers := facts.Blockers
	if blockers == nil {
		blockers = []string{}
	}
	return Report{
		Schema:       1,
		ID:           st.ID,
		Status:       st.Status,
		Motivo:       motivo,
		Resumo:       facts.Resumo,
		Maquina:      jsonString(fields, "maquina"),
		Repo:         jsonString(fields, "repo"),
		Base:         jsonString(fields, "base"),
		Branch:       "job/" + st.ID,
		Head:         facts.Head,
		HeadRemoto:   facts.HeadRemoto,
		Sincronizado: inSync,
		Commits:      commits,
		PR:           facts.PR,
		Itens:        items,
		Parciais:     parciais,
		Testes:       testes,
		Revisao:      facts.Revisao,
		Artefatos:    artefatos,
		Blockers:     blockers,
		Perguntas:    openQuestions(events),
		Memoria: Memoria{
			Lida:              []MemRead{},
			Global:            global,
			Projeto:           projeto,
			DecisionsTotal:    decisions,
			DecisionsAckedSeq: st.DecisionsAckedSeq,
		},
		Equipe: facts.Equipe,
		Eventos: Eventos{
			Total:     len(events),
			UltimoSeq: lastSeq(events),
			Arquivo:   filepath.Join(dir, "events.jsonl"),
		},
		Custo: facts.Custo,
		Logs:  facts.Logs,
		Limpeza: Limpeza{
			Workspace:     facts.Workspace,
			Worktree:      "kept",
			PanesFechados: facts.PanesFechados,
			Removivel:     facts.Clean && inSync,
		},
		Publico: Publico{Verdict: verdict, Blockers: blockers, IDs: ids},
	}, nil
}

// decisionAggregates groups the decision events of the log by escopo, in
// seq order.
func decisionAggregates(events []Event) (global, projeto []MemItem, total int) {
	global = []MemItem{}
	projeto = []MemItem{}
	for _, event := range events {
		if event.Tipo != "decision" {
			continue
		}
		total++
		item := MemItem{
			Tipo: "decision", Seq: event.Seq, Resumo: event.Resumo,
			Motivo: event.Motivo, Refs: cloneRefs(event.Refs),
		}
		switch event.Escopo {
		case "global":
			global = append(global, item)
		case "projeto":
			projeto = append(projeto, item)
		}
	}
	return global, projeto, total
}

// reportItemFence mirrors the fence semantics of reportscan.PartialCount,
// which does not export its fence tracking: the items and the partial count
// must agree on which lines are inside a code block.
var (
	reportItemFenceOpen  = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
	reportItemFenceClose = regexp.MustCompile("^ {0,3}(`{3,}|~{3,}) *$")
	reportItemPrefix     = regexp.MustCompile(`^\s*(?:[-*][ \t]+|[0-9]+\.[ \t]+|\|[ \t]*)`)
)

// readReportMD reads the orchestrator report.md in the job dir: the marker
// items, the partial count (reportscan's rule) and the artefact with the
// file's sha256. A missing or unreadable file leaves all three empty.
func readReportMD(dir string) (items []Item, parciais int, artefatos []Artefato) {
	items = []Item{}
	artefatos = []Artefato{}
	raw, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		return items, 0, artefatos
	}
	text := string(raw)
	items = reportItems(text)
	parciais = reportscan.PartialCount(text)
	sum := sha256.Sum256(raw)
	artefatos = append(artefatos, Artefato{Path: "report.md", SHA256: hex.EncodeToString(sum[:])})
	return items, parciais, artefatos
}

// reportItems extracts the orchestrator report items: the lines outside
// fenced code blocks that carry exactly one of the [done], [partial] or
// [skipped] markers, numbered in file order. The item text is the line with
// the marker, the leading list and table syntax and the surrounding
// whitespace removed, cut to 280 code points.
func reportItems(text string) []Item {
	items := []Item{}
	inFence, fenceChar, fenceLen := false, byte(0), 0
	for _, line := range strings.Split(text, "\n") {
		open := reportItemFenceOpen.FindStringSubmatch(line)
		if !inFence && open != nil {
			inFence, fenceChar, fenceLen = true, open[1][0], len(open[1])
			continue
		}
		if inFence {
			if close := reportItemFenceClose.FindStringSubmatch(line); close != nil && close[1][0] == fenceChar && len(close[1]) >= fenceLen {
				inFence = false
			}
			continue
		}
		estado := singleItemMarker(line)
		if estado == "" {
			continue
		}
		item := reportItemPrefix.ReplaceAllString(line, "")
		items = append(items, Item{N: len(items) + 1, Item: cutRunes(strings.TrimSpace(item), resumoLimit), Estado: estado})
	}
	return items
}

// singleItemMarker reports the marker of a line carrying exactly one report
// marker, or "" for a line with none or with two or more (a doubled marker
// of the same kind included), which the brief skips.
func singleItemMarker(line string) string {
	total := strings.Count(line, "[done]") + strings.Count(line, "[partial]") + strings.Count(line, "[skipped]")
	if total != 1 {
		return ""
	}
	switch {
	case strings.Contains(line, "[done]"):
		return "done"
	case strings.Contains(line, "[partial]"):
		return "partial"
	default:
		return "skipped"
	}
}

// openQuestions is the resumo of the question events with a seq greater than
// the last unblocked event's seq, in seq order; empty when none is open.
func openQuestions(events []Event) []string {
	last := 0
	for _, event := range events {
		if event.Tipo == "unblocked" && event.Seq > last {
			last = event.Seq
		}
	}
	questions := []string{}
	for _, event := range events {
		if event.Tipo == "question" && event.Seq > last {
			questions = append(questions, event.Resumo)
		}
	}
	return questions
}

// cutRunes shortens value to at most limit code points, keeping the start.
func cutRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}

// refreshReport reconciles the stored report.json with the job log and
// state. It overwrites only the log/state-derived fields: the status, and
// only for a terminal outcome (collected and closed never replace the stored
// outcome, so a closed job still reports done), the motivo when the state
// has one, the event metadata, the decision aggregates, the effective ack
// watermark and limpeza.worktree. Every other field (head, pr, resumo,
// removivel, ...) is kept as stored; a missing report is built from the
// known facts.
func refreshReport(dir string, st State) error {
	events, err := readEvents(dir)
	if err != nil {
		return err
	}
	rep, err := readReport(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		built, buildErr := buildReport(dir, st, events, ReportFacts{})
		if buildErr != nil {
			return buildErr
		}
		rep = built
	}
	if isTerminalOutcome(st.Status) {
		rep.Status = st.Status
	}
	if st.Motivo != nil {
		rep.Motivo = st.Motivo
	}
	rep.Eventos = Eventos{
		Total:     len(events),
		UltimoSeq: lastSeq(events),
		Arquivo:   filepath.Join(dir, "events.jsonl"),
	}
	global, projeto, total := decisionAggregates(events)
	rep.Memoria.Global = global
	rep.Memoria.Projeto = projeto
	rep.Memoria.DecisionsTotal = total
	rep.Memoria.DecisionsAckedSeq = effectiveWatermark(st.DecisionsAckedSeq, events)
	rep.Limpeza.Worktree = "kept"
	return saveReport(dir, rep)
}

// isTerminalOutcome reports the terminal outcomes: done, failed, timeout or
// canceled. Collected and closed are not outcomes.
func isTerminalOutcome(status string) bool {
	switch status {
	case StatusDone, StatusFailed, StatusTimeout, StatusCanceled:
		return true
	default:
		return false
	}
}

// reportSetState reports whether the state carries a stored report that must
// stay current: a terminal outcome, or collected or closed.
func reportSetState(status string) bool {
	return status == StatusCollected || status == StatusClosed || isTerminalOutcome(status)
}

// refreshIfWrote refreshes the stored report when the caller just wrote to a
// job in a report-carrying state.
func (s *Store) refreshIfWrote(dir string, st State, wrote bool) error {
	if !wrote || !reportSetState(st.Status) {
		return nil
	}
	return refreshReport(dir, st)
}

func readBriefFields(dir string) (map[string]any, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "brief.json"))
	if err != nil {
		return nil, err
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	return fields, nil
}

func readReport(dir string) (Report, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "report.json"))
	if err != nil {
		return Report{}, err
	}
	var rep Report
	if err = json.Unmarshal(raw, &rep); err != nil {
		return Report{}, err
	}
	return rep, nil
}

func saveReport(dir string, rep Report) error {
	rep.Schema = 1
	rep.Limpeza.Worktree = "kept"
	// The contract fields are always present with empty values: a report
	// written before they existed unmarshals them as nil, and the rewrite
	// stores them empty rather than null.
	if rep.Commits == nil {
		rep.Commits = []Commit{}
	}
	if rep.Itens == nil {
		rep.Itens = []Item{}
	}
	if rep.Testes == nil {
		rep.Testes = []Teste{}
	}
	if rep.Artefatos == nil {
		rep.Artefatos = []Artefato{}
	}
	if rep.Blockers == nil {
		rep.Blockers = []string{}
	}
	if rep.Perguntas == nil {
		rep.Perguntas = []string{}
	}
	raw, err := json.Marshal(rep)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return writeAtomic0600(filepath.Join(dir, "report.json"), raw)
}

func jsonString(fields map[string]any, key string) string {
	value, _ := fields[key].(string)
	return value
}

func cloneRefs(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
