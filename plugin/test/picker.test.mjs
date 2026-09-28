// Tests for the session picker (plugin/picker.mjs) and the clipboard
// (plugin/clipboard.mjs).
//
// Everything is hermetic: the skill CLI (`find --json`) and the `herdr`
// binary (`machine list --json`, `notification show`) are fakes in a temp
// dir, with a controlled PATH for the clipboard tools. The picker logic
// (state, filter, render, key handling) is driven without a TTY; the
// `main` integration test feeds a PassThrough stdin and captures the
// PassThrough stdout.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { Writable, PassThrough } from 'node:stream';
import {
  createState,
  filterEntries,
  feedChunk,
  flushEsc,
  render,
  entryLine,
  copyPayload,
  parseFindOutput,
  parseMachineList,
  FindError,
  loadEntries,
  main as pickerMain,
} from '../picker.mjs';
import {
  copyText,
  toolCandidates,
  osc52Sequence,
} from '../clipboard.mjs';

// ---------- fixtures and fakes ----------

// Contract line: one JSON per pane (the S2 `find --json` contract).
const LOCAL_1 = JSON.stringify({
  ref: 'local/w12:p1', machine: 'local', workspace_id: 'w12',
  workspace_label: 'appliance', tab_id: 'w12:t1', tab_label: '1',
  pane_id: 'w12:p1', name: 'orchestrator-10', kind: 'claude',
  status: 'working', cwd: '/Users/dj4lm/repo', title: 't1', focused: true,
});
const LOCAL_2 = JSON.stringify({
  ref: 'local/w14:pW', machine: 'local', workspace_id: 'w14',
  workspace_label: 'soho', tab_id: 'w14:t1', tab_label: '1',
  pane_id: 'w14:pW', name: null, kind: null,
  status: 'idle', cwd: '/tmp/soho', title: null, focused: false,
});
const WINDOWS_1 = JSON.stringify({
  ref: 'windows/w3:p1', machine: 'windows', workspace_id: 'w3',
  workspace_label: 'pinar', tab_id: 'w3:t1', tab_label: '1',
  pane_id: 'w3:p1',
  name: 'orchestrator', kind: 'codex',
  status: 'working', cwd: 'C:\\Users\\dj4lm\\pinar', title: null, focused: true,
});
const ENTRIES = [JSON.parse(LOCAL_1), JSON.parse(LOCAL_2), JSON.parse(WINDOWS_1)];

// A JS fake behind the platform's launcher (sh on POSIX, .cmd on
// Windows), mirroring plugin/test/bridge.test.mjs.
function writeLauncherFake(dir, name, script) {
  const js = path.join(dir, `${name}.fake.mjs`);
  fs.writeFileSync(js, `${script}\n`);
  if (process.platform === 'win32') {
    const cmd = path.join(dir, `${name}.cmd`);
    fs.writeFileSync(cmd, `@"${process.execPath}" "%~dp0${name}.fake.mjs" %*\r\n`);
    return cmd;
  }
  const sh = path.join(dir, name);
  const q = (v) => `'${String(v).replace(/'/g, "'\\''")}'`;
  fs.writeFileSync(sh, `#!/bin/sh\nexec ${q(process.execPath)} ${q(js)} "$@"\n`, { mode: 0o755 });
  return sh;
}

// A fake skill CLI answering `find --json [--machine <m>]`:
// cfg = { marker, localLines, per-machine specs {sleepMs, lines, exit, stderr} }.
function writeFakeFindCli(dir, name, cfg) {
  const script = `import fs from 'node:fs';
const cfg = ${JSON.stringify(cfg)};
const a = process.argv.slice(2);
const i = a.indexOf('--machine');
const machine = i === -1 ? null : a[i + 1];
if (cfg.marker) fs.appendFileSync(cfg.marker, JSON.stringify({ machine }) + '\\n');
const spec = machine === null ? cfg.local : (cfg[machine] || { sleepMs: 0, lines: [] });
const emit = () => {
  for (const l of (spec.lines || [])) process.stdout.write(l + '\\n');
  if (spec.stderr) process.stderr.write(spec.stderr);
  if (spec.doneFile) fs.writeFileSync(spec.doneFile, 'done');
  process.exit(spec.exit ?? 0);
};
if (spec.sleepMs) setTimeout(emit, spec.sleepMs); else emit();
`;
  const file = path.join(dir, `cli-${name}.mjs`);
  fs.writeFileSync(file, `#!/usr/bin/env node\n${script}\n`, { mode: 0o755 });
  return file;
}

