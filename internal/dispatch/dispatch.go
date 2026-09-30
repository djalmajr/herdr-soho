package dispatch

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/kinds"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

var forFamilies = map[string]bool{"anthropic": true, "openai": true, "xai": true, "google": true, "alibaba": true}
var dispatchPairName = regexp.MustCompile(`^(.+)-(\d{8}T\d{6})(-\d+)?$`)

func FamilyConflicts(sd, family string, env platform.Env, cwd string) []string {
	if family == "" || family == "unknown" {
		return nil
	}
	out := []string{}
	for _, row := range core.RosterRows(sd) {
		f := strings.Split(row, "\t")
		at := func(i int) string {
			if i < len(f) {
				return f[i]
			}
			return ""
		}
		if at(0) == "" || at(4) != family {
			continue
		}
		history := at(10)
		if at(3) == "documenter" {
			docOnly := true
			for _, role := range strings.Split(history, ",") {
				if strings.TrimSpace(role) != "" && strings.TrimSpace(role) != "documenter" {
					docOnly = false
				}
			}
			if docOnly {
				continue
			}
		}
		if core.RoleIsEdit(at(3), env, cwd) || core.HistoryHasEdit(history, env, cwd) {
			out = append(out, fmt.Sprintf("%s (%s)", at(0), at(2)))
		}
	}
	return out
}

func ForSpecFamily(spec, sd string, env platform.Env, cwd string, ctx *core.Config) (string, error) {
	for _, row := range core.RosterRows(sd) {
		f := strings.Split(row, "\t")
		if len(f) > 0 && f[0] == spec {
			family := ""
			if len(f) > 4 {
				family = f[4]
			}
			if family != "" && family != "unknown" {
				return family, nil
			}
			kind, model := "", ""
			if len(f) > 2 {
				kind = f[2]
			}
			if len(f) > 8 {
				model = f[8]
			}
			return kinds.AgentFamily(kind, model), nil
		}
	}
	if forFamilies[spec] {
		return spec, nil
	}
	if family := kinds.KindFamily(spec); family != "unknown" {
		return family, nil
	}
	tmp := env.Get("TMPDIR")
	if tmp == "" {
		tmp = os.TempDir()
	}
	reports := filepath.Join(tmp, "herdr-soho", core.WorkspaceID(ctx, env, cwd), "reports")
	paths := []string{}
	for _, dir := range []string{filepath.Join(sd, "briefs"), reports} {
		entries, _ := os.ReadDir(dir)
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasSuffix(name, ".dispatch.json") {
				match := dispatchPairName.FindStringSubmatch(strings.TrimSuffix(name, ".dispatch.json"))
				if len(match) > 1 && match[1] == spec {
					paths = append(paths, filepath.Join(dir, name))
				}
			}
		}
	}
	sort.Strings(paths)
	counts := map[string]int{}
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		v, err := jsonjs.Parse(raw)
		if err != nil {
			continue
		}
		obj, ok := v.(*jsonjs.Object)
		if !ok {
			continue
		}
		submission, _ := obj.Get("submission")
		if submission != "accepted" {
			continue
		}
		kind, _ := obj.Get("kind")
		model, _ := obj.Get("model")
		fam := kinds.AgentFamily(fmt.Sprint(kind), fmt.Sprint(model))
		counts[fam]++
	}
	if len(counts) == 0 {
		return "", fmt.Errorf("dispatch: --for '%s': not an agent in the roster, a family (anthropic|openai|xai|google|alibaba), a kind with a fixed family, or an agent with an accepted dispatch recorded in this workspace", spec)
	}
	if len(counts) == 1 {
		for fam := range counts {
			if fam != "unknown" {
				return fam, nil
			}
		}
	}
	families := make([]string, 0, len(counts))
	for fam := range counts {
		families = append(families, fam)
	}
	sort.Strings(families)
	detail := []string{}
	for _, fam := range families {
		detail = append(detail, fmt.Sprintf("%s ×%d", fam, counts[fam]))
	}
	return "", fmt.Errorf("dispatch: --for '%s': the recorded dispatches of this released agent do not agree on one known model family (%s); pass the family instead (anthropic|openai|xai|google|alibaba)", spec, strings.Join(detail, ", "))
}

const standingRules = "- Only you write this report, once all of the brief is done, including any part you handed to subagents or background tasks; a subagent never writes it. Report every item as it stands in the files, not as a subagent summarized it.\n" +
	"- Report only the current brief and its explicit amendments; do not import unrelated work from earlier briefs retained in a reused session. Mention prior work only when it directly affects this brief, stating the relationship.\n" +
	"- Command output you put in the report is pasted from the run, never retyped or reconstructed.\n" +
	"- Nobody watches this terminal: do not ask interactive questions or wait for a confirmation. When the brief does not decide something, follow its \"When the brief does not decide\" section, or mark the item partial and list the gap and the options under open questions.\n" +
	"- Never invent names, endpoints, flags, credentials, URLs or requirements.\n" +
	"- Do not commit, push, tag, or open pull requests.\n" +
	"- When finished, reply in the terminal with exactly the report path and nothing else.\n"

const sharedTreeNote = "- Another worker edits this same tree now: run the global checks the brief asks for, but report failures in files you do not own as outside your slice (name the files), not as [partial] items of yours.\n"

