// The `status` age columns (#4): every TSV line gains task_s and
// activity_s as its last two columns (integers in seconds, '-' when
// unknown), and the JSON lines (quota, provider-error, capacity, question)
// gain task_s and activity_s (number or null) as their last keys. task_s
// runs from the mtime of <state>/last-report-<agent> (written on every
// dispatch) to the report's mtime when it exists and is non-empty, else to
// now — no marker is unknown. activity_s is only for the working state:
// activityAgeSeconds with the visible screen read once (0 on a changed
// normalized screen, the age of the last observed change with an equal
// hash, null otherwise). No screen text goes to the output, and the status
// stays read-only (no marker is written). The status runs through the real
// entry (child process) so stdout and the exit code are observable; a fake
// `herdr` (writeFakeCli) is the only herdr the code sees.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { writeFakeCli } from './fakes.mjs';
import { nodeBin } from './parity.mjs';
import { loadConfig } from '../lib/config.mjs';
import { cksumField, normalizeScreen } from '../lib/wait.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const JS_ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');

// ---------- fake herdr (the status tests' fake) ----------

// `agent get` per target (mode-<t> file) else the global mode file
// (denied → an unqueryable error, missing → agent_not_found, else the mode
// as agent_status); `agent read` prints screen-<t> (or the global screen
// file). Every call is logged as one "$*" line (FAKE_LOG).
const HERDR_FAKE = `
import fs from 'node:fs';
const argv = process.argv.slice(2);
if (process.env.FAKE_LOG) fs.appendFileSync(process.env.FAKE_LOG, argv.join(' ') + '\\n');
const t = argv[2] ?? '';
const cmd = (argv[0] ?? '') + ' ' + (argv[1] ?? '');
const modeOf = (t) => {
  let per = '';
  if (process.env.FAKE_MODE_DIR) {
    try { per = fs.readFileSync(process.env.FAKE_MODE_DIR + '/mode-' + t, 'utf8').trim(); } catch {}
  }
  if (per) return per;
  try { return fs.readFileSync(process.env.FAKE_MODE, 'utf8').trim(); } catch { return 'working'; }
};
const screenOf = (t) => {
  try { return fs.readFileSync(process.env.FAKE_SCREEN_DIR + '/screen-' + t, 'utf8'); }
  catch { try { return fs.readFileSync(process.env.FAKE_SCREEN, 'utf8'); } catch { return ''; } }
};
if (cmd === 'agent get') {
  const m = modeOf(t);
  if (m === 'denied') {
    process.stderr.write('Error: Os { code: 13, kind: PermissionDenied, message: "Permission denied" }\\n');
    process.exit(1);
  }
  if (m === 'missing') {
    process.stderr.write('{"error":{"code":"agent_not_found","message":"agent target ' + t + ' not found"}}\\n');
    process.exit(1);
  }
  process.stdout.write('{"result":{"agent":{"name":"' + t + '","agent_status":"' + m + '"}}}\\n');
} else if (cmd === 'agent read') {
  process.stdout.write(screenOf(t));
} else if (cmd === 'agent list') {
  process.stdout.write('{"result":{"agents":[]}}\\n');
} else {
  process.stderr.write('unexpected: ' + argv.join(' ') + '\\n');
  process.exit(1);
}
`;

// ---------- fixture plumbing ----------

