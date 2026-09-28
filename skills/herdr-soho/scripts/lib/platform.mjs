// Platform helpers for the herdr-soho JS port (slice 1).
//
// Port of the Unix plumbing the bash script relies on, with the Windows
// decisions from the port spec (section 6 + orchestrator decisions):
//   - user config dir: $XDG_CONFIG_HOME/herdr-soho/config when set (any
//     platform); otherwise Unix ~/.config/herdr-soho/config or
//     Windows %APPDATA%\herdr-soho\config (decision 4).
//   - CRLF -> LF normalization on every text read (decision 7).
//   - HOME / USERPROFILE via env, falling back to os.homedir().
// No npm dependencies: node:fs, node:path, node:os, node:child_process only.
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';

// die <msg> [code] — same message format and codes as the bash script.
export function die(message, code = 1) {
  process.stderr.write(`herdr-soho: ${message}\n`);
  process.exit(code);
}

export function homeDir(platform = process.platform, env = process.env) {
  if (platform === 'win32') return env.USERPROFILE || os.homedir();
  return env.HOME || os.homedir();
}

// User config file path (the `user` layer). Decision 4: XDG_CONFIG_HOME wins
// on every platform; otherwise per-platform defaults.
export function userConfigPath(platform = process.platform, env = process.env) {
  if (env.XDG_CONFIG_HOME) return path.join(env.XDG_CONFIG_HOME, 'herdr-soho', 'config');
  if (platform === 'win32') {
    if (env.APPDATA) return path.join(env.APPDATA, 'herdr-soho', 'config');
    return path.join(homeDir(platform, env), 'AppData', 'Roaming', 'herdr-soho', 'config');
  }
  return path.join(homeDir(platform, env), '.config', 'herdr-soho', 'config');
}

// Read a text file as UTF-8, normalizing CRLF to LF (decision 7). Throws on
// missing/unreadable files, like fs.readFileSync.
export function readTextFile(file) {
  return fs.readFileSync(file, 'utf8').replace(/\r\n/g, '\n');
}

