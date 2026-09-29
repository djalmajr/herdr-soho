package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Env is an explicit environment passed to platform helpers and commands.
type Env map[string]string

func (e Env) Get(name string) string { return e[name] }

func (e Env) Lookup(name string) (string, bool) {
	v, ok := e[name]
	return v, ok
}

func (e Env) Clone() Env {
	out := make(Env, len(e))
	for k, v := range e {
		out[k] = v
	}
	return out
}

func (e Env) List() []string {
	keys := make([]string, 0, len(e))
	for k := range e {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, k+"="+e[k])
	}
	return out
}

func EnvFromOS() Env {
	env := make(Env)
	for _, entry := range os.Environ() {
		name, value, ok := strings.Cut(entry, "=")
		if ok {
			env[name] = value
		}
	}
	return env
}

type ExitError struct {
	Code     int
	Msg      string
	Friction bool
}

func (e *ExitError) Error() string { return e.Msg }

// Die interrupts the current command; cli.Run is the recovery boundary.
func Die(msg string, code int) { panic(&ExitError{Code: code, Msg: msg}) }

// DieFriction interrupts the current command and marks the error for the
// CLI recovery boundary to append to friction.log.
func DieFriction(msg string, code int) {
	panic(&ExitError{Code: code, Msg: msg, Friction: true})
}

var Stdout io.Writer = os.Stdout
var Stderr io.Writer = os.Stderr
var Now = time.Now
var renameFile = os.Rename

func Current() string {
	switch runtime.GOOS {
	case "windows":
		return "win32"
	case "darwin":
		return "darwin"
	case "aix", "android", "freebsd", "linux", "openbsd", "netbsd", "solaris", "illumos":
		if runtime.GOOS == "solaris" || runtime.GOOS == "illumos" {
			return "sunos"
		}
		return runtime.GOOS
	default:
		return runtime.GOOS
	}
}

func HomeDir(platform string, env Env) string {
	if platform == "win32" {
		if env.Get("USERPROFILE") != "" {
			return env.Get("USERPROFILE")
		}
	} else if env.Get("HOME") != "" {
		return env.Get("HOME")
	}
	home, err := currentUserHome()
	if err == nil && home != "" {
		return home
	}
	variable := "HOME"
	if platform == "win32" {
		variable = "USERPROFILE"
	}
	Die("cannot resolve the home directory; set "+variable, 2)
	return ""
}

var currentUserHome = func() (string, error) {
	current, err := user.Current()
	if err != nil {
		return "", err
	}
	return current.HomeDir, nil
}

var currentExecutable = os.Executable

func UserConfigPath(platform string, env Env) string {
	if xdg := env.Get("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "herdr-soho", "config")
	}
	if platform == "win32" {
		if appdata := env.Get("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "herdr-soho", "config")
		}
		return filepath.Join(HomeDir(platform, env), "AppData", "Roaming", "herdr-soho", "config")
	}
	return filepath.Join(HomeDir(platform, env), ".config", "herdr-soho", "config")
}

func ReadTextFile(file string) (string, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	text := decodeWHATWGUTF8(data)
	return strings.ReplaceAll(text, "\r\n", "\n"), nil
}

func decodeWHATWGUTF8(data []byte) string {
	var out strings.Builder
	for i := 0; i < len(data); {
		b := data[i]
		if b < 0x80 {
			out.WriteByte(b)
			i++
			continue
		}
		n := 0
		if b >= 0xC2 && b <= 0xDF {
			n = 2
		} else if b >= 0xE0 && b <= 0xEF {
			n = 3
		} else if b >= 0xF0 && b <= 0xF4 {
			n = 4
		}
		if n == 0 {
			out.WriteRune('\uFFFD')
			i++
			continue
		}
		consumed := 1
		valid := true
		for j := 1; j < n; j++ {
			if i+j >= len(data) {
				valid = false
				break
			}
			c := data[i+j]
			if c < 0x80 || c > 0xBF || (j == 1 && ((b == 0xE0 && c < 0xA0) || (b == 0xED && c > 0x9F) || (b == 0xF0 && c < 0x90) || (b == 0xF4 && c > 0x8F))) {
				valid = false
				break
			}
			consumed++
		}
		if !valid {
			out.WriteRune('\uFFFD')
			i += consumed
			continue
		}
		out.Write(data[i : i+n])
		i += n
	}
	return out.String()
}

