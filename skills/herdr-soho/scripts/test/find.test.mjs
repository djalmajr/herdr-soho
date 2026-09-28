// `find`: the pane search over `herdr api snapshot` — TSV/JSON output,
// word/ref matching, machine selection (--machine/--all), and the exit
// codes (0 entries, 1 none, 2 usage, 4 local down). The fake `herdr`
// (writeFakeCli) answers `api snapshot` with fixed snapshots per machine
// (`--machine` on the argument line) and `machine list --json` with a
// fixed machine array, like herdr 0.9.1 (a bare JSON array).
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { writeFakeCli } from './fakes.mjs';
import { nodeBin, fixtureEnv } from './parity.mjs';
import { fetchSessions, machineList, matchEntries, sessionEntries } from '../lib/sessions.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');
const USAGE_LINE = 'herdr-soho: usage: find [search words] [--machine <label>]... [--all] [--json]\n';

// Fixed snapshots (the shape of `herdr api snapshot` result.snapshot,
// herdr 0.9.1): local has one workspace with two panes (one agentless)
// and a second workspace; windows has two panes, one agentless and one
// without a cwd.
const LOCAL_SNAP = {
  version: '0.9.1', protocol: 22,
  workspaces: [
    { workspace_id: 'w1', number: 1, label: 'soho', focused: true, pane_count: 2, tab_count: 1, active_tab_id: 'w1:t1', agent_status: 'working' },
    { workspace_id: 'w2', number: 2, label: 'docs', focused: false, pane_count: 1, tab_count: 1, active_tab_id: 'w2:t1', agent_status: 'idle' },
  ],
  tabs: [
    { tab_id: 'w1:t1', workspace_id: 'w1', number: 1, label: 'main', focused: true, pane_count: 2, agent_status: 'working' },
    { tab_id: 'w2:t1', workspace_id: 'w2', number: 1, label: '1', focused: false, pane_count: 1, agent_status: 'idle' },
  ],
  panes: [
    { pane_id: 'w1:p1', terminal_id: 't1', workspace_id: 'w1', tab_id: 'w1:t1', focused: true, agent_status: 'working', revision: 1,
      agent: 'pi', cwd: '/Users/x', foreground_cwd: '/Users/x/soho', terminal_title: 'raw title', title: 'pane title S2' },
    { pane_id: 'w1:p2', terminal_id: 't2', workspace_id: 'w1', tab_id: 'w1:t1', focused: false, agent_status: 'unknown', revision: 1,
      cwd: '/srv/agents/jobs', terminal_title_stripped: 'shell on /srv/agents/jobs' },
    { pane_id: 'w2:p1', terminal_id: 't3', workspace_id: 'w2', tab_id: 'w2:t1', focused: false, agent_status: 'idle', revision: 1,
      agent: 'claude', cwd: '/repo/docs' },
  ],
  agents: [
    { terminal_id: 't1', agent_status: 'working', workspace_id: 'w1', tab_id: 'w1:t1', pane_id: 'w1:p1', focused: true, revision: 1,
      agent: 'pi', name: 'build', cwd: '/Users/x', foreground_cwd: '/Users/x/soho', title: 'implementer: S2' },
    { terminal_id: 't3', agent_status: 'idle', workspace_id: 'w2', tab_id: 'w2:t1', pane_id: 'w2:p1', focused: false, revision: 1,
      agent: 'claude', name: 'review', cwd: '/repo/docs' },
  ],
  layouts: [],
  focused_workspace_id: 'w1', focused_tab_id: 'w1:t1', focused_pane_id: 'w1:p1',
};