func ComposePrompt(roleFile, role, agent, briefRaw, report string, ctx *core.Config, env platform.Env, kind, args string, shared bool) string {
	var out strings.Builder
	fmt.Fprintf(&out, "# Role: %s\n\n", core.FmGet(roleFile, "name"))
	fmt.Fprintf(&out, "You are running as the `%s` role, agent name `%s`, inside a multi-agent run coordinated by an orchestrator that cannot see your terminal.\n\n", role, agent)
	out.WriteString(core.RoleBody(roleFile))
	out.WriteString("\n\n# Brief\n\n")
	out.WriteString(briefRaw)
	out.WriteString("\n\n# Report contract\n\n")
	fmt.Fprintf(&out, "- Write your report as Markdown to `%s` (create parent directories if needed) following the `<report>` section of your role. Give every item its state as `[done]`, `[partial]` or `[skipped]`, followed by the reason.\n", report)
	out.WriteString("- This report path is authoritative: if the brief names a different report path, ignore it and write to this path; a brief can be hand-written or reused with a stale path, and the dispatch's report path never follows the brief's.\n")
	if lang := core.Cfg(ctx, "report_language", "", env); lang != "" {
		fmt.Fprintf(&out, "- Write the report in %s.\n", lang)
	}
	out.WriteString("- Write the report in one go, as the last action of your work; the orchestrator treats its existence as completion. Do not message the orchestrator to announce it (no `herdr-soho send`, no notice): a busy orchestrator leaves the sender waiting, and the report file is the only signal it needs.\n")
	if core.Cfg(ctx, "worker_context", "full", env) == "lean" {
		out.WriteString("- This brief is self-contained. Do NOT read CLAUDE.md, AGENTS.md, ai-memory rules, wiki pages or other project instruction files unless the brief names them explicitly; the rules that apply are quoted in the brief. Start on the task immediately.\n")
	}
	for _, n := range SandboxNotes(kind, args) {
		out.WriteString(n)
	}
	fmt.Fprintf(&out, "- Run every `herdr-soho` command this prompt names through the launcher at `%s`, not through PATH.\n", platform.LauncherPath(env))
	if shared {
		out.WriteString(sharedTreeNote)
	}
	out.WriteString(standingRules)
	return out.String()
}

func ComposeAmendment(raw, report string, ctx *core.Config, env platform.Env, kind, args string, shared bool) string {
	var out strings.Builder
	out.WriteString("# Amendment to your current brief\n\n")
	out.WriteString(raw)
	out.WriteString("\n\n# Report contract\n\n")
	fmt.Fprintf(&out, "- This amendment overrides your current brief where they differ; the rest of that brief still holds.\n- Check every item again against the files as they are now: rerun the checks it needs, and never copy findings, outputs or states from your earlier report.\n- Write your report as Markdown to `%s` (create parent directories if needed). If you have not written the report of your current brief yet, write one report there that covers the brief and this amendment; otherwise report only on the amendment.\n", report)
	out.WriteString("- This report path is authoritative: if your current brief names a different report path, ignore it and write to this path; a brief can be hand-written or reused with a stale path, and the dispatch's report path never follows the brief's.\n- Give every item its state as `[done]`, `[partial]` or `[skipped]`, followed by the reason.\n")
	if lang := core.Cfg(ctx, "report_language", "", env); lang != "" {
		fmt.Fprintf(&out, "- Write the report in %s.\n", lang)
	}
	out.WriteString("- Write the report in one go, as the last action of your work; the orchestrator treats its existence as completion. Do not message the orchestrator to announce it (no `herdr-soho send`, no notice): a busy orchestrator leaves the sender waiting, and the report file is the only signal it needs.\n")
	for _, n := range SandboxNotes(kind, args) {
		out.WriteString(n)
	}
	if shared {
		out.WriteString(sharedTreeNote)
	}
	out.WriteString(standingRules)
	return out.String()
}

func DispatchPairSuffix(composedAt, reportAt func(string) string, exists func(string) bool, lastReport string) string {
	for n := 1; ; n++ {
		suffix := ""
		if n > 1 {
			suffix = fmt.Sprintf("-%d", n)
		}
		c, r := composedAt(suffix), reportAt(suffix)
		if !exists(c) && !exists(r) && lastReport != r {
			return suffix
		}
	}
}

func DispatchSidecar(composed string) string {
	base := filepath.Base(composed)
	base = strings.TrimSuffix(base, ".brief.md")
	if base == filepath.Base(composed) {
		base = strings.TrimSuffix(base, ".md")
	}
	return filepath.Join(filepath.Dir(composed), base+".dispatch.json")
}

func WriteSidecar(path string, kind, model, effort, submission, arrival, session string) error {
	pairs := []any{"version", 1, "kind", kind, "model", model, "effort", effort, "submission", submission}
	if arrival != "" {
		pairs = append(pairs, "arrival", arrival)
	}
	if session != "" {
		pairs = append(pairs, "session", session)
	}
	content := jsonjs.Stringify(jsonjs.O(pairs...))
	if content == "" {
		return fmt.Errorf("sidecar is not JSON serializable")
	}
	return platform.AtomicWrite(path, content+"\n")
}

func SamePath(left, right, goos string) bool {
	if goos == "win32" {
		left, right = strings.ToLower(path.Clean(strings.ReplaceAll(left, `\`, "/"))), strings.ToLower(path.Clean(strings.ReplaceAll(right, `\`, "/")))
		if len(left) >= 3 && left[1] == ':' && left[2] == '/' {
			left = strings.ToLower(left[:2]) + left[2:]
		}
		if len(right) >= 3 && right[1] == ':' && right[2] == '/' {
			right = strings.ToLower(right[:2]) + right[2:]
		}
		return left == right
	}
	return filepath.Clean(left) == filepath.Clean(right)
}