func commandPath(name string, env Env) (string, bool) {
	return FindExecutable(name, env, Current())
}

func runGit(env Env, cwd string, args ...string) (string, int) {
	git, ok := commandPath("git", env)
	if !ok {
		return "", -1
	}
	cmd := exec.Command(git, args...)
	cmd.Dir = cwd
	cmd.Env = env.List()
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = io.Discard
	err := cmd.Run()
	if err == nil {
		return stdout.String(), 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout.String(), exitErr.ExitCode()
	}
	return stdout.String(), -1
}

var gitRunner = runGit

var rootCache = struct {
	sync.Mutex
	values map[string]string
}{values: make(map[string]string)}

func rootCacheKey(kind string, env Env, cwd string) string {
	resolved, err := filepath.Abs(cwd)
	if err == nil {
		cwd = filepath.Clean(resolved)
	}
	return strings.Join([]string{kind, cwd, env.Get("GIT_DIR"), env.Get("GIT_WORK_TREE"), env.Get("GIT_COMMON_DIR")}, "\x00")
}

func cachedRoot(key string, resolve func() string) string {
	rootCache.Lock()
	if value, ok := rootCache.values[key]; ok {
		rootCache.Unlock()
		return value
	}
	rootCache.Unlock()
	value := resolve()
	rootCache.Lock()
	rootCache.values[key] = value
	rootCache.Unlock()
	return value
}

