package job

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/dispatch"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

var (
	idPattern   = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}/[A-Za-z0-9._-]{1,100}$`)
	basePattern = regexp.MustCompile(`^[A-Za-z0-9._/-]{1,100}$`)
)

type aceiteItem struct {
	Criterio string
	Prova    string
}

type briefDoc struct {
	Canonical  []byte
	Hash       string
	Objetivo   string
	Contexto   string
	Markdown   string
	Repo       string
	Base       string
	Maquina    string
	OrigemRef  string
	Decisoes   []string
	Restricoes []string
	NonGoals   []string
	Idioma     string
}

func parseBrief(raw []byte, id string) (briefDoc, error) {
	if len(raw) > MaxBriefBytes {
		return briefDoc{}, errUsage("job: brief exceeds 256 KiB")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	fields, ok := value.(map[string]any)
	if !ok {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	numero, ok := fields["schema"].(json.Number)
	if !ok || numero.String() != "1" {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	briefID, ok := fields["id"].(string)
	if !ok || briefID != id || !idPattern.MatchString(briefID) {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	origem, ok := fields["origem"].(map[string]any)
	if !ok {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	tipo, _ := origem["tipo"].(string)
	switch tipo {
	case "conversa", "card", "cron":
	default:
		return briefDoc{}, errUsage("job: invalid brief")
	}
	ref, ok := origem["ref"].(string)
	if !ok || ref == "" {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	repo, ok := fields["repo"].(string)
	if !ok || !repoPattern.MatchString(repo) {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	objetivo, ok := fields["objetivo"].(string)
	if !ok || objetivo == "" {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	items, err := parseAceite(fields["aceite"])
	if err != nil {
		return briefDoc{}, err
	}
	contexto := ""
	if value, exists := fields["contexto"]; exists {
		parsed, ok := value.(string)
		if !ok {
			return briefDoc{}, errUsage("job: invalid brief")
		}
		contexto = parsed
	}
	decisoes, err := parseOptionalStringList(fields["decisoes"])
	if err != nil {
		return briefDoc{}, err
	}
	restricoes, err := parseOptionalStringList(fields["restricoes"])
	if err != nil {
		return briefDoc{}, err
	}
	nonGoals, err := parseOptionalStringList(fields["nao_objetivos"])
	if err != nil {
		return briefDoc{}, err
	}
	idioma := ""
	if value, exists := fields["idioma"]; exists {
		parsed, ok := value.(string)
		if !ok {
			return briefDoc{}, errUsage("job: invalid brief")
		}
		idioma = parsed
	}
	base := ""
	if value, exists := fields["base"]; exists {
		parsed, ok := value.(string)
		if !ok || !basePattern.MatchString(parsed) || strings.Contains(parsed, "..") {
			return briefDoc{}, errUsage("job: invalid brief")
		}
		base = parsed
	}
	if value, exists := fields["modo"]; exists {
		parsed, ok := value.(string)
		if !ok || (parsed != "worktree" && parsed != "workspace") {
			return briefDoc{}, errUsage("job: invalid brief")
		}
	}
	// TODO(DJA-194): verify maquina equals machine_label once machine config is loaded.
	maquina := ""
	if value, exists := fields["maquina"]; exists {
		parsed, ok := value.(string)
		if !ok || parsed == "" {
			return briefDoc{}, errUsage("job: invalid brief")
		}
		maquina = parsed
	}
	// TODO(DJA-194): verify equipe keys with the session set parser.
	if value, exists := fields["equipe"]; exists {
		if _, ok := value.(map[string]any); !ok {
			return briefDoc{}, errUsage("job: invalid brief")
		}
	}
	canonical, err := canonicalJSON(value)
	if err != nil {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	sum := sha256.Sum256(canonical)
	markdown := renderBrief(objetivo, contexto, idioma, items, decisoes, restricoes, nonGoals)
	if missing := dispatch.BriefMissingSections(markdown, false, nil); missing != "" {
		return briefDoc{}, errUsage("job: invalid brief")
	}
	return briefDoc{
		Canonical:  canonical,
		Hash:       hex.EncodeToString(sum[:]),
		Objetivo:   objetivo,
		Contexto:   contexto,
		Markdown:   markdown,
		Repo:       repo,
		Base:       base,
		Maquina:    maquina,
		OrigemRef:  ref,
		Decisoes:   decisoes,
		Restricoes: restricoes,
		NonGoals:   nonGoals,
		Idioma:     idioma,
	}, nil
}

func parseAceite(value any) ([]aceiteItem, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, errUsage("job: invalid brief")
	}
	out := make([]aceiteItem, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			return nil, errUsage("job: invalid brief")
		}
		criterio, ok := fields["criterio"].(string)
		prova, provaOK := fields["prova"].(string)
		if !ok || !provaOK || criterio == "" || prova == "" {
			return nil, errUsage("job: invalid brief")
		}
		out = append(out, aceiteItem{Criterio: criterio, Prova: prova})
	}
	return out, nil
}

// parseOptionalStringList reads an optional brief field that must be a list
// of non-empty strings when present. An absent field yields a nil list.
func parseOptionalStringList(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, errUsage("job: invalid brief")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text, ok := item.(string)
		if !ok || text == "" {
			return nil, errUsage("job: invalid brief")
		}
		out = append(out, text)
	}
	return out, nil
}

func canonicalJSON(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// renderBrief converts the supplied brief into the standard Markdown brief
// (skills/herdr-soho/templates/brief.md sections). Absent optional fields
// produce no empty section.
func renderBrief(objetivo, contexto, idioma string, items []aceiteItem, decisoes, restricoes, naoObjetivos []string) string {
	var buf strings.Builder
	buf.WriteString("# Brief — job\n\n")
	if idioma != "" {
		buf.WriteString("Report language: " + idioma + "\n\n")
	}
	buf.WriteString("## Goal\n\n")
	buf.WriteString(objetivo)
	buf.WriteString("\n")
	if contexto != "" {
		buf.WriteString("\nContext: " + contexto + "\n")
	}
	if len(decisoes) > 0 {
		buf.WriteString("\n\n## Decisions already made\n\n")
		for _, item := range decisoes {
			buf.WriteString("- " + item + "\n")
		}
	}
	buf.WriteString("\n## Acceptance criteria\n\n")
	if len(items) == 0 {
		buf.WriteString("No acceptance items were listed.\n")
	} else {
		for i, item := range items {
			fmt.Fprintf(&buf, "%d. %s — proved by `%s`\n", i+1, item.Criterio, item.Prova)
		}
	}
	buf.WriteString("\n## Owned files\n\n- the job worktree\n\n")
	buf.WriteString("## Forbidden\n\n")
	for _, item := range restricoes {
		buf.WriteString("- " + item + "\n")
	}
	buf.WriteString("- workers do not commit or push\n\n")
	if len(naoObjetivos) > 0 {
		buf.WriteString("## Non-goals\n\n")
		for _, item := range naoObjetivos {
			buf.WriteString("- " + item + "\n")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("## Report\n\nWrite the report with one done, partial, or skipped marker per item.\n")
	return buf.String()
}

// LintBrief runs the shared config-aware brief lint on a written brief.md.
// A strict failure is a refused exit 2; every other result is nil. The CLI
// slice calls this on the written brief before dispatch.
func LintBrief(path string, ctx *core.Config, env platform.Env) error {
	result := dispatch.BriefLintFindings(path, ctx, env, dispatch.BriefLintOptions{})
	if result.Mode == "strict" && result.MissingMessage != "" {
		return &ExitError{Code: ExitUsage, Msg: result.MissingMessage}
	}
	return nil
}
