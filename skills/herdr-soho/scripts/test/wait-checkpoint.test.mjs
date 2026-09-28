// wait --timeout on a pending agent (#3 + amendment): a short timeout that
// elapses while the worker is working with an OBSERVED screen change under
// the stuck window (a real normalized-hash move, recorded by the wait in
// <state>/wait/<agent>.activity-at) is a neutral checkpoint — exit 9, the
// JSON line gains checkpoint/activity_age_s after state, one stderr line,
// no friction entry. Reading a still screen never proves activity: the
// first observation of a screen writes stuck-hash but no activity-at, and
// a counter-only change is not a change (the stuck hash normalizes
// digits and progress glyphs). A stopped worker (no observed change) keeps
// today's timeout warn (the friction line). activityAgeSeconds is
// read-only. waitFor runs through the real entry (child process) so its
// stdout and exit code are observable; a fake `herdr` (writeFakeCli, the
// wait tests' fake plus a screen flip between reads) is the only herdr the
// code sees.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { writeFakeCli } from './fakes.mjs';
import { nodeBin } from './parity.mjs';
import { loadConfig } from '../lib/config.mjs';
import {
  activityAgeSeconds, waitFor, cksumField, normalizeScreen,
} from '../lib/wait.mjs';

const SCRIPTS = path.resolve(path.dirname(new URL(import.meta.url).pathname), '..');
const JS_ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');

// ---------- fake herdr (the wait tests' fake, plus a read flip) ----------

