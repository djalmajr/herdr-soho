// I20 / issue #20: on Windows, a .cmd/.bat CLI that times out must not leave
// a single process of its tree behind — spawnSync killed only cmd.exe, so
// the `node …\cli.js` behind an npm .cmd shim survived the timeout.
//
// The fix (lib/platform.mjs runCli + lib/treekill-run.mjs): when platform
// is win32, the resolved target ends in .cmd/.bat and a timeout was given,
// runCli spawns the treekill helper (the same runtime running the CLI) with
// exactly the stdio/input/encoding/env/cwd of the mode in use, without the
// command's timeout — only a safety cap of timeoutMs + 15 s. The helper
// arms the timeout, and on it firing kills the whole tree (taskkill
// /PID <pid> /T /F while the tree root is alive); it writes
// { status, signal, timedOut, error } to a temp resultFile that runCli
// maps back to today's spawnSync shape (timeout → status null, signal
// SIGTERM, timedOut, error ETIMEDOUT). The command's streams: mergeOutput
// and outputFiles run it with stdio 'inherit' over the mode's file fds;
// the default mode (spec ownPipes: true) runs it on the helper's own
// pipes, replayed onto the helper's stdout/stderr in finish — a grandchild
// that outlives the command holds only those internal pipes, so the call
// cannot be held open until the safety cap. The HERDR_SOHO_TREEKILL_TEST_KILLER
// injection replaces taskkill only when it is an absolute path inside the
// (resolved) system temp dir; any other value is ignored.
//
// What runs where:
//  - portable tests: the helper directly with a Node command (a) finished
//    before the timeout (status 3, streams) and (b) past it, with
//    taskkill replaced by the HERDR_SOHO_TREEKILL_TEST_KILLER injection
//    (the killer receives the child pid); (c) a killer outside the temp
//    dir (a relative path, a system executable) is never called; and
//    runCli's win32 path simulated end-to-end on POSIX with a fake
//    COMSPEC (the "cmd.exe") and the injected killer — every mode
//    (mergeOutput, outputFiles, default with input) × (finishes before
//    the timeout, times out), plus a default-mode grandchild that holds
//    the pipes: the call must return at the command's exit, not the cap.
//  - the simulated tests and the killer tests run with POSIX fakes (sh
//    killers) and skip on Windows with a reason; the Windows-only test
//    (taskkill /T /F on a real .cmd → node → grandchild tree, both pids
//    gone) runs there instead; the orchestrator runs it.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { fixtureEnv } from './parity.mjs';
import { writeFakeCli, sleepingFake } from './fakes.mjs';
import { runCli } from '../lib/platform.mjs';

const HELPER = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'lib', 'treekill-run.mjs');

function tmpRoot(prefix) {
  let root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), prefix)));
  return root;
}

function isAlive(pid) {
  try {
    process.kill(pid, 0);
    return true;
  } catch (e) {
    return e.code !== 'ESRCH'; // EPERM: exists, not ours
  }
}

// process.kill(pid, 0) throws ESRCH once the pid is gone (short grace,
// polled).
function assertGone(pid, label = '') {
  const deadline = Date.now() + 2000;
  while (isAlive(pid)) {
    if (Date.now() > deadline) assert.fail(`pid ${pid} still alive 2 s after the kill${label ? ` (${label})` : ''}`);
    Atomics.wait(new Int32Array(new SharedArrayBuffer(4)), 0, 0, 100);
  }
}

function killSilently(pid) {
  try { process.kill(pid, 'SIGKILL'); } catch { /* already gone */ }
}

// ---------- the helper itself (portable) ----------

