// The direct herdr-agents → herdr-soho switch, end to end, with the exact
// hook commands old projects carry: once the old skill is gone the legacy
// SessionStart hook finds no script (and says so, exit 0); `herdr-soho
// setup` then renames the old block in place (keeping its text), replaces
// the old hooks, keeps the
// project's own content and hooks, and the new hooks run the new doctor.
// Every HOME and project is temporary; every spawnSync carries a timeout.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { cmdInvocation, findExecutable } from '../lib/platform.mjs';
import {
  LEGACY_SETUP_END, LEGACY_SETUP_START, SETUP_END, SETUP_START, legacyHookDoctor, legacyHookReminder,
  setupHookDoctor, setupHookReminder,
} from '../lib/setuptext.mjs';

const SKILL_SRC = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const USER_HOOK = "sh -c 'echo my own herdr-agents note'";
const SHELL = process.platform === 'win32'
  ? findExecutable('sh') ?? (() => {
    const git = findExecutable('git');
    const bundled = git && path.resolve(path.dirname(git), '..', 'usr', 'bin', 'sh.exe');
    return bundled && fs.existsSync(bundled) ? bundled : findExecutable('bash') ?? 'sh';
  })()
  : '/bin/sh';

function fixture() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'ha-migration-')));
  const home = path.join(root, 'home');
  const proj = path.join(root, 'project');
  const tmp = path.join(root, 'tmp');
  for (const d of [home, proj, tmp, path.join(proj, '.claude')]) fs.mkdirSync(d, { recursive: true });
  // The new skill installed the way `bunx skills add … -g` lays it out; the
  // old herdr-agents directory is gone (the direct switch).
  fs.cpSync(SKILL_SRC, path.join(home, '.agents', 'skills', 'herdr-soho'), {
    recursive: true,
    filter: (src) => !src.includes(`${path.sep}scripts${path.sep}test${path.sep}`),
  });
  spawnSync('git', ['init', '-q'], { cwd: proj, stdio: 'ignore', timeout: 30000 });
  fs.writeFileSync(path.join(proj, 'AGENTS.md'),
    `# Project\n\nOwn intro.\n\n${LEGACY_SETUP_START}\nold block body\n${LEGACY_SETUP_END}\n\nOwn tail.\n`);
  const settings = {
    permissions: { allow: ['Bash(git status)'] },
    hooks: {
      UserPromptSubmit: [
        { hooks: [{ type: 'command', command: legacyHookReminder() }] },
        { hooks: [{ type: 'command', command: USER_HOOK }] },
      ],
      SessionStart: [{ hooks: [{ type: 'command', command: legacyHookDoctor() }] }],
    },
  };
  fs.writeFileSync(path.join(proj, '.claude', 'settings.json'), `${JSON.stringify(settings, null, 2)}\n`);
  const env = { PATH: [path.dirname(SHELL), process.env.PATH].filter(Boolean).join(path.delimiter), HOME: home, USERPROFILE: home, TMPDIR: tmp };
  // Claude Code runs a hook command through the shell, in the project, with
  // CLAUDE_PROJECT_DIR set.
  const runHook = (command) => spawnSync(SHELL, ['-c', command], {
    cwd: proj, env: { ...env, HERDR_ENV: '1', CLAUDE_PROJECT_DIR: proj }, encoding: 'utf8', timeout: 120000,
  });
  const launcher = path.join(home, '.agents', 'skills', 'herdr-soho', 'scripts', 'herdr-soho');
  const cli = (args) => {
    const windowsLauncher = `${launcher}.cmd`;
    const invocation = process.platform === 'win32'
      ? cmdInvocation(windowsLauncher, args, env)
      : { command: launcher, args };
    return spawnSync(invocation.command, invocation.args, {
      cwd: proj, env, encoding: 'utf8', timeout: 120000,
      windowsVerbatimArguments: invocation.windowsVerbatimArguments,
    });
  };
  return { root, home, proj, runHook, cli, cleanup: () => fs.rmSync(root, { recursive: true, force: true }) };
}

const hookCommands = (proj) => {
  const doc = JSON.parse(fs.readFileSync(path.join(proj, '.claude', 'settings.json'), 'utf8'));
  const out = [];
  for (const ev of Object.values(doc.hooks)) for (const e of ev) for (const h of e.hooks) out.push(h.command);
  return { doc, cmds: out };
};

