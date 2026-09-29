package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/djalmajr/herdr-soho/internal/jsonjs"
	"github.com/djalmajr/herdr-soho/internal/platform"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

const EditRoles = "implementer designer tasker"
const ReviewRoles = "reviewer security-reviewer"
const ReviewRolesAll = "reviewer security-reviewer ui-reviewer inspector"

var repeatedSpacesRE = regexp.MustCompile(` +`)

func RoleDirs(env platform.Env, cwd string) []string {
	dirs := make([]string, 0, 3)
	project := filepath.Join(platform.ProjectRoot(env, cwd), ".agents", "herdr-roles")
	if isDir(project) {
		dirs = append(dirs, project)
	}
	if extra := env.Get("HERDR_SOHO_ROLES"); extra != "" && isDir(extra) {
		dirs = append(dirs, extra)
	}
	dirs = append(dirs, filepath.Join(platform.SkillDir(env), "roles"))
	return dirs
}

func RoleFile(role string, env platform.Env, cwd string) string {
	for _, dir := range RoleDirs(env, cwd) {
		file := filepath.Join(dir, role+".md")
		if isFile(file) {
			return file
		}
	}
	return ""
}

func ResolveRole(role string, env platform.Env, cwd string) string {
	if file := RoleFile(role, env, cwd); file != "" {
		return file
	}
	platform.Die(fmt.Sprintf("unknown role '%s' (run: herdr-soho roles)", role), 3)
	return ""
}

func isDir(path string) bool { info, err := os.Stat(path); return err == nil && info.IsDir() }
func isFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func FmGet(file, key string) string {
	text, err := platform.ReadTextFile(file)
	if err != nil {
		return ""
	}
	lines := splitLines(text)
	if len(lines) == 0 || lines[0] != "---" {
		return ""
	}
	for _, line := range lines[1:] {
		if line == "---" {
			break
		}
		colon := strings.IndexByte(line, ':')
		if colon < 0 || trimJSWhitespace(line[:colon]) != key {
			continue
		}
		value := trimJSWhitespace(line[colon+1:])
		if strings.HasPrefix(value, "[") {
			value = value[1:]
		}
		if strings.HasSuffix(value, "]") {
			value = value[:len(value)-1]
		}
		value = strings.ReplaceAll(value, ",", " ")
		value = strings.ReplaceAll(value, `"`, "")
		return repeatedSpacesRE.ReplaceAllString(value, " ")
	}
	return ""
}

func RoleBody(file string) string {
	text, err := platform.ReadTextFile(file)
	if err != nil {
		return ""
	}
	lines := splitLines(text)
	if len(lines) == 0 {
		return ""
	}
	if lines[0] != "---" {
		return strings.Join(lines, "\n") + "\n"
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] == "---" {
			if i+1 == len(lines) {
				return ""
			}
			return strings.Join(lines[i+1:], "\n") + "\n"
		}
	}
	return ""
}

func RoleIsEdit(role string, env platform.Env, cwd string) bool {
	if hasWord(EditRoles, role) {
		return true
	}
	file := RoleFile(role, env, cwd)
	return file != "" && FmGet(file, "mode") == "edit"
}

func HistoryHasEdit(history string, env platform.Env, cwd string) bool {
	for _, role := range strings.Split(history, ",") {
		if RoleIsEdit(strings.TrimSpace(role), env, cwd) {
			return true
		}
	}
	return false
}

func IsReviewRole(role string) bool { return hasWord(ReviewRolesAll, role) }

func hasWord(list, needle string) bool {
	for _, word := range strings.Fields(list) {
		if word == needle {
			return true
		}
	}
	return false
}

func CmdRole(argv []string, env platform.Env, cwd string) {
	if len(argv) == 0 {
		_, _ = platform.Stderr.Write([]byte("herdr-soho.mjs: 1: role\n"))
		panic(&platform.ExitError{Code: 1})
	}
	f := ResolveRole(argv[0], env, cwd)
	alt := []string{}
	for _, value := range strings.Split(FmGet(f, "alternatives"), " ") {
		if value != "" {
			alt = append(alt, value)
		}
	}
	role := jsonjs.O("file", f, "name", FmGet(f, "name"), "kind", FmGet(f, "kind"), "alternatives", stringsToAny(alt), "mode", FmGet(f, "mode"), "timeout", FmGet(f, "timeout"), "effort", FmGet(f, "effort"), "model", FmGet(f, "model"), "approvals", FmGet(f, "approvals"), "description", FmGet(f, "description"))
	_, _ = fmt.Fprintln(platform.Stdout, jsonjs.StringifyIndent(role, 2))
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, value := range values {
		out[i] = value
	}
	return out
}

func CmdRoles(env platform.Env, cwd string) {
	ctx := LoadConfig(env, cwd)
	lines := []string{padUTF16("ROLE", 18) + " " + padUTF16("KIND", 8) + " " + padUTF16("MODEL", 24) + " " + padUTF16("EFFORT", 8) + " " + padUTF16("MODE", 10) + " FROM"}
	seen := map[string]bool{}
	for _, dir := range RoleDirs(env, cwd) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".md") {
				names = append(names, entry.Name())
			}
		}
		textutil.SortUTF16(names)
		for _, name := range names {
			file := filepath.Join(dir, name)
			if !isFile(file) {
				continue
			}
			role := strings.TrimSuffix(name, ".md")
			if seen[role] {
				continue
			}
			seen[role] = true
			res := ResolveRoleSettings(role, &ctx, env, cwd, RoleFlags{})
			cell := func(value string) string {
				if value == "" {
					return "-"
				}
				return value
			}
			from := fmt.Sprintf("kind: %s; model: %s; effort: %s; file: %s", res.KindFrom, res.ModelFrom, res.EffortFrom, file)
			lines = append(lines, padUTF16(role, 18)+" "+padUTF16(cell(res.Kind), 8)+" "+padUTF16(cell(res.ModelSpec), 24)+" "+padUTF16(cell(res.Effort), 8)+" "+padUTF16(cell(FmGet(file, "mode")), 10)+" "+from)
		}
	}
	_, _ = fmt.Fprintln(platform.Stdout, strings.Join(lines, "\n"))
}