const WINDOWS_SNAP = {
  version: '0.9.1', protocol: 22,
  workspaces: [
    { workspace_id: 'w3', number: 3, label: 'pinar', focused: true, pane_count: 2, tab_count: 1, active_tab_id: 'w3:t1', agent_status: 'working' },
  ],
  tabs: [
    { tab_id: 'w3:t1', workspace_id: 'w3', number: 1, label: '1', focused: true, pane_count: 2, agent_status: 'working' },
  ],
  panes: [
    { pane_id: 'w3:p1', terminal_id: 't5', workspace_id: 'w3', tab_id: 'w3:t1', focused: true, agent_status: 'working', revision: 1,
      agent: 'codex', cwd: 'C:\\Users\\dj4lm\\pinar' },
    { pane_id: 'w3:p2', terminal_id: 't6', workspace_id: 'w3', tab_id: 'w3:t1', focused: false, agent_status: 'unknown', revision: 1 },
  ],
  agents: [
    { terminal_id: 't5', agent_status: 'working', workspace_id: 'w3', tab_id: 'w3:t1', pane_id: 'w3:p1', focused: true, revision: 1,
      agent: 'codex', name: 'orchestrator', cwd: 'C:\\Users\\dj4lm\\pinar' },
  ],
  layouts: [],
  focused_workspace_id: 'w3', focused_tab_id: 'w3:t1', focused_pane_id: 'w3:p1',
};

// The herdr 0.9.1 shape of `herdr machine list --json`: a bare JSON array.
const MACHINES = [
  { id: '629837b6a0ce6b0229bc33117efc55f6', label: 'hetzner', target: 'agent@167.235.206.217', session: 'default', enabled: true, selected: false },
  { id: 'e6f86db76246f5b20a5ced7a5c713622', label: 'windows', target: 'alien', session: 'default', enabled: true, selected: false },
  { id: '9f8e7d6c5b4a39281706f5e4d3c2b1a0', label: 'retired', target: 'old@example.com', session: 'default', enabled: false, selected: false },
];

const FAKE = `const SNAPSHOTS = ${JSON.stringify({ local: LOCAL_SNAP, windows: WINDOWS_SNAP })};
const MACHINES = ${JSON.stringify(MACHINES)};
let machine = 'local';
const args = process.argv.slice(2);
const rest = [];
for (let i = 0; i < args.length; i += 1) {
  if (args[i] === '--machine') { machine = String(args[i + 1] ?? 'local'); i += 1; continue; }
  rest.push(args[i]);
}
if (rest[0] === 'machine' && rest[1] === 'list') {
  if (rest.includes('--json')) {
    process.stdout.write(JSON.stringify(MACHINES) + '\\n');
    process.exit(0);
  }
  process.exit(0);
}
if (rest[0] === 'api' && rest[1] === 'snapshot') {
  if (machine === 'local' && process.env.FAKE_HERDR_LOCAL_DOWN === '1') {
    process.stderr.write('herdr server not running (fake)\\n');
    process.exit(3);
  }
  const snap = SNAPSHOTS[machine];
  if (!snap) {
    process.stderr.write('unknown machine (fake)\\n');
    process.exit(7);
  }
  process.stdout.write(JSON.stringify({ id: 'cli:api:snapshot', result: { snapshot: snap } }) + '\\n');
  process.exit(0);
}
process.exit(0);
`;

const L1 = 'local/w1:p1\tbuild\tpi\tworking\tsoho\tmain\t/Users/x/soho';
const L2 = 'local/w1:p2\t-\t-\tunknown\tsoho\tmain\t/srv/agents/jobs';
const L3 = 'local/w2:p1\treview\tclaude\tidle\tdocs\t1\t/repo/docs';
const LOCAL_LINES = `${L1}\n${L2}\n${L3}\n`;
const W1 = 'windows/w3:p1\torchestrator\tcodex\tworking\tpinar\t1\tC:\\Users\\dj4lm\\pinar';
const W2 = 'windows/w3:p2\t-\t-\tunknown\tpinar\t1\t-';
const WINDOWS_LINES = `${W1}\n${W2}\n`;