test('before setup, the legacy SessionStart hook finds no script once herdr-agents is removed (exit 0)', (t) => {
  const f = fixture();
  t.after(f.cleanup);
  // Mutation captured: a legacy hook text that no longer matches what old
  // projects carry (another script path) changes this output.
  const r = f.runHook(legacyHookDoctor());
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, 'herdr-agents doctor: skill script not found\n');
});

test('herdr-soho setup renames the old block in place, replaces the old hooks, and keeps the project\'s own content and hooks', (t) => {
  const f = fixture();
  t.after(f.cleanup);
  // Mutation captured: dropping the legacy-command match in the hooks merge
  // leaves both the old and the new SessionStart hook; regenerating the
  // block drops the project's own `old block body` line.
  const r = f.cli(['setup']);
  assert.equal(r.status, 0, `${r.stdout}\n${r.stderr}`);
  const agents = fs.readFileSync(path.join(f.proj, 'AGENTS.md'), 'utf8');
  assert.equal(agents, `# Project\n\nOwn intro.\n\n${SETUP_START}\nold block body\n${SETUP_END}\n\nOwn tail.\n`);
  const { doc, cmds } = hookCommands(f.proj);
  assert.ok(!cmds.includes(legacyHookReminder()) && !cmds.includes(legacyHookDoctor()), cmds.join('\n'));
  assert.ok(cmds.includes(setupHookReminder()) && cmds.includes(setupHookDoctor()), cmds.join('\n'));
  assert.ok(cmds.includes(USER_HOOK), 'the project\'s own hook stays');
  assert.deepEqual(doc.permissions, { allow: ['Bash(git status)'] });
});

test('after setup, the new SessionStart hook runs the new doctor with no legacy block or hook line', (t) => {
  const f = fixture();
  t.after(f.cleanup);
  assert.equal(f.cli(['setup']).status, 0);
  // Mutation captured: a doctor that still reads the pre-setup file state
  // (or a hook merge that kept the legacy command) prints a legacy line.
  const r = f.runHook(setupHookDoctor());
  assert.equal(r.status, 0, r.stderr);
  const lines = r.stdout.split('\n').filter((l) => l !== '');
  for (const l of lines) assert.ok(l.startsWith('herdr-soho doctor: '), l);
  assert.ok(!r.stdout.includes('skill script not found'), r.stdout);
  assert.ok(!r.stdout.includes('legacy herdr-agents'), r.stdout);
  const d = f.cli(['doctor']);
  assert.ok(d.stdout.includes('instruction block present in AGENTS.md'), d.stdout);
  assert.ok(d.stdout.includes('Claude hooks present in .claude/settings.json'), d.stdout);
  assert.ok(!fs.readFileSync(path.join(f.proj, 'AGENTS.md'), 'utf8').includes(SETUP_START.replace('soho', 'agents')));
});

test('setup --dry-run previews the in-place rename and refuses an unterminated legacy block, writing nothing', (t) => {
  const f = fixture();
  t.after(f.cleanup);
  const file = path.join(f.proj, 'AGENTS.md');
  const before = fs.readFileSync(file, 'utf8');
  // Mutation captured: printing setupBlock() for a legacy-only file hides
  // the project's own `old block body` line the write keeps.
  const r = f.cli(['setup', '--dry-run', '--no-hooks']);
  assert.equal(r.status, 0, r.stderr);
  assert.equal(r.stdout, `# would write to ${file}\n${SETUP_START}\nold block body\n${SETUP_END}\n`);
  assert.equal(fs.readFileSync(file, 'utf8'), before);
  // Mutation captured: skipping setupBlockResult in the dry run prints the
  // canonical block and exits 0 where the write dies 4.
  const open = `# Project\n\n${LEGACY_SETUP_START}\nno end\n`;
  fs.writeFileSync(file, open);
  const u = f.cli(['setup', '--dry-run', '--no-hooks']);
  assert.equal(u.status, 4, u.stderr);
  assert.equal(u.stdout, '');
  assert.match(u.stderr, /produced an incomplete file for .*AGENTS\.md \(file left untouched\)/);
  assert.equal(fs.readFileSync(file, 'utf8'), open);
});
