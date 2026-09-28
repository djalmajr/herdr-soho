// F2a: legacy (herdr-agents) reads. Drives applyLegacyEnv (the spec
// examples, the empty-new-value case, the sorted list, the old variables
// kept, legacyEnvCopied), the legacy user/project config paths,
// effectiveConfigFile (the three outcomes), migrateLegacyConfigFile
// (byte for byte, mode preserved, legacy intact, refused on a second
// copy), defaultStateDirName (the three cases), and the CLI end to end
// through the launcher scripts/herdr-soho: `config` reading the legacy
// user/project file and the HERDR_AGENTS_* env prefix, the first-write
// migration of the legacy user config, and every `doctor` legacy warning
// line (none when nothing is legacy). Fixtures are temporary HOME /
// XDG_CONFIG_HOME / project roots (no real ~/.config, ~/.agents or
// worktree project is ever read or written).
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { fixtureEnv, nodeBin } from './parity.mjs';
import { writeFakeCli } from './fakes.mjs';
import { canSymlink, linkTool } from './tools.mjs';
import { cmdInvocation, findExecutable } from '../lib/platform.mjs';
import {
  applyLegacyEnv, defaultStateDirName, effectiveConfigFile,
  legacyEnvCopied, legacyProjectConfigPath, legacyStatePath, legacyUserConfigPath, migrateLegacyConfigFile,
} from '../lib/legacy.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const LAUNCHER = path.join(SCRIPTS, process.platform === 'win32' ? 'herdr-soho.cmd' : 'herdr-soho');
// The doctor runs with a controlled PATH (like test-doctor-fix.sh): the
// fake herdr plus links to the tools it spawns, so no host herdr or agent
// CLI is ever called.
const FAKE_HERDR = [
  'const a = process.argv.slice(2);',
  'if (a[0] === "--version") process.stdout.write("herdr 0.1.0\\n");',
  'else if (a[0] === "status" && a[1] === "server") process.stdout.write("0.1.0\\n");',
].join('\n');

function setup() {
  let root = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-legacy-'));
  root = fs.realpathSync(root); // git reports the resolved path (macOS /var -> /private/var)
  const repo = path.join(root, 'repo');
  const home = path.join(root, 'home');
  const conf = path.join(root, 'conf');
  const tmp = path.join(root, 'tmp');
  for (const d of [repo, home, conf, tmp]) fs.mkdirSync(d, { recursive: true });
  spawnSync('git', ['init', '-q'], { cwd: repo, stdio: 'ignore', timeout: 30000 });
  const env = fixtureEnv({ HOME: home, USERPROFILE: home, XDG_CONFIG_HOME: conf, TMPDIR: tmp });
  const run = (args, over = {}, cwd = repo) => {
    const childEnv = { ...env, ...over };
    const invocation = process.platform === 'win32'
      ? cmdInvocation(LAUNCHER, args, childEnv)
      : { command: LAUNCHER, args };
    const r = spawnSync(invocation.command, invocation.args, {
      cwd, env: childEnv, encoding: 'utf8', timeout: 60000,
      windowsVerbatimArguments: invocation.windowsVerbatimArguments,
    });
    return { rc: r.status === null ? -1 : r.status, out: r.stdout ?? '', err: r.stderr ?? '' };
  };
  const doctorRun = (over = {}) => {
    // The fake herdr + tool links on a PATH the doctor's spawns resolve.
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin, { recursive: true });
    writeFakeCli(bin, 'herdr', FAKE_HERDR);
    for (const name of ['node', 'git', 'jq', 'timeout', 'bun']) {
      if (name === 'node' && process.platform === 'win32') continue;
      const p = name === 'node' ? nodeBin() : findExecutable(name);
      if (!p) continue;
      try { linkTool(bin, name, p); } catch { /* present already */ }
    }
    // A node.cmd link without `call` ends the parent batch launcher on Windows.
    const tools = process.platform === 'win32' ? [bin, path.dirname(nodeBin())] : [bin, '/usr/bin', '/bin'];
    return run(['doctor'], { ...over, PATH: tools.join(path.delimiter) });
  };
  return {
    root, repo, home, conf, tmp, env, run, doctorRun,
    proj: path.join(repo, '.agents', 'herdr-soho.conf'),
    legacyProj: path.join(repo, '.agents', 'herdr-agents.conf'),
    user: path.join(conf, 'herdr-soho', 'config'),
    legacyUser: path.join(conf, 'herdr-agents', 'config'),
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
}

