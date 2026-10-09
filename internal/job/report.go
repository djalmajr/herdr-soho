package job

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ReportFacts is the worktree snapshot used to fill limpeza.removivel.
// Removivel is true only when the tree is clean and Head equals HeadRemoto.
type ReportFacts struct {
	Clean      bool
	Head       string
	HeadRemoto string
	Resumo     string
}

// PullRequest is the draft pull request recorded by a later slice.
type PullRequest struct {
	Numero int    `json:"numero,omitempty"`
	URL    string `json:"url,omitempty"`
	Estado string `json:"estado,omitempty"`
}

// Report is the supervisor report.json document. It has no retencao_ate key.
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
	PR           *PullRequest `json:"pr"`
	Memoria      Memoria      `json:"memoria"`
	Eventos      Eventos      `json:"eventos"`
	Limpeza      Limpeza      `json:"limpeza"`
	Publico      Publico      `json:"publico"`
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
	inSync := facts.Head != "" && facts.Head == facts.HeadRemoto
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
		Memoria: Memoria{
			Lida:              []MemRead{},
			Global:            global,
			Projeto:           projeto,
			DecisionsTotal:    decisions,
			DecisionsAckedSeq: st.DecisionsAckedSeq,
		},
		Eventos: Eventos{
			Total:     len(events),
			UltimoSeq: lastSeq(events),
			Arquivo:   filepath.Join(dir, "events.jsonl"),
		},
		Limpeza: Limpeza{
			Worktree:  "kept",
			Removivel: facts.Clean && inSync,
		},
		Publico: Publico{Blockers: []string{}, IDs: ids},
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
