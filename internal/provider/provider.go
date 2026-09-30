package provider

import (
	"regexp"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/text"
)

type Detection struct {
	Status string `json:"status"`
	Cause  string `json:"cause"`
	Auth   bool   `json:"auth"`
}

var (
	retryingRE       = regexp.MustCompile(`retrying|retry in|will retry|reconnecting`)
	econnRE          = regexp.MustCompile(`\bECONN(REFUSED|RESET)\b`)
	providerStatusRE = regexp.MustCompile(`\b(500|502|503|504)\b` + jsWhitespace + `*[:{(]`)
	capacity529RE    = regexp.MustCompile(`\b529\b`)
	auth401RE        = regexp.MustCompile(`\b401\b`)
	errorStartRE     = regexp.MustCompile(`^` + jsWhitespace + `*([■✗✘×⚠●•⎿]` + jsWhitespace + `+)?(API Error|ERROR|Error)[: (]`)
	errorGlyphRE     = regexp.MustCompile(`^` + jsWhitespace + `*■` + jsWhitespace)
	outputBulletRE   = regexp.MustCompile(`^` + jsWhitespace + `*[•●⏺✓✔]` + jsWhitespace)
	capacityRes      = []*regexp.Regexp{
		regexp.MustCompile(`"type"` + jsWhitespace + `*:` + jsWhitespace + `*"[a-z0-9_]*(capacity|overload)[a-z0-9_]*"`),
		regexp.MustCompile(`\boverloaded\b`),
		regexp.MustCompile(`\b(at|over) capacity\b`),
	}
	providerRes = []*regexp.Regexp{
		regexp.MustCompile(`request timed out`),
		regexp.MustCompile(`connection error`),
		regexp.MustCompile(`connection (refused|reset)`),
		regexp.MustCompile(`retry failed after \d+ attempts?`),
		regexp.MustCompile(`\b(internal server error|bad gateway|service unavailable|gateway time-?out)\b`),
		regexp.MustCompile(`unexpected status`),
		regexp.MustCompile(`stream disconnected before completion`),
		regexp.MustCompile(`socket hang up`),
		regexp.MustCompile(`fetch failed`),
	}
	authRes = []*regexp.Regexp{
		regexp.MustCompile(`unexpected status[^\r\x{2028}\x{2029}]*401|401[^\r\x{2028}\x{2029}]*unexpected status`),
		regexp.MustCompile(`incorrect api key`),
		regexp.MustCompile(`refresh token[^\r\x{2028}\x{2029}]*revok|revok[^\r\x{2028}\x{2029}]*refresh token`),
		regexp.MustCompile(`failed to refresh[^\r\x{2028}\x{2029}]*token`),
	}
)

// ProviderDetect returns the most recent provider stop from the bottom ten non-empty lines.
func ProviderDetect(state, screen string) *Detection {
	return ProviderDetectTexts(state, screen, "", "")
}

// ProviderDetectTexts is ProviderDetect with the user-configured capacity and
// error texts (pipe-separated): a line containing one of them, case-insensitively
// and without the leading box prefix and spaces, is a capacity or
// provider-error stop even without the Error prefix. The existing rules win;
// an empty text changes nothing.
func ProviderDetectTexts(state, screen, capacityTexts, errorTexts string) *Detection {
	if state == "working" || screen == "" {
		return nil
	}
	capacity := splitTexts(capacityTexts)
	errTexts := splitTexts(errorTexts)
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		if !isJSEmpty(line) {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) > 10 {
		nonEmpty = nonEmpty[len(nonEmpty)-10:]
	}
	for i := len(nonEmpty) - 1; i >= 0; i-- {
		line := nonEmpty[i]
		lowerLine := text.ASCIILower(line)
		if retryingRE.MatchString(lowerLine) || QuotaLineIsCode(line) {
			continue
		}
		if outputBulletRE.MatchString(line) && !errorStartRE.MatchString(line) {
			return nil
		}
		if errorGlyphRE.MatchString(line) || errorStartRE.MatchString(line) {
			cause := text.SanitizeCause(text.RedactSecrets(line))
			if matchesAny(capacityRes, lowerLine) || capacity529RE.MatchString(line) {
				return &Detection{Status: "capacity", Cause: cause}
			}
			if auth401RE.MatchString(line) || matchesAny(authRes, lowerLine) {
				return &Detection{Status: "provider-error", Cause: cause, Auth: true}
			}
			if matchesAny(providerRes, lowerLine) || providerStatusRE.MatchString(line) || econnRE.MatchString(line) {
				return &Detection{Status: "provider-error", Cause: cause}
			}
		}
		stripped := configuredTextLine(line)
		if equalsText(stripped, capacity) {
			return &Detection{Status: "capacity", Cause: text.SanitizeCause(text.RedactSecrets(line))}
		}
		if equalsText(stripped, errTexts) {
			return &Detection{Status: "provider-error", Cause: text.SanitizeCause(text.RedactSecrets(line))}
		}
	}
	return nil
}

func stripBoxPrefix(line string) string {
	value := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(value, "┃") {
		return strings.TrimLeft(value[len("┃"):], " \t")
	}
	return value
}

func splitTexts(value string) []string {
	pieces := []string{}
	for _, piece := range strings.Split(value, "|") {
		piece = text.ASCIILower(strings.TrimSpace(piece))
		if piece != "" {
			pieces = append(pieces, piece)
		}
	}
	return pieces
}

// configuredTextLine is the line as a configured text must equal it: without a
// full-screen TUI's box prefix, an error glyph or an `Error:`-style label, the
// surrounding spaces and a final period, lowercased. The whole line must be the
// text: a sentence that only quotes it (the worker writing about the error, a
// brief on screen) is not the provider's answer.
func configuredTextLine(line string) string {
	value := stripBoxPrefix(line)
	if loc := errorStartRE.FindStringIndex(value); loc != nil && strings.HasSuffix(value[:loc[1]], ":") {
		value = value[loc[1]:]
	} else if errorGlyphRE.MatchString(value) {
		value = strings.TrimLeft(value, " \t■")
	}
	value = strings.TrimSuffix(strings.TrimSpace(value), ".")
	return text.ASCIILower(strings.TrimSpace(value))
}

func equalsText(value string, pieces []string) bool {
	for _, piece := range pieces {
		if value == strings.TrimSuffix(piece, ".") {
			return true
		}
	}
	return false
}

func matchesAny(patterns []*regexp.Regexp, value string) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(value) {
			return true
		}
	}
	return false
}
