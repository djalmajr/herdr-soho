// Windows .cmd/.bat timeout runner for runCli (lib/platform.mjs, issue #20).
//
// spawnSync kills only cmd.exe on timeout; the processes the batch started
// (for an npm-installed CLI, the `node …\cli.js` behind the .cmd shim)
// keep running. This helper is spawned by runCli as
// `spawnSync(process.execPath, [this file, specFile], …)` with exactly the
// stdio/input/encoding/env/cwd of the mode in use (mergeOutput, outputFiles
// or the default pipes+input). The helper arms the command's timeout
// itself; when it fires while the child is alive it runs taskkill
// /PID <pid> /T /F (10 s cap) while the tree root is still alive, so /T
// reaches the whole tree. It then waits for the child's exit and writes
// { status, signal, timedOut, error } (error: the spawn error code when
// the command never started, e.g. ENOENT) to spec.resultFile — runCli maps
// timedOut to the shape spawnSync's timeout returns today (status null,
// signal SIGTERM, error ETIMEDOUT) and passes everything else through.
//
// The command's streams: in mergeOutput and outputFiles the helper's stdio
// is the mode's file fd(s) and the child takes them as-is (stdio
// 'inherit'), so the output files capture everything the tree writes, even
// past the kill. In the default mode the helper's stdio is the mode's
// pipes and the spec carries ownPipes: true: the child then runs on the
// helper's own pipes (stdio ['inherit', 'pipe', 'pipe'] — stdin is still
// the helper's, which carries the mode's input), and the helper replays
// the child's bytes on its own stdout/stderr when it finishes. A grandchild
// that outlives the child (and inherited the child's streams) holds only
// those internal pipes, which die with the helper — the outer call returns
// when the helper exits instead of waiting for the grandchild until the
// safety cap.
//
// specFile (JSON, 0600, in the same TMPDIR as runCli, removed at the end):
// { command, args, windowsVerbatimArguments, timeoutMs, resultFile,
// ownPipes } — the arguments never pass through a command-line reparse.
//
// The helper prints nothing on its streams in normal operation. Failure
// paths (bad spec, no resultFile written) end without the result file, so
// runCli returns the wrapper error instead of inventing a status.
//
// Test hook (read only by the helper process): HERDR_SOHO_TREEKILL_TEST_KILLER
// replaces taskkill with that executable, called with the child pid as its
// only argument — the portable (macOS/Linux) tests use it to prove the
// killer receives the right pid without a real taskkill. It is honored only
// when the value is an absolute path whose realpath is inside the system
// temp dir (os.tmpdir()); any other value (a relative path, a system
// executable, …) is ignored and taskkill runs.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';

const specFile = process.argv[2];
if (specFile === undefined) process.exit(1);

let spec;
try {
  spec = JSON.parse(fs.readFileSync(specFile, 'utf8'));
} catch {
  process.exit(1); // bad spec: no resultFile, the wrapper error surfaces
}
if (spec === null || typeof spec !== 'object') process.exit(1);
const { command, args, windowsVerbatimArguments, timeoutMs, resultFile, ownPipes } = spec;
if (
  typeof command !== 'string'
  || !Array.isArray(args)
  || typeof windowsVerbatimArguments !== 'boolean'
  || !Number.isFinite(timeoutMs) || timeoutMs <= 0
  || typeof resultFile !== 'string'
  || typeof ownPipes !== 'boolean'
) process.exit(1);

// testKiller — HERDR_SOHO_TREEKILL_TEST_KILLER is honored only when it is
// an absolute path whose realpath is strictly inside the (resolved) system
// temp dir; anything else — a relative path, a system executable, a value
// outside the temp dir — is ignored and the real taskkill runs, so the hook
// can never point the helper at an arbitrary executable. Returns the
// resolved killer path, or null for "use taskkill".
function testKiller() {
  const killer = process.env.HERDR_SOHO_TREEKILL_TEST_KILLER;
  if (killer === undefined || killer === '') return null;
  if (!path.isAbsolute(killer)) return null;
  let real;
  try { real = fs.realpathSync(killer); } catch { return null; }
  let tmp;
  try { tmp = fs.realpathSync(os.tmpdir()); } catch { return null; }
  return real.startsWith(tmp + path.sep) ? real : null;
}

