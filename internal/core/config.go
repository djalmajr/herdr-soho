package core

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/djalmajr/herdr-soho/internal/platform"
	textutil "github.com/djalmajr/herdr-soho/internal/text"
)

var writeConfigAtomic = platform.AtomicWrite

type ConfigEntry struct {
	Value    string
	Source   string
	Original string
}

// Config records the normalized key values and preserves first insertion order.
type Config struct {
	Entries map[string]ConfigEntry
	Order   []string
	Sources []string
}

var ConfigScalarKeys = []string{
	"orchestrator_name", "layout", "regrid", "max_workers", "split_max_panes",
	"split_min_pane", "herd_label", "herd_label_max", "reuse_workers",
	"multi_role", "panes", "lanes", "pane_mode", "flex_extra", "flex_roles",
	"worker_context", "brief_lint", "brief_lint_aliases", "brief_lint_placeholders", "approvals",
	"auto_approve", "max_auto_approvals", "max_effort", "family_check",
	"settled_grace", "spawn_timeout", "dispatch_timeout", "provider_retries",
	"provider_retry_delay", "provider_capacity_texts", "provider_error_texts",
	"prompt_check_seconds", "prompt_settle_seconds", "stuck_warn_minutes", "context_warn_percent", "state_dir",
	"report_language", "notify", "feedback", "feedback_repo", "feedback_dir", "feedback_to",
	"setup_target", "inbound", "metrics",
}

var KnownKinds = []string{"claude", "codex", "grok", "agy", "gemini", "cursor", "pi", "opencode"}
var EffortLadder = []string{"low", "medium", "high", "xhigh", "max"}

var dottedKeyRE = regexp.MustCompile(`^(?:role\.[a-z][a-z0-9_-]*\.(?:kind|model|effort|args)|lane\.[a-z][a-z0-9_-]*\.(?:roles|kind|model|effort|approvals|panes|args)|model\.[a-z][a-z0-9_.-]+|effort\.[a-z][a-z0-9_-]+|args\.[a-z][a-z0-9_-]+)$`)
var decimalRE = regexp.MustCompile(`^[0-9]+$`)
var positiveDecimalRE = regexp.MustCompile(`^[1-9][0-9]*$`)

func NormalizeKey(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if isJSWhitespace(r) {
			continue
		}
		if r == '.' || r == '-' {
			b.WriteByte('_')
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isJSWhitespace(r rune) bool {
	return r == '\t' || r == '\n' || r == '\v' || r == '\f' || r == '\r' || r == ' ' || unicode.Is(unicode.Zs, r) || r == '\u2028' || r == '\u2029' || r == '\ufeff'
}

func LoadConfig(env platform.Env, cwd string) Config {
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	ctx := Config{Entries: make(map[string]ConfigEntry), Order: make([]string, 0), Sources: make([]string, 0, 4)}
	loadConfigFile(filepath.Join(platform.SkillDir(env), "config.defaults"), "defaults", &ctx)
	loadConfigFile(EffectiveConfigFile(platform.UserConfigPath(platform.Current(), env), LegacyUserConfigPath(platform.Current(), env)), "user", &ctx)
	loadConfigFile(ProjectConfigFileUsed(env, cwd), "project", &ctx)
	if file := SessionConfPath(&ctx, env, cwd); file != "" {
		loadConfigFile(file, "session", &ctx)
	}
	return ctx
}

func loadConfigFile(file, label string, ctx *Config) {
	raw, err := platform.ReadTextFile(file)
	if err != nil {
		return
	}
	ctx.Sources = append(ctx.Sources, label)
	for _, rawLine := range splitLines(raw) {
		if hash := strings.IndexByte(rawLine, '#'); hash >= 0 {
			rawLine = rawLine[:hash]
		}
		line := trimJSWhitespace(rawLine)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		rawKey := trimJSWhitespace(line[:eq])
		key := NormalizeKey(rawKey)
		value := trimJSWhitespace(line[eq+1:])
		if len(value) >= 2 && strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			value = value[1 : len(value)-1]
		}
		if key == "" {
			continue
		}
		if _, exists := ctx.Entries[key]; !exists {
			ctx.Order = append(ctx.Order, key)
		}
		ctx.Entries[key] = ConfigEntry{Value: value, Source: label, Original: rawKey}
	}
}

func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	if strings.HasSuffix(text, "\n") {
		text = text[:len(text)-1]
	}
	return strings.Split(text, "\n")
}