function makeFix(prefix) {
  let root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  root = fs.realpathSync(root);
  const bin = path.join(root, 'bin');
  const repo = path.join(root, 'repo');
  const state = path.join(root, 'state');
  const ws = path.join(state, 'ws');
  const modeDir = path.join(root, 'modes');
  const screenDir = path.join(root, 'screens');
  for (const d of [bin, repo, ws, path.join(ws, 'briefs'), path.join(ws, 'reports'), path.join(ws, 'wait'),
    modeDir, screenDir, path.join(root, 'home'), path.join(root, 'conf'), path.join(root, 'tmp')]) {
    fs.mkdirSync(d, { recursive: true });
  }
  writeFakeCli(bin, 'herdr', HERDR_FAKE);
  const env = {
    HOME: path.join(root, 'home'),
    USERPROFILE: path.join(root, 'home'),
    XDG_CONFIG_HOME: path.join(root, 'conf'),
    TMPDIR: path.join(root, 'tmp'),
    HERDR_SOHO_DIR: state,
    HERDR_WORKSPACE_ID: 'ws',
    HERDR_ENV: '1',
    HERDR_SOHO_REGRID: 'off',
    FAKE_MODE: path.join(root, 'mode'),
    FAKE_MODE_DIR: modeDir,
    FAKE_SCREEN: path.join(root, 'screen'),
    FAKE_SCREEN_DIR: screenDir,
    FAKE_LOG: path.join(root, 'herdr.log'),
    PATH: `${bin}${path.delimiter}${process.env.PATH}`,
    COMSPEC: process.env.COMSPEC,
    PATHEXT: process.env.PATHEXT,
  };
  fs.writeFileSync(env.FAKE_MODE, 'working\n');
  fs.writeFileSync(env.FAKE_SCREEN, '');
  const H12 = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';
  const fix = {
    root, repo, state, ws, env, ctx: loadConfig(env, repo),
    mode(m) { fs.writeFileSync(env.FAKE_MODE, `${m}\n`); },
    modeOf(agent, m) { fs.writeFileSync(path.join(modeDir, `mode-${agent}`), `${m}\n`); },
    screenOf(agent, s) { fs.writeFileSync(path.join(screenDir, `screen-${agent}`), s); },
    writeRoster(...rows) { fs.writeFileSync(path.join(ws, 'agents.tsv'), H12 + rows.join('\n') + '\n'); },
    waitFile(agent, name, content) { fs.writeFileSync(path.join(ws, 'wait', `${agent}.${name}`), content); },
    waitRead(agent, name) {
      try { return fs.readFileSync(path.join(ws, 'wait', `${agent}.${name}`), 'utf8'); } catch { return null; }
    },
    logLines() {
      try { return fs.readFileSync(env.FAKE_LOG, 'utf8').split('\n').filter((l) => l !== ''); } catch { return []; }
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
  fix.writeRoster();
  return fix;
}

// Row with every 12-column field (pane p-<name>, created 1).
const ROW = (name, role, kind = 'grok', model = '', lane = '') =>
  `${name}\tp-${name}\t${kind}\t${role}\txai\t1\t/tmp/work\tnow\t${model}\tfull\t${role}\t${lane}`;

function cmd(fix, args) {
  return spawnSync(nodeBin(), [JS_ENTRY, ...args], { cwd: fix.repo, env: fix.env, encoding: 'utf8', timeout: 60_000 });
}

// The TSV rows of a status run, split by agent (JSON lines are skipped).
function tsvRows(out) {
  return out.trim().split('\n').filter((l) => l !== '' && !l.startsWith('{'))
    .map((l) => l.split('\t'));
}

// ---------- the activity ages (acceptance 4 of the amendment) ----------

// Three working agents with different observed-activity states, all
// without a last-report marker (task_s unknown): a changed normalized
// screen is 0, an equal hash with an activity-at 600 s old is at least
// 600, and an equal hash with no activity-at is unknown. The ages differ,
// no screen text leaks to stdout, and the status writes no marker.
test('status: activity_s dates a changed screen at the last probe, the aged one on an equal hash, unknown without activity-at', { timeout: 60000 }, () => {
  const fix = makeFix('ha-status-ages-activity-');
  try {
    const fresh = 'Fresh screen alpha ZZ1\n';
    const same = 'Same screen 12% ◐\n';
    const still = 'Still screen 34% ◑\n';
    fix.writeRoster(
      ROW('fresh', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('aged', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('still', 'implementer', 'grok', 'grok-4.7', 'build'),
    );
    fix.modeOf('fresh', 'working');
    fix.modeOf('aged', 'working');
    fix.modeOf('still', 'working');
    fix.screenOf('fresh', fresh);
    fix.screenOf('aged', same);
    fix.screenOf('still', still);
    const now = Math.floor(Date.now() / 1000);
    // fresh: the recorded hash is NOT the current screen (a real change
    // since the last probe, 2 s ago) → dated at that probe, whatever the
    // activity-at holds.
    fix.waitFile('fresh', 'stuck-hash', `${cksumField(normalizeScreen('Some other screen\n'))}\n`);
    fix.waitFile('fresh', 'probe-at', `${now - 2}\n`);
    fix.waitFile('fresh', 'activity-at', `${now - 5}\n`);
    // aged: the hash matches and the last observed change is 600 s old.
    fix.waitFile('aged', 'stuck-hash', `${cksumField(normalizeScreen(same))}\n`);
    fix.waitFile('aged', 'activity-at', `${now - 600}\n`);
    // still: the hash matches but no change was ever observed.
    fix.waitFile('still', 'stuck-hash', `${cksumField(normalizeScreen(still))}\n`);
    // Mutation captured: the status never computing activity_s (the
    // working branch removed) prints '-' for the three ages below
    // (0 / ≥ 600 / unknown by the marker — the first two fail).
    const r = cmd(fix, ['status', 'fresh', 'aged', 'still']);
    assert.equal(r.status, 0, r.stderr);
    const rows = tsvRows(r.stdout);
    assert.equal(rows.length, 3);
    assert.deepEqual(rows.map((c) => c[0]), ['fresh', 'aged', 'still']);
    for (const row of rows) {
      assert.equal(row[1], 'working');
      assert.equal(row[2], '', 'no report recorded');
      assert.equal(row[3], '-', 'no last-report marker → task_s unknown');
      assert.equal(row.length, 5, `five columns: ${JSON.stringify(row)}`);
    }
    const fresh0 = Number(rows[0][4]);
    assert.ok(fresh0 >= 2 && fresh0 <= 10, `a changed screen is dated at the last probe (got ${rows[0][4]})`);
    const aged = Number(rows[1][4]);
    assert.ok(aged >= 600, `the aged change is at least 600 s (got ${aged})`);
    assert.equal(rows[2][4], '-', 'no observed change → unknown');
    assert.notEqual(rows[0][4], rows[1][4], 'the ages differ');
    // No screen text in the output.
    assert.ok(!r.stdout.includes('Fresh screen alpha') && !r.stdout.includes('Same screen') && !r.stdout.includes('Still screen'),
      'no screen text in stdout: ' + JSON.stringify(r.stdout));
    // Read-only: the wait markers are untouched and no settled bookkeeping
    // (.screen/.since) was created by the status.
    assert.equal(fix.waitRead('fresh', 'stuck-hash'), `${cksumField(normalizeScreen('Some other screen\n'))}\n`, 'the changed hash was not re-recorded');
    assert.equal(fix.waitRead('aged', 'activity-at'), `${now - 600}\n`, 'the aged activity-at was not rewritten');
    for (const a of ['fresh', 'aged', 'still']) {
      assert.equal(fix.waitRead(a, 'screen'), null, 'no .screen written');
      assert.equal(fix.waitRead(a, 'since'), null, 'no .since written');
    }
    // The visible screen was read exactly once per working agent.
    const reads = fix.logLines().filter((l) => l.startsWith('agent read'));
    assert.deepEqual(reads, ['agent read fresh --source visible', 'agent read aged --source visible', 'agent read still --source visible'],
      'one visible read per working agent: ' + reads.join('\n'));
  } finally { fix.cleanup(); }
});

// ---------- task_s: from the last-report marker to the report (or now) ----------

test('status: task_s is the marker-to-report duration (or to now, or unknown)', { timeout: 60000 }, () => {
  const fix = makeFix('ha-status-ages-task-');
  try {
    fix.writeRoster(
      ROW('t1', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('t2', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('t3', 'implementer', 'grok', 'grok-4.7', 'build'),
    );
    fix.modeOf('t1', 'working');
    fix.modeOf('t2', 'idle'); // irrelevant: the ready report wins
    fix.modeOf('t3', 'working');
    fix.screenOf('t1', 'working on it\n');
    fix.screenOf('t3', 'working on it too\n');
    const now = Math.floor(Date.now() / 1000);
    // t1: the marker is 100 s old and the report it points at does not
    // exist yet → the task ends at now (≈ 100 s).
    const p1 = path.join(fix.ws, 'reports', 't1.md');
    fs.writeFileSync(path.join(fix.ws, 'last-report-t1'), p1 + '\n');
    fs.utimesSync(path.join(fix.ws, 'last-report-t1'), now - 100, now - 100);
    // t2: a ready report 60 s after the marker (both mtimes pinned) →
    // exactly 60 s, the state done, no herdr query.
    const p2 = path.join(fix.ws, 'reports', 't2.md');
    fs.writeFileSync(p2, 'done\n');
    fs.writeFileSync(path.join(fix.ws, 'last-report-t2'), p2 + '\n');
    fs.utimesSync(path.join(fix.ws, 'last-report-t2'), now - 100, now - 100);
    fs.utimesSync(p2, now - 40, now - 40);
    // t3: no last-report marker at all → task_s unknown.
    // Mutation captured: the status never computing task_s (the marker
    // branch removed) prints '-' for t1 and t2 below (the numbers fail).
    const r = cmd(fix, ['status', 't1', 't2', 't3']);
    assert.equal(r.status, 0, r.stderr);
    const rows = tsvRows(r.stdout);
    assert.equal(rows.length, 3);
    assert.equal(rows[0][0], 't1');
    assert.equal(rows[0][1], 'working');
    assert.equal(rows[0][2], p1, 'the report path is the marker content');
    const task1 = Number(rows[0][3]);
    assert.ok(task1 >= 99 && task1 <= 105, `t1 ≈ now - marker (got ${task1})`);
    assert.equal(rows[0][4], '-', 't1 is working without an observed change');
    assert.equal(rows[1][0], 't2');
    assert.equal(rows[1][1], 'done', 'the ready report wins over the query');
    assert.equal(rows[1][2], p2);
    const task2 = Number(rows[1][3]);
    assert.ok(task2 >= 59 && task2 <= 61, `t2 is the pinned 60 s (got ${task2})`);
    assert.equal(rows[1][4], '-', 'the done state has no activity');
    assert.equal(rows[2][0], 't3');
    assert.equal(rows[2][3], '-', 'no marker → task_s unknown');
    assert.equal(rows[2][4], '-', 'no marker → activity_s unknown');
    // The ready report short-circuits the herdr query.
    assert.deepEqual(fix.logLines().filter((l) => l.startsWith('agent get t2')), [], 'no agent get for a ready report');
  } finally { fix.cleanup(); }
});

// ---------- the JSON lines gain task_s and activity_s as last keys ----------

test('status: the quota, provider-error, capacity and question JSON lines end with task_s and activity_s', { timeout: 60000 }, () => {
  const fix = makeFix('ha-status-ages-json-');
  try {
    const now = Math.floor(Date.now() / 1000);
    fix.writeRoster(
      ROW('q', 'researcher', 'grok', 'grok-4.7', 'build'),
      ROW('p', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('cap', 'implementer', 'grok', 'grok-4.7', 'build'),
      ROW('quest', 'implementer', 'codex', '', 'build'),
    );
    fix.modeOf('q', 'idle');
    fix.screenOf('q', 'hit your usage limit\ntry again in 2 hours\n');
    // A 100 s old marker: the quota line carries a numeric task_s.
    const pq = path.join(fix.ws, 'reports', 'q.md');
    fs.writeFileSync(path.join(fix.ws, 'last-report-q'), pq + '\n');
    fs.utimesSync(path.join(fix.ws, 'last-report-q'), now - 100, now - 100);
    fix.modeOf('p', 'idle');
    fix.screenOf('p', 'Error: Retry failed after 3 attempts: Request timed out.\n');
    fix.modeOf('cap', 'idle');
    fix.screenOf('cap', 'API Error: 529 {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}\n');
    fix.modeOf('quest', 'blocked');
    fix.screenOf('quest', '  1. Use the local cache\n  2. Fetch from remote\n\nEnter to submit answer, esc to cancel\n');
    // Mutation captured: the JSON lines dropping the two new keys (or
    // putting them before cause/question) fails the key-order asserts
    // below.
    const r = cmd(fix, ['status', 'q', 'p', 'cap', 'quest']);
    assert.equal(r.status, 11, 'the combined rc is the quota rank (it outranks 14 and 7): ' + r.stderr);
    const lines = r.stdout.trim().split('\n').filter((l) => l !== '').map((l) => JSON.parse(l));
    assert.equal(lines.length, 4);
    const q = lines.find((l) => l.agent === 'q');
    assert.deepEqual(Object.keys(q), ['agent', 'status', 'report', 'lane', 'kind', 'model', 'match', 'renewal', 'task_s', 'activity_s']);
    assert.equal(q.status, 'quota');
    assert.equal(typeof q.task_s, 'number');
    assert.ok(q.task_s >= 99 && q.task_s <= 105, `the quota task_s is the marker age (got ${q.task_s})`);
    assert.equal(q.activity_s, null, 'the idle state is not working');
    const p = lines.find((l) => l.agent === 'p');
    assert.deepEqual(Object.keys(p), ['agent', 'status', 'report', 'cause', 'task_s', 'activity_s']);
    assert.equal(p.status, 'provider-error');
    assert.equal(p.task_s, null, 'no last-report marker → null');
    assert.equal(p.activity_s, null);
    const cap = lines.find((l) => l.agent === 'cap');
    assert.deepEqual(Object.keys(cap), ['agent', 'status', 'report', 'cause', 'task_s', 'activity_s']);
    assert.equal(cap.status, 'capacity');
    assert.equal(cap.task_s, null);
    assert.equal(cap.activity_s, null);
    const quest = lines.find((l) => l.agent === 'quest');
    assert.deepEqual(Object.keys(quest), ['agent', 'status', 'report', 'question', 'task_s', 'activity_s']);
    assert.equal(quest.status, 'question');
    assert.equal(quest.task_s, null);
    assert.equal(quest.activity_s, null, 'the blocked state is not working');
  } finally { fix.cleanup(); }
});

// ---------- the not-received and unavailable lines gain the columns ----------

test('status: the not-received TSV line and the unavailable cause line gain the two age columns', { timeout: 60000 }, () => {
  const fix = makeFix('ha-status-ages-lines-');
  try {
    fix.writeRoster(
      ROW('nr', 'tasker', 'grok', '', 'build'),
      ROW('u', 'implementer', 'grok', 'grok-4.7', 'build'),
    );
    fix.modeOf('nr', 'idle');
    fix.screenOf('nr', 'Welcome to the worker\n');
    fs.writeFileSync(path.join(fix.ws, 'wait', 'nr.not-received'), `${Math.floor(Date.now() / 1000) - 120}\n`);
    fix.modeOf('u', 'denied');
    fix.screenOf('u', 'MARKER_SCREEN_TEXT should never appear\n');
    // Mutation captured: the TSV lines dropping the new columns (five
    // instead of six on the cause line, four instead of five on the
    // not-received line) fails the column asserts below.
    const r = cmd(fix, ['status', 'nr', 'u']);
    assert.equal(r.status, 4, 'the unavailable rank overrides the not-received rc (the old rule): ' + r.stderr);
    const rows = tsvRows(r.stdout);
    assert.equal(rows.length, 2);
    assert.deepEqual(rows[0], ['nr', 'not-received', '', '-', '-'], 'the not-received line with unknown ages');
    assert.equal(rows[1].length, 6, 'six columns with a cause: ' + JSON.stringify(rows[1]));
    assert.equal(rows[1][0], 'u');
    assert.equal(rows[1][1], 'unavailable');
    assert.match(rows[1][3], /PermissionDenied/);
    assert.equal(rows[1][4], '-', 'no last-report marker → task_s unknown');
    assert.equal(rows[1][5], '-', 'not the working state → activity_s unknown');
    assert.ok(!r.stdout.includes('MARKER_SCREEN_TEXT'), 'no screen text in stdout: ' + JSON.stringify(r.stdout));
  } finally { fix.cleanup(); }
});
