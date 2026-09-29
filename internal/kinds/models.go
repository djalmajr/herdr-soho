package kinds

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/djalmajr/herdr-soho/internal/platform"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

var ansiSGR = regexp.MustCompile("\\x1b\\[[0-9;]*m")
var effortSuffixRE = regexp.MustCompile("-(minimal|low|medium|high|xhigh|max)(-fast)?$")

func ParseCursorModels(stdout string) []string {
	var ids []string
	re := regexp.MustCompile("^([a-z0-9.-]+) - ")
	for _, line := range strings.Split(ansiSGR.ReplaceAllString(stdout, ""), "\n") {
		if m := re.FindStringSubmatch(line); m != nil {
			ids = append(ids, m[1])
		}
	}
	return ids
}
func ParseAgyModels(stdout string) []string {
	var ids []string
	valid := regexp.MustCompile("^[a-z0-9.-]+$")
	for _, line := range strings.Split(ansiSGR.ReplaceAllString(stdout, ""), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && valid.MatchString(fields[0]) {
			ids = append(ids, fields[0])
		}
	}
	return ids
}
func ParseGrokModels(stdout string) []string {
	re := regexp.MustCompile("grok-[0-9][0-9a-z.-]*")
	set := map[string]bool{}
	for _, line := range strings.Split(ansiSGR.ReplaceAllString(stdout, ""), "\n") {
		for _, id := range re.FindAllString(line, -1) {
			set[id] = true
		}
	}
	ids := make([]string, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	textutil.SortUTF16(ids)
	return ids
}
func ModelsCacheFile(kind string, env platform.Env) string {
	dir := env.Get("TMPDIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "herdr-soho-models-"+kind+".txt")
}

func ModelIDs(kind string, env platform.Env) []string {
	file := ModelsCacheFile(kind, env)
	shortRaw, short := env.Lookup("HERDR_SOHO_MODELS_TIMEOUT")
	short = short && shortRaw != ""
	if st, err := os.Stat(file); err == nil && st.Size() > 0 && st.ModTime().After(platform.Now().Add(-time.Hour)) {
		if data, err := os.ReadFile(file); err == nil {
			return nonemptyLines(string(data))
		}
	}
	seconds := func(fallback float64) (int, bool) {
		if !short {
			return int(fallback * 1000), true
		}
		n, err := strconv.ParseFloat(shortRaw, 64)
		return int(n * 1000), err == nil && n >= 0
	}
	var out []string
	switch kind {
	case "codex":
		name := filepath.Join(platform.HomeDir(platform.Current(), env), ".codex", "models_cache.json")
		var data struct {
			Models []struct {
				Slug any `json:"slug"`
			} `json:"models"`
		}
		if b, err := os.ReadFile(name); err == nil && json.Unmarshal(b, &data) == nil {
			for _, model := range data.Models {
				if model.Slug == nil {
					continue
				}
				value := fmt.Sprint(model.Slug)
				if value != "" {
					out = append(out, value)
				}
			}
		}
	case "cursor", "agy", "grok":
		timeout, ok := seconds(20)
		if kind == "agy" {
			timeout, ok = seconds(30)
		}
		if ok {
			exe, args := kind, []string{"models"}
			if kind == "cursor" {
				exe, args = "cursor-agent", []string{"--list-models"}
			}
			r := platform.RunCli(exe, args, platform.RunOptions{Env: env, TimeoutMs: timeout})
			if r.Status != nil && *r.Status == 0 {
				switch kind {
				case "cursor":
					out = ParseCursorModels(r.Stdout)
				case "agy":
					out = ParseAgyModels(r.Stdout)
				case "grok":
					out = ParseGrokModels(r.Stdout)
				}
			}
		}
	}
	if !short && len(out) > 0 {
		_ = os.WriteFile(file, []byte(strings.Join(out, "\n")+"\n"), 0600)
	}
	return out
}
func nonemptyLines(data string) []string {
	var out []string
	for _, line := range strings.Split(data, "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

type versionKey struct{ id, key string }

func VersionSortDesc(ids []string) []string {
	keyed := make([]versionKey, 0, len(ids))
	partsRE := regexp.MustCompile("[0-9]+")
	for _, id := range ids {
		parts := partsRE.FindAllString(id, -1)
		var b strings.Builder
		for _, part := range parts {
			b.WriteString(strings.Repeat("0", max(0, 6-len(part))))
			b.WriteString(part)
			b.WriteByte('.')
		}
		keyed = append(keyed, versionKey{id: id, key: b.String()})
	}
	sort.SliceStable(keyed, func(i, j int) bool {
		if keyed[i].key != keyed[j].key {
			return keyed[i].key > keyed[j].key
		}
		return keyed[i].id < keyed[j].id
	})
	out := make([]string, 0, len(ids))
	for _, item := range keyed {
		out = append(out, item.id)
	}
	return out
}
func ERERegExp(pattern string) (*regexp.Regexp, bool) {
	if strings.Contains(pattern, "(?") {
		return nil, false
	}
	classes := map[string]string{"alpha": "A-Za-z", "digit": "0-9", "alnum": "A-Za-z0-9", "upper": "A-Z", "lower": "a-z", "xdigit": "0-9A-Fa-f", "space": " \\t\\n\\r\\f\\v", "blank": " \\t", "punct": "!-\\/:-@\\[-`{-~"}
	classRE := regexp.MustCompile("\\[:([a-z]+):\\]")
	pattern = classRE.ReplaceAllStringFunc(pattern, func(match string) string {
		name := match[2 : len(match)-2]
		if value, ok := classes[name]; ok {
			return value
		}
		return match
	})
	pattern = strings.ReplaceAll(pattern, "\\d", "[0-9]")
	pattern = strings.ReplaceAll(pattern, "\\w", "[A-Za-z0-9_]")
	pattern = strings.ReplaceAll(pattern, "\\s", "[\\t\\n\\r\\f\\v ]")
	pattern = asciiFoldPattern(pattern)
	re, err := regexp.Compile(pattern)
	return re, err == nil
}
func asciiFoldPattern(pattern string) string {
	var b strings.Builder
	inClass, escaped := false, false
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if escaped {
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			b.WriteRune(r)
			escaped = true
			continue
		}
		if r == '[' {
			inClass = true
			b.WriteRune(r)
			continue
		}
		if r == ']' {
			inClass = false
			b.WriteRune(r)
			continue
		}
		if inClass {
			// An inverted letter range ([a-Z], [z-a]) is a syntax error in the
			// JS RegExp, so ereRegExp returns null; folding each letter on its
			// own would hide it behind a valid range (aA-zZ is A-z). Keep it
			// as written so Compile fails the same way.
			if isASCIILetter(r) && i+2 < len(runes) && runes[i+1] == '-' && isASCIILetter(runes[i+2]) && r > runes[i+2] {
				b.WriteRune(r)
				b.WriteRune('-')
				b.WriteRune(runes[i+2])
				i += 2
				continue
			}
			if r >= 'a' && r <= 'z' && i+2 < len(runes) && runes[i+1] == '-' && runes[i+2] >= 'a' && runes[i+2] <= 'z' {
				b.WriteRune(r)
				b.WriteRune('-')
				b.WriteRune(runes[i+2])
				b.WriteRune(r - ('a' - 'A'))
				b.WriteRune('-')
				b.WriteRune(runes[i+2] - ('a' - 'A'))
				i += 2
				continue
			}
			if r >= 'A' && r <= 'Z' && i+2 < len(runes) && runes[i+1] == '-' && runes[i+2] >= 'A' && runes[i+2] <= 'Z' {
				b.WriteRune(r + ('a' - 'A'))
				b.WriteRune('-')
				b.WriteRune(runes[i+2] + ('a' - 'A'))
				b.WriteRune(r)
				b.WriteRune('-')
				b.WriteRune(runes[i+2])
				i += 2
				continue
			}
			if r >= 'a' && r <= 'z' {
				b.WriteRune(r)
				b.WriteRune(r - ('a' - 'A'))
				continue
			}
			if r >= 'A' && r <= 'Z' {
				b.WriteRune(r + ('a' - 'A'))
				b.WriteRune(r)
				continue
			}
			b.WriteRune(r)
			continue
		}
		if r >= 'a' && r <= 'z' {
			b.WriteByte('[')
			b.WriteRune(r)
			b.WriteRune(r - ('a' - 'A'))
			b.WriteByte(']')
			continue
		}
		if r >= 'A' && r <= 'Z' {
			b.WriteByte('[')
			b.WriteRune(r + ('a' - 'A'))
			b.WriteRune(r)
			b.WriteByte(']')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
func ResolveModel(kind, spec, effort string, env platform.Env, warn WarnFunc) (string, error) {
	if spec == "" {
		return "", nil
	}
	if warn == nil {
		warn = DefaultWarn
	}
	ids := ModelIDs(kind, env)
	if len(ids) == 0 {
		return spec, nil
	}
	for _, raw := range strings.Split(spec, "|") {
		alt := strings.TrimSpace(raw)
		if alt == "" {
			continue
		}
		for _, id := range ids {
			if id == alt {
				return alt, nil
			}
		}
		re, ok := ERERegExp(alt)
		if kind == "cursor" || kind == "agy" {
			seen := map[string]bool{}
			var bases []string
			for _, id := range ids {
				base := effortSuffixRE.ReplaceAllString(id, "")
				if !seen[base] {
					seen[base] = true
					bases = append(bases, base)
				}
			}
			var matched []string
			if ok {
				for _, base := range bases {
					if re.MatchString(base) && !strings.HasSuffix(base, "-fast") {
						matched = append(matched, base)
					}
				}
			}
			sorted := VersionSortDesc(matched)
			base := ""
			if len(sorted) > 0 {
				base = sorted[0]
			}
			if base == "" {
				continue
			}
			if effort != "" {
				for _, id := range ids {
					if id == base+"-"+effort {
						return id, nil
					}
				}
			}
			for _, id := range ids {
				if id == base {
					return id, nil
				}
			}
			for _, candidate := range []string{"max", "xhigh", "high", "medium", "low", "minimal"} {
				if effort != "" && EffortRank(candidate) > EffortRank(effort) {
					continue
				}
				for _, id := range ids {
					if id == base+"-"+candidate {
						return id, nil
					}
				}
			}
			prefix, err := regexp.Compile("^" + base + "-")
			for _, id := range ids {
				if err == nil && prefix.MatchString(id) {
					return id, nil
				}
				if err != nil && strings.HasPrefix(id, base+"-") {
					return id, nil
				}
			}
			return "", nil
		}
		if ok {
			var matched []string
			for _, id := range ids {
				if re.MatchString(id) {
					matched = append(matched, id)
				}
			}
			sorted := VersionSortDesc(matched)
			if len(sorted) > 0 {
				return sorted[0], nil
			}
		}
	}
	if kind == "cursor" {
		return "", &platform.ExitError{Code: 2, Msg: fmt.Sprintf("no cursor model matches '%s'; cursor-agent rejects model ids absent from --list-models (context overrides are only usable when that model/account exposes them)", spec)}
	}
	warn(fmt.Sprintf("no %s model matches '%s'; passing it through unchanged", kind, spec))
	return spec, nil
}

type cachedCodexModel struct {
	Slug   string `json:"slug"`
	Levels []struct {
		Effort string `json:"effort"`
	} `json:"supported_reasoning_levels"`
}

func codexModels(env platform.Env) []cachedCodexModel {
	var data struct {
		Models []cachedCodexModel `json:"models"`
	}
	file := filepath.Join(platform.HomeDir(platform.Current(), env), ".codex", "models_cache.json")
	b, err := os.ReadFile(file)
	if err != nil || json.Unmarshal(b, &data) != nil {
		return nil
	}
	return data.Models
}
func CodexModelCeiling(model string, env platform.Env) string {
	best, rank := "", 0
	for _, entry := range codexModels(env) {
		if entry.Slug != model {
			continue
		}
		for _, level := range entry.Levels {
			r := EffortRank(level.Effort)
			if r > rank {
				best, rank = level.Effort, r
			}
		}
	}
	return best
}
func CodexEffortCeiling(model string, env platform.Env) string {
	if ceiling := CodexModelCeiling(model, env); ceiling != "" {
		return ceiling
	}
	return "xhigh"
}
func CursorModelWithEffort(model, effort string, env platform.Env, warn WarnFunc) string {
	if warn == nil {
		warn = DefaultWarn
	}
	suffix := regexp.MustCompile("-(low|medium|high|xhigh|max|none)$").FindStringSubmatch(model)
	if suffix != nil {
		if suffix[1] == effort || effort == "" {
			return model
		}
		warn(fmt.Sprintf("cursor model '%s' already encodes effort '%s'; --effort %s ignored", model, suffix[1], effort))
		return model
	}
	r := platform.RunCli("cursor-agent", []string{"--list-models"}, platform.RunOptions{Env: env, TimeoutMs: 20000})
	ids := ParseCursorModels(r.Stdout)
	target := model + "-" + effort
	for _, id := range ids {
		if id == target {
			return target
		}
	}
	for _, id := range ids {
		if id == model {
			warn(fmt.Sprintf("cursor has no '%s'; using '%s' (effort = model default)", target, model))
			return model
		}
	}
	warn(fmt.Sprintf("cursor model '%s' not in --list-models; passing it through unchanged", model))
	return model
}

func isASCIILetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
