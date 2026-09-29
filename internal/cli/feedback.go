package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"

	"github.com/djalmajr/herdr-soho/internal/core"
	"github.com/djalmajr/herdr-soho/internal/herdr"
	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	herdrtext "github.com/djalmajr/herdr-soho/internal/text"
)

func cmdFeedback(argv []string, ctx *core.Config, env platform.Env, cwd string) int {
	sub := ""
	words := []string{}
	for _, arg := range argv {
		if strings.HasPrefix(arg, "--") {
			platform.DieFriction("feedback: unknown option "+arg, 2)
		}
		if sub == "" && len(words) == 0 {
			sub = arg
		} else {
			words = append(words, arg)
		}
	}
	if sub != "send" {
		platform.DieFriction(fmt.Sprintf("feedback: unknown subcommand '%s'", sub), 2)
	}
	if len(words) != 2 {
		platform.DieFriction("usage: feedback send <report.md> \"<one-line summary>\"", 2)
	}
	report, rawSummary := words[0], words[1]
	if rawSummary == "" {
		platform.DieFriction("feedback send: the one-line summary is required", 2)
	}
	summary := core.FrictionSafe(rawSummary)
	policy := core.Cfg(ctx, "feedback", "ask", env)
	if policy != "local" {
		platform.DieFriction(fmt.Sprintf("feedback send: feedback=%s, not local; file an issue instead (see \"Improving this skill\")", policy), 2)
	}
	dir := core.Cfg(ctx, "feedback_dir", "", env)
	info, statErr := os.Stat(dir)
	if !filepath.IsAbs(dir) || statErr != nil || !info.IsDir() {
		platform.DieFriction("feedback send: feedback_dir is empty, relative or not a directory; set feedback_dir to the maintainer's directory (absolute path)", 2)
	}
	reportInfo, err := os.Stat(report)
	if err != nil || !reportInfo.Mode().IsRegular() || reportInfo.Size() == 0 {
		platform.DieFriction(fmt.Sprintf("feedback send: the report must be a non-empty file: %s", report), 2)
	}
	content, err := os.ReadFile(report)
	if err != nil {
		platform.DieFriction(fmt.Sprintf("feedback send: the report must be a non-empty file: %s", report), 2)
	}
	project := feedbackProjectName(filepath.Base(platform.ProjectRoot(env, cwd)))
	now := platform.Now()
	base := fmt.Sprintf("from-%s-%04d-%02d-%02d", project, now.Year(), now.Month(), now.Day())
	dest := ""
	for i := 0; i < 26; i++ {
		name := base + ".md"
		if i > 0 {
			name = fmt.Sprintf("%s-%c.md", base, rune('a'+i))
		}
		dest = filepath.Join(dir, name)
		f, openErr := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if openErr != nil {
			if os.IsExist(openErr) && i < 25 {
				continue
			}
			code := feedbackOpenErrorCode(openErr)
			platform.DieFriction(fmt.Sprintf("feedback send: could not write %s (%s)", dest, code), 4)
		}
		_, writeErr := f.Write([]byte(herdrtext.ToWellFormedUTF8(string(content))))
		closeErr := f.Close()
		if writeErr != nil || closeErr != nil {
			platform.DieFriction(fmt.Sprintf("feedback send: could not write %s (EIO)", dest), 4)
		}
		break
	}
	to := core.Cfg(ctx, "feedback_to", "", env)
	if to != "" {
		message := fmt.Sprintf("herdr-soho feedback from %s (%s): %s — %s", project, core.WorkspaceID(ctx, env, cwd), summary, dest)
		result := herdr.AgentPrompt(to, message, env)
		if !result.Ok {
			core.Warn(fmt.Sprintf("feedback send: the notice to %s failed: %s", to, result.Raw), frictionLogPath, "feedback")
			_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(jsonjs.O("status", "filed", "file", dest, "notified", nil, "error", result.Raw)))
			return 4
		}
	}
	sd := core.StateDir(ctx, env, cwd)
	log := filepath.Join(sd, "friction.log")
	line := fmt.Sprintf("%s\tnote\tfeedback\t%s\n", core.NowISO(now), core.FrictionSafe("feedback sent: "+dest))
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.WriteString(line)
		_ = f.Close()
	}
	if err != nil {
		platform.DieFriction(fmt.Sprintf("feedback send: could not write %s", log), 4)
	}
	var notified any
	if to != "" {
		notified = to
	}
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.Stringify(jsonjs.O("status", "sent", "file", dest, "notified", notified)))
	return 0
}

func feedbackOpenErrorCode(err error) string {
	if errors.Is(err, fs.ErrExist) {
		return "EEXIST"
	}
	if errors.Is(err, fs.ErrPermission) {
		return "EACCES"
	}
	return err.Error()
}

func feedbackProjectName(value string) string {
	units := utf16.Encode([]rune(value))
	var out strings.Builder
	for _, unit := range units {
		if (unit >= 'A' && unit <= 'Z') || (unit >= 'a' && unit <= 'z') || (unit >= '0' && unit <= '9') || unit == '.' || unit == '_' || unit == '-' {
			out.WriteRune(rune(unit))
		} else {
			out.WriteByte('-')
		}
	}
	return out.String()
}