// The warn line holding `msg` (the say format is `warn` padded to 6).
const warnLine = (out, msg) => out.split('\n').find((l) => l.startsWith('warn  ') && l.includes(msg));

test('applyLegacyEnv: a lone HERDR_AGENTS_* is copied, the old name stays, and the list is remembered', () => {
  const env = { HERDR_AGENTS_DIR: 'x' };
  // Mutation captured: dropping the `legacyCopied = copied` line (or the return)
  // makes legacyEnvCopied() keep [] and this fails on the remembered list.
  assert.deepEqual(applyLegacyEnv(env), ['HERDR_AGENTS_DIR']);
  assert.equal(env.HERDR_SOHO_DIR, 'x');
  assert.equal(env.HERDR_AGENTS_DIR, 'x'); // never removed
  assert.deepEqual(legacyEnvCopied(), ['HERDR_AGENTS_DIR']);
});

test('applyLegacyEnv: a non-empty HERDR_SOHO_* wins and nothing is copied', () => {
  const env = { HERDR_AGENTS_DIR: 'x', HERDR_SOHO_DIR: 'y' };
  // Mutation captured: copying unconditionally (no guard on env[fresh])
  // overwrites 'y' with 'x' and this fails on both assertions.
  assert.deepEqual(applyLegacyEnv(env), []);
  assert.equal(env.HERDR_SOHO_DIR, 'y');
  assert.deepEqual(legacyEnvCopied(), []);
});

test('applyLegacyEnv: an empty HERDR_SOHO_* value is replaced; empty old values and other names are ignored', () => {
  const env = { HERDR_AGENTS_DIR: 'x', HERDR_SOHO_DIR: '', HERDR_AGENTS_EMPTY: '', HERDR_AGENTSX: '1' };
  // Mutation captured: dropping the `|| env[fresh] === ''` half of the guard
  // leaves HERDR_SOHO_DIR '' and this fails on env.HERDR_SOHO_DIR.
  assert.deepEqual(applyLegacyEnv(env), ['HERDR_AGENTS_DIR']);
  assert.equal(env.HERDR_SOHO_DIR, 'x');
  assert.equal(env.HERDR_SOHO_EMPTY, undefined);
  assert.equal(env.HERDR_SOHOX, undefined);
  assert.equal(env.HERDR_AGENTSX, '1'); // no underscore after AGENTS: left alone
});

test('applyLegacyEnv: the copied old names come back sorted', () => {
  const env = { HERDR_AGENTS_Z: '1', HERDR_AGENTS_A: '2', HERDR_AGENTS_M: '3' };
  // Mutation captured: dropping the `.sort()` call fails this deepEqual.
  assert.deepEqual(applyLegacyEnv(env), ['HERDR_AGENTS_A', 'HERDR_AGENTS_M', 'HERDR_AGENTS_Z']);
});

test('legacy config paths: the old directory and file names next to the new ones', () => {
  // Mutation captured: writing 'herdr-soho' instead of 'herdr-agents' in
  // legacyUserConfigPath fails the first assertion (the project path is
  // checked here too).
  const xdg = { XDG_CONFIG_HOME: '/c' };
  assert.equal(legacyUserConfigPath('darwin', xdg), path.join('/c', 'herdr-agents', 'config'));
  assert.equal(legacyUserConfigPath('linux', { HOME: '/h' }), path.join('/h', '.config', 'herdr-agents', 'config'));
  assert.equal(legacyUserConfigPath('win32', { APPDATA: '/a' }), path.join('/a', 'herdr-agents', 'config'));
  assert.equal(legacyUserConfigPath('win32', { USERPROFILE: '/u' }), path.join('/u', 'AppData', 'Roaming', 'herdr-agents', 'config'));
  assert.equal(legacyProjectConfigPath('/r'), path.join('/r', '.agents', 'herdr-agents.conf'));
});