// `agent get` per target (mode-<t> file) else the global mode file
// (denied → an unqueryable error, missing → agent_not_found, else the mode
// as agent_status); `agent read` prints screen-<t> (or the global screen
// file) on every call. Every call is logged as one "$*" line (FAKE_LOG).
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
} else if (cmd === 'agent send-keys' || cmd === 'agent prompt') {
  process.stdout.write('{"result":{}}\\n');
} else if (cmd === 'notification show') {
  process.stdout.write('{"result":{}}\\n');
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
    XDG_CONFIG_HOME: path.join(root, 'conf'),
    TMPDIR: path.join(root, 'tmp'),
    HERDR_SOHO_DIR: state,
    HERDR_WORKSPACE_ID: 'ws',
    HERDR_ENV: '1',
    HERDR_SOHO_REGRID: 'off',
    HERDR_SOHO_WAIT_POLL_MS: '20',
    FAKE_MODE: path.join(root, 'mode'),
    FAKE_MODE_DIR: modeDir,
    FAKE_SCREEN: path.join(root, 'screen'),
    FAKE_SCREEN_DIR: screenDir,
    FAKE_LOG: path.join(root, 'herdr.log'),
    PATH: `${bin}${path.delimiter}${process.env.PATH}`,
  };
  fs.writeFileSync(env.FAKE_MODE, 'working\n');
  fs.writeFileSync(env.FAKE_SCREEN, '');
  const H12 = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';
  const fix = {
    root, repo, state, ws, env, ctx: loadConfig(env, repo),
    mode(m) { fs.writeFileSync(env.FAKE_MODE, `${m}\n`); },
    modeOf(agent, m) { fs.writeFileSync(path.join(modeDir, `mode-${agent}`), `${m}\n`); },
    screen(s) { fs.writeFileSync(env.FAKE_SCREEN, s); },
    screenOf(agent, s) { fs.writeFileSync(path.join(screenDir, `screen-${agent}`), s); },
    // Atomic screen swap while the wait child runs: the fake herdr reads
    // the file on every call, so this lands between two of its probes
    // regardless of load (a rename replaces the file whole — the fake
    // never reads a partial screen).
    swapScreen(s) {
      const tmp = path.join(root, 'screen.swap');
      fs.writeFileSync(tmp, s);
      fs.renameSync(tmp, env.FAKE_SCREEN);
    },
    writeRoster(...rows) { fs.writeFileSync(path.join(ws, 'agents.tsv'), H12 + rows.join('\n') + '\n'); },
    waitFile(agent, name, content) { fs.writeFileSync(path.join(ws, 'wait', `${agent}.${name}`), content); },
    waitRead(agent, name) {
      try { return fs.readFileSync(path.join(ws, 'wait', `${agent}.${name}`), 'utf8'); } catch { return null; }
    },
    friction() {
      try { return fs.readFileSync(path.join(ws, 'friction.log'), 'utf8'); } catch { return ''; }
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
  fix.writeRoster();
  return fix;
}

// Row with every 12-column field (pane p-<name>, created 1).
const ROW = (name, role, kind = 'grok', model = '', lane = '') =>
  `${name}\tp-${name}\t${kind}\t${role}\txai\t1\t/tmp/work\tnow\t${model}\tfull\t${role}\t${lane}`;

// `herdr-soho.mjs wait …` as a child process (the waitFor stdout and
// exit code are only observable outside the process).
function waitCmd(fix, args, extraEnv = {}) {
  return spawnSync(nodeBin(), [JS_ENTRY, 'wait', ...args], { cwd: fix.repo, env: { ...fix.env, ...extraEnv }, encoding: 'utf8', timeout: 60_000 });
}
// The same wait, started asynchronously: the screen can be swapped while
// the child probes (a real change between two probes of the same wait).
function waitAsync(fix, args) {
  return new Promise((resolve) => {
    const child = spawn(nodeBin(), [JS_ENTRY, 'wait', ...args], { cwd: fix.repo, env: fix.env });
    let out = '';
    let err = '';
    child.stdout.on('data', (d) => { out += d; });
    child.stderr.on('data', (d) => { err += d; });
    child.on('close', (code) => resolve({ status: code, stdout: out, stderr: err }));
  });
}
function jsonLines(out) {
  return out.trim().split('\n').filter((l) => l !== '').map((l) => JSON.parse(l));
}

// ---------- acceptance 1: reading a still screen is not activity ----------

// A working agent whose screen is static from the start and has no prior
// markers: the wait's probes only READ the screen (the first observation
// writes stuck-hash but no activity-at), so both waits are not active —
// exit 9, checkpoint false, activity_age_s null, and the friction log
// gains one `timeout waiting` line per wait.
test('wait: two short timeouts on a read-only still screen are not active (friction warn each)', { timeout: 60000 }, () => {
  const fix = makeFix('ha-wait-checkpoint-readonly-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.mode('working');
    fix.screen('typing on the same line…\n');
    // Mutation captured: writing activity-at also on the FIRST
    // observation (no previous stuck-hash) makes both waits below active
    // (checkpoint true, activity_age_s 0, no friction line) — this
    // fixture asserts the opposite for both waits.
    const r1 = waitCmd(fix, ['w', '--timeout', '1000']);
    assert.equal(r1.status, 9, r1.stderr);
    const l1 = jsonLines(r1.stdout)[0];
    assert.deepEqual(Object.keys(l1), ['agent', 'status', 'elapsed_ms', 'state', 'checkpoint', 'activity_age_s']);
    assert.equal(l1.agent, 'w');
    assert.equal(l1.status, 'timeout');
    assert.equal(l1.state, 'working');
    assert.equal(l1.checkpoint, false, 'a fresh read of a still screen is not active');
    assert.equal(l1.activity_age_s, null, 'no observed change → null');
    assert.equal(fix.waitRead('w', 'activity-at'), null, 'the first observation writes no activity-at');
    assert.match(r1.stderr, /timeout waiting for 'w'; it may still be working \(state: working\)\. Run: herdr-soho wait w --timeout 2000/);
    assert.equal((fix.friction().match(/timeout waiting for 'w'/g) ?? []).length, 1, 'the friction line stands: ' + JSON.stringify(fix.friction()));
    // Second wait: the screen was read again, but reading is still not
    // activity.
    const r2 = waitCmd(fix, ['w', '--timeout', '1000']);
    assert.equal(r2.status, 9, r2.stderr);
    const l2 = jsonLines(r2.stdout)[0];
    assert.equal(l2.checkpoint, false, 'the second read is not activity either');
    assert.equal(l2.activity_age_s, null);
    assert.equal(fix.waitRead('w', 'activity-at'), null, 'still no activity-at after two reads');
    assert.equal((fix.friction().match(/timeout waiting for 'w'/g) ?? []).length, 2, 'one friction line per wait');
  } finally { fix.cleanup(); }
});

// ---------- acceptance 2: a real change between probes is active ----------

// The screen changes (a real text change, not only counters) between two
// probes of the same wait — the test swaps the screen file the fake herdr
// serves while the wait child runs, so the change lands between two of the
// wait's own probes. The probe that observes the move writes activity-at,
// and the timeout is a neutral checkpoint — exit 9, checkpoint true, no
// friction line.
test('wait: a real screen change observed between probes of one wait is a neutral checkpoint', { timeout: 60000 }, async () => {
  const fix = makeFix('ha-wait-checkpoint-change-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.mode('working');
    const A = 'Compiling app 12% ◐ 3.1s\n';
    const B = 'Running the e2e suite for the billing tree\n';
    fix.screen(A);
    // Seed the bookkeeping as if a probe saw A a second ago and an older
    // change happened 100 s ago: the wait must REWRITE activity-at when it
    // observes A → B, dated at the probe before the move (≥ t0 - 1).
    const t0 = Math.floor(Date.now() / 1000);
    fix.waitFile('w', 'stuck-hash', `${cksumField(normalizeScreen(A))}\n`);
    fix.waitFile('w', 'probe-at', `${t0 - 1}\n`);
    fix.waitFile('w', 'activity-at', `${t0 - 100}\n`);
    // Mutation captured: the probe never writing activity-at on a hash
    // move leaves the seeded age (≥ 100 s, over the window) in place —
    // the checkpoint below is lost (checkpoint false, the friction warn
    // instead, and activity-at still the seeded old value).
    const pending = waitAsync(fix, ['w', '--timeout', '3000']);
    setTimeout(() => fix.swapScreen(B), 300);
    const r = await pending;
    assert.equal(r.status, 9, r.stderr);
    const l = jsonLines(r.stdout)[0];
    assert.equal(l.state, 'working');
    assert.equal(l.checkpoint, true, 'an observed change under the window is active');
    assert.equal(typeof l.activity_age_s, 'number');
    assert.ok(l.activity_age_s >= 0 && l.activity_age_s <= 15, `the change is fresh (age ${l.activity_age_s}s)`);
    assert.equal(fix.waitRead('w', 'stuck-hash'), `${cksumField(normalizeScreen(B))}\n`, 'the probe re-recorded the moved screen');
    const at = Number((fix.waitRead('w', 'activity-at') ?? '').trim());
    assert.ok(at >= t0 - 1 && at <= t0 + 10, `activity-at was rewritten by the wait (seeded ${t0 - 100}, now ${at})`);
    assert.equal(r.stderr,
      `herdr-soho: checkpoint: 'w' is still working (screen changed ${l.activity_age_s}s ago); wait again: herdr-soho wait w --timeout 3000\n`,
      'the exact checkpoint line, nothing else on stderr');
    assert.ok(!fix.friction().includes('timeout waiting'), 'no friction line for the checkpoint: ' + JSON.stringify(fix.friction()));
  } finally { fix.cleanup(); }
});

// ---------- acceptance 3: a counter-only change is not a change ----------

// The screen moves only in digits and progress glyphs (the stuck hash
// normalizes both) between two probes of the same wait: the probes see
// the same normalized hash, write no activity-at, and the timeout keeps
// the friction warn.
test('wait: a counter-only screen change is treated as static (not active)', { timeout: 60000 }, async () => {
  const fix = makeFix('ha-wait-checkpoint-counters-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.mode('working');
    const A1 = 'Running tests 12% ◐ 1.2s\n';
    const A2 = 'Running tests 47% ◑ 8.4s\n';
    fix.screen(A1);
    fix.waitFile('w', 'stuck-hash', `${cksumField(normalizeScreen(A1))}\n`);
    // no activity-at: nothing was ever observed as a real change
    // Mutation captured: activityAgeSeconds ignoring the hash comparison
    // (0 for any existing stuck-hash) makes this wait active (checkpoint
    // true) and drops the friction line below.
    const pending = waitAsync(fix, ['w', '--timeout', '3000']);
    setTimeout(() => fix.swapScreen(A2), 300); // only the counters and the glyph move
    const r = await pending;
    assert.equal(r.status, 9, r.stderr);
    const l = jsonLines(r.stdout)[0];
    assert.equal(l.state, 'working');
    assert.equal(l.checkpoint, false, 'counters alone are not a screen change');
    assert.equal(l.activity_age_s, null, 'no activity-at was written');
    assert.equal(fix.waitRead('w', 'activity-at'), null, 'the probes wrote no activity-at');
    assert.equal((fix.friction().match(/timeout waiting for 'w'/g) ?? []).length, 1, 'the friction line stands');
  } finally { fix.cleanup(); }
});

// ---------- activityAgeSeconds: the semantics, hostile markers, read-only ----------

test('activityAgeSeconds: 0 on a changed screen, the age on an equal one, null without valid markers', () => {
  const fix = makeFix('ha-wait-activityage-');
  try {
    const sd = fix.ws;
    const screen = 'Screen with 42 counters ◐\n';
    const H = String(cksumField(normalizeScreen(screen)));
    const now = Math.floor(Date.now() / 1000);
    const files = () => fs.readdirSync(path.join(sd, 'wait')).sort();
    // No stuck-hash at all: null, even with a valid activity-at.
    fix.waitFile('a', 'activity-at', `${now - 10}\n`);
    assert.equal(activityAgeSeconds(sd, 'a', screen, now), null, 'no stuck-hash → null');
    // Hostile marker contents: an empty stuck-hash or a missing/empty/
    // non-integer/non-positive activity-at is null with an equal hash.
    for (const [hash, at] of [
      [''],
      [H, null],
      [H, ''],
      [H, 'x y\n'],
      [H, '0\n'],
      [H, '-5\n'],
      [H, '1.5\n'],
      [H, '  \n'],
    ]) {
      if (hash === null) fs.rmSync(path.join(sd, 'wait', 'a.stuck-hash'), { force: true });
      else fix.waitFile('a', 'stuck-hash', `${hash}\n`);
      if (at === null) fs.rmSync(path.join(sd, 'wait', 'a.activity-at'), { force: true });
      else if (at !== undefined) fix.waitFile('a', 'activity-at', at);
      assert.equal(activityAgeSeconds(sd, 'a', screen, now), null, `null for stuck-hash ${JSON.stringify(hash)}, activity-at ${JSON.stringify(at)}`);
    }
    // A changed normalized screen is a change since the last probe: its
    // age is now - probe-at (else now - stuck-since), whatever activity-at
    // holds; without either marker nothing dates it (null).
    fix.waitFile('a', 'stuck-hash', `${H}\n`);
    fix.waitFile('a', 'activity-at', `x\n`); // invalid on purpose
    assert.equal(activityAgeSeconds(sd, 'a', 'Another screen 99% ◑\n', now), null, 'a change with no probe time is undated');
    fix.waitFile('a', 'stuck-since', `${now - 40}\n`);
    assert.equal(activityAgeSeconds(sd, 'a', 'Another screen 99% ◑\n', now), 40, 'dated at stuck-since without probe-at');
    fix.waitFile('a', 'probe-at', `${now - 3}\n`);
    assert.equal(activityAgeSeconds(sd, 'a', 'Another screen 99% ◑\n', now), 3, 'dated at the last probe');
    // Mutation captured: dating a change at `now` (0) makes a move seen
    // across a long gap fresh.
    fix.waitFile('a', 'probe-at', `${now - 1800}\n`);
    assert.equal(activityAgeSeconds(sd, 'a', 'Another screen 99% ◑\n', now), 1800, 'a long gap stays old');
    // The same screen with moved counters (digits → #, glyphs → *) keeps
    // the hash: the age is nowS - activity-at, exact.
    fix.waitFile('a', 'activity-at', `${now - 10}\n`);
    const before = files();
    assert.equal(activityAgeSeconds(sd, 'a', 'Screen with 99 counters ◑\n', now), 10);
    assert.equal(activityAgeSeconds(sd, 'a', screen, now + 5), 15);
    assert.deepEqual(files(), before, 'read-only: no marker is written or rewritten');
    // Mutation captured: hashing a failed (empty) read as a real screen makes
    // it "different" (0, active); a recorded empty-screen hash would do the
    // same on the next good read.
    assert.equal(activityAgeSeconds(sd, 'a', '', now), null, 'an empty (failed) read proves nothing');
    assert.equal(activityAgeSeconds(sd, 'a', '  \n', now), null, 'a blank read proves nothing');
    fix.waitFile('a', 'stuck-hash', `${String(cksumField(normalizeScreen('')))}\n`);
    assert.equal(activityAgeSeconds(sd, 'a', screen, now), null, 'moving from an empty-screen hash is not a change');
  } finally { fix.cleanup(); }
});

// ---------- a long gap and a disabled stuck check are not fresh activity ----------

// The last probe saw A 30 min ago; the next wait sees B. The move is real
// but can be 30 min old: dated at that probe, it is over the window — no
// checkpoint, the friction warn stands.
test('wait: a move seen across a long gap between waits is dated at the old probe (not active)', { timeout: 60000 }, () => {
  const fix = makeFix('ha-wait-checkpoint-gap-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.mode('working');
    const t0 = Math.floor(Date.now() / 1000);
    fix.waitFile('w', 'stuck-hash', `${cksumField(normalizeScreen('Screen A of the old wait\n'))}\n`);
    fix.waitFile('w', 'probe-at', `${t0 - 1800}\n`);
    fix.screen('Screen B now, unchanged since\n');
    // Mutation captured: dating the observed move at the current probe
    // turns this 30-minute-old move into "changed 0s ago" (checkpoint true).
    const r = waitCmd(fix, ['w', '--timeout', '1000']);
    assert.equal(r.status, 9, r.stderr);
    const l = jsonLines(r.stdout)[0];
    assert.equal(l.checkpoint, false, 'an old move is not fresh activity');
    assert.ok(l.activity_age_s >= 1800, `aged from the old probe (got ${l.activity_age_s})`);
    assert.equal((fix.friction().match(/timeout waiting for 'w'/g) ?? []).length, 1);
  } finally { fix.cleanup(); }
});

// stuck_warn_minutes=0 turns the stuck warning off, not the bookkeeping: a
// hash left by an earlier wait is replaced by the current screen, so a
// still screen does not stay "changed 0s ago" forever.
test('wait: with stuck_warn_minutes=0 the hash still follows the screen (no endless fresh change)', { timeout: 60000 }, () => {
  const fix = makeFix('ha-wait-checkpoint-off-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.mode('working');
    const still = 'A still screen after the earlier wait\n';
    const t0 = Math.floor(Date.now() / 1000);
    fix.waitFile('w', 'stuck-hash', `${cksumField(normalizeScreen('Another screen from before\n'))}\n`);
    fix.waitFile('w', 'stuck-since', `${t0 - 3600}\n`);
    fix.screen(still);
    // Mutation captured: keeping the hash bookkeeping behind
    // stuck_warn_minutes > 0 leaves the stale hash, and every timeout
    // reads as a fresh change (checkpoint true, age 0).
    for (const n of [1, 2]) {
      const r = waitCmd(fix, ['w', '--timeout', '1000'], { HERDR_SOHO_STUCK_WARN_MINUTES: '0' });
      assert.equal(r.status, 9, r.stderr);
      const l = jsonLines(r.stdout)[0];
      assert.equal(l.checkpoint, false, `wait ${n}: a still screen is not active`);
    }
    assert.equal(fix.waitRead('w', 'stuck-hash'), `${cksumField(normalizeScreen(still))}\n`, 'the hash follows the screen');
  } finally { fix.cleanup(); }
});

// ---------- the non-numeric timeout keeps the old shapes (in process) ----------

// waitFor with a non-numeric timeoutMs (unreachable through the CLI, which
// rejects it) has no value to repeat in the suggestion: the active agent
// gets the bare checkpoint line and the inactive one the bare warn — in
// both cases rc 9, nothing else changes.
test('wait: a non-numeric timeout drops the --timeout suggestion (checkpoint and warn)', { timeout: 60000 }, () => {
  const fix = makeFix('ha-wait-checkpoint-notime-');
  try {
    fix.writeRoster(ROW('w', 'implementer', 'grok', 'grok-4.7', 'build'));
    fix.modeOf('w', 'working');
    fix.screenOf('w', 'Busy on the task 30%\n');
    const screen = 'Busy on the task 30%\n';
    const now = Math.floor(Date.now() / 1000);
    const lines = [];
    const errLines = [];
    const realErr = process.stderr.write.bind(process.stderr);
    process.stderr.write = (l) => { errLines.push(String(l)); return true; };
    try {
      // Active: a previous hash exists, the screen matches it, and the
      // last observed change is 5 s old (under the default 20-min window).
      fix.waitFile('w', 'stuck-hash', `${cksumField(normalizeScreen(screen))}\n`);
      fix.waitFile('w', 'activity-at', `${now - 5}\n`);
      const rc = waitFor(['w'], { sd: fix.ws, ctx: fix.ctx, env: fix.env, timeoutMs: 'not-a-number', any: false, sink: (l) => lines.push(l) });
      assert.equal(rc, 9);
      const l = JSON.parse(lines.at(-1));
      assert.deepEqual(Object.keys(l), ['agent', 'status', 'elapsed_ms', 'state', 'checkpoint', 'activity_age_s']);
      assert.equal(l.checkpoint, true);
      assert.ok(errLines.some((e) => e ===
        `herdr-soho: checkpoint: 'w' is still working (screen changed ${l.activity_age_s}s ago); wait again: herdr-soho wait w\n`),
        'the checkpoint line without a --timeout suggestion: ' + errLines.join('|'));
      // Inactive: the same screen aged past the default window.
      fix.waitFile('w', 'activity-at', `${now - 3600}\n`);
      const rc2 = waitFor(['w'], { sd: fix.ws, ctx: fix.ctx, env: fix.env, timeoutMs: 'not-a-number', any: false, sink: (l) => lines.push(l) });
      assert.equal(rc2, 9);
      const l2 = JSON.parse(lines.at(-1));
      assert.equal(l2.checkpoint, false);
      assert.ok(errLines.some((e) => e === "herdr-soho: warning: timeout waiting for 'w'; it may still be working (state: working)\n"),
        'the bare warn without a suggestion: ' + errLines.join('|'));
    } finally {
      process.stderr.write = realErr;
    }
  } finally { fix.cleanup(); }
});