// Mutation captured: writing a fixed status 0 instead of the child's exit
// code in the helper's resultFile fails the `status: 3` assert below (run
// on a throwaway copy: the helper's `finish({ status: code, … })` mutated
// to `finish({ status: 0, … })`).
test('treekill helper: a command that finishes before the timeout keeps its status, its streams and no error', { timeout: 30_000 }, () => {
  const root = tmpRoot('ha-tk-a-');
  const specFile = path.join(root, 'spec.json');
  const resultFile = path.join(root, 'result.json');
  try {
    fs.writeFileSync(specFile, JSON.stringify({
      command: process.execPath,
      args: ['-e', 'process.stdout.write("a-out\\n"); process.stderr.write("a-err\\n"); process.exit(3);'],
      windowsVerbatimArguments: false,
      timeoutMs: 15_000,
      resultFile,
      ownPipes: true,
    }), { mode: 0o600 });
    const outer = spawnSync(process.execPath, [HELPER, specFile], { encoding: 'utf8', timeout: 30_000, cwd: root });
    assert.equal(outer.status, 0, `the helper itself exits 0: ${outer.stderr}`);
    // ownPipes (the default-mode shape): the child's streams are replayed
    // on the helper's own streams — the shape the runCli default mode
    // relies on.
    assert.equal(outer.stdout, 'a-out\n', 'the child stdout reached the caller');
    assert.equal(outer.stderr, 'a-err\n', 'the child stderr reached the caller');
    assert.ok(fs.existsSync(resultFile), 'the helper wrote the resultFile');
    // The resultFile shape is the spawnSync shape: status/signal of the
    // child, timedOut false, error null.
    const result = JSON.parse(fs.readFileSync(resultFile, 'utf8'));
    assert.deepEqual(result, { status: 3, signal: null, timedOut: false, error: null });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Mutation captured: passing the killer the wrong pid (the helper's
// `killTree(child.pid)` mutated to `killTree(child.pid + 1)`) leaves the
// child alive — the killer log no longer matches the child pid and the
// helper only ends when the outer cap kills it (run on a throwaway copy).
test('treekill helper: past the timeout the killer receives the child pid and the result marks the timeout', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const root = tmpRoot('ha-tk-b-');
  const pidsFile = path.join(root, 'pids');
  const killerLog = path.join(root, 'killed');
  const killer = path.join(root, 'killer.sh');
  const specFile = path.join(root, 'spec.json');
  const resultFile = path.join(root, 'result.json');
  try {
    // The injected killer: log the pid it received, then SIGKILL it. It
    // replaces taskkill (no /T on POSIX) — the tree reach is proven on
    // Windows by the Windows-only test.
    fs.writeFileSync(killer, `#!/bin/sh\nprintf '%s\\n' "$1" >> "${killerLog}"\nkill -9 "$1" 2>/dev/null || true\n`, { mode: 0o755 });
    // A Node command that records its own pid and the pid of the grandchild
    // it starts, then hangs well past the timeout.
    const src = `import fs from 'node:fs';
import { spawn } from 'node:child_process';
const g = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 60000)'], { stdio: 'ignore' });
fs.writeFileSync(${JSON.stringify(pidsFile)}, String(process.pid) + '\\n' + String(g.pid) + '\\n');
process.stdout.write('b-out\\n');
setTimeout(() => {}, 60000);
`;
    fs.writeFileSync(specFile, JSON.stringify({
      command: process.execPath,
      args: ['-e', src],
      windowsVerbatimArguments: false,
      timeoutMs: 800,
      resultFile,
      ownPipes: true,
    }), { mode: 0o600 });
    const t0 = Date.now();
    const outer = spawnSync(process.execPath, [HELPER, specFile], {
      encoding: 'utf8',
      timeout: 30_000,
      cwd: root,
      env: { ...process.env, HERDR_SOHO_TREEKILL_TEST_KILLER: killer },
    });
    const ms = Date.now() - t0;
    // The helper ends on the child's exit right after the kill — not when
    // the outer cap would fire (a missed kill would only end at the cap).
    assert.equal(outer.status, 0, `the helper ended on the child's exit, not the cap (took ${ms} ms): ${outer.stderr}`);
    assert.ok(ms < 5000, `timeout + kill under 5 s (took ${ms} ms)`);
    assert.equal(outer.stdout, 'b-out\n', 'the child output still reached the caller');
    const result = JSON.parse(fs.readFileSync(resultFile, 'utf8'));
    assert.equal(result.timedOut, true, 'the timer fired while the child lived');
    assert.equal(result.status, null);
    // The injected killer's signal; taskkill /F reports an exit code on
    // Windows — runCli's mapping depends on timedOut, not on it.
    assert.equal(result.signal, 'SIGKILL');
    const pids = fs.readFileSync(pidsFile, 'utf8').trim().split('\n').map(Number);
    assert.equal(pids.length, 2);
    assert.equal(fs.readFileSync(killerLog, 'utf8').trim(), String(pids[0]), 'the killer received the child pid (not the grandchild)');
    assertGone(pids[0], 'the child is dead');
    // The POSIX killer killed only the pid it received (no /T): the
    // grandchild is cleaned up by the test itself.
    killSilently(pids[1]);
    assertGone(pids[1], 'the grandchild was cleaned up by the test');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// Mutation captured: honoring any HERDR_SOHO_TREEKILL_TEST_KILLER value
// (the helper's testKiller validation mutated to return the raw value)
// makes the helper run the killer it is handed: the relative killer logs
// the child pid and SIGKILLs it (the helper then exits 0 early instead of
// being ended by the outer timeout), and the system executable leaves a
// trace file named after the child pid — the asserts below fail (run on a
// throwaway copy).
test('treekill helper: a killer outside the temp dir (relative, or a system executable) is ignored and taskkill runs', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const root = tmpRoot('ha-tk-c-');
  const killerLog = path.join(root, 'killed');
  // In a subdir: the relative value below then carries a slash, so a
  // (buggy) helper would resolve it against its cwd and run it.
  const killer = path.join(root, 'killer', 'killer.sh');
  const pidsFile = path.join(root, 'pids');
  try {
    // The logging killer: only its absolute inside-tmp path is honored
    // (proven by the test above); the same file is reached here by an
    // invalid relative path.
    fs.mkdirSync(path.dirname(killer), { recursive: true });
    fs.writeFileSync(killer, `#!/bin/sh\nprintf '%s\\n' "$1" >> "${killerLog}"\nkill -9 "$1" 2>/dev/null || true\n`, { mode: 0o755 });
    // A Node child that records its pid and hangs long past the timeout:
    // nothing but a killer the helper actually runs can end it, so the
    // helper is still waiting when the outer 5 s timeout ends it.
    const src = `import fs from 'node:fs';
fs.writeFileSync(${JSON.stringify(pidsFile)}, String(process.pid) + '\\n');
setTimeout(() => {}, 60000);
`;
    const run = (label, value) => {
      const specFile = path.join(root, `spec-${label}.json`);
      const resultFile = path.join(root, `result-${label}.json`);
      fs.writeFileSync(specFile, JSON.stringify({
        command: process.execPath,
        args: ['-e', src],
        windowsVerbatimArguments: false,
        timeoutMs: 800,
        resultFile,
        ownPipes: false,
      }), { mode: 0o600 });
      const outer = spawnSync(process.execPath, [HELPER, specFile], {
        encoding: 'utf8',
        timeout: 5_000,
        cwd: root,
        env: { ...process.env, HERDR_SOHO_TREEKILL_TEST_KILLER: value },
      });
      const pid = Number(fs.readFileSync(pidsFile, 'utf8').trim());
      return { outer, resultFile, pid };
    };
    // A system executable outside the temp dir (touch): if the helper
    // called it with the child pid, it would create a file named after the
    // pid in the helper's cwd.
    const touchPath = ((spawnSync('sh', ['-c', 'command -v touch'], { encoding: 'utf8' }).stdout) || '').trim() || '/usr/bin/touch';
    assert.ok(path.isAbsolute(touchPath), 'the system executable resolves to an absolute path');

    // Relative killer (it resolves to the logging killer from the
    // helper's cwd): the helper must ignore it and fall back to taskkill.
    const rel = run('rel', path.relative(root, killer));
    assert.equal(rel.outer.signal, 'SIGTERM', 'the helper was ended by the outer timeout, not the child exit');
    assert.ok(!fs.existsSync(killerLog), 'the relative killer was never called');
    assert.ok(!fs.existsSync(rel.resultFile), 'the helper never reached the child exit (nothing killed the child)');
    assert.ok(isAlive(rel.pid), 'the relative killer did not kill the child');
    killSilently(rel.pid);
    assertGone(rel.pid, 'the child was cleaned up');

    // Absolute system-executable killer (outside the temp dir): same
    // fallback, and the executable itself never runs (no trace file).
    const abs = run('abs', touchPath);
    assert.equal(abs.outer.signal, 'SIGTERM', 'the helper was ended by the outer timeout, not the child exit');
    assert.ok(!fs.existsSync(path.join(root, String(abs.pid))), 'the system executable was never called with the child pid');
    assert.ok(!fs.existsSync(abs.resultFile), 'the helper never reached the child exit (nothing killed the child)');
    assert.ok(isAlive(abs.pid), 'the system executable did not kill the child');
    killSilently(abs.pid);
    assertGone(abs.pid, 'the child was cleaned up');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// ---------- runCli's win32 path, simulated on POSIX ----------

// A fake "cmd.exe" (the COMSPEC of the simulation): writes one line to each
// stream, records the stdin it saw, and behaves per TK_CMD_MODE — `hang`
// starts a grandchild and stays alive past the timeout; `exit3` exits 3;
// `hold` starts a grandchild that inherits the cmd's stdout/stderr and
// exits 0 right away. The `hang` grandchild uses stdio 'ignore': the
// injected POSIX killer cannot kill the tree (no /T), and the default-mode
// pipes would otherwise stay open behind it. On Windows taskkill /T kills
// it (Windows-only test).
function cmdFakeSource() {
  return `import fs from 'node:fs';
import { spawn } from 'node:child_process';
let stdin = '';
try { stdin = fs.readFileSync(0, 'utf8'); } catch { stdin = null; }
if (process.env.TK_STDIN_FILE !== undefined && stdin !== null) fs.writeFileSync(process.env.TK_STDIN_FILE, stdin);
process.stdout.write('out-from-cmd\\n');
process.stderr.write('err-from-cmd\\n');
if (process.env.TK_CMD_MODE === 'exit3') process.exit(3);
if (process.env.TK_CMD_MODE === 'hang') {
  const g = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 60000)'], { stdio: 'ignore' });
  fs.writeFileSync(process.env.TK_PIDS_FILE, String(process.pid) + '\\n' + String(g.pid) + '\\n');
  setTimeout(() => {}, 60000);
}
if (process.env.TK_CMD_MODE === 'hold') {
  const g = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 60000)'], { stdio: ['ignore', 'inherit', 'inherit'] });
  fs.writeFileSync(process.env.TK_PIDS_FILE, String(process.pid) + '\\n' + String(g.pid) + '\\n');
  process.exit(0);
}
`;
}

function simSetup() {
  const root = tmpRoot('ha-tk-sim-');
  const bin = path.join(root, 'bin');
  const tmp = path.join(root, 'tmp');
  const cwd = path.join(root, 'cwd');
  const log = path.join(root, 'log');
  for (const d of [bin, tmp, cwd, log]) fs.mkdirSync(d, { recursive: true });
  // The .cmd target: its content is irrelevant — runCli wraps it through
  // cmd.exe /d /s /c, and in the simulation the "cmd.exe" is the fake
  // COMSPEC below.
  fs.writeFileSync(path.join(bin, 'slow.cmd'), '@echo fake\r\n');
  const fakeCmd = writeFakeCli(bin, 'cmd', cmdFakeSource());
  const killerLog = path.join(log, 'killed');
  // Inside the test's TMPDIR: the helper honors the killer only when it
  // is inside its own os.tmpdir(), and runCliTreeKill hands the helper the
  // test's TMPDIR (the helper process sees it as its temp dir).
  const killer = path.join(tmp, 'killer.sh');
  fs.writeFileSync(killer, `#!/bin/sh\nprintf '%s\\n' "$1" >> "${killerLog}"\nkill -9 "$1" 2>/dev/null || true\n`, { mode: 0o755 });
  return {
    root, bin, tmp, cwd, fakeCmd, killer, killerLog,
    pidsFile: path.join(log, 'pids'),
    stdinFile: path.join(log, 'stdin'),
    // platform 'win32' selects findExecutable's PATHEXT resolution and the
    // treekill path; COMSPEC points at the fake so the whole win32 shape
    // runs end-to-end on POSIX.
    env(over = {}) {
      return fixtureEnv({
        TMPDIR: tmp,
        PATH: `${bin}${path.delimiter}${process.env.PATH}`,
        PATHEXT: '.CMD;.cmd',
        COMSPEC: fakeCmd,
        ...over,
      });
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
}

function simRun(s, modeOpts, envOver, cliOpts) {
  return runCli('slow', ['arg1'], {
    platform: 'win32',
    env: s.env(envOver),
    cwd: s.cwd,
    timeoutMs: cliOpts.timeoutMs,
    input: cliOpts.input,
    ...(modeOpts === 'mergeOutput' ? { mergeOutput: true } : {}),
    ...(modeOpts === 'outputFiles' ? { outputFiles: true } : {}),
  });
}

// Mutation captured: routing the helper through pipes instead of the
// mode's fds (the mergeOutput branch's `stdio: ['ignore', fd, fd]` mutated
// to `stdio: ['ignore', 'pipe', 'pipe']`) empties the mode's file and the
// merged text assert below fails (run on a throwaway copy).
test('runCli win32 (simulated): mergeOutput mode — finishes before the timeout and times out', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const s = simSetup();
  try {
    // Finishes before the timeout: the child's status and the merged
    // text (stdout then stderr, in write order) come back as today.
    const ok = simRun(s, 'mergeOutput', {}, { timeoutMs: 15_000 });
    assert.equal(ok.notFound, false);
    assert.equal(ok.status, 0);
    assert.equal(ok.signal, null);
    assert.equal(ok.timedOut, false);
    assert.equal(ok.error, null);
    assert.equal(ok.stdout, 'out-from-cmd\nerr-from-cmd\n');
    assert.equal(ok.stderr, '');
    // Times out: the killer ends the fake cmd.exe, runCli returns the
    // spawnSync timeout shape, and the merged text written before the
    // timeout is still captured.
    fs.rmSync(s.killerLog, { force: true });
    const t0 = Date.now();
    const to = simRun(s, 'mergeOutput', {
      TK_CMD_MODE: 'hang',
      HERDR_SOHO_TREEKILL_TEST_KILLER: s.killer,
      TK_PIDS_FILE: s.pidsFile,
    }, { timeoutMs: 800 });
    const ms = Date.now() - t0;
    assert.ok(ms < 5000, `the call ends at the timeout + kill, not the cap (took ${ms} ms)`);
    assert.equal(to.status, null);
    assert.equal(to.signal, 'SIGTERM');
    assert.equal(to.timedOut, true);
    assert.equal(to.error, 'ETIMEDOUT');
    assert.equal(to.stdout, 'out-from-cmd\nerr-from-cmd\n');
    assert.equal(to.stderr, '');
    assertSimKill(s, 'mergeOutput');
  } finally { s.cleanup(); }
});

// Mutation captured: mapping the timeout to the child's own signal/error
// instead of the fixed shape (the timedOut branch's `signal: 'SIGTERM'`
// mutated to `signal: null`) breaks the SIGTERM/ETIMEDOUT asserts below
// (run on a throwaway copy).
test('runCli win32 (simulated): outputFiles mode — finishes before the timeout and times out', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const s = simSetup();
  try {
    const ok = simRun(s, 'outputFiles', {}, { timeoutMs: 15_000 });
    assert.equal(ok.status, 0);
    assert.equal(ok.signal, null);
    assert.equal(ok.timedOut, false);
    assert.equal(ok.error, null);
    assert.equal(ok.stdout, 'out-from-cmd\n', 'stdout goes to its own file');
    assert.equal(ok.stderr, 'err-from-cmd\n', 'stderr goes to its own file');
    fs.rmSync(s.killerLog, { force: true });
    const t0 = Date.now();
    const to = simRun(s, 'outputFiles', {
      TK_CMD_MODE: 'hang',
      HERDR_SOHO_TREEKILL_TEST_KILLER: s.killer,
      TK_PIDS_FILE: s.pidsFile,
    }, { timeoutMs: 800 });
    const ms = Date.now() - t0;
    assert.ok(ms < 5000, `the call ends at the timeout + kill, not the cap (took ${ms} ms)`);
    assert.equal(to.status, null);
    assert.equal(to.signal, 'SIGTERM');
    assert.equal(to.timedOut, true);
    assert.equal(to.error, 'ETIMEDOUT');
    assert.equal(to.stdout, 'out-from-cmd\n');
    assert.equal(to.stderr, 'err-from-cmd\n');
    assertSimKill(s, 'outputFiles');
  } finally { s.cleanup(); }
});

// Mutation captured: dropping the `input` pass-through of the default mode
// (the helper spawn's `input: opts.input` mutated to `input: undefined`)
// empties the stdin file and the stdin assert below fails (run on a
// throwaway copy).
test('runCli win32 (simulated): default mode with input — finishes before the timeout and times out', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const s = simSetup();
  try {
    const ok = simRun(s, null, { TK_STDIN_FILE: s.stdinFile }, { timeoutMs: 15_000, input: 'stdin-bytes\n' });
    assert.equal(ok.status, 0);
    assert.equal(ok.signal, null);
    assert.equal(ok.timedOut, false);
    assert.equal(ok.error, null);
    assert.equal(ok.stdout, 'out-from-cmd\n');
    assert.equal(ok.stderr, 'err-from-cmd\n');
    assert.equal(fs.readFileSync(s.stdinFile, 'utf8'), 'stdin-bytes\n', 'the input reached the command through the helper');
    // A non-zero child status passes through the resultFile unchanged.
    const e3 = simRun(s, null, { TK_CMD_MODE: 'exit3' }, { timeoutMs: 15_000 });
    assert.equal(e3.status, 3, 'the child status comes back through the helper');
    assert.equal(e3.timedOut, false);
    assert.equal(e3.error, null);
    fs.rmSync(s.killerLog, { force: true });
    const t0 = Date.now();
    const to = simRun(s, null, {
      TK_CMD_MODE: 'hang',
      HERDR_SOHO_TREEKILL_TEST_KILLER: s.killer,
      TK_PIDS_FILE: s.pidsFile,
    }, { timeoutMs: 800 });
    const ms = Date.now() - t0;
    assert.ok(ms < 5000, `the call ends at the timeout + kill, not the cap (took ${ms} ms)`);
    assert.equal(to.status, null);
    assert.equal(to.signal, 'SIGTERM');
    assert.equal(to.timedOut, true);
    assert.equal(to.error, 'ETIMEDOUT');
    assert.equal(to.stdout, 'out-from-cmd\n');
    assert.equal(to.stderr, 'err-from-cmd\n');
    assertSimKill(s, 'default');
  } finally { s.cleanup(); }
});

// Mutation captured: running the default-mode command with stdio 'inherit'
// instead of the helper's own pipes (the helper's ownPipes spawn mutated
// back to `spawn(command, args, { stdio: 'inherit', … })`) lets the
// grandchild hold the outer pipes open: the call only ends at the
// timeoutMs + 15 s safety cap (~15.8 s here) and the ms < 5000 assert
// below fails (run against the pre-fix helper on a throwaway copy).
test('runCli win32 (simulated): default mode — a grandchild holding the pipes cannot hold the call past the timeout', {
  skip: process.platform === 'win32'
    ? 'simulates win32 with POSIX fakes; on Windows the real .cmd test runs'
    : false,
  timeout: 60_000,
}, () => {
  const s = simSetup();
  try {
    // The fake cmd starts a grandchild that inherits its stdout/stderr
    // (and hangs), then exits 0 well before the timeout. In default mode
    // the helper's own pipes carry the cmd's streams, so the grandchild
    // holds only those internal pipes — the call returns at the cmd's
    // exit, with the output written before it, long before the cap.
    const t0 = Date.now();
    const r = simRun(s, null, {
      TK_CMD_MODE: 'hold',
      TK_PIDS_FILE: s.pidsFile,
    }, { timeoutMs: 800 });
    const ms = Date.now() - t0;
    assert.ok(ms < 5000, `the call ends at the command's exit, not the safety cap (took ${ms} ms)`);
    assert.equal(r.status, 0, 'the command exited 0 before the timeout');
    assert.equal(r.signal, null);
    assert.equal(r.timedOut, false);
    assert.equal(r.error, null);
    assert.equal(r.stdout, 'out-from-cmd\n', 'the output written before the exit is still captured');
    assert.equal(r.stderr, 'err-from-cmd\n');
    const pids = fs.readFileSync(s.pidsFile, 'utf8').trim().split('\n').map(Number);
    assert.equal(pids.length, 2, 'the fake recorded the cmd pid and the grandchild pid');
    // The grandchild outlives the call (holding the helper's internal
    // pipes); the test cleans it up.
    killSilently(pids[1]);
    assertGone(pids[1], 'the grandchild was cleaned up by the test');
  } finally { s.cleanup(); }
});

// Shared assertions of the simulated timeout rows: the killer received the
// fake cmd.exe pid, that pid is gone, and the grandchild (the POSIX killer
// cannot reach it) is cleaned up by the test.
function assertSimKill(s, label) {
  const pids = fs.readFileSync(s.pidsFile, 'utf8').trim().split('\n').map(Number);
  assert.equal(pids.length, 2, `${label}: the fake recorded the cmd.exe pid and the grandchild pid`);
  assert.equal(fs.readFileSync(s.killerLog, 'utf8').trim(), String(pids[0]), `${label}: the killer received the cmd.exe pid`);
  assertGone(pids[0], `${label}: the fake cmd.exe is dead`);
  killSilently(pids[1]);
  assertGone(pids[1], `${label}: the grandchild was cleaned up by the test`);
}

// ---------- Windows only: the real .cmd tree and taskkill /T /F ----------

// Mutation captured: taskkill without /T (the helper's `['/PID', pid,
// '/T', '/F']` mutated to `['/PID', pid, '/F']`) leaves the grandchild
// alive: the pipe it holds stays open, the 5 s bound fails and the grandchild
// pid is not ESRCH (run on Windows by the orchestrator — not executable on
// this host, where the test skips).
test('runCli (Windows only): a .cmd that hangs past the timeout returns the timeout shape and no process of the tree survives', {
  skip: process.platform === 'win32'
    ? false
    : 'Windows-only: a real .cmd over a Node script and taskkill /T /F only exist there (the orchestrator runs this test); the portable tests above cover the helper and the runCli mapping',
  timeout: 60_000,
}, () => {
  const root = tmpRoot('ha-tk-win-');
  const bin = path.join(root, 'bin');
  const tmp = path.join(root, 'tmp');
  const cwd = path.join(root, 'cwd');
  for (const d of [bin, tmp, cwd]) fs.mkdirSync(d, { recursive: true });
  const pidsFile = path.join(root, 'pids');
  // A fake .cmd over a Node script (the npm-shim shape: cmd.exe →
  // node slow.fake.mjs → grandchild). The script writes its own pid and
  // the pid of the grandchild it starts, holds the output pipes, and hangs.
  fs.writeFileSync(path.join(bin, 'slow.fake.mjs'), `import fs from 'node:fs';
import { spawn } from 'node:child_process';
const g = spawn(process.execPath, ['-e', 'setTimeout(() => {}, 120000)'], { stdio: ['ignore', 'inherit', 'inherit'] });
fs.writeFileSync(${JSON.stringify(pidsFile)}, String(process.pid) + '\\n' + String(g.pid) + '\\n');
process.stdout.write('cmd-out\\n');
setTimeout(() => {}, 120000);
`);
  fs.writeFileSync(path.join(bin, 'slow.cmd'), `@"${process.execPath}" "%~dp0slow.fake.mjs" %*\r\n`);
  const env = fixtureEnv({ TMPDIR: tmp, PATH: `${bin}${path.delimiter}${process.env.PATH}` });
  const t0 = Date.now();
  const r = runCli('slow', ['a'], { env, cwd, timeoutMs: 1000 });
  const ms = Date.now() - t0;
  const pids = fs.existsSync(pidsFile) ? fs.readFileSync(pidsFile, 'utf8').trim().split('\n').map(Number) : [];
  try {
    assert.ok(ms < 5000, `under 5 s (took ${ms} ms)`);
    assert.equal(r.notFound, false);
    assert.equal(r.status, null);
    assert.equal(r.signal, 'SIGTERM');
    assert.equal(r.timedOut, true);
    assert.equal(r.error, 'ETIMEDOUT');
    assert.equal(r.stdout, 'cmd-out\n', 'the command output is still captured');
    assert.equal(pids.length, 2, 'the fake recorded its own pid and the grandchild pid');
    // Right after: the two pids do not exist any more (process.kill(pid, 0)
    // throws ESRCH, with a short grace).
    assertGone(pids[0], 'the .cmd node process');
    assertGone(pids[1], 'the grandchild');
  } finally {
    for (const pid of pids) killSilently(pid);
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// ---------- no behavior change outside Windows ----------

// Mutation captured: triggering the helper on every platform (the runCli
// guard `platform === 'win32' && verbatim && opts.timeoutMs > 0` mutated to
// `opts.timeoutMs > 0`) sends this POSIX run through the helper, where no
// taskkill/killer can end the child — the call only ends at the timeoutMs +
// 15 s safety cap and the timing assert below fails (run on a throwaway
// copy).
test('POSIX: runCli with a timeout never takes the treekill path (classic spawnSync shape, no helper artifacts)', { timeout: 30_000 }, () => {
  const root = tmpRoot('ha-tk-posix-');
  const bin = path.join(root, 'bin');
  const tmp = path.join(root, 'tmp');
  for (const d of [bin, tmp]) fs.mkdirSync(d, { recursive: true });
  writeFakeCli(bin, 'slowcli', sleepingFake(10_000));
  const env = fixtureEnv({ TMPDIR: tmp, PATH: `${bin}${path.delimiter}${process.env.PATH}` });
  const t0 = Date.now();
  const r = runCli('slowcli', [], { env, timeoutMs: 500 });
  const ms = Date.now() - t0;
  try {
    assert.equal(r.notFound, false);
    assert.equal(r.status, null);
    assert.equal(r.signal, 'SIGTERM');
    assert.equal(r.timedOut, true);
    assert.equal(r.error, 'ETIMEDOUT');
    // The command was killed by spawnSync's own SIGTERM at the timeout
    // (~500 ms + startup). If the helper path had fired, no killer could
    // end the child and the call would only end at the 15.5 s cap.
    assert.ok(ms < 3000, `killed at the command timeout, not the safety cap (took ${ms} ms)`);
    // The treekill path creates its spec file before spawning the helper:
    // none of its artifacts is in TMPDIR.
    const tk = fs.readdirSync(tmp).filter((f) => f.startsWith('.herdr-soho-tk-'));
    assert.deepEqual(tk, [], 'no treekill spec/result file was created');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