test('legacyStatePath uses Windows separators', () => {
  // Mutation captured: interpolating `${root}/.herdr-agents` prints a path
  // the Windows doctor tests cannot match or use.
  assert.equal(legacyStatePath('C:\\workspace\\repo', path.win32), path.win32.join('C:\\workspace\\repo', '.herdr-agents'));
});

test('effectiveConfigFile: the new file wins, the legacy one wins over an absent new one, absent both gives the new path', (t) => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-legacy-eff-'));
  t.after(() => fs.rmSync(d, { recursive: true, force: true }));
  const fresh = path.join(d, 'fresh.conf');
  const old = path.join(d, 'old.conf');
  // Mutation captured: checking the legacy file first makes the last case
  // return the legacy path and fail.
  assert.equal(effectiveConfigFile(fresh, old), fresh); // neither exists
  fs.writeFileSync(old, 'k=1\n');
  assert.equal(effectiveConfigFile(fresh, old), old); // only the legacy one
  fs.writeFileSync(fresh, 'k=2\n');
  assert.equal(effectiveConfigFile(fresh, old), fresh); // both: the new one
});

test('defaultStateDirName: .herdr-agents only while .herdr-soho is absent and .herdr-agents is a directory', (t) => {
  const d = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-legacy-state-'));
  t.after(() => fs.rmSync(d, { recursive: true, force: true }));
  // Mutation captured: returning '.herdr-soho' unconditionally fails the
  // middle case (only the legacy dir exists).
  assert.equal(defaultStateDirName(d), '.herdr-soho'); // neither dir
  fs.mkdirSync(path.join(d, '.herdr-agents'));
  assert.equal(defaultStateDirName(d), '.herdr-agents'); // only the legacy one
  fs.mkdirSync(path.join(d, '.herdr-soho'));
  assert.equal(defaultStateDirName(d), '.herdr-soho'); // both dirs
});

test('migrateLegacyConfigFile: the user and project new paths copy byte for byte with the legacy mode; a second copy and a foreign dest are refused; the legacy file stays', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: an `fs.rmSync(legacy)` after the rename in
  // migrateLegacyConfigFile fails the legacy-intact assertions below.
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  const bytes = Buffer.from('# keep\r\nk=1\n', 'utf8'); // CRLF proves the copy is byte for byte
  fs.writeFileSync(s.legacyUser, bytes);
  fs.chmodSync(s.legacyUser, 0o640);
  assert.equal(migrateLegacyConfigFile(s.user, s.env, s.repo), true);
  assert.deepEqual(fs.readFileSync(s.user), bytes);
  if (process.platform !== 'win32') assert.equal(fs.statSync(s.user).mode & 0o777, 0o640);
  assert.deepEqual(fs.readFileSync(s.legacyUser), bytes); // untouched
  if (process.platform !== 'win32') assert.equal(fs.statSync(s.legacyUser).mode & 0o777, 0o640);
  assert.equal(migrateLegacyConfigFile(s.user, s.env, s.repo), false); // dest exists now
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'k=2\n');
  assert.equal(migrateLegacyConfigFile(s.proj, s.env, s.repo), true);
  assert.equal(fs.readFileSync(s.proj, 'utf8'), 'k=2\n');
  assert.equal(migrateLegacyConfigFile(path.join(s.repo, 'other.conf'), s.env, s.repo), false);
  assert.ok(!fs.existsSync(path.join(s.repo, 'other.conf')));
});

test('config (launcher): the legacy user file is read under the user layer, shown flagged, and not created', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: loadConfig reading the new user path again (no
  // effectiveConfigFile) loses the legacy keys — herd_label goes back to
  // its default and the file line loses the (legacy) flag.
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  fs.writeFileSync(s.legacyUser, '# keep\nherd_label=crew\n');
  const r = s.run(['config']);
  assert.equal(r.rc, 0, r.err);
  const row = (out, name) => out.split('\n').find((l) => l.startsWith(name));
  const herd = row(r.out, 'herd_label ').split(/\s+/).filter(Boolean);
  assert.deepEqual(herd.slice(1), ['crew', 'user']);
  assert.ok(row(r.out, 'user file:').endsWith(`${s.legacyUser} (legacy)`), `user file line:\n${r.out}`);
  assert.ok(row(r.out, 'project file:').endsWith(path.join('.agents', 'herdr-soho.conf')), r.out);
  assert.ok(!row(r.out, 'project file:').includes('(legacy)'), r.out);
  assert.ok(!fs.existsSync(s.user), 'a read must not create the new file');
});

