package codexenv

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var jsPolicySectionRE = regexp.MustCompile("^\\[([^\\]]+)\\]$")
var jsPolicyInheritRE = regexp.MustCompile("^(?:\"([^\"]*)\"|'([^']*)')")
var jsPolicyArrayStringRE = regexp.MustCompile("\"([^\"\\\\]*(?:\\\\[^\\r\\n\\x{2028}\\x{2029}][^\"\\\\]*)*)\"|'([^'\\\\]*(?:\\\\[^\\r\\n\\x{2028}\\x{2029}][^'\\\\]*)*)'")

func isJavaScriptWhitespace(r rune) bool {
	return r == '\u0009' || r == '\u000a' || r == '\u000b' || r == '\u000c' || r == '\u000d' ||
		r == '\u0020' || r == '\u00a0' || r == '\u1680' || (r >= '\u2000' && r <= '\u200a') ||
		r == '\u2028' || r == '\u2029' || r == '\u202f' || r == '\u205f' || r == '\u3000' || r == '\ufeff'
}

func trimJavaScript(s string) string {
	return strings.TrimFunc(s, isJavaScriptWhitespace)
}

func skipJavaScriptWhitespace(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isJavaScriptWhitespace(r) {
			break
		}
		i += size
	}
	return i
}

func parsePolicyKV(line string) (string, string, bool) {
	i := 0
	if len(line) == 0 {
		return "", "", false
	}
	if line[0] == '"' || line[0] == '\'' {
		quote := line[0]
		i = 1
		for i < len(line) && line[i] != quote {
			i++
		}
		if i >= len(line) || i == 1 {
			return "", "", false
		}
		i++
	} else {
		for i < len(line) && ((line[i] >= 'a' && line[i] <= 'z') || (line[i] >= 'A' && line[i] <= 'Z') ||
			(line[i] >= '0' && line[i] <= '9') || line[i] == '_' || line[i] == '-') {
			i++
		}
		if i == 0 {
			return "", "", false
		}
	}
	key := line[:i]
	i = skipJavaScriptWhitespace(line, i)
	if i >= len(line) || line[i] != '=' {
		return "", "", false
	}
	i++
	i = skipJavaScriptWhitespace(line, i)
	if strings.ContainsAny(line[i:], "\r\u2028\u2029") {
		return "", "", false
	}
	return key, line[i:], true
}

func javascriptSectionHeader(line string) bool {
	return jsPolicySectionRE.MatchString(trimJavaScript(line))
}

// ParseCodexPolicy mirrors the JavaScript parser, including its accepted and
// rejected inputs outside strict TOML syntax.
func ParseCodexPolicy(content string) *Policy {
	current := ""
	hasPolicy, hasSet := false, false
	seenPolicy, seenSet := map[string]bool{}, map[string]bool{}
	policy := &Policy{Set: []string{}}
	lines := strings.Split(content, "\n")

	for i := 0; i < len(lines); i++ {
		line := trimJavaScript(StripTomlComment(lines[i]))
		if jsPolicySectionRE.MatchString(line) {
			section := trimJavaScript(jsPolicySectionRE.FindStringSubmatch(line)[1])
			if (strings.HasPrefix(section, "\"") && strings.HasSuffix(section, "\"")) ||
				(strings.HasPrefix(section, "'") && strings.HasSuffix(section, "'")) {
				section = section[1 : len(section)-1]
			}
			switch strings.ToLower(section) {
			case "shell_environment_policy":
				if hasPolicy {
					return nil
				}
				hasPolicy, current = true, "policy"
			case "shell_environment_policy.set":
				if hasSet || seenPolicy["set"] {
					return nil
				}
				hasSet, current = true, "set"
			default:
				current = "other"
			}
			continue
		}
		if line == "" || (current != "policy" && current != "set") {
			continue
		}
		rawKey, value, ok := parsePolicyKV(line)
		if !ok {
			continue
		}
		key := rawKey
		if len(key) >= 2 && ((key[0] == '"' && key[len(key)-1] == '"') || (key[0] == '\'' && key[len(key)-1] == '\'')) {
			key = key[1 : len(key)-1]
		}
		if current == "set" {
			if seenSet[key] {
				return nil
			}
			seenSet[key] = true
			policy.Set = append(policy.Set, key)
			continue
		}

		normalized := strings.ToLower(key)
		if seenPolicy[normalized] {
			return nil
		}
		seenPolicy[normalized] = true
		value = trimJavaScript(value)
		switch normalized {
		case "inherit":
			if match := jsPolicyInheritRE.FindStringSubmatch(value); match != nil {
				parsed := match[1]
				if strings.HasPrefix(value, "'") {
					parsed = match[2]
				}
				policy.Inherit = &parsed
			} else {
				end := 0
				for end < len(value) {
					r, size := utf8.DecodeRuneInString(value[end:])
					if isJavaScriptWhitespace(r) {
						break
					}
					end += size
				}
				parsed := value[:end]
				policy.Inherit = &parsed
			}
		case "include_only", "exclude":
			var values []string
			if strings.HasPrefix(value, "[") {
				arrayText := value
				for !strings.Contains(arrayText, "]") && i+1 < len(lines) {
					if javascriptSectionHeader(StripTomlComment(lines[i+1])) {
						break
					}
					i++
					arrayText += "\n" + StripTomlComment(lines[i])
				}
				matches := jsPolicyArrayStringRE.FindAllStringSubmatch(arrayText, -1)
				values = make([]string, 0, len(matches))
				for _, match := range matches {
					if strings.HasPrefix(match[0], "\"") {
						values = append(values, match[1])
					} else {
						values = append(values, match[2])
					}
				}
			} else if match := jsPolicyInheritRE.FindStringSubmatch(value); match != nil {
				if strings.HasPrefix(value, "'") {
					values = []string{match[2]}
				} else {
					values = []string{match[1]}
				}
			}
			if normalized == "include_only" {
				policy.IncludeOnly = values
			} else {
				policy.Exclude = values
			}
		case "set":
			if hasSet {
				return nil
			}
			if strings.HasPrefix(value, "{") {
				tableText := value
				for !javascriptInlineTableClosed(tableText) && i+1 < len(lines) {
					if javascriptSectionHeader(StripTomlComment(lines[i+1])) {
						break
					}
					i++
					tableText += "\n" + StripTomlComment(lines[i])
				}
				keys, valid := parseJavaScriptInlineKeys(tableText)
				if !valid {
					return nil
				}
				for _, name := range keys {
					if seenSet[name] {
						return nil
					}
					seenSet[name] = true
					policy.Set = append(policy.Set, name)
				}
			}
		}
	}
	if !hasPolicy && !hasSet {
		return nil
	}
	return policy
}

