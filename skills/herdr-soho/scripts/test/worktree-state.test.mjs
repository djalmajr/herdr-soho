import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { writeFakeCli } from './fakes.mjs';
import { nodeBin } from './parity.mjs';
import { stateProjectRoot } from '../lib/platform.mjs';
import { stateRootPath } from '../lib/config.mjs';

const ENTRY = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'herdr-soho.mjs');
const HEADER = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';

function run(root, env, args) {
  return spawnSync(nodeBin(), [ENTRY, ...args], { cwd: root, env, encoding: 'utf8', timeout: 60_000 });
}

function initRepo(root) {
  fs.mkdirSync(root, { recursive: true });
  const git = (args) => {
    const r = spawnSync('git', args, { cwd: root, encoding: 'utf8' });
    assert.equal(r.status, 0, r.stderr);
  };
  git(['init', '-q']);
  git(['config', 'user.email', 'test@example.invalid']);
  git(['config', 'user.name', 'Test']);
  fs.writeFileSync(path.join(root, '.gitignore'), '.herdr-soho/\n');
  fs.writeFileSync(path.join(root, 'tracked'), 'test\n');
  git(['add', '.gitignore', 'tracked']);
  git(['commit', '-qm', 'fixture']);
}

// Mutation captured: resolving linked-worktree state under the worker creates
// an empty roster and causes commands to miss the main checkout's agent row.
test('roster, status, and wait in a linked worktree read the main checkout state', { timeout: 60000 }, () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-state-worktree-'));
  const repo = path.join(temp, 'repo');
  const worker = path.join(repo, '.worktrees', 'worker');
  const bin = path.join(temp, 'bin');
  try {
    initRepo(repo);
    const realRepo = fs.realpathSync(repo);
    fs.mkdirSync(path.dirname(worker), { recursive: true });
    const added = spawnSync('git', ['worktree', 'add', '-q', '-b', 'worker', worker], { cwd: repo, encoding: 'utf8' });
    assert.equal(added.status, 0, added.stderr);
    const state = path.join(repo, '.herdr-soho', 'ws');
    fs.mkdirSync(path.join(state, 'reports'), { recursive: true });
    fs.mkdirSync(path.join(state, 'wait'), { recursive: true });
    fs.writeFileSync(path.join(state, 'agents.tsv'), HEADER + `build\tp1\tgrok\timplementer\txai\t0\t${worker}\t20260928T120000\t\task\timplementer\t\n`);
    fs.writeFileSync(path.join(state, 'last-report-build'), path.join(state, 'reports', 'build.md') + '\n');
    fs.writeFileSync(path.join(state, 'reports', 'build.md'), 'completed\n');
    fs.mkdirSync(bin);
    writeFakeCli(bin, 'herdr', `
const [group, action] = process.argv.slice(2, 4);
if (group === 'agent' && action === 'list') process.stdout.write('{"result":{"agents":[]}}\\n');
else if (group === 'pane' && action === 'list') process.stdout.write('{"result":{"panes":[]}}\\n');
else if (group === 'tab' && action === 'list') process.stdout.write('{"result":{"tabs":[]}}\\n');
else { process.stderr.write('unexpected fake herdr call\\n'); process.exit(1); }
`);
    const env = {
      ...process.env,
      PATH: `${bin}${path.delimiter}${process.env.PATH}`,
      HERDR_SOCKET_PATH: path.join(temp, 'no-herdr-socket'),
      HERDR_WORKSPACE_ID: 'ws',
      HERDR_ENV: '1',
    };
    const roster = run(worker, env, ['roster']);
    assert.equal(roster.status, 0, roster.stderr);
    assert.match(roster.stdout, /build\s+implementer/);
    assert.equal(fs.existsSync(path.join(worker, '.herdr-soho')), false);

    const status = run(worker, env, ['status', 'build']);
    assert.equal(status.status, 0, status.stderr);
    assert.match(status.stdout, /build\tdone/);
    assert.equal(fs.existsSync(path.join(worker, '.herdr-soho')), false);

    const wait = run(worker, env, ['wait', 'build', '--timeout', '1000']);
    assert.equal(wait.status, 0, wait.stderr);
    assert.match(wait.stdout, /"agent":"build"/);
    assert.equal(fs.existsSync(path.join(worker, '.herdr-soho')), false);
    assert.equal(stateProjectRoot(env, worker), realRepo);
    assert.equal(stateRootPath({ entries: new Map() }, env, worker), path.join(realRepo, '.herdr-soho'));
  } finally {
    fs.rmSync(temp, { recursive: true, force: true });
  }
});

// Mutation captured: applying the shared-root rule to a normal checkout or
// non-git cwd changes its established state location.
test('state root preserves the main checkout and non-git path behavior', () => {
  const temp = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-state-root-'));
  const repo = path.join(temp, 'repo');
  const outside = path.join(temp, 'outside');
  try {
    initRepo(repo);
    fs.mkdirSync(outside);
    const env = { ...process.env };
    const realRepo = fs.realpathSync(repo);
    const realOutside = fs.realpathSync(outside);
    assert.equal(stateProjectRoot(env, realRepo), realRepo);
    assert.equal(stateRootPath({ entries: new Map() }, env, realRepo), path.join(realRepo, '.herdr-soho'));
    assert.equal(stateProjectRoot(env, realOutside), realOutside);
    assert.equal(stateRootPath({ entries: new Map() }, env, realOutside), path.join(realOutside, '.herdr-soho'));
  } finally { fs.rmSync(temp, { recursive: true, force: true }); }
});
