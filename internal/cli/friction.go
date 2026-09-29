package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/platform"
)

func cmdFriction(argv []string, ctx *core.Config, env platform.Env, cwd string) {
	sub, brief := "", ""
	words := []string{}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--brief":
			if i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
				platform.DieFriction("friction add: --brief expects a path", 2)
			}
			brief = argv[i+1]
			i++
		case strings.HasPrefix(a, "--"):
			platform.DieFriction("friction: unknown option "+a, 2)
		case sub == "" && len(words) == 0:
			sub = a
		default:
			words = append(words, a)
		}
	}
	sd := core.StateDir(ctx, env, cwd)
	logPath := filepath.Join(sd, "friction.log")
	if sub == "" {
		st, err := os.Stat(logPath)
		if err != nil || st.Size() == 0 {
			fmt.Fprintf(platform.Stdout, "no friction recorded under %s\n", logPath)
			return
		}
		fmt.Fprintf(platform.Stdout, "friction log (%s): timestamp, level, command, message\n", logPath)
		if text, readErr := platform.ReadTextFile(logPath); readErr == nil {
			_, _ = platform.Stdout.Write([]byte(text))
		}
		return
	}
	if sub != "add" {
		platform.DieFriction(fmt.Sprintf("friction: unknown subcommand '%s'", sub), 2)
	}
	message := strings.Join(words, " ")
	if message == "" {
		platform.DieFriction("friction add: give the text to record", 2)
	}
	if brief != "" {
		message += " (brief: " + brief + ")"
	}
	line := fmt.Sprintf("%s\tnote\tfriction\t%s\n", core.NowISO(platform.Now()), core.FrictionSafe(message))
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		platform.DieFriction(fmt.Sprintf("friction add: could not write %s (%v)", logPath, err), 4)
	}
	_, writeErr := f.WriteString(line)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		platform.DieFriction(fmt.Sprintf("friction add: could not write %s", logPath), 4)
	}
	fmt.Fprintln(platform.Stdout, "recorded")
}