test('config (launcher): the legacy project file is read under the project layer and shown flagged', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: loadConfig reading the new project path again (no
  // effectiveConfigFile) loses max_workers=5 (the row shows the default)
  // and the file line loses the (legacy) flag.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'max_workers=5\n');
  const r = s.run(['config']);
  assert.equal(r.rc, 0, r.err);
  const row = (out, name) => out.split('\n').find((l) => l.startsWith(name));
  const mw = row(r.out, 'max_workers').split(/\s+/).filter(Boolean);
  assert.deepEqual(mw.slice(1), ['5', 'project']);
  assert.ok(row(r.out, 'project file:').endsWith(`${s.legacyProj} (legacy)`), `project file line:\n${r.out}`);
});

test('config (launcher): HERDR_AGENTS_LAYOUT is read as HERDR_SOHO_LAYOUT before the layers load', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the applyLegacyEnv(env) call in
  // herdr-soho.mjs before loadConfig leaves layout at its default (the
  // env var is never seen).
  const r = s.run(['config'], { HERDR_AGENTS_LAYOUT: 'tab' });
  assert.equal(r.rc, 0, r.err);
  const row = r.out.split('\n').find((l) => l.startsWith('layout '));
  assert.deepEqual(row.split(/\s+/).filter(Boolean).slice(1), ['tab', 'env']);
});

test('config set --user (launcher): the first write copies the legacy user file (content and mode), warns, keeps the legacy intact, and does not copy again', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the migrateLegacyConfigFile call at the top
  // of configWritePair leaves the new file without the legacy lines and
  // stderr without the warning; a legacy-deleting copy would fail the
  // legacy-intact assertion.
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  const legacyBytes = Buffer.from('# keep this comment\nherd_label=crew\n', 'utf8');
  fs.writeFileSync(s.legacyUser, legacyBytes);
  fs.chmodSync(s.legacyUser, 0o640);
  const r1 = s.run(['config', 'set', '--user', 'layout', 'tab']);
  assert.equal(r1.rc, 0, r1.err);
  assert.ok(r1.err.includes(`herdr-soho: warning: copied legacy config ${s.legacyUser} to ${s.user}; the legacy file is no longer read\n`), `stderr:\n${r1.err}`);
  assert.equal(fs.readFileSync(s.user, 'utf8'), '# keep this comment\nherd_label=crew\nlayout=tab\n');
  if (process.platform !== 'win32') assert.equal(fs.statSync(s.user).mode & 0o777, 0o640);
  assert.deepEqual(fs.readFileSync(s.legacyUser), legacyBytes); // intact
  const r2 = s.run(['config', 'set', '--user', 'max_workers', '4']);
  assert.equal(r2.rc, 0, r2.err);
  assert.ok(!r2.err.includes('copied legacy config'), `second write copied again:\n${r2.err}`);
  assert.equal(fs.readFileSync(s.user, 'utf8'), '# keep this comment\nherd_label=crew\nlayout=tab\nmax_workers=4\n');
  assert.deepEqual(fs.readFileSync(s.legacyUser), legacyBytes);
});

test('doctor --fix (launcher): without --panes a legacy-only project reads panes from the legacy file and migrates it', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: reading `panes` from the new (still absent) project
  // file makes `doctor --fix` die 2 asking for 2, 3 or 4.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'panes=3\nherd_label=crew\n');
  const r = s.run(['doctor', '--fix']);
  assert.equal(r.rc, 0, `${r.out}\n${r.err}`);
  assert.ok(r.err.includes(`copied legacy config ${s.legacyProj} to ${s.proj}`), r.err);
  const fixed = fs.readFileSync(s.proj, 'utf8');
  assert.match(fixed, /^panes=3$/m);
  assert.match(fixed, /^herd_label=crew$/m);
  assert.equal(fs.readFileSync(s.legacyProj, 'utf8'), 'panes=3\nherd_label=crew\n');
});