// killTree <pid>: taskkill /T /F on the tree root while it is alive reaches
// every descendant. Its own failure never changes the result — the helper
// still waits for the child's exit, and runCli's outer safety cap
// (timeoutMs + 15 s) bounds the whole call.
function killTree(pid) {
  const killer = testKiller();
  if (killer !== null) {
    spawnSync(killer, [String(pid)], { timeout: 10_000, stdio: 'ignore' });
    return;
  }
  const systemRoot = process.env.SystemRoot || process.env.WINDIR || 'C:\\Windows';
  spawnSync(path.join(systemRoot, 'System32', 'taskkill.exe'), ['/PID', String(pid), '/T', '/F'], {
    timeout: 10_000,
    stdio: 'ignore',
  });
}

let child;
try {
  if (ownPipes) {
    // Default mode: the child takes its own pipes (stdin is still the
    // helper's, so the mode's input reaches it); finish replays the
    // child's bytes on the helper's own stdout/stderr — see the header.
    child = spawn(command, args, { stdio: ['inherit', 'pipe', 'pipe'], windowsVerbatimArguments });
  } else {
    // mergeOutput / outputFiles: 'inherit' — the child takes the
    // helper's stdio, i.e. the mode's file fds, and the output files
    // capture the command's streams exactly as today.
    child = spawn(command, args, { stdio: 'inherit', windowsVerbatimArguments });
  }
} catch {
  process.exit(1); // synchronous spawn throw: no resultFile, no invented status
}

// ownPipes: consume the child's pipes (an unread pipe would fill and stall
// the child) and keep the bytes; finish replays them on the helper's own
// streams before exiting.
const outChunks = [];
const errChunks = [];
if (ownPipes) {
  child.stdout?.on('data', (chunk) => outChunks.push(chunk));
  child.stderr?.on('data', (chunk) => errChunks.push(chunk));
}

let done = false;
let timedOut = false;
const timer = setTimeout(() => {
  if (done || typeof child.pid !== 'number') return;
  timedOut = true;
  killTree(child.pid);
}, timeoutMs);

function finish(result) {
  if (done) return;
  done = true;
  clearTimeout(timer);
  if (ownPipes) {
    // Replay what already arrived on the helper's own streams, before
    // exiting: a bare process.exit could drop bytes still queued in the JS
    // stream layer, a synchronous fd write cannot. What has not arrived
    // yet is the tree's problem (the kill fired while it lived).
    try {
      const out = Buffer.concat(outChunks);
      const err = Buffer.concat(errChunks);
      if (out.length > 0) writeFdAll(1, out);
      if (err.length > 0) writeFdAll(2, err);
    } catch { /* the outer call is gone: the resultFile below still decides */ }
  }
  try {
    fs.writeFileSync(resultFile, JSON.stringify(result), { mode: 0o600 });
  } catch {
    // no resultFile: runCli returns the wrapper error, no invented status
  }
  process.exit(0);
}

// writeFdAll — synchronous full write to an fd: a pipe can return partial
// writes, so loop until the buffer is drained (0 ends the loop rather than
// spinning on a pipe that cannot take more right now).
function writeFdAll(fd, buf) {
  let offset = 0;
  while (offset < buf.length) {
    const written = fs.writeSync(fd, buf, offset, buf.length - offset);
    if (written <= 0) break;
    offset += written;
  }
}

child.on('error', (err) => {
  // The command never started (ENOENT, EACCES, …): report the spawn error
  // code like spawnSync does today, with no status.
  finish({ status: null, signal: null, timedOut: false, error: err?.code ?? null });
});
child.on('exit', (code, signal) => {
  finish({ status: code, signal, timedOut, error: null });
});