// A fake `herdr` answering `machine list --json` and `notification show`
// (recorded in cfg.argvFile, one argv per line).
function writeFakeHerdr(dir, name, cfg) {
  const script = `import fs from 'node:fs';
const cfg = ${JSON.stringify(cfg)};
const a = process.argv.slice(2);
if (cfg.argvFile) fs.appendFileSync(cfg.argvFile, JSON.stringify(a) + '\\n');
if (a[0] === 'machine' && a[1] === 'list') { process.stdout.write(cfg.machineList); process.exit(0); }
if (a[0] === 'notification') process.exit(0);
process.exit(0);
`;
  return writeLauncherFake(dir, `herdr-${name}`, script);
}

// The real `machine list --json` shape: a bare array of
// { id, label, target, session, enabled, selected }.
function machineListJson(machines) {
  return JSON.stringify(machines.map((m) => ({
    id: `id-${m.label}`, label: m.label, target: 'x', session: 'default',
    enabled: m.enabled, selected: false,
  })));
}

function makeTmp(t) {
  const dir = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), 'herdr-soho-picker-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function waitUntil(fn, { timeoutMs = 5000, stepMs = 20 } = {}) {
  const end = Date.now() + timeoutMs;
  while (Date.now() < end) {
    if (fn()) return true;
    await sleep(stepMs);
  }
  return fn();
}

// A Writable that captures everything written to it.
function captureStream() {
  const chunks = [];
  const s = new Writable({
    write(c, _e, cb) { chunks.push(Buffer.from(c)); cb(); },
  });
  s.text = () => Buffer.concat(chunks).toString('utf8');
  return s;
}

// A fake clipboard tool: writes its stdin (and, when argvFile is set,
// its argv) to `out` and exits `exit`. On Windows it is a self-contained
// node fake behind a .cmd launcher (there is no /bin/sh).
function writeFakeTool(dir, name, { out, exit = 0, argvFile = null } = {}) {
  if (process.platform === 'win32') {
    const js = path.join(dir, `tool-${name}.mjs`);
    const rec = argvFile ? `require('node:fs').writeFileSync(${JSON.stringify(argvFile)}, process.argv.slice(2).join(' ') + ' ' + '\\n');` : '';
    fs.writeFileSync(js, `let d = ''; process.stdin.on('data', (c) => { d += c; }); process.stdin.on('end', () => { ${rec}require('node:fs').writeFileSync(${JSON.stringify(out)}, d); process.exit(${exit}); });`);
    const cmd = path.join(dir, `${name}.cmd`);
    fs.writeFileSync(cmd, `@"${process.execPath}" "%~dp0tool-${name}.mjs" %*\r\n`);
    return cmd;
  }
  const sh = path.join(dir, name);
  const rec = argvFile ? `printf '%s\\n' "$*" > ${JSON.stringify(argvFile)}\n` : '';
  fs.writeFileSync(sh, `#!/bin/sh\n${rec}cat > ${JSON.stringify(out)}\nexit ${exit}\n`, { mode: 0o755 });
  return sh;
}

// ---------- filter ----------

test('filter: words (whole or partial) match case-insensitively over the fields', () => {
  const rows = [
    ['ORCHE (uppercase name)', 'ORCHE', ['local/w12:p1', 'windows/w3:p1']],
    ['che (partial, middle of the word)', 'che', ['local/w12:p1', 'windows/w3:p1']],
    ['workspace label', 'appliance', ['local/w12:p1']],
    ['tab label', '1', ['local/w12:p1', 'local/w14:pW', 'windows/w3:p1']],
    ['machine (uppercase)', 'WINDOWS', ['windows/w3:p1']],
    ['cwd (windows path, uppercase query)', 'C:\\USERS\\DJ4LM', ['windows/w3:p1']],
    ['workspace label uppercase', 'PINAR', ['windows/w3:p1']],
    ['kind', 'codex', ['windows/w3:p1']],
    ['status', 'idle', ['local/w14:pW']],
    ['no match', 'zzz-nope', []],
    ['null name/kind do not match and do not crash', 'orchestrator-10', ['local/w12:p1']],
  ];
  for (const [label, query, wantRefs] of rows) {
    const got = filterEntries(ENTRIES, query).map((e) => e.ref).sort();
    assert.deepEqual(got, [...wantRefs].sort(), `${label}: query ${JSON.stringify(query)}`);
  }
  // Multiple words: every word must match (across the fields).
  assert.deepEqual(
    filterEntries(ENTRIES, 'orchestrator codex').map((e) => e.ref),
    ['windows/w3:p1'],
    'multi-word: orchestrator (name) + codex (kind)',
  );
  assert.deepEqual(filterEntries(ENTRIES, 'orchestrator idle'), [], 'multi-word: no entry has both');
  // Mutation captured: making the filter case-sensitive (removing
  // toLowerCase on the query or the field) fails the 'ORCHE' and
  // 'WINDOWS' rows.
  // Mutation captured: dropping the multi-word split (matching the whole
  // query as one string) fails 'orchestrator codex' (it spans two fields).
});

