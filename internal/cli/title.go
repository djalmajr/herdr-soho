package cli

import (
	"fmt"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func normalizeObjective(value string) string {
	value = strings.NewReplacer("\r", " ", "\t", " ", "\n", " ").Replace(value)
	value = strings.TrimFunc(value, isJSTrimSpace)
	runes := []rune(value)
	if len(runes) > 60 {
		value = string(runes[:60])
	}
	return value
}

func isJSTrimSpace(r rune) bool {
	return r == '\u0009' || r == '\u000a' || r == '\u000b' || r == '\u000c' || r == '\u000d' || r == '\u0020' || r == '\u00a0' || r == '\u1680' || (r >= '\u2000' && r <= '\u200a') || r == '\u2028' || r == '\u2029' || r == '\u202f' || r == '\u205f' || r == '\u3000' || r == '\ufeff'
}

func cmdTitle(argv []string, env platform.Env) {
	clear := false
	rest := []string{}
	for _, arg := range argv {
		if arg == "--clear" {
			clear = true
		} else if strings.HasPrefix(arg, "--") {
			core.DieFriction("title: unknown option "+arg, 2, frictionLogPath, "title")
		} else {
			rest = append(rest, arg)
		}
	}
	pane := env.Get("HERDR_PANE_ID")
	var title *string
	if clear {
		if len(rest) > 0 {
			core.DieFriction("title: --clear takes no objective", 2, frictionLogPath, "title")
		}
		title = nil
	} else {
		objective := normalizeObjective(strings.Join(rest, " "))
		if objective == "" {
			core.DieFriction("title: give the current objective, or --clear", 2, frictionLogPath, "title")
		}
		value := "orchestrator: " + objective
		title = &value
	}
	if !herdr.PaneTitle(pane, title, env) {
		core.DieFriction("title: herdr pane report-metadata failed", 4, frictionLogPath, "title")
	}
	value := ""
	if title != nil {
		value = *title
	}
	out := jsonjs.StringifyIndent(jsonjs.O("pane_id", pane, "title", value), 2)
	_, _ = fmt.Fprintln(platform.Stdout, out)
}