// ProjectRoot returns the repository root for cwd or cwd when it is not in a Git repository.
func ProjectRoot(env Env, cwd string) string {
	return cachedRoot(rootCacheKey("project", env, cwd), func() string {
		out, status := gitRunner(env, cwd, "rev-parse", "--show-toplevel")
		if status == 0 {
			if root := strings.TrimSpace(out); root != "" {
				if runtime.GOOS == "windows" {
					root = filepath.Clean(strings.ReplaceAll(root, "/", `\`))
				}
				return root
			}
		}
		return cwd
	})
}

func StateProjectRoot(env Env, cwd string) string {
	return cachedRoot(rootCacheKey("state", env, cwd), func() string {
		return stateProjectRootUncached(env, cwd)
	})
}

func stateProjectRootUncached(env Env, cwd string) string {
	gitDirOut, gitStatus := gitRunner(env, cwd, "rev-parse", "--git-dir")
	commonOut, commonStatus := gitRunner(env, cwd, "rev-parse", "--git-common-dir")
	if gitStatus != 0 || commonStatus != 0 {
		return ProjectRoot(env, cwd)
	}
	gitPath := resolveFrom(cwd, strings.TrimSpace(gitDirOut))
	commonPath := resolveFrom(cwd, strings.TrimSpace(commonOut))
	if gitPath == "" || gitPath == commonPath {
		return ProjectRoot(env, cwd)
	}
	if filepath.Base(commonPath) == ".git" {
		root := filepath.Dir(commonPath)
		if hasGitPathSegment(root) {
			return ProjectRoot(env, cwd)
		}
		return root
	}
	worktreesOut, worktreeStatus := gitRunner(env, cwd, "worktree", "list", "--porcelain")
	if worktreeStatus == 0 {
		first := strings.SplitN(strings.TrimRight(worktreesOut, "\n"), "\n\n", 2)[0]
		var firstPath string
		bare := false
		for _, line := range strings.Split(strings.ReplaceAll(first, "\r\n", "\n"), "\n") {
			if strings.HasPrefix(line, "worktree ") {
				firstPath = strings.TrimPrefix(line, "worktree ")
			}
			if line == "bare" {
				bare = true
			}
		}
		if firstPath != "" && !bare {
			root := resolveFrom(cwd, firstPath)
			if isWithin(commonPath, root) {
				worktreeOut, status := gitRunner(env, cwd, "--git-dir", commonPath, "config", "--path", "core.worktree")
				if status == 0 && strings.TrimSpace(worktreeOut) != "" {
					root = resolveFrom(commonPath, strings.TrimSpace(worktreeOut))
				}
			}
			if !isWithin(commonPath, root) && !hasGitPathSegment(root) {
				return root
			}
		}
	}
	return ProjectRoot(env, cwd)
}

func hasGitPathSegment(value string) bool {
	for _, segment := range strings.FieldsFunc(value, func(r rune) bool { return r == '/' || r == '\\' }) {
		if strings.EqualFold(segment, ".git") {
			return true
		}
	}
	return false
}

func resolveFrom(base, value string) string {
	if runtime.GOOS == "windows" {
		base = strings.ReplaceAll(base, "/", `\`)
		value = strings.ReplaceAll(value, "/", `\`)
		if filepath.VolumeName(value) == "" && strings.HasPrefix(value, `\`) {
			value = filepath.VolumeName(base) + value
		}
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(base, value))
}

// SkillDir returns the configured skill root or finds it beside the executable.
func SkillDir(env Env) string {
	if configured := env.Get("HERDR_SOHO_SKILL_DIR"); configured != "" {
		return configured
	}
	executable, err := currentExecutable()
	if err == nil {
		if dir := skillDirFromExecutable(executable); dir != "" {
			return dir
		}
	}
	Die("cannot find skill directory; set HERDR_SOHO_SKILL_DIR", 2)
	return ""
}

func skillDirFromExecutable(executable string) string {
	if resolved, resolveErr := filepath.EvalSymlinks(executable); resolveErr == nil {
		executable = resolved
	}
	for dir := filepath.Dir(executable); ; dir = filepath.Dir(dir) {
		if info, statErr := os.Stat(filepath.Join(dir, "SKILL.md")); statErr == nil && info.Mode().IsRegular() {
			if info, statErr = os.Stat(filepath.Join(dir, "roles")); statErr == nil && info.IsDir() {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return ""
}

func isWithin(parent, candidate string) bool {
	rel, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

func FindExecutable(name string, env Env, platform string) (string, bool) {
	pathValue, ok := env.Lookup("PATH")
	if !ok {
		pathValue = env.Get("Path")
	}
	dirs := filepath.SplitList(pathValue)
	extensions := []string{""}
	if platform == "win32" {
		pathext := env.Get("PATHEXT")
		if pathext == "" {
			pathext = ".EXE;.CMD;.BAT;.COM"
		}
		extensions = strings.Split(pathext, ";")
		filtered := make([]string, 0, len(extensions))
		for _, ext := range extensions {
			ext = strings.TrimSpace(ext)
			if ext != "" {
				filtered = append(filtered, ext)
			}
		}
		extensions = filtered
		if nameExt := filepath.Ext(name); nameExt != "" {
			for _, ext := range extensions {
				if strings.EqualFold(nameExt, ext) {
					extensions = []string{""}
					break
				}
			}
		}
	}
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		for _, ext := range extensions {
			candidate := filepath.Join(dir, name+ext)
			if platform == "win32" {
				info, err := os.Stat(candidate)
				if err != nil {
					candidate = windowsPathCaseMatch(dir, filepath.Base(candidate))
					if candidate == "" {
						continue
					}
					info, err = os.Stat(candidate)
				}
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				return candidate, true
			}
			info, err := os.Stat(candidate)
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			if info.Mode().Perm()&0o111 != 0 {
				return candidate, true
			}
		}
	}
	return "", false
}

func windowsPathCaseMatch(dir, name string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Name(), name) {
			return filepath.Join(dir, entry.Name())
		}
	}
	return ""
}

var cmdMeta = map[rune]bool{}

func init() {
	for _, r := range "()[]%!^\"`<>&|;, *?" {
		cmdMeta[r] = true
	}
}

func escapeCmdMeta(value string) string {
	var out strings.Builder
	for _, r := range value {
		if cmdMeta[r] {
			out.WriteByte('^')
		}
		out.WriteRune(r)
	}
	return out.String()
}

var backslashQuote = regexp.MustCompile(`(\\*)"`)
var trailingBackslash = regexp.MustCompile(`\\+$`)

func escapeCmdArg(arg string, twice bool) string {
	s := backslashQuote.ReplaceAllString(arg, `$1$1\"`)
	s = trailingBackslash.ReplaceAllStringFunc(s, func(v string) string { return v + v })
	s = escapeCmdMeta(`"` + s + `"`)
	if twice {
		s = escapeCmdMeta(s)
	}
	return s
}

func windowsNormalize(value string) string {
	s := strings.ReplaceAll(value, `\`, `/`)
	unc := strings.HasPrefix(s, "//")
	drive := ""
	if len(s) >= 2 && s[1] == ':' {
		drive, s = s[:2], s[2:]
	}
	clean := path.Clean(s)
	if clean == "." {
		clean = ""
	}
	if unc {
		clean = "//" + strings.TrimLeft(clean, "/")
	}
	return strings.ReplaceAll(drive+clean, "/", `\`)
}

type CmdInvocationResult struct {
	Command                  string
	Args                     []string
	WindowsVerbatimArguments bool
}

func CmdInvocation(resolved string, args []string, env Env) CmdInvocationResult {
	twice := regexp.MustCompile(`(?i)node_modules[\\/]\.bin[\\/][^\\/]+\.cmd$`).MatchString(resolved)
	command := escapeCmdMeta(windowsNormalize(resolved))
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, command)
	for _, arg := range args {
		parts = append(parts, escapeCmdArg(arg, twice))
	}
	comspec := env.Get("COMSPEC")
	if comspec == "" {
		comspec = env.Get("ComSpec")
	}
	if comspec == "" {
		comspec = "cmd.exe"
	}
	return CmdInvocationResult{
		Command:                  comspec,
		Args:                     []string{"/d", "/s", "/c", `"` + strings.Join(parts, " ") + `"`},
		WindowsVerbatimArguments: true,
	}
}

func resolveWriteTarget(dest string) (string, error) {
	info, err := os.Lstat(dest)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return dest, nil
	}
	resolved, err := filepath.EvalSymlinks(dest)
	if err == nil {
		return resolved, nil
	}
	if errors.Is(err, syscall.ELOOP) || strings.Contains(strings.ToLower(err.Error()), "too many links") {
		return "", fmt.Errorf("ELOOP: %w", syscall.ELOOP)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	current := dest
	for hops := 0; hops < 40; hops++ {
		link, readErr := os.Readlink(current)
		if readErr != nil {
			return "", readErr
		}
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(current), link)
		}
		current = filepath.Clean(link)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return current, nil
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink == 0 {
			return current, nil
		}
	}
	return "", fmt.Errorf("ELOOP: %w", syscall.ELOOP)
}

func AtomicWrite(dest, content string) error {
	target, err := resolveWriteTarget(dest)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if info, statErr := os.Stat(dest); statErr == nil {
		mode = info.Mode().Perm()
	}
	nameBytes := make([]byte, 4)
	if _, err = rand.Read(nameBytes); err != nil {
		return err
	}
	tmp := filepath.Join(filepath.Dir(target), fmt.Sprintf(".%s.%d.%s.tmp", filepath.Base(target), os.Getpid(), hex.EncodeToString(nameBytes)))
	file, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	cleanup := func() { _ = os.Remove(tmp) }
	if _, err = io.WriteString(file, content); err != nil {
		_ = file.Close()
		cleanup()
		return err
	}
	if err = file.Chmod(mode); err != nil {
		_ = file.Close()
		cleanup()
		return err
	}
	if err = file.Close(); err != nil {
		cleanup()
		return err
	}
	if err = renameFile(tmp, target); err != nil {
		cleanup()
		return err
	}
	return nil
}

type RunOptions struct {
	Context     context.Context
	Env         Env
	OnStart     func(int)
	Platform    string
	Cwd         string
	Input       string
	TimeoutMs   int
	MergeOutput bool
	OutputFiles bool
}

type RunResult struct {
	NotFound bool
	Resolved string
	Status   *int
	Signal   string
	Stdout   string
	Stderr   string
	TimedOut bool
	Error    string
}

func RunCli(exe string, args []string, opts RunOptions) (result RunResult) {
	platform := opts.Platform
	if platform == "" {
		platform = Current()
	}
	resolved, ok := FindExecutable(exe, opts.Env, platform)
	if !ok {
		return RunResult{NotFound: true}
	}
	return runResolved(resolved, args, opts)
}

// RunExecutable runs the given executable path without searching PATH.
func RunExecutable(executable string, args []string, opts RunOptions) RunResult {
	if !filepath.IsAbs(executable) {
		return RunResult{NotFound: true}
	}
	return runResolved(executable, args, opts)
}

func runResolved(resolved string, args []string, opts RunOptions) (result RunResult) {
	platform := opts.Platform
	if platform == "" {
		platform = Current()
	}
	command := resolved
	argv := append([]string(nil), args...)
	var invocation *CmdInvocationResult
	if platform == "win32" && (strings.HasSuffix(strings.ToLower(resolved), ".cmd") || strings.HasSuffix(strings.ToLower(resolved), ".bat")) {
		inv := CmdInvocation(resolved, args, opts.Env)
		invocation = &inv
		command, argv = inv.Command, inv.Args
	}
	if shouldUseWindowsCmdTreeKill(platform, resolved, opts.TimeoutMs) {
		if result, handled := runWindowsCmdTreeKill(resolved, *invocation, opts); handled {
			return result
		}
	}
	ctx := opts.Context
	if ctx == nil {
		ctx = context.Background()
	}
	cancel := func() {}
	if opts.TimeoutMs > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(opts.TimeoutMs)*time.Millisecond)
	}
	defer cancel()
	cmd := exec.CommandContext(ctx, command, argv...)
	if invocation != nil && invocation.WindowsVerbatimArguments {
		setWindowsInvocation(cmd, *invocation)
	}
	if platform != "win32" {
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			return cmd.Process.Signal(syscall.SIGTERM)
		}
	} else {
		cmd.Cancel = func() error {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			return cmd.Process.Kill()
		}
	}
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Env = opts.Env.List()
	cmd.Dir = opts.Cwd
	if opts.Input != "" {
		cmd.Stdin = strings.NewReader(opts.Input)
	}
	result = RunResult{Resolved: resolved}
	var stdout, stderr bytes.Buffer
	var tempFiles []*os.File
	var tempPaths []string
	closeTemps := func() {
		for _, f := range tempFiles {
			_ = f.Close()
		}
		for _, p := range tempPaths {
			_ = os.Remove(p)
		}
	}
	defer closeTemps()
	if opts.MergeOutput || opts.OutputFiles {
		tmpDir, tmpErr := tempDirFor(opts.Env)
		if tmpErr != "" {
			return tempFileError(result, tmpErr)
		}
		openTemp := func(suffix string) (*os.File, string, error) {
			f, err := os.CreateTemp(tmpDir, ".herdr-soho-out-*")
			if err != nil {
				return nil, "", err
			}
			_ = f.Chmod(0o600)
			tempFiles = append(tempFiles, f)
			tempPaths = append(tempPaths, f.Name())
			return f, f.Name(), nil
		}
		outFile, outPath, err := openTemp("")
		if err != nil {
			return tempFileError(result, errorCode(err))
		}
		if opts.MergeOutput {
			cmd.Stdout, cmd.Stderr = outFile, outFile
		} else {
			errFile, errPath, openErr := openTemp(".err")
			if openErr != nil {
				return tempFileError(result, errorCode(openErr))
			}
			cmd.Stdout, cmd.Stderr = outFile, errFile
			defer func() {
				if data, readErr := os.ReadFile(errPath); readErr == nil {
					result.Stderr = decodeWHATWGUTF8(data)
				}
			}()
		}
		defer func() {
			if data, readErr := os.ReadFile(outPath); readErr == nil {
				result.Stdout = decodeWHATWGUTF8(data)
			}
		}()
	} else {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}
	if err := cmd.Start(); err != nil {
		result.Error = errorCode(err)
		return result
	}
	if opts.OnStart != nil {
		opts.OnStart(cmd.Process.Pid)
	}
	err := cmd.Wait()
	if state := cmd.ProcessState; state != nil {
		if signal := processSignal(&exec.ExitError{ProcessState: state}); signal != "" {
			result.Signal = signal
		} else if state.ExitCode() >= 0 {
			status := state.ExitCode()
			result.Status = &status
		}
		result.TimedOut = result.Status == nil && result.Signal != ""
		if opts.TimeoutMs > 0 && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Error = "ETIMEDOUT"
			if platform == "win32" {
				result.TimedOut = true
				result.Status = nil
			}
		}
	} else if err != nil {
		result.Error = errorCode(err)
	} else {
		status := 0
		result.Status = &status
	}
	if !opts.MergeOutput && !opts.OutputFiles {
		result.Stdout, result.Stderr = decodeWHATWGUTF8(stdout.Bytes()), decodeWHATWGUTF8(stderr.Bytes())
	}
	return result
}

func shouldUseWindowsCmdTreeKill(platform, resolved string, timeoutMs int) bool {
	return platform == "win32" && timeoutMs > 0 && (strings.HasSuffix(strings.ToLower(resolved), ".cmd") || strings.HasSuffix(strings.ToLower(resolved), ".bat"))
}

func assignThenResume(assign func() error, resume func() error) (bool, error) {
	if err := assign(); err != nil {
		return false, resume()
	}
	return true, resume()
}

const treeKillForceFallbackEnv = "HERDR_SOHO_TREEKILL_TEST_FORCE_JOB_FAILURE"

func forceWindowsTreeKillFallback(env Env) bool {
	return env.Get(treeKillForceFallbackEnv) == "1"
}

func setRunTimeoutResult(result *RunResult) {
	result.Status = nil
	result.Signal = "SIGTERM"
	result.TimedOut = true
	result.Error = "ETIMEDOUT"
}

// runTreeKillTestKiller honors the test override only for an executable
// resolved inside the caller's temporary directory. The callback is injected
// so this policy can be verified on platforms without taskkill.
func runTreeKillTestKiller(env Env, tempDir string, pid int, run func(string, []string)) bool {
	killer := env.Get("HERDR_SOHO_TREEKILL_TEST_KILLER")
	if killer == "" || !filepath.IsAbs(killer) {
		return false
	}
	realKiller, err := filepath.EvalSymlinks(killer)
	if err != nil {
		return false
	}
	realTemp, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(realTemp, realKiller)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false
	}
	run(realKiller, []string{strconv.Itoa(pid)})
	return true
}

// tempDirFor is the JS tempDirFor: TMPDIR (or the OS default) resolved and
// created with mkdir -p. mkdirSync reports EEXIST when a file sits on the
// path, and so does this.
func tempDirFor(env Env) (string, string) {
	dir := env.Get("TMPDIR")
	if dir == "" {
		dir = os.TempDir()
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o777); err != nil {
		if info, statErr := os.Stat(dir); statErr == nil && !info.IsDir() {
			return "", "EEXIST"
		}
		return "", errorCode(err)
	}
	return dir, ""
}

// tempFileError is the JS tempFileError: no status, the code in Error, and
// the reason on stderr, so callers name the temporary file and not the
// command that never ran.
func tempFileError(result RunResult, code string) RunResult {
	result.Status = nil
	result.Stdout = ""
	result.Stderr = "herdr-soho: cannot write temporary files: " + code + "\n"
	result.Error = code
	return result
}

func errorCode(err error) string {
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		if errors.Is(execErr.Err, exec.ErrNotFound) {
			return "ENOENT"
		}
		for _, item := range []struct {
			err  error
			code string
		}{
			{syscall.EACCES, "EACCES"}, {syscall.ENOEXEC, "ENOEXEC"}, {syscall.E2BIG, "E2BIG"},
			{syscall.EINVAL, "EINVAL"}, {syscall.ETIMEDOUT, "ETIMEDOUT"},
		} {
			if errors.Is(execErr.Err, item.err) {
				return item.code
			}
		}
		return execErr.Err.Error()
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if code := errnoName(errno); code != "" {
			return code
		}
		return errno.Error()
	}
	return err.Error()
}