test('setup (launcher): a team choice in the legacy project file counts, so no config prompt', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: checking only the new (absent) project file warns
  // "sets neither multi_role…" although the legacy file chose a lane kind.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'lane.build.kind=codex\n');
  const r = s.run(['setup', '--no-hooks']);
  assert.equal(r.rc, 0, `${r.out}\n${r.err}`);
  assert.ok(!r.err.includes('sets neither multi_role'), r.err);
  assert.ok(!fs.existsSync(s.proj), 'setup without --panes must not create the new project file');
});

test('config set (launcher): a symlinked legacy config file is copied through the link on the first write', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  if (!canSymlink(s.root)) return t.skip('symlinks need privilege on Windows');
  // Mutation captured: deciding with lstatSync (the link is not a regular
  // file) skips the copy, so the new file holds only the new key and the
  // legacy keys stop being read.
  const target = path.join(s.root, 'shared.conf');
  fs.writeFileSync(target, 'herd_label=crew\nmax_workers=9\n');
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  fs.symlinkSync(target, s.legacyUser);
  const r = s.run(['config', 'set', '--user', 'layout', 'tab']);
  assert.equal(r.rc, 0, r.err);
  assert.ok(r.err.includes(`copied legacy config ${s.legacyUser} to ${s.user}`), r.err);
  assert.equal(fs.readFileSync(s.user, 'utf8'), 'herd_label=crew\nmax_workers=9\nlayout=tab\n');
  assert.equal(fs.lstatSync(s.user).isSymbolicLink(), false);
  assert.ok(fs.lstatSync(s.legacyUser).isSymbolicLink(), 'the legacy link stays');
  assert.equal(fs.readFileSync(target, 'utf8'), 'herd_label=crew\nmax_workers=9\n');
});

test('config (launcher): state_dir from the defaults shows the legacy directory the commands use', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: printing cfg(state_dir) for the defaults row shows
  // .herdr-soho while every command uses .herdr-agents.
  fs.mkdirSync(path.join(s.repo, '.herdr-agents'));
  const r = s.run(['config']);
  assert.equal(r.rc, 0, r.err);
  assert.match(r.out, /^state_dir +\.herdr-agents +defaults$/m);
});

test('setup + doctor (launcher): a separate CLAUDE.md that keeps the legacy block is named', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: treating the legacy marker as "has a block" in the
  // separate-CLAUDE.md check hides it from setup, and a doctor that only
  // reads the target file prints ok for AGENTS.md.
  const legacyBlock = '<!-- herdr-agents:start -->\nold\n<!-- herdr-agents:end -->\n';
  fs.writeFileSync(path.join(s.repo, 'AGENTS.md'), `# A\n\n${legacyBlock}`);
  fs.writeFileSync(path.join(s.repo, 'CLAUDE.md'), `# C\n\n${legacyBlock}`);
  const r = s.run(['setup', '--no-hooks']);
  assert.equal(r.rc, 0, r.err);
  assert.ok(r.err.includes("CLAUDE.md exists separately and still has the legacy herdr-agents block: run 'setup --target CLAUDE.md' too"), r.err);
  const d = s.doctorRun();
  assert.ok(warnLine(d.out, 'legacy herdr-agents instruction block in CLAUDE.md'), d.out);
  const t2 = s.run(['setup', '--no-hooks', '--target', 'CLAUDE.md']);
  assert.equal(t2.rc, 0, t2.err);
  const d2 = s.doctorRun();
  assert.ok(!d2.out.includes('legacy herdr-agents instruction block'), d2.out);
});

const LEGACY_PHRASES = [
  'legacy environment variable',
  'legacy user config',
  'legacy project config',
  'legacy state dir',
];

test('doctor (launcher): without any legacy nothing new is printed', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: the legacyDoctorWarnings loop printing unconditionally
  // in doctorCheck (or a warning pushed without its condition) adds a line.
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  for (const p of LEGACY_PHRASES) assert.ok(!r.out.includes(p), `unexpected legacy line:\n${r.out}`);
});

test('doctor (launcher): the copied env variable is named', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the copied-variable loop from
  // legacyDoctorWarnings (or the legacyDoctorWarnings call in doctorCheck)
  // drops the line.
  const r = s.doctorRun({ HERDR_AGENTS_LAYOUT: 'tab' });
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, 'legacy environment variable HERDR_AGENTS_LAYOUT is read as HERDR_SOHO_LAYOUT; rename it'), r.out);
});