test('filter: an empty query shows everything', () => {
  assert.equal(filterEntries(ENTRIES, '').length, 3);
  assert.equal(filterEntries(ENTRIES, null).length, 3);
  // Mutation captured: a truthy check instead of an empty-string check
  // (query ?? '' then !== '') would hide everything on an empty query.
});

// ---------- navigation ----------

test('navigation: up/down move the selection with clamping at the ends', () => {
  const st = createState();
  st.entries = ENTRIES;
  feedChunk(st, 'q'); // 'q' is in no field: the filter narrows to nothing
  assert.equal(st.query, 'q');
  assert.equal(filterEntries(st.entries, st.query).length, 0);
  feedChunk(st, '\x7f'); // backspace clears it
  assert.equal(st.query, '');
  assert.equal(st.selected, 0);
  feedChunk(st, '\x1b[B'); // down
  assert.equal(st.selected, 1);
  feedChunk(st, '\x1b[B'); // down
  assert.equal(st.selected, 2);
  feedChunk(st, '\x1b[B'); // down: clamped at the last row
  assert.equal(st.selected, 2);
  feedChunk(st, '\x1b[A'); // up
  assert.equal(st.selected, 1);
  feedChunk(st, '\x1b[A'); // up
  assert.equal(st.selected, 0);
  feedChunk(st, '\x1b[A'); // up: clamped at the first row
  assert.equal(st.selected, 0);
  assert.equal(st.exit, null);
  // Mutation captured: removing the Math.max(0, …) / Math.min(n-1, …)
  // clamping lets selected leave [0, n-1] on the extra up/down keys.
});

test('navigation: the selection is clamped when the filter shrinks the list', () => {
  const st = createState();
  st.entries = ENTRIES;
  feedChunk(st, '\x1b[B'); // select row 2
  assert.equal(st.selected, 1);
  feedChunk(st, 'pinar'); // only the windows entry matches
  assert.equal(st.query, 'pinar');
  assert.equal(st.selected, 0, 'clamped to the single visible row');
  assert.equal(filterEntries(st.entries, st.query).length, 1);
  feedChunk(st, '\x1b[B'); // down with one visible row: stays
  assert.equal(st.selected, 0);
  // Mutation captured: not clamping on a query change leaves selected=1
  // out of range and Enter would copy the wrong (or no) row.
});

// ---------- copy text ----------

test('copy text: <ref> (<name>, <kind>, <status>) <cwd> with - on nulls', () => {
  assert.equal(
    copyPayload(JSON.parse(LOCAL_1)),
    'local/w12:p1 (orchestrator-10, claude, working) /Users/dj4lm/repo',
  );
  // null name and kind: dashes
  assert.equal(
    copyPayload(JSON.parse(LOCAL_2)),
    'local/w14:pW (-, -, idle) /tmp/soho',
  );
  // null cwd: dash
  assert.equal(
    copyPayload({ ref: 'windows/w3:p1', name: 'a', kind: 'b', status: 'done', cwd: null }),
    'windows/w3:p1 (a, b, done) -',
  );
  // the first token is always the reference
  for (const e of ENTRIES) {
    assert.equal(copyPayload(e).split(' ')[0], e.ref);
  }
  // Mutation captured: reordering the template (name before ref, or a
  // different separator) fails the exact-string asserts.
});

// ---------- keys: Enter / Esc / Ctrl-C / Backspace ----------