function makeFix() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'ha-find-')));
  const bin = path.join(root, 'bin');
  const state = path.join(root, 'state');
  fs.mkdirSync(state);
  fs.mkdirSync(bin);
  const env = fixtureEnv({
    HOME: path.join(root, 'home'),
    XDG_CONFIG_HOME: path.join(root, 'config'),
    HERDR_SOHO_DIR: state,
    TMPDIR: path.join(root, 'tmp'),
  });
  for (const d of [env.HOME, env.XDG_CONFIG_HOME, env.TMPDIR]) fs.mkdirSync(d, { recursive: true });
  writeFakeCli(bin, 'herdr', FAKE);
  return {
    root, bin, env,
    // The child runs the real entry with the fake `herdr` first on PATH.
    runFind(args, extraEnv = {}) {
      const result = spawnSync(nodeBin(), [ENTRY, 'find', ...args], {
        cwd: root,
        env: { ...env, PATH: `${bin}${path.delimiter}${env.PATH}`, ...extraEnv },
        encoding: 'utf8',
        timeout: 60_000,
      });
      return { status: result.status, stdout: result.stdout ?? '', stderr: result.stderr ?? '', error: result.error };
    },
    // A direct (in-process) herdr env for the lib-level calls.
    herdrEnv(extraEnv = {}) {
      return { ...env, PATH: `${bin}${path.delimiter}${env.PATH}`, ...extraEnv };
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
}

// Mutation captured: printing an empty cell blank instead of '-', reordering
// the TSV columns, or listing the panes out of snapshot order breaks the
// exact default output.
test('find: no search lists the local panes as TSV in snapshot order', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind([]);
    assert.equal(r.error, undefined);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, LOCAL_LINES);
    assert.equal(r.stderr, '');
  } finally { fix.cleanup(); }
});

// Mutation captured: case-sensitive matching, or dropping `name` from the
// searched fields, misses the agent's name.
test('find: a partial agent name matches case-insensitively', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    for (const word of ['build', 'BUILD']) {
      const r = fix.runFind([word]);
      assert.equal(r.status, 0, r.stderr);
      assert.equal(r.stdout, `${L1}\n`);
    }
    const r = fix.runFind(['rev']);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, `${L3}\n`);
  } finally { fix.cleanup(); }
});

// Mutation captured: OR between the words, or dropping `kind`/`status`
// from the searched fields, makes `claude working` match a pane.
test('find: two words require both (kind + status)', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['claude', 'idle']);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, `${L3}\n`);
    const none = fix.runFind(['claude', 'working']);
    assert.equal(none.status, 1, none.stderr);
    assert.equal(none.stdout, '');
    assert.equal(none.stderr, '');
  } finally { fix.cleanup(); }
});

// Mutation captured: dropping `workspace_label` from the searched fields
// loses the panes of a workspace searched by its label.
test('find: a workspace label matches its panes', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['soho']);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, `${L1}\n${L2}\n`);
    const docs = fix.runFind(['docs']);
    assert.equal(docs.status, 0, docs.stderr);
    assert.equal(docs.stdout, `${L3}\n`);
  } finally { fix.cleanup(); }
});

// Mutation captured: dropping `cwd` from the searched fields loses the
// pane searched by a cwd fragment (hostile slashes included).
test('find: a cwd fragment matches on both machines', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['/srv/agents/jobs']);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, `${L2}\n`);
    const win = fix.runFind(['--machine', 'windows', 'C:\\Users\\dj4lm']);
    assert.equal(win.status, 0, win.stderr);
    assert.equal(win.stdout, `${W1}\n`);
    assert.equal(win.stderr, '');
  } finally { fix.cleanup(); }
});

// Mutation captured: treating a reference as a plain substring (w3:p2
// leaks into `windows/w3:p1`), or not querying the machine the reference
// names (the windows pane is missing, exit 1).
test('find: a reference matches only its machine and pane id', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    for (const ref of ['w1:p1', 'local/w1:p1']) {
      const r = fix.runFind([ref]);
      assert.equal(r.status, 0, r.stderr);
      assert.equal(r.stdout, `${L1}\n`);
      assert.equal(r.stderr, '');
    }
    // Not a reference (parseRef is null): the substring search applies.
    const partial = fix.runFind(['w1:p']);
    assert.equal(partial.status, 0, partial.stderr);
    assert.equal(partial.stdout, `${L1}\n${L2}\n`);
    // A remote reference queries that machine without --machine, and only
    // that pane (w3:p2 stays out).
    const remote = fix.runFind(['windows/w3:p1']);
    assert.equal(remote.status, 0, remote.stderr);
    assert.equal(remote.stdout, `${W1}\n`);
    assert.equal(remote.stderr, '');
    const remoteBare = fix.runFind(['windows/w3:p2']);
    assert.equal(remoteBare.status, 0, remoteBare.stderr);
    assert.equal(remoteBare.stdout, `${W2}\n`);
  } finally { fix.cleanup(); }
});