func trimJSWhitespace(value string) string {
	return strings.TrimFunc(value, isJSWhitespace)
}

func Cfg(ctx *Config, key, fallback string, env platform.Env) string {
	if value := env.Get("HERDR_SOHO_" + strings.ToUpper(key)); value != "" {
		return value
	}
	if entry, ok := ctx.Entries[key]; ok && entry.Value != "" {
		return entry.Value
	}
	return fallback
}

func CfgSource(ctx *Config, key string, env platform.Env) string {
	if env.Get("HERDR_SOHO_"+strings.ToUpper(key)) != "" {
		return "env"
	}
	if entry, ok := ctx.Entries[key]; ok {
		return entry.Source
	}
	return "builtin"
}

func ConfigKeyOk(key string) bool {
	for _, scalar := range ConfigScalarKeys {
		if scalar == key {
			return true
		}
	}
	return dottedKeyRE.MatchString(key)
}

func ConfigRolesOk(raw string, env platform.Env, cwd string) bool {
	if raw == "" {
		return false
	}
	for _, role := range strings.Split(raw, ",") {
		role = trimJSWhitespace(role)
		if role == "" || RoleFile(role, env, cwd) == "" {
			return false
		}
	}
	return true
}

func ConfigValueOk(key, value string, env platform.Env, cwd string) bool {
	if strings.ContainsAny(value, "\n\t#") {
		return false
	}
	switch {
	case key == "approvals" || strings.HasPrefix(key, "lane.") && strings.HasSuffix(key, ".approvals"):
		return value == "ask" || value == "edits" || value == "full"
	case key == "max_workers" || key == "provider_retries" || key == "provider_retry_delay" || key == "prompt_check_seconds" || key == "prompt_settle_seconds" || key == "stuck_warn_minutes":
		return decimalRE.MatchString(value)
	case key == "multi_role" || key == "reuse_workers" || key == "lanes":
		return value == "on" || value == "off"
	case key == "setup_target":
		return value == "canonical" || value == "local"
	case key == "inbound":
		return value == "auto" || value == "off"
	case key == "panes":
		return value == "2" || value == "3" || value == "4"
	case key == "pane_mode":
		return value == "strict" || value == "flex"
	case key == "flex_extra":
		return decimalRE.MatchString(value)
	case key == "flex_roles":
		return ConfigRolesOk(value, env, cwd)
	case strings.HasPrefix(key, "lane.") && strings.HasSuffix(key, ".panes"):
		return positiveDecimalRE.MatchString(value)
	case strings.HasPrefix(key, "role.") && strings.HasSuffix(key, ".kind"), strings.HasPrefix(key, "lane.") && strings.HasSuffix(key, ".kind"):
		return contains(KnownKinds, value)
	case strings.HasPrefix(key, "lane.") && strings.HasSuffix(key, ".roles"):
		return ConfigRolesOk(value, env, cwd)
	case strings.HasPrefix(key, "lane.") && strings.HasSuffix(key, ".effort"):
		return contains(EffortLadder, value)
	default:
		return true
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

// ProjectConfigFileUsed returns the file the project layer reads for cwd: the
// checkout's effective config (new, else legacy) when it exists, or, when
// cwd is a linked worktree without one, the main checkout's (the
// StateProjectRoot root's) effective config; "" when no project config
// exists, so the layer contributes nothing.
func ProjectConfigFileUsed(env platform.Env, cwd string) string {
	root := platform.ProjectRoot(env, cwd)
	if file := EffectiveConfigFile(filepath.Join(root, ".agents", "herdr-soho.conf"), LegacyProjectConfigPath(root)); isFile(file) {
		return file
	}
	stateRoot := platform.StateProjectRoot(env, cwd)
	if stateRoot == root {
		return ""
	}
	main := EffectiveConfigFile(filepath.Join(stateRoot, ".agents", "herdr-soho.conf"), LegacyProjectConfigPath(stateRoot))
	if isFile(main) {
		return main
	}
	return ""
}

func ConfigFileFor(where string, env platform.Env, cwd string) string {
	if where == "user" {
		return platform.UserConfigPath(platform.Current(), env)
	}
	root := platform.ProjectRoot(env, cwd)
	newProject := filepath.Join(root, ".agents", "herdr-soho.conf")
	if isFile(newProject) || isFile(LegacyProjectConfigPath(root)) {
		return newProject
	}
	stateRoot := platform.StateProjectRoot(env, cwd)
	if stateRoot == root {
		return newProject
	}
	return EffectiveConfigFile(filepath.Join(stateRoot, ".agents", "herdr-soho.conf"), LegacyProjectConfigPath(stateRoot))
}

func StateRootPath(ctx *Config, env platform.Env, cwd string) string {
	root := platform.StateProjectRoot(env, cwd)
	dir := env.Get("HERDR_SOHO_DIR")
	if dir == "" {
		dir = StateDirSetting(ctx, env, cwd)
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Clean(dir)
}

func StateDirSetting(ctx *Config, env platform.Env, cwd string) string {
	if value := env.Get("HERDR_SOHO_STATE_DIR"); value != "" {
		return value
	}
	if entry, ok := ctx.Entries["state_dir"]; ok && entry.Source != "defaults" && entry.Value != "" {
		return entry.Value
	}
	return DefaultStateDirName(platform.StateProjectRoot(env, cwd))
}

func Nowrite(env platform.Env) bool { return env.Get("HERDR_SOHO_NOWRITE") == "1" }

func RelativeStatePath(root, statePath string) string {
	rel, err := filepath.Rel(root, statePath)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return rel
}

func StateGitignoreRel(root, statePath string) string {
	return filepath.ToSlash(RelativeStatePath(root, statePath))
}

func StateRoot(ctx *Config, env platform.Env, cwd string) string {
	root := platform.StateProjectRoot(env, cwd)
	dir := StateRootPath(ctx, env, cwd)
	rel := StateGitignoreRel(root, dir)
	if rel != "" && !Nowrite(env) {
		result := platform.RunCli("git", []string{"-C", root, "rev-parse", "--is-inside-work-tree"}, platform.RunOptions{Env: env, Cwd: root})
		if result.Status != nil && *result.Status == 0 && GitignoreNeeds(root, rel, env) {
			file := filepath.Join(root, ".gitignore")
			text, err := platform.ReadTextFile(file)
			if err != nil {
				text = ""
			}
			appendText := GitignoreAfter(text, rel)[len(text):]
			if appendText != "" {
				f, err := os.OpenFile(file, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o666)
				if err != nil {
					panic(err)
				}
				if _, err := f.WriteString(appendText); err != nil {
					_ = f.Close()
					panic(err)
				}
				if err := f.Close(); err != nil {
					panic(err)
				}
			}
		}
	}
	return dir
}

func GitignoreNeeds(root, rel string, env platform.Env) bool {
	result := platform.RunCli("git", []string{"-C", root, "check-ignore", "-q", rel}, platform.RunOptions{Env: env, Cwd: root, TimeoutMs: 30_000})
	if result.Status == nil || *result.Status != 1 {
		return false
	}
	text, err := platform.ReadTextFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return true
	}
	names := map[string]bool{rel: true, rel + "/": true, "/" + rel: true, "/" + rel + "/": true}
	for _, line := range strings.Split(text, "\n") {
		if names[strings.TrimSuffix(line, "\r")] {
			return false
		}
	}
	return true
}

func GitignoreAfter(text, rel string) string {
	sep := ""
	if text != "" && !strings.HasSuffix(text, "\n") {
		sep = "\n"
	}
	return text + sep + rel + "/\n"
}

func ConfigWritePair(dest, key, value string, env platform.Env, cwd string) {
	configWritePair(dest, key, value, env, cwd, "config set")
}

func configWritePair(dest, key, value string, env platform.Env, cwd, command string) {
	MigrateLegacyConfigFile(dest, env, cwd)
	raw := ""
	if _, err := os.Stat(dest); err == nil {
		var readErr error
		raw, readErr = platform.ReadTextFile(dest)
		if readErr != nil {
			platform.Die(fmt.Sprintf("%s: could not rewrite %s (file left untouched)", command, dest), 4)
		}
	}
	out := make([]string, 0)
	found := false
	for _, rawLine := range splitLines(raw) {
		body, comment := splitTrailingComment(rawLine)
		stripped := trimJSWhitespace(body)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			out = append(out, rawLine)
			continue
		}
		eq := strings.IndexByte(stripped, '=')
		if eq < 0 {
			out = append(out, rawLine)
			continue
		}
		k := trimJSWhitespace(stripped[:eq])
		if k == key {
			if !found {
				out = append(out, key+"="+value+comment)
			}
			found = true
			continue
		}
		out = append(out, rawLine)
	}
	if !found {
		out = append(out, key+"="+value)
	}
	content := strings.Join(out, "\n") + "\n"
	if !strings.Contains(content, key+"=") {
		platform.Die(fmt.Sprintf("%s: rewrite of %s dropped %s (file left untouched)", command, dest, key), 4)
	}
	if err := writeConfigAtomic(dest, content); err != nil {
		platform.Die(fmt.Sprintf("%s: could not rewrite %s (file left untouched)", command, dest), 4)
	}
}

func splitTrailingComment(line string) (string, string) {
	idx := -1
	for i := 0; i+1 < len(line); i++ {
		if (line[i] == ' ' || line[i] == '\t') && line[i+1] == '#' && !strings.ContainsAny(line[i:], "\r\u2028\u2029") {
			idx = i
			break
		}
	}
	if idx < 0 {
		return line, ""
	}
	return line[:idx], line[idx:]
}

func FileKeyValue(file, key string) string {
	raw, err := platform.ReadTextFile(file)
	if err != nil {
		return ""
	}
	value := ""
	for _, rawLine := range splitLines(raw) {
		body, _ := splitTrailingComment(rawLine)
		stripped := trimJSWhitespace(body)
		if stripped == "" || strings.HasPrefix(stripped, "#") {
			continue
		}
		eq := strings.IndexByte(stripped, '=')
		if eq < 0 {
			continue
		}
		if trimJSWhitespace(stripped[:eq]) == key {
			value = trimJSWhitespace(stripped[eq+1:])
		}
	}
	return value
}

func ConfigClearKey(file, key string) {
	raw, err := platform.ReadTextFile(file)
	if err != nil {
		platform.Die(fmt.Sprintf("session clear: could not rewrite %s (file left untouched)", file), 4)
	}
	out := make([]string, 0)
	for _, rawLine := range splitLines(raw) {
		body, _ := splitTrailingComment(rawLine)
		stripped := trimJSWhitespace(body)
		if stripped != "" && !strings.HasPrefix(stripped, "#") {
			if eq := strings.IndexByte(stripped, '='); eq >= 0 && trimJSWhitespace(stripped[:eq]) == key {
				continue
			}
		}
		out = append(out, rawLine)
	}
	content := strings.Join(out, "\n")
	if len(out) > 0 {
		content += "\n"
	}
	if err := writeConfigAtomic(file, content); err != nil {
		platform.Die(fmt.Sprintf("session clear: could not rewrite %s (file left untouched)", file), 4)
	}
}

func DottedKeyName(key string, ctx *Config, env platform.Env, cwd string) string {
	parts := strings.Split(key, "_")
	if len(parts) == 0 {
		return key
	}
	head := parts[0]
	if head == "args" || head == "effort" {
		return strings.Join(parts, ".")
	}
	if head == "model" {
		position := parts[len(parts)-1]
		if len(parts) >= 3 && (position == "worker" || position == "orchestrator") {
			return "model." + strings.Join(parts[1:len(parts)-1], ".") + "." + position
		}
		return strings.Join(parts, ".")
	}
	if (head == "role" || head == "lane") && len(parts) >= 3 {
		attr := parts[len(parts)-1]
		middle := strings.Join(parts[1:len(parts)-1], "_")
		known := make(map[string]string)
		if head == "role" {
			for _, dir := range RoleDirs(env, cwd) {
				entries, err := os.ReadDir(dir)
				if err != nil {
					continue
				}
				for _, entry := range entries {
					name := entry.Name()
					if strings.HasSuffix(name, ".md") {
						role := strings.TrimSuffix(name, ".md")
						norm := strings.ReplaceAll(role, "-", "_")
						if _, exists := known[norm]; !exists {
							known[norm] = role
						}
					}
				}
			}
		} else {
			originalRE := regexp.MustCompile(`^lane\.([A-Za-z0-9_-]+)\.`)
			for _, k := range ctx.Order {
				if !strings.HasPrefix(k, "lane_") {
					continue
				}
				entry := ctx.Entries[k]
				if match := originalRE.FindStringSubmatch(entry.Original); match != nil {
					spelling := match[1]
					norm := strings.ReplaceAll(spelling, "-", "_")
					if _, exists := known[norm]; !exists {
						known[norm] = spelling
					}
				}
			}
			for _, lane := range LaneNames(ctx, env) {
				norm := strings.ReplaceAll(lane, "-", "_")
				if _, exists := known[norm]; !exists {
					known[norm] = lane
				}
			}
		}
		if spelling, ok := known[strings.ReplaceAll(middle, "-", "_")]; ok {
			return head + "." + spelling + "." + attr
		}
		return head + "." + middle + "." + attr
	}
	return strings.Join(parts, ".")
}

func CmdConfig(ctx *Config, env platform.Env, cwd string) {
	row := func(key, value, source string) string {
		return padUTF16(key, 18) + " " + padUTF16(value, 30) + " " + source
	}
	lines := []string{row("KEY", "VALUE", "SOURCE")}
	for _, key := range ConfigScalarKeys {
		value := Cfg(ctx, key, "", env)
		if key == "state_dir" && CfgSource(ctx, key, env) == "defaults" {
			value = StateDirSetting(ctx, env, cwd)
		}
		lines = append(lines, row(key, value, CfgSource(ctx, key, env)))
	}
	dotted := map[string]bool{}
	for _, key := range ctx.Order {
		if isDottedNormalizedKey(key) {
			dotted[key] = true
		}
	}
	for name, value := range env {
		if !strings.HasPrefix(name, "HERDR_SOHO_") || value == "" {
			continue
		}
		key := strings.ToLower(NormalizeKey(strings.TrimPrefix(name, "HERDR_SOHO_")))
		if isDottedNormalizedKey(key) {
			dotted[key] = true
		}
	}
	keys := make([]string, 0, len(dotted))
	for key := range dotted {
		keys = append(keys, key)
	}
	textutil.SortUTF16(keys)
	for _, key := range keys {
		entry := ctx.Entries[key]
		name := entry.Original
		if name == "" {
			name = DottedKeyName(key, ctx, env, cwd)
		}
		lines = append(lines, row(name, Cfg(ctx, key, "", env), CfgSource(ctx, key, env)))
	}
	layers := " (none)"
	if len(ctx.Sources) != 0 {
		layers = " " + strings.Join(ctx.Sources, " ")
	}
	lines = append(lines, "\nlayers read:"+layers)
	newUser := platform.UserConfigPath(platform.Current(), env)
	oldUser := LegacyUserConfigPath(platform.Current(), env)
	userFile := EffectiveConfigFile(newUser, oldUser)
	legacy := ""
	if userFile == oldUser {
		legacy = " (legacy)"
	}
	lines = append(lines, "user file:    "+userFile+legacy)
	projectFile := ProjectConfigFileUsed(env, cwd)
	if projectFile == "" {
		projectFile = filepath.Join(platform.ProjectRoot(env, cwd), ".agents", "herdr-soho.conf")
	}
	legacy = ""
	if projectFile == LegacyProjectConfigPath(platform.ProjectRoot(env, cwd)) || projectFile == LegacyProjectConfigPath(platform.StateProjectRoot(env, cwd)) {
		legacy = " (legacy)"
	}
	lines = append(lines, "project file: "+projectFile+legacy)
	sessionFile := SessionConfPath(ctx, env, cwd)
	if sessionFile == "" {
		sessionFile = "(no workspace here)"
	}
	lines = append(lines, "session file: "+sessionFile)
	fmt.Fprintln(platform.Stdout, strings.Join(lines, "\n"))
}

func isDottedNormalizedKey(key string) bool {
	return strings.HasPrefix(key, "args_") || strings.HasPrefix(key, "role_") || strings.HasPrefix(key, "model_") || strings.HasPrefix(key, "effort_") || strings.HasPrefix(key, "lane_")
}

func padUTF16(value string, width int) string {
	length := 0
	for _, r := range value {
		if r > 0xffff {
			length += 2
		} else {
			length++
		}
	}
	if length < width {
		return value + strings.Repeat(" ", width-length)
	}
	return value
}

func SplitPairArg(key, value string, sawValue bool, cmd string) (string, string, bool) {
	if sawValue || key == "" {
		return key, value, sawValue
	}
	if eq := strings.IndexByte(key, '='); eq > 0 {
		return key[:eq], key[eq+1:], true
	}
	if strings.IndexFunc(key, isJSWhitespace) >= 0 {
		platform.Die(fmt.Sprintf(`%s: '%s' arrived as one argument; pass the key and the value as two arguments or as key=value (zsh does not split "$var": use ${=var})`, cmd, key), 2)
	}
	return key, value, false
}

func CmdConfigSet(argv []string, ctx *Config, env platform.Env, cwd string) {
	key, value, where, sawValue := "", "", "project", false
	for _, arg := range argv {
		if arg == "--project" || arg == "--user" {
			where = strings.TrimPrefix(arg, "--")
			continue
		}
		if strings.HasPrefix(arg, "--") {
			platform.Die("config set: unknown option '"+arg+"'", 2)
		}
		if key == "" {
			if eq := strings.IndexByte(arg, '='); eq > 0 {
				key, value = arg[:eq], arg[eq+1:]
				sawValue = value != ""
			} else {
				key = arg
			}
			continue
		}
		if !sawValue {
			value, sawValue = arg, true
		} else if strings.HasPrefix(value, "-") {
			value += " " + arg
		} else {
			platform.Die("config set: unexpected argument '"+arg+"'", 2)
		}
	}
	key, value, sawValue = SplitPairArg(key, value, sawValue, "config set")
	if key == "" || !sawValue {
		platform.Die("usage: config set <key> <value> | <key>=<value> [--project|--user]", 2)
	}
	if value == "" {
		platform.Die("config set: empty value", 2)
	}
	if !ConfigKeyOk(key) {
		platform.Die("config set: unknown key '"+key+"'", 2)
	}
	if !ConfigValueOk(key, value, env, cwd) {
		platform.Die("config set: invalid value '"+value+"' for "+key, 2)
	}
	dest := ConfigFileFor(where, env, cwd)
	if err := os.MkdirAll(filepath.Dir(dest), 0o777); err != nil {
		platform.Die(fmt.Sprintf("config set: could not rewrite %s (file left untouched)", dest), 4)
	}
	ConfigWritePair(dest, key, value, env, cwd)
	fmt.Fprintf(platform.Stdout, "set %s=%s in %s\n", key, value, dest)
}