test('doctor (launcher): the legacy user config in use is named with the copy destination', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: inverting the user-config condition in
  // legacyDoctorWarnings (in use only when both exist) drops the line.
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  fs.writeFileSync(s.legacyUser, 'herd_label=crew\n');
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, `legacy user config in use: ${s.legacyUser} (the next 'config set --user' copies it to ${s.user})`), r.out);
});

test('doctor (launcher): the legacy user config next to the new one is no longer read', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the "both exist" user-config branch from
  // legacyDoctorWarnings drops the line.
  fs.mkdirSync(path.dirname(s.legacyUser), { recursive: true });
  fs.writeFileSync(s.legacyUser, 'herd_label=crew\n');
  fs.mkdirSync(path.dirname(s.user), { recursive: true });
  fs.writeFileSync(s.user, 'herd_label=crew\n');
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, `legacy user config ${s.legacyUser} is no longer read (${s.user} exists); remove it once you no longer need it`), r.out);
});

test('doctor (launcher): the legacy project config in use is named with the copy destination', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the project-config block from
  // legacyDoctorWarnings drops the line.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'max_workers=5\n');
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, `legacy project config in use: ${s.legacyProj} (the next 'config set' copies it to ${s.proj})`), r.out);
});

test('doctor (launcher): the legacy project config next to the new one is no longer read', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the "both exist" project-config branch from
  // legacyDoctorWarnings drops the line.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'max_workers=5\n');
  fs.writeFileSync(s.proj, 'max_workers=5\n');
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, `legacy project config ${s.legacyProj} is no longer read (${s.proj} exists); remove it once you no longer need it`), r.out);
});

test('doctor (launcher): the legacy state dir in use is named when nothing pins the state dir', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the state-dir-in-use branch from
  // legacyDoctorWarnings (or counting the defaults file's state_dir as a
  // choice) drops the line; the state dir line shows the legacy dir.
  fs.mkdirSync(path.join(s.repo, '.herdr-agents'));
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  const legacyState = path.join(s.repo, '.herdr-agents');
  assert.ok(r.out.split('\n').some((l) => l.startsWith('ok     state dir writable: ') && l.endsWith(legacyState)), r.out);
  assert.ok(warnLine(r.out, `legacy state dir in use: ${legacyState} (once no worker is live, rename it to .herdr-soho and ignore .herdr-soho/ in git)`), r.out);
});

test('doctor (launcher): the legacy state dir next to the new one is no longer used', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: removing the "both dirs exist" state-dir branch from
  // legacyDoctorWarnings drops the line.
  fs.mkdirSync(path.join(s.repo, '.herdr-agents'));
  fs.mkdirSync(path.join(s.repo, '.herdr-soho'));
  const r = s.doctorRun();
  assert.equal(r.rc, 0, r.err);
  assert.ok(warnLine(r.out, `legacy state dir ${path.join(s.repo, '.herdr-agents')} is no longer used (${path.join(s.repo, '.herdr-soho')} exists); clean it once its reports are no longer needed`), r.out);
});

test('setup --plan (launcher): the before side seeds from the legacy project file when the new one is absent', (t) => {
  const s = setup();
  t.after(() => s.cleanup());
  // Mutation captured: the plan copying the new project path again (no
  // effectiveConfigFile in setup-plan.mjs) seeds an empty file: the custom
  // lane reads as a preset and the lane.build.roles line (and the stderr
  // "custom" warn) are gone from the plan.
  fs.mkdirSync(path.dirname(s.legacyProj), { recursive: true });
  fs.writeFileSync(s.legacyProj, 'lane.build.roles=scouter,implementer\n');
  const r = s.run(['setup', '--plan', '--panes', '4']);
  assert.equal(r.rc, 0, r.err);
  assert.ok(r.err.includes('lane roles in') && r.err.includes('are custom'), `stderr:\n${r.err}`);
  assert.ok(r.out.includes('  lane.build.roles     (unset) → scouter,implementer'), `plan:\n${r.out}`);
  // The path shown stays the new one.
  assert.ok(r.out.includes(`${s.proj}\n`), `plan:\n${r.out}`);
  assert.ok(!fs.existsSync(s.proj), 'the plan must not write the project file');
});