// Mutation captured: pane-level fields winning over the agent's, or a
// missing title → terminal_title_stripped fallback, changes the --json
// entry (w1:p1 keeps the pane title; w1:p2 loses its title).
test('find: --json prints one entry object per line', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['--json', 'w1:p2']);
    assert.equal(r.status, 0, r.stderr);
    const lines = r.stdout.trim().split('\n');
    assert.equal(lines.length, 1);
    assert.deepEqual(JSON.parse(lines[0]), {
      ref: 'local/w1:p2', machine: 'local',
      workspace_id: 'w1', workspace_label: 'soho',
      tab_id: 'w1:t1', tab_label: 'main',
      pane_id: 'w1:p2', name: null, kind: null,
      status: 'unknown', cwd: '/srv/agents/jobs',
      title: 'shell on /srv/agents/jobs', focused: false,
    });
    const occupied = fix.runFind(['--json', 'w1:p1']);
    assert.equal(occupied.status, 0, occupied.stderr);
    const e = JSON.parse(occupied.stdout.trim());
    assert.equal(e.name, 'build');
    assert.equal(e.kind, 'pi');
    assert.equal(e.status, 'working');
    assert.equal(e.cwd, '/Users/x/soho');
    assert.equal(e.title, 'implementer: S2');
    assert.equal(e.focused, true);
    assert.equal(Object.keys(e).join(','),
      'ref,machine,workspace_id,workspace_label,tab_id,tab_label,pane_id,name,kind,status,cwd,title,focused');
  } finally { fix.cleanup(); }
});

// Mutation captured: a failing remote machine aborting the lookup (non-zero
// exit, missing windows lines), or querying the disabled machine (an extra
// failure line), breaks the continue-on-failure contract.
test('find: --all queries the enabled machines; a failing one warns and does not stop', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['--all']);
    assert.equal(r.status, 0, r.stderr);
    assert.equal(r.stdout, LOCAL_LINES + WINDOWS_LINES);
    assert.equal(r.stderr, "herdr-soho: find: machine 'hetzner' unavailable: unknown machine (fake)\n");
  } finally { fix.cleanup(); }
});

// Mutation captured: exiting 0 (or 4) when no entry matches, instead of
// grep's exit 1.
test('find: no match exits 1 with empty output', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const r = fix.runFind(['zzz-no-such-thing']);
    assert.equal(r.status, 1, r.stderr);
    assert.equal(r.stdout, '');
    assert.equal(r.stderr, '');
  } finally { fix.cleanup(); }
});

// Mutation captured: a local failure treated as an ordinary machine failure
// (exit 1, or 0 when the other machine matched) instead of exit 4.
test('find: the local machine down exits 4, even when another machine has entries', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const down = fix.runFind([], { FAKE_HERDR_LOCAL_DOWN: '1' });
    assert.equal(down.status, 4, down.stderr);
    assert.equal(down.stdout, '');
    assert.equal(down.stderr, 'herdr-soho: find: machine \'local\' unavailable: herdr server not running (fake)\n');
    const other = fix.runFind(['--machine', 'windows', 'pinar'], { FAKE_HERDR_LOCAL_DOWN: '1' });
    assert.equal(other.status, 4, other.stderr);
    assert.equal(other.stdout, WINDOWS_LINES);
    assert.equal(other.stderr, 'herdr-soho: find: machine \'local\' unavailable: herdr server not running (fake)\n');
  } finally { fix.cleanup(); }
});

// Mutation captured: accepting an unknown flag or a `--machine` without its
// value (exit 0/1) instead of the usage exit 2.
test('find: bad usage exits 2 with the usage line', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    for (const args of [
      ['--bogus'], ['--machine'], ['--machine', '--json'], ['soho', '--machine'],
    ]) {
      const r = fix.runFind(args);
      assert.equal(r.status, 2, `${JSON.stringify(args)}: ${r.stderr}`);
      assert.equal(r.stdout, '');
      assert.equal(r.stderr, USAGE_LINE);
    }
  } finally { fix.cleanup(); }
});