func javascriptInlineTableClosed(tableText string) bool {
	depth := 0
	quote := byte(0)
	for i := 0; i < len(tableText); i++ {
		ch := tableText[i]
		if quote != 0 {
			if quote == '"' && ch == '\\' {
				i++
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				return true
			}
		}
	}
	return false
}

func parseJavaScriptInlineKeys(tableText string) ([]string, bool) {
	trimmed := trimJavaScript(tableText)
	if !strings.HasPrefix(trimmed, "{") || !strings.HasSuffix(trimmed, "}") {
		return nil, false
	}
	inner := trimJavaScript(trimmed[1 : len(trimmed)-1])
	if inner == "" {
		return []string{}, true
	}
	keys := []string{}
	seen := map[string]bool{}
	for pos := 0; pos < len(inner); {
		for pos < len(inner) && (inner[pos] == ',' || skipJavaScriptWhitespace(inner, pos) != pos) {
			if inner[pos] == ',' {
				pos++
			} else {
				pos = skipJavaScriptWhitespace(inner, pos)
			}
		}
		if pos >= len(inner) {
			break
		}

		start := pos
		key := ""
		if inner[pos] == '"' || inner[pos] == '\'' {
			quote := inner[pos]
			pos++
			for pos < len(inner) && inner[pos] != quote {
				if inner[pos] == '\\' && pos+1 < len(inner) {
					pos++
				}
				key += inner[pos : pos+1]
				pos++
			}
			if pos >= len(inner) {
				return nil, false
			}
			pos++
		} else {
			for pos < len(inner) && ((inner[pos] >= 'a' && inner[pos] <= 'z') || (inner[pos] >= 'A' && inner[pos] <= 'Z') ||
				(inner[pos] >= '0' && inner[pos] <= '9') || inner[pos] == '_' || inner[pos] == '-') {
				pos++
			}
			key = inner[start:pos]
		}
		key = trimJavaScript(key)
		if key == "" || seen[key] {
			return nil, false
		}
		seen[key] = true
		keys = append(keys, key)
		pos = skipJavaScriptWhitespace(inner, pos)
		if pos >= len(inner) || inner[pos] != '=' {
			return nil, false
		}
		pos++

		inDouble, inSingle, braceDepth, bracketDepth := false, false, 0, 0
		for pos < len(inner) {
			ch := inner[pos]
			if ch == '"' && !inSingle && (pos == 0 || inner[pos-1] != '\\') {
				inDouble = !inDouble
			} else if ch == '\'' && !inDouble {
				inSingle = !inSingle
			} else if !inDouble && !inSingle {
				switch ch {
				case '{':
					braceDepth++
				case '}':
					braceDepth--
				case '[':
					bracketDepth++
				case ']':
					bracketDepth--
				case ',':
					if braceDepth == 0 && bracketDepth == 0 {
						pos++
						goto nextInlineKey
					}
				}
			}
			pos++
		}
		return keys, true
	nextInlineKey:
	}
	return keys, true
}
