package provider

import (
	"regexp"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/djalmajr/herdr-soho/internal/text"
)

var (
	codexQuestionRE         = regexp.MustCompile(`enter to submit answer|enter to submit all`)
	claudeQuestionRE        = regexp.MustCompile(`enter to select`)
	claudeQuestionExtraRE   = regexp.MustCompile(`to navigate|submit answers`)
	openCodeQuestionRE      = regexp.MustCompile(`esc dismiss`)
	openCodeQuestionExtraRE = regexp.MustCompile(`enter submit|enter toggle`)
	claudeNotQuestionRE     = regexp.MustCompile(`do you want to`)
	openCodeNotQuestionRE   = regexp.MustCompile(`permission required`)
)

// DialogKind classifies a stopped dialog as a question or approval.
func DialogKind(kind, screen string) string {
	textScreen := text.ASCIILower(screen)
	switch kind {
	case "codex":
		if codexQuestionRE.MatchString(textScreen) {
			return "question"
		}
	case "claude":
		if !claudeNotQuestionRE.MatchString(textScreen) && claudeQuestionRE.MatchString(textScreen) && claudeQuestionExtraRE.MatchString(textScreen) {
			return "question"
		}
	case "opencode":
		if !openCodeNotQuestionRE.MatchString(textScreen) && openCodeQuestionRE.MatchString(textScreen) && openCodeQuestionExtraRE.MatchString(textScreen) {
			return "question"
		}
	}
	return "approval"
}

// QuestionText returns the last 20 non-empty trimmed lines, redacted and capped at 1200 UTF-16 units.
// It may contain a lone UTF-16 surrogate encoded as WTF-8 for jsonjs; file writers must call text.ToWellFormedUTF8.
func QuestionText(screen string) string {
	lines := strings.Split(strings.ReplaceAll(screen, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRightFunc(line, isJSWhitespace)
		if line != "" {
			kept = append(kept, text.RedactSecrets(line))
		}
	}
	if len(kept) > 20 {
		kept = kept[len(kept)-20:]
	}
	joined := strings.Join(kept, "\n")
	units := utf16.Encode([]rune(joined))
	if len(units) <= 1200 {
		return joined
	}
	units = units[:1200]
	return encodeUTF16(units)
}

func encodeUTF16(units []uint16) string {
	var out []byte
	for i := 0; i < len(units); i++ {
		unit := units[i]
		if 0xd800 <= unit && unit <= 0xdbff && i+1 < len(units) && 0xdc00 <= units[i+1] && units[i+1] <= 0xdfff {
			r := rune(0x10000 + (uint32(unit-0xd800) << 10) + uint32(units[i+1]-0xdc00))
			out = utf8.AppendRune(out, r)
			i++
		} else if 0xd800 <= unit && unit <= 0xdfff {
			out = append(out, 0xe0|byte(unit>>12), 0x80|byte((unit>>6)&0x3f), 0x80|byte(unit&0x3f))
		} else {
			out = utf8.AppendRune(out, rune(unit))
		}
	}
	return string(out)
}