// project_root() port: `git rev-parse --show-toplevel`, else the cwd.
export function projectRoot(env = process.env, cwd = process.cwd()) {
  const r = spawnSync('git', ['rev-parse', '--show-toplevel'], { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
  if (r.status === 0) {
    const p = (r.stdout || '').trim();
    if (p) return p;
  }
  return cwd;
}

// State belongs to the primary checkout when this cwd is in a linked
// worktree. Normal checkouts and non-git directories keep projectRoot's
// existing behavior.
export function stateProjectRoot(env = process.env, cwd = process.cwd()) {
  const gitDir = spawnSync('git', ['rev-parse', '--git-dir'], { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
  const commonDir = spawnSync('git', ['rev-parse', '--git-common-dir'], { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
  if (gitDir.status !== 0 || commonDir.status !== 0) return projectRoot(env, cwd);
  const gitPath = path.resolve(cwd, (gitDir.stdout || '').trim());
  const commonPath = path.resolve(cwd, (commonDir.stdout || '').trim());
  if (!gitPath || gitPath === commonPath) return projectRoot(env, cwd);
  if (path.basename(commonPath) === '.git') return path.dirname(commonPath);
  const worktrees = spawnSync('git', ['worktree', 'list', '--porcelain'], { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
  if (worktrees.status === 0) {
    const first = (worktrees.stdout || '').split(/\r?\n\r?\n/, 1)[0];
    const firstPath = first.split(/\r?\n/).find((line) => line.startsWith('worktree '));
    if (firstPath && !first.split(/\r?\n/).some((line) => line === 'bare')) {
      let root = path.resolve(cwd, firstPath.slice('worktree '.length));
      const listedRel = path.relative(commonPath, root);
      const listedInsideCommon = listedRel === '' || (listedRel !== '..' && !listedRel.startsWith(`..${path.sep}`) && !path.isAbsolute(listedRel));
      if (listedInsideCommon) {
        const worktree = spawnSync('git', ['--git-dir', commonPath, 'config', '--path', 'core.worktree'], { cwd, env, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] });
        if (worktree.status === 0 && (worktree.stdout || '').trim()) root = path.resolve(commonPath, worktree.stdout.trim());
      }
      const rootRel = path.relative(commonPath, root);
      const rootInsideCommon = rootRel === '' || (rootRel !== '..' && !rootRel.startsWith(`..${path.sep}`) && !path.isAbsolute(rootRel));
      if (!rootInsideCommon) return root;
    }
  }
  return projectRoot(env, cwd);
}

// `command -v` port: resolve an executable on PATH, honoring PATHEXT on
// Windows (decision 6). Returns the full path or null.
export function findExecutable(name, env = process.env, platform = process.platform) {
  const pathVar = env.PATH ?? env.Path ?? '';
  const dirs = pathVar.split(path.delimiter).filter(Boolean);
  const exts = platform === 'win32'
    ? (env.PATHEXT || '.EXE;.CMD;.BAT;.COM').split(';').filter(Boolean)
    : [''];
  for (const dir of dirs) {
    for (const ext of exts) {
      const candidate = path.join(dir, name + ext);
      let st;
      try { st = fs.statSync(candidate); } catch { continue; }
      if (!st.isFile()) continue;
      if (platform === 'win32') return candidate;
      if (st.mode & 0o111) return candidate;
    }
  }
  return null;
}

// cmdInvocation <resolved .cmd/.bat> <args> [env] — how to run a batch file
// on Windows without handing unescaped arguments to a shell (the cross-spawn
// rules): `cmd.exe /d /s /c "<command> <args>"` with verbatim arguments. Each
// argument is wrapped in double quotes (embedded quotes and trailing
// backslashes escaped) and every cmd.exe metacharacter is caret-escaped —
// twice for npm shims under node_modules\.bin, which re-parse %*. A space,
// a quote, `&` or `%` in an argument can neither split it nor run a command.
const CMD_META_RE = /([()\][%!^"`<>&|;, *?])/g;
function cmdEscapeArg(arg, twice) {
  let s = `${arg}`;
  s = s.replace(/(\\*)"/g, '$1$1\\"');
  s = s.replace(/(\\*)$/, '$1$1');
  s = `"${s}"`.replace(CMD_META_RE, '^$1');
  return twice ? s.replace(CMD_META_RE, '^$1') : s;
}
export function cmdInvocation(resolved, args, env = process.env) {
  const twice = /node_modules[\\/]\.bin[\\/][^\\/]+\.cmd$/i.test(resolved);
  const command = path.win32.normalize(resolved).replace(CMD_META_RE, '^$1');
  const line = [command, ...args.map((a) => cmdEscapeArg(a, twice))].join(' ');
  return {
    command: env.COMSPEC || env.ComSpec || 'cmd.exe',
    args: ['/d', '/s', '/c', `"${line}"`],
    windowsVerbatimArguments: true,
  };
}

// Effective write target of `dest`: when `dest` is a symlink, the final
// file of the chain, so a write lands on the target and the link stays a
// link (a project with AGENTS.md -> CLAUDE.md keeps the link across setup).
// A resolvable chain goes through realpath (a cycle throws realpath's ELOOP
// error, which callers treat as "file left untouched"); a broken chain
// follows readlink hops (a relative target resolves against the link's
// directory) to the first path that is not a link, and the file is created
// there. node:fs/node:path only, so it behaves the same on Windows. Non-
// symlink and absent paths pass through unchanged.
function resolveWriteTarget(dest) {
  let st;
  try { st = fs.lstatSync(dest); } catch { return dest; } // absent: new file
  if (!st.isSymbolicLink()) return dest;
  try {
    return fs.realpathSync(dest); // resolvable chain; throws ELOOP on a cycle
  } catch (err) {
    if (err.code !== 'ENOENT') throw err; // ELOOP and the rest surface as-is
    // Broken chain: walk the links by hand to the first non-link path and
    // create the file there; the hop cap keeps a cycle that slipped past
    // realpath from looping forever (same ELOOP error as realpath).
    let cur = dest;
    for (let hops = 0; hops < 40; hops++) {
      const target = fs.readlinkSync(cur);
      cur = path.resolve(path.dirname(cur), target);
      let ns;
      try { ns = fs.lstatSync(cur); } catch { return cur; } // missing: create it
      if (!ns.isSymbolicLink()) return cur;
    }
    const loop = new Error(`ELOOP: too many symbolic links encountered, realpath '${dest}'`);
    loop.code = 'ELOOP';
    throw loop;
  }
}

// atomicWrite replaces `dest` with `content` and never loses it: the temp
// file sits next to the effective target (resolveWriteTarget, same
// filesystem, so the rename never crosses devices, e.g. a tmpfs /tmp), keeps
// the target's mode (0600 for a new file, like bash's mktemp), and one
// rename replaces the destination — nothing is removed first, so a failure
// leaves `dest` as it was. A symlinked `dest` is written through to its
// final target and stays a link. Shared by the config rewrites and every
// roster rewrite (port decision 1).
export function atomicWrite(dest, content) {
  const target = resolveWriteTarget(dest);
  let mode = 0o600;
  try { mode = fs.statSync(dest).mode & 0o777; } catch { /* new file */ }
  const tmp = path.join(path.dirname(target), `.${path.basename(target)}.${process.pid}.${crypto.randomBytes(4).toString('hex')}.tmp`);
  try {
    fs.writeFileSync(tmp, content, { mode });
    fs.chmodSync(tmp, mode);
    fs.renameSync(tmp, target);
  } catch (err) {
    try { fs.rmSync(tmp, { force: true }); } catch { /* best effort */ }
    throw err;
  }
}

// runCli <exe> <args> — resolve the CLI on PATH (PATHEXT-aware) and run it
// with an argument array, no shell, with an optional timeout (the `timeout`
// port). On Windows a .cmd/.bat target runs through cmd.exe with every
// argument escaped (cmdInvocation), never through `shell: true`. Returns
// { notFound, resolved, status, signal, stdout, stderr, timedOut, error }
// (error: the spawn error code, e.g. ENOENT for a bad shebang interpreter).
//
// Windows-only (issue #20): a .cmd/.bat target with a timeout runs through
// the treekill helper (lib/treekill-run.mjs, spawned with the same runtime
// as the CLI) because spawnSync kills only cmd.exe on timeout and the
// batch's children survive. The helper reuses the mode's stdio/input/
// encoding/env/cwd, arms the timeout itself and kills the whole process
// tree (taskkill /T /F). Everything else — POSIX, .exe, no timeout — goes
// through spawnSync exactly as before, byte for byte.
const TREEKILL_HELPER = path.join(path.dirname(fileURLToPath(import.meta.url)), 'treekill-run.mjs');

export function runCli(exe, args, opts = {}) {
  const env = opts.env ?? process.env;
  const platform = opts.platform ?? process.platform;
  const resolved = findExecutable(exe, env, platform);
  if (!resolved) {
    return { notFound: true, resolved: null, status: null, signal: null, stdout: '', stderr: '', timedOut: false, error: null };
  }
  let command = resolved;
  let argv = args;
  let verbatim = false;
  if (platform === 'win32' && /\.(bat|cmd)$/i.test(resolved)) {
    const inv = cmdInvocation(resolved, args, env);
    command = inv.command;
    argv = inv.args;
    verbatim = inv.windowsVerbatimArguments;
  }
  // The treekill path is exactly: win32 + a .cmd/.bat target (the
  // cmdInvocation wrap above set `verbatim`) + a real timeout. A timeout of
  // 0 (no timeout) or any other platform/extension keeps today's spawnSync
  // call, unchanged.
  if (platform === 'win32' && verbatim && opts.timeoutMs > 0) {
    return runCliTreeKill(resolved, command, argv, verbatim, opts, env);
  }
  // mergeOutput: stdout and stderr share one file descriptor, so the text
  // keeps the order it was written in (the bash `"$(cmd 2>&1)"`); it comes
  // back as `stdout`, with `stderr` empty.
  if (opts.mergeOutput) {
    const tmp = path.join(env.TMPDIR || os.tmpdir(), `.herdr-soho-out-${process.pid}-${crypto.randomBytes(4).toString('hex')}`);
    const fd = fs.openSync(tmp, 'w', 0o600);
    let merged;
    try {
      merged = spawnSync(command, argv, {
        env,
        cwd: opts.cwd,
        stdio: ['ignore', fd, fd],
        timeout: opts.timeoutMs,
        killSignal: 'SIGTERM',
        windowsVerbatimArguments: verbatim,
      });
    } finally { fs.closeSync(fd); }
    let text = '';
    try { text = fs.readFileSync(tmp, 'utf8'); } catch { /* nothing written */ }
    fs.rmSync(tmp, { force: true });
    return {
      notFound: false,
      resolved,
      status: merged.status,
      signal: merged.signal,
      stdout: text,
      stderr: '',
      timedOut: merged.status === null && merged.signal != null,
      error: merged.error?.code ?? null,
    };
  }
  // outputFiles: stdout and stderr each go to their own file (no pipe), so
  // a child left alive holding them cannot hold the call past the timeout,
  // and the two streams stay apart (bash `>"$outf" 2>"$errf"`).
  if (opts.outputFiles) {
    const base = path.join(env.TMPDIR || os.tmpdir(), `.herdr-soho-out-${process.pid}-${crypto.randomBytes(4).toString('hex')}`);
    const outFd = fs.openSync(`${base}.out`, 'w', 0o600);
    const errFd = fs.openSync(`${base}.err`, 'w', 0o600);
    let child;
    try {
      child = spawnSync(command, argv, {
        env,
        cwd: opts.cwd,
        stdio: ['ignore', outFd, errFd],
        timeout: opts.timeoutMs,
        killSignal: 'SIGTERM',
        windowsVerbatimArguments: verbatim,
      });
    } finally { fs.closeSync(outFd); fs.closeSync(errFd); }
    const read = (f) => { try { return fs.readFileSync(f, 'utf8'); } catch { return ''; } };
    const stdout = read(`${base}.out`);
    const stderr = read(`${base}.err`);
    fs.rmSync(`${base}.out`, { force: true });
    fs.rmSync(`${base}.err`, { force: true });
    return {
      notFound: false,
      resolved,
      status: child.status,
      signal: child.signal,
      stdout,
      stderr,
      timedOut: child.status === null && child.signal != null,
      error: child.error?.code ?? null,
    };
  }
  const child = spawnSync(command, argv, {
    env,
    cwd: opts.cwd,
    input: opts.input,
    encoding: 'utf8',
    timeout: opts.timeoutMs,
    killSignal: 'SIGTERM',
    windowsVerbatimArguments: verbatim,
  });
  return {
    notFound: false,
    resolved,
    status: child.status,
    signal: child.signal,
    stdout: typeof child.stdout === 'string' ? child.stdout : '',
    stderr: typeof child.stderr === 'string' ? child.stderr : '',
    timedOut: child.status === null && child.signal != null,
    error: child.error?.code ?? null,
  };
}

// runCliTreeKill — the Windows .cmd/.bat + timeout path of runCli (issue
// #20). The spec JSON (command/args/windowsVerbatimArguments/timeoutMs/
// resultFile/ownPipes) goes through a 0600 temp file — never through a
// command line — in the same TMPDIR as the mode's own output files, and
// both temp files are removed at the end. The helper is spawned with the
// mode's own stdio/input/encoding/env/cwd; the command then runs on the
// mode's file fds (mergeOutput, outputFiles — stdio 'inherit') or, in the
// default mode (ownPipes: true), on the helper's own pipes replayed onto
// the helper's stdout/stderr — the command's output reaches exactly the
// same places as today, and a grandchild that outlives the command cannot
// hold the outer pipes open. The only other difference from the command's
// own call is the timeout: the helper arms it (and kills the tree), so the
// outer spawnSync gets no command timeout — only a safety cap of
// timeoutMs + 15 s against a stuck taskkill or a tree that ignores the
// kill.
function runCliTreeKill(resolved, command, argv, verbatim, opts, env) {
  const tmpdir = env.TMPDIR || os.tmpdir();
  const suffix = `${process.pid}-${crypto.randomBytes(4).toString('hex')}`;
  const specFile = path.join(tmpdir, `.herdr-soho-tk-${suffix}.spec`);
  const resultFile = path.join(tmpdir, `.herdr-soho-tk-${suffix}.result`);
  // Default mode only: the helper carries the command's streams on its own
  // pipes (replayed onto the helper's streams in finish); mergeOutput and
  // outputFiles keep the command on the mode's file fds (inherit).
  const ownPipes = !opts.mergeOutput && !opts.outputFiles;
  fs.writeFileSync(specFile, JSON.stringify({
    command,
    args: argv,
    windowsVerbatimArguments: verbatim,
    timeoutMs: opts.timeoutMs,
    resultFile,
    ownPipes,
  }), { mode: 0o600 });
  const cap = opts.timeoutMs + 15_000;
  const helper = [TREEKILL_HELPER, specFile];
  let outer;
  // mergeOutput: the mode's single shared fd for stdout+stderr.
  if (opts.mergeOutput) {
    const tmp = path.join(tmpdir, `.herdr-soho-out-${suffix}`);
    const fd = fs.openSync(tmp, 'w', 0o600);
    try {
      outer = spawnSync(process.execPath, helper, {
        env,
        cwd: opts.cwd,
        stdio: ['ignore', fd, fd],
        timeout: cap,
        killSignal: 'SIGTERM',
      });
    } finally { fs.closeSync(fd); }
    let text = '';
    try { text = fs.readFileSync(tmp, 'utf8'); } catch { /* nothing written */ }
    fs.rmSync(tmp, { force: true });
    return finishTreeKill(resolved, outer, specFile, resultFile, { stdout: text, stderr: '' });
  }
  // outputFiles: the mode's two separate fds.
  if (opts.outputFiles) {
    const base = path.join(tmpdir, `.herdr-soho-out-${suffix}`);
    const outFd = fs.openSync(`${base}.out`, 'w', 0o600);
    const errFd = fs.openSync(`${base}.err`, 'w', 0o600);
    try {
      outer = spawnSync(process.execPath, helper, {
        env,
        cwd: opts.cwd,
        stdio: ['ignore', outFd, errFd],
        timeout: cap,
        killSignal: 'SIGTERM',
      });
    } finally { fs.closeSync(outFd); fs.closeSync(errFd); }
    const read = (f) => { try { return fs.readFileSync(f, 'utf8'); } catch { return ''; } };
    const stdout = read(`${base}.out`);
    const stderr = read(`${base}.err`);
    fs.rmSync(`${base}.out`, { force: true });
    fs.rmSync(`${base}.err`, { force: true });
    return finishTreeKill(resolved, outer, specFile, resultFile, { stdout, stderr });
  }
  // Default: pipes (the helper's own pipes carry the command's streams) +
  // the mode's `input`.
  outer = spawnSync(process.execPath, helper, {
    env,
    cwd: opts.cwd,
    input: opts.input,
    encoding: 'utf8',
    timeout: cap,
    killSignal: 'SIGTERM',
  });
  return finishTreeKill(resolved, outer, specFile, resultFile, {
    stdout: typeof outer.stdout === 'string' ? outer.stdout : '',
    stderr: typeof outer.stderr === 'string' ? outer.stderr : '',
  });
}

// finishTreeKill — read and remove the treekill temp files, then return the
// same shape as today's spawnSync: timedOut → { status null, signal
// SIGTERM, error ETIMEDOUT } (what herdr.mjs and setup-probe classify on);
// a command that never started → its spawn error code with no status;
// anything else → the child's own status/signal. When the helper left no
// resultFile (it failed, or the safety cap killed it) the wrapper's own
// error is returned — never an invented status.
function finishTreeKill(resolved, outer, specFile, resultFile, streams) {
  let doc = null;
  try {
    const parsed = JSON.parse(fs.readFileSync(resultFile, 'utf8'));
    if (parsed !== null && typeof parsed === 'object' && !Array.isArray(parsed)) doc = parsed;
  } catch { /* the helper left no result */ }
  fs.rmSync(specFile, { force: true });
  fs.rmSync(resultFile, { force: true });
  if (doc === null) {
    const signal = outer.signal ?? null;
    return {
      notFound: false,
      resolved,
      status: null,
      signal,
      stdout: streams.stdout,
      stderr: streams.stderr,
      timedOut: outer.status === null && signal != null,
      error: outer.error?.code ?? null,
    };
  }
  if (doc.timedOut) {
    return {
      notFound: false,
      resolved,
      status: null,
      signal: 'SIGTERM',
      stdout: streams.stdout,
      stderr: streams.stderr,
      timedOut: true,
      error: 'ETIMEDOUT',
    };
  }
  return {
    notFound: false,
    resolved,
    status: typeof doc.status === 'number' ? doc.status : null,
    signal: doc.signal ?? null,
    stdout: streams.stdout,
    stderr: streams.stderr,
    timedOut: false,
    error: typeof doc.error === 'string' ? doc.error : null,
  };
}