test('Enter copies the selection and exits; Esc and Ctrl-C exit without copying', () => {
  const st = createState();
  st.entries = [JSON.parse(LOCAL_1)];
  const act = feedChunk(st, '\r');
  assert.equal(act, 'copy');
  assert.equal(st.exit, 'copy');
  assert.equal(st.copied, 'local/w12:p1 (orchestrator-10, claude, working) /Users/dj4lm/repo');
  assert.equal(st.lastEntry.ref, 'local/w12:p1');

  const st2 = createState();
  st2.entries = ENTRIES;
  feedChunk(st2, '\x1b[B'); // row 1
  // A lone ESC at a chunk boundary is pending in the pure logic: the TTY
  // layer resolves it (flushEsc) when the next chunk never comes.
  assert.equal(feedChunk(st2, '\x1b'), null, 'the lone ESC waits for the next chunk');
  assert.equal(flushEsc(st2), 'esc');
  assert.equal(st2.exit, 'esc');
  assert.equal(st2.copied, null, 'Esc exits without copying');
  assert.equal(st2.lastEntry, null);

  const st3 = createState();
  st3.entries = ENTRIES;
  assert.equal(feedChunk(st3, '\x03'), 'esc');
  assert.equal(st3.exit, 'esc');
  assert.equal(st3.copied, null, 'Ctrl-C exits without copying');

  // Enter with no visible row copies nothing and does not exit.
  const st4 = createState();
  st4.entries = ENTRIES;
  feedChunk(st4, 'zzz-nope');
  assert.equal(feedChunk(st4, '\r'), null);
  assert.equal(st4.exit, null);
  assert.equal(st4.copied, null);
  // Mutation captured: making Esc (or Ctrl-C) set copied/exit='copy'
  // fails the st2/st3 asserts; making Enter exit with an empty filter
  // fails the st4 assert.
});

test('a lone ESC split across chunks is still the exit key, and ESC[A split across chunks is up', () => {
  const st = createState();
  st.entries = ENTRIES;
  feedChunk(st, '\x1b'); // chunk 1: only the ESC
  assert.equal(st.exit, null, 'the pending ESC waits for the next chunk');
  assert.equal(feedChunk(st, 'B'), 'esc', 'ESC followed by a non-"[" byte is a lone ESC');
  assert.equal(st.copied, null);

  const st2 = createState();
  st2.entries = ENTRIES;
  feedChunk(st2, '\x1b');
  feedChunk(st2, '[A');
  assert.equal(st2.exit, null, 'ESC[ split across chunks is an up-arrow, not an exit');
  assert.equal(st2.selected, 0);

  // A modified arrow key (ESC[1;5B) must not leak its parameter bytes
  // into the query.
  const st3 = createState();
  st3.entries = ENTRIES;
  feedChunk(st3, '\x1b[1;5B');
  assert.equal(st3.exit, null);
  assert.equal(st3.query, '', 'no parameter byte leaked into the filter');
  assert.equal(st3.selected, 1, 'it is still a down-arrow');
  // Mutation captured: treating the pending ESC as an immediate exit
  // (without waiting for the next chunk) fails the st2 assert; not
  // skipping the CSI parameter bytes fails the st3 query assert.
});

// ---------- incremental loading ----------