// Mutation captured: a failing machine stopping the loop, or a non-array
// answer from `machine list` counted as a success, breaks the { entries,
// failures } pair (and --all would miss windows).
test('sessions: fetchSessions keeps going past a failure; machineList reads the bare array', { timeout: 60_000 }, () => {
  const fix = makeFix();
  try {
    const { entries, failures } = fetchSessions(['local', 'hetzner', 'windows'], { env: fix.herdrEnv() });
    assert.equal(entries.length, 5);
    assert.deepEqual(entries.map((e) => e.ref), ['local/w1:p1', 'local/w1:p2', 'local/w2:p1', 'windows/w3:p1', 'windows/w3:p2']);
    assert.deepEqual(failures, [{ machine: 'hetzner', cause: 'unknown machine (fake)' }]);
    const list = machineList({ env: fix.herdrEnv() });
    assert.equal(list.ok, true);
    assert.deepEqual(list.machines, [
      { label: 'hetzner', enabled: true },
      { label: 'windows', enabled: true },
      { label: 'retired', enabled: false },
    ]);
  } finally { fix.cleanup(); }
});

// Mutation captured: `foreground_cwd` not winning over `cwd`, the title
// fallback chain, or a missing workspace/tab label surfacing as anything
// but null, changes the entry fields.
test('sessions: sessionEntries joins pane + agent with the documented fallbacks', { timeout: 60_000 }, () => {
  const entries = sessionEntries(LOCAL_SNAP, 'local');
  assert.equal(entries.length, 3);
  const [p1, p2, p3] = entries;
  assert.equal(p1.cwd, '/Users/x/soho'); // foreground_cwd, not the launch cwd '/Users/x'
  assert.equal(p1.title, 'implementer: S2'); // the agent's title, over the pane's
  assert.equal(p2.cwd, '/srv/agents/jobs'); // the pane's cwd (no foreground_cwd, no agent)
  assert.equal(p2.title, 'shell on /srv/agents/jobs'); // terminal_title_stripped fallback
  assert.equal(p2.name, null);
  assert.equal(p2.kind, null);
  assert.equal(p3.status, 'idle');
  for (const unusable of [null, undefined, {}, { panes: 'nope' }, { panes: [null, { }, 'x', { pane_id: 'w9:p1' }] }]) {
    assert.deepEqual(sessionEntries(unusable, 'local'), unusable && Array.isArray(unusable.panes) ? [
      { ref: 'local/w9:p1', machine: 'local', workspace_id: null, workspace_label: null, tab_id: null,
        tab_label: null, pane_id: 'w9:p1', name: null, kind: null, status: null, cwd: null, title: null, focused: false },
    ] : []);
  }
});

// Mutation captured: dropping a field from MATCH_FIELDS (e.g. `machine`),
// turning the AND into an OR, or letting a reference match by substring,
// changes which of these entries survive.
test('sessions: matchEntries is AND over fields, and a single reference is exact', { timeout: 60_000 }, () => {
  const entries = [
    ...sessionEntries(LOCAL_SNAP, 'local'),
    ...sessionEntries(WINDOWS_SNAP, 'windows'),
  ];
  assert.equal(matchEntries(entries, []).length, 5);
  assert.equal(matchEntries(entries, ['W1:T1']).map((e) => e.pane_id).join(','), 'w1:p1,w1:p2', 'tab_id is searched');
  assert.equal(matchEntries(entries, ['WINDOWS']).map((e) => e.pane_id).join(','), 'w3:p1,w3:p2', 'the machine field is searched');
  // AND across different fields (name + cwd); OR would also match w1:p1
  // (name build) or the windows panes (cwd).
  assert.equal(matchEntries(entries, ['build', 'x/soho']).map((e) => e.pane_id).join(','), 'w1:p1');
  // A single reference: exact machine + pane id, nothing else.
  assert.equal(matchEntries(entries, ['windows/w3:p1']).map((e) => e.pane_id).join(','), 'w3:p1');
  assert.equal(matchEntries(entries, ['w3:p1']).map((e) => e.pane_id).join(','), ''); // bare ids are the local machine
  assert.deepEqual(matchEntries(entries, ['windows/w3:p1', 'extra']), [], 'a second word drops the reference form');
  assert.equal(matchEntries([], ['anything']).length, 0);
});