test('incremental load: local first, remotes appended as they arrive; a failing machine becomes a status line', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const marker = path.join(dir, 'cli-calls.jsonl');
  const cli = writeFakeFindCli(dir, 'load', {
    marker,
    local: { lines: [LOCAL_1, LOCAL_2] },
    windows: { sleepMs: 400, lines: [WINDOWS_1] },
    hetzner: { exit: 1, stderr: 'boom-hetzner\n' },
  });
  const herdrBin = writeFakeHerdr(dir, 'load', {
    machineList: machineListJson([
      { label: 'hetzner', enabled: true },
      { label: 'windows', enabled: true },
      { label: 'off-machine', enabled: false },
    ]),
    argvFile: path.join(dir, 'herdr-argv.jsonl'),
  });
  const st = createState();
  const p = loadEntries(st, {
    env: { HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
  });
  // Local is up first, while the remotes are still loading.
  await waitUntil(() => st.entries.length === 2 && st.loading > 0, { timeoutMs: 5000 });
  assert.deepEqual(st.entries.map((e) => e.ref), ['local/w12:p1', 'local/w14:pW'], 'local first');
  assert.match(render(st, 120), /carregando windows…/);
  const done = await p;
  assert.deepEqual(st.entries.map((e) => e.ref),
    ['local/w12:p1', 'local/w14:pW', 'windows/w3:p1'],
    'the remote entry is appended on arrival');
  assert.equal(st.loading, 0, 'no load left in flight');
  assert.equal(done.failures.length, 1);
  assert.equal(done.failures[0].label, 'hetzner');
  assert.match(done.failures[0].cause, /exit 1/);
  // The failure is a status line, not an error.
  assert.match(render(st, 120), /máquina hetzner: falhou \(exit 1/);
  assert.doesNotMatch(render(st, 120), /carregando windows…/);
  // The disabled machine is never queried (the CLI calls are recorded
  // in the marker): exactly the local call plus one per enabled machine.
  const calls = fs.readFileSync(marker, 'utf8').trim().split('\n').map((l) => JSON.parse(l).machine);
  assert.equal(calls.length, 3);
  assert.ok(calls.includes(null), 'the local find ran');
  assert.deepEqual(calls.filter((c) => c !== null).sort(), ['hetzner', 'windows']);
  // Mutation captured: letting one failing machine reject the whole
  // load (dropping the per-machine try/catch) loses the windows entry
  // and the test rejects before the asserts.
});

test('incremental load: a failing machine list keeps the local entries and becomes a status line', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const cli = writeFakeFindCli(dir, 'ml', {
    marker: path.join(dir, 'calls.jsonl'),
    local: { lines: [LOCAL_1] },
  });
  // A herdr that answers machine list with a non-zero exit.
  const herdrBin = writeLauncherFake(dir, 'herdr-mlfail', `process.exit(7);`);
  const st = createState();
  const r = await loadEntries(st, {
    env: { HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
  });
  assert.deepEqual(st.entries.map((e) => e.ref), ['local/w12:p1']);
  assert.equal(st.loading, 0);
  assert.equal(r.failures.length, 1);
  assert.equal(r.failures[0].label, 'máquinas');
  assert.match(render(st, 120), /máquina máquinas: falhou \(exit 7\)/);
  assert.doesNotMatch(render(st, 120), /carregando windows…/);
  // Mutation captured: not decrementing the loading counter on a failed
  // machine list leaves 'carregando windows…' on the screen forever.
});

test('incremental load: a failing local find becomes a status line and nothing more', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const cli = writeFakeFindCli(dir, 'localfail', {
    local: { exit: 3, stderr: 'no local panes\n' },
    windows: { lines: [WINDOWS_1] },
  });
  const herdrBin = writeFakeHerdr(dir, 'lf', {
    machineList: machineListJson([{ label: 'windows', enabled: true }]),
  });
  const st = createState();
  const r = await loadEntries(st, {
    env: { HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
  });
  assert.equal(st.entries.length, 0);
  assert.equal(st.loading, 0);
  assert.equal(r.failures.length, 1);
  assert.equal(r.failures[0].label, 'local');
  assert.match(render(st, 120), /máquina local: falhou \(exit 3/);
  // Mutation captured: re-throwing the local failure aborts the picker
  // instead of degrading to a status line.
});

// ---------- parseFindOutput / parseMachineList ----------

test('parseFindOutput: good NDJSON, CRLF, and a malformed line rejects the whole load', () => {
  assert.equal(parseFindOutput(`${LOCAL_1}\r\n${LOCAL_2}\n`).length, 2);
  assert.deepEqual(parseFindOutput(''), []);
  assert.throws(() => parseFindOutput(`${LOCAL_1}\n{broken`), FindError, 'a broken line rejects the load');
  assert.throws(() => parseFindOutput('{"machine":"local","workspace_id":"w1","tab_id":"w1:t1","pane_id":"w1:p1"}'), FindError, 'a line without ref rejects');
  assert.throws(() => parseFindOutput('[1,2]'), FindError, 'an array line rejects');
  assert.throws(() => parseFindOutput('42'), FindError, 'a scalar line rejects');
  // Mutation captured: silently skipping a bad line instead of throwing
  // loses the 'rejects the load' behavior (the entry count would be 1).
});

test('parseMachineList: bare array, enabled-only, hostile items reject', () => {
  assert.deepEqual(parseMachineList(machineListJson([
    { label: 'a', enabled: true },
    { label: 'b', enabled: false },
  ])), ['a']);
  // a string "false" (or "true") is not an enabled boolean
  assert.deepEqual(parseMachineList(JSON.stringify([
    { label: 'a', enabled: 'false' },
    { label: 'b', enabled: 'true' },
    { label: 'c', enabled: true },
  ])), ['c']);
  // the {machines:[...]} wrapper is accepted
  assert.deepEqual(parseMachineList(JSON.stringify({ machines: [{ label: 'a', enabled: true }] })), ['a']);
  // no label: the id is used
  assert.deepEqual(parseMachineList(JSON.stringify([{ id: 'deadbeef', enabled: true }])), ['deadbeef']);
  // hostile items reject the whole response
  assert.throws(() => parseMachineList('not json'), FindError);
  assert.throws(() => parseMachineList(JSON.stringify({ nope: true })), FindError, 'no machine array');
  assert.throws(() => parseMachineList(JSON.stringify([null])), FindError, 'a null item');
  assert.throws(() => parseMachineList(JSON.stringify([42])), FindError, 'a scalar item');
  assert.throws(() => parseMachineList(JSON.stringify([{ enabled: true }])), FindError, 'no label/id');
  // Mutation captured: treating enabled as truthy (removing === true)
  // lets 'false' through and fails the string row.
});

// ---------- render ----------

test('render: selection marker, loading line, failure line and count', () => {
  const st = createState();
  st.entries = ENTRIES;
  st.loading = 1;
  st.failures = [{ label: 'hetzner', cause: 'exit 1' }];
  feedChunk(st, '\x1b[B'); // select row 2 (index 1)
  const out = render(st, 200);
  const lines = out.trimEnd().split('\n');
  assert.equal(lines[0], '> ');
  assert.match(lines[1], /^  local\/w12:p1/);
  assert.match(lines[2], /^\* local\/w14:pW/);
  assert.match(lines[3], /^  windows\/w3:p1/);
  assert.equal(lines[4], 'carregando windows…');
  assert.equal(lines[5], 'máquina hetzner: falhou (exit 1)');
  assert.equal(lines[6], '3 panes');
  // null name/kind render as -
  assert.match(lines[2], /local\/w14:pW  -  -  idle  soho\/1  \/tmp\/soho/);
  // filtered count (load settled and no failures: only rows + count)
  st.loading = 0;
  st.failures = [];
  feedChunk(st, 'pinar');
  const lines2 = render(st, 200).trimEnd().split('\n');
  assert.equal(lines2[1], '* windows/w3:p1  orchestrator  codex  working  pinar/1  C:\\Users\\dj4lm\\pinar');
  assert.equal(lines2[2], '1 pane');
  // no match
  feedChunk(st, 'zzzz');
  assert.match(render(st, 200), /nenhum resultado para "pinarzzzz"/);
  // empty picker before any load resolves
  const st0 = createState();
  st0.loading = 1;
  assert.match(render(st0, 80), /carregando windows…\n0 panes/);
  const st1 = createState();
  assert.match(render(st1, 80), /nenhum pane/);
  // Mutation captured: dropping the 'carregando windows…' line fails
  // the exact line assert.
});

test('row truncation: cwd is cut to the terminal width with an ellipsis; very narrow rows hard-cut', () => {
  const e = { ...JSON.parse(LOCAL_1), cwd: '/Users/dj4lm/very/long/path/that/goes/on/and/on' };
  const line = entryLine(e, 100);
  assert.ok(line.length <= 100, `length ${line.length}`);
  assert.ok(line.endsWith('…'), 'the cwd absorbs the cut with an ellipsis');
  assert.ok(line.startsWith('local/w12:p1  orchestrator-10  claude  working  appliance/1  /Users/'), 'leading columns intact');
  const narrow = entryLine(e, 20);
  assert.ok(narrow.length <= 20, `length ${narrow.length}`);
  const wide = entryLine(e, 400);
  assert.equal(wide, 'local/w12:p1  orchestrator-10  claude  working  appliance/1  /Users/dj4lm/very/long/path/that/goes/on/and/on');
  // Mutation captured: ignoring the width (returning the full row)
  // fails the length asserts.
});

// A controlled PATH: the fake dir first (so the fakes win the lookup),
// followed by the system dirs so the fake scripts' own `cat`/`printf`
// resolve (the bridge tests use the same pattern).
function withFakeDir(dir) {
  const env = { ...process.env };
  const pathKey = Object.keys(env).find((k) => k === 'PATH' || k === 'Path') ?? 'PATH';
  env[pathKey] = dir + path.delimiter + (env[pathKey] ?? '');
  return env;
}

// ---------- clipboard ----------

test('clipboard: darwin uses pbcopy from a controlled PATH, text on stdin', (t) => {
  const dir = makeTmp(t);
  const out = path.join(dir, 'clip.txt');
  writeFakeTool(dir, 'pbcopy', { out });
  const r = copyText('hello clip', { platform: 'darwin', env: withFakeDir(dir) });
  assert.equal(r.path, 'pbcopy');
  assert.equal(fs.readFileSync(out, 'utf8'), 'hello clip');
  // Mutation captured: not delivering the text on the tool's stdin
  // (dropping the input option) leaves the fake's output file empty
  // and fails the stdin assert.
});

test('clipboard: linux tries wl-copy, then xclip, then xsel — a failing tool yields the next', (t) => {
  const dir = makeTmp(t);
  const outWl = path.join(dir, 'wl.txt');
  const outXc = path.join(dir, 'xc.txt');
  const outXs = path.join(dir, 'xs.txt');
  writeFakeTool(dir, 'wl-copy', { out: outWl, exit: 1 });
  writeFakeTool(dir, 'xclip', { out: outXc });
  writeFakeTool(dir, 'xsel', { out: outXs });
  assert.deepEqual(toolCandidates('linux').map((a) => a[0]), ['wl-copy', 'xclip', 'xsel']);
  const r = copyText('linux text', { platform: 'linux', env: withFakeDir(dir) });
  assert.equal(r.path, 'xclip', 'wl-copy failed: xclip takes over');
  assert.equal(fs.readFileSync(outXc, 'utf8'), 'linux text');
  assert.ok(!fs.existsSync(outXs), 'xsel is not reached');
  // and when wl-copy works, it wins
  const dir2 = makeTmp(t);
  const outWl2 = path.join(dir2, 'wl.txt');
  writeFakeTool(dir2, 'wl-copy', { out: outWl2 });
  const r2 = copyText('linux text', { platform: 'linux', env: withFakeDir(dir2) });
  assert.equal(r2.path, 'wl-copy');
  assert.equal(fs.readFileSync(outWl2, 'utf8'), 'linux text');
  // Mutation captured: reversing the candidate order fails the
  // 'wl-copy wins' row; not moving on after a failure fails 'xclip
  // takes over'.
});

test('clipboard: windows uses powershell with the text on stdin', (t) => {
  const dir = makeTmp(t);
  const out = path.join(dir, 'clip.txt');
  const argvFile = path.join(dir, 'argv.txt');
  writeFakeTool(dir, 'powershell', { out, argvFile });
  const r = copyText('win text', { platform: 'win32', env: withFakeDir(dir) });
  assert.equal(r.path, 'powershell');
  assert.equal(fs.readFileSync(out, 'utf8'), 'win text');
  const argv = fs.readFileSync(argvFile, 'utf8');
  assert.match(argv, /-NoProfile/);
  assert.match(argv, /Set-Clipboard/);
  // Mutation captured: using clip.exe instead of PowerShell fails the
  // tool name and the argv asserts.
});

test('clipboard: no tool, or a failing tool, writes the exact OSC 52 bytes', (t) => {
  const dir = makeTmp(t);
  const text = 'olá referência windows/w3:p1';
  const expected = osc52Sequence(text);
  assert.equal(expected, `\x1b]52;c;${Buffer.from(text, 'utf8').toString('base64')}\x07`, 'format: ESC ] 52 ; c ; <base64> BEL');
  // no tool on PATH at all
  const cap1 = captureStream();
  const r1 = copyText(text, { platform: 'darwin', env: { PATH: path.join(dir, 'no-such-dir') }, out: cap1 });
  assert.equal(r1.path, 'osc52');
  assert.equal(cap1.text(), expected, 'exact bytes, no tool');
  // a tool that exists but fails
  const out = path.join(dir, 'never.txt');
  writeFakeTool(dir, 'pbcopy', { out, exit: 1 });
  const cap2 = captureStream();
  const r2 = copyText(text, { platform: 'darwin', env: withFakeDir(dir), out: cap2 });
  assert.equal(r2.path, 'osc52');
  assert.equal(cap2.text(), expected, 'exact bytes, failing tool');
  assert.equal(Buffer.from(cap2.text(), 'utf8').toString('latin1'), Buffer.from(expected).toString('latin1'), 'byte-for-byte');
  // the bytes decode back to the UTF-8 text
  const m = expected.match(/^\x1b\]52;c;([A-Za-z0-9+/=]+)\x07$/);
  assert.ok(m, 'a strict OSC 52 shape');
  assert.equal(Buffer.from(m[1], 'base64').toString('utf8'), text);
  // Mutation captured: writing the text raw instead of the base64
  // sequence (or dropping the BEL) fails the exact-bytes asserts.
});

// ---------- main: end-to-end without a TTY ----------

test('main: Enter copies via the platform tool and notifies; the screen shows the rows', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const clipOut = path.join(dir, 'clip.txt');
  const toolName = toolCandidates(process.platform)[0][0]; // first native tool
  writeFakeTool(dir, toolName, { out: clipOut });
  const cli = writeFakeFindCli(dir, 'e2e', {
    local: { lines: [LOCAL_1] },
    windows: { sleepMs: 400, lines: [WINDOWS_1] },
  });
  const herdrBin = writeFakeHerdr(dir, 'e2e', {
    machineList: machineListJson([{ label: 'windows', enabled: true }]),
    argvFile: path.join(dir, 'herdr-argv.jsonl'),
  });
  const sin = new PassThrough();
  const sout = captureStream();
  const p = pickerMain({
    env: { ...withFakeDir(dir), HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
    stdin: sin,
    stdout: sout,
    width: 120,
  });
  // The local row is up first; 'carregando windows…' is visible while
  // the remote load is in flight; the remote row is appended after.
  await waitUntil(() => sout.text().includes('local/w12:p1') && sout.text().includes('carregando windows…'), { timeoutMs: 5000 });
  await waitUntil(() => {
    const last = sout.text().slice(sout.text().lastIndexOf('\x1b[H'));
    return last.includes('windows/w3:p1') && !last.includes('carregando windows…');
  }, { timeoutMs: 5000 });
  // Filter to the remote row, then Enter.
  sin.write('windows');
  await waitUntil(() => {
    const tx = sout.text();
    const last = tx.slice(tx.lastIndexOf('\x1b[H'));
    return last.startsWith('\x1b[H> windows') && last.includes('* windows/w3:p1') && /\n1 pane/.test(last);
  }, { timeoutMs: 5000 });
  sin.write('\r');
  const r = await p;
  assert.equal(r.action, 'copy');
  assert.equal(r.copied, 'windows/w3:p1 (orchestrator, codex, working) C:\\Users\\dj4lm\\pinar');
  assert.equal(r.clipboard.path, toolName, 'the native tool of this platform was used');
  assert.equal(fs.readFileSync(clipOut, 'utf8'), r.copied);
  // The notification ran with the exact arguments (failure is not an error).
  const argvs = fs.readFileSync(path.join(dir, 'herdr-argv.jsonl'), 'utf8').trim().split('\n').map((l) => JSON.parse(l));
  const note = argvs.find((a) => a[0] === 'notification');
  assert.deepEqual(note, ['notification', 'show', 'herdr-soho', '--body', 'copied windows/w3:p1', '--sound', 'none']);
  // The screen rendered the rows (selected marker on the copied row).
  assert.match(sout.text(), /\* windows\/w3:p1/);
  // Mutation captured: dropping the notification call leaves no
  // 'notification' argv in the fake herdr log.
});

test('main: Esc during a slow remote load exits without copying and kills the loads', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const doneFile = path.join(dir, 'windows-done');
  // A remote that would finish 500 ms after its spawn if not killed; it
  // marks doneFile on normal completion, so a surviving child is
  // observable after the exit.
  const cli = writeFakeFindCli(dir, 'esc', {
    local: { lines: [LOCAL_1] },
    windows: { sleepMs: 500, lines: [WINDOWS_1], doneFile },
  });
  const herdrBin = writeFakeHerdr(dir, 'esc', {
    machineList: machineListJson([{ label: 'windows', enabled: true }]),
  });
  const sin = new PassThrough();
  const sout = captureStream();
  const p = pickerMain({
    env: { PATH: dir, HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
    stdin: sin,
    stdout: sout,
    width: 100,
  });
  await waitUntil(() => sout.text().includes('carregando windows…'), { timeoutMs: 5000 });
  const started = Date.now();
  sin.write('\x1b');
  const r = await p;
  assert.ok(Date.now() - started < 5000, 'the exit is not held by the load');
  assert.equal(r.action, 'esc');
  assert.equal(r.copied, null);
  assert.equal(r.clipboard, null, 'no copy on Esc');
  assert.equal(r.notification, null, 'no notification on Esc');
  // The load children were killed: the remote that would have finished
  // 500 ms later never reached its completion marker.
  await sleep(1000);
  assert.ok(!fs.existsSync(doneFile), 'the load child was killed on exit');
  // Mutation captured: not killing the load children on exit leaves the
  // remote running and it writes doneFile, failing the last assert.
});

test('main: piped stdin ending exits as Esc without copying', { timeout: 30000 }, async (t) => {
  const dir = makeTmp(t);
  const cli = writeFakeFindCli(dir, 'eof', { local: { lines: [LOCAL_1] } });
  const herdrBin = writeFakeHerdr(dir, 'eof', {
    machineList: machineListJson([]),
  });
  const sin = new PassThrough();
  const sout = captureStream();
  const p = pickerMain({
    env: { PATH: dir, HERDR_BIN_PATH: herdrBin },
    nodeBin: process.execPath,
    cliScript: cli,
    herdrBin,
    stdin: sin,
    stdout: sout,
    width: 100,
  });
  await waitUntil(() => sout.text().includes('1 pane'), { timeoutMs: 5000 });
  sin.end();
  const r = await p;
  assert.equal(r.action, 'esc');
  assert.equal(r.copied, null);
  // Mutation captured: treating a piped EOF as an Enter (copy) copies
  // the first row and fails the copied=null assert.
});
