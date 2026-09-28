// Windows .cmd/.bat timeout runner for runCli (lib/platform.mjs, issue #20).
//
// spawnSync kills only cmd.exe on timeout; the processes the batch started
// (for an npm-installed CLI, the `node …\cli.js` behind the .cmd shim)
// keep running. This helper is spawned by runCli as
// `spawnSync(process.execPath, [this file, specFile], …)` with exactly the
// stdio/input/encoding/env/cwd of the mode in use (mergeOutput, outputFiles
// or the default pipes+input), so its child — spawned here with
// stdio 'inherit' — writes straight into that mode's fds/pipes. The helper
// arms the command's timeout itself; when it fires while the child is
// alive it runs taskkill /PID <pid> /T /F (10 s cap) while the tree root
// is still alive, so /T reaches the whole tree. It then waits for the
// child's exit and writes { status, signal, timedOut, error } (error: the
// spawn error code when the command never started, e.g. ENOENT) to
// spec.resultFile — runCli maps timedOut to the shape spawnSync's timeout
// returns today (status null, signal SIGTERM, error ETIMEDOUT) and passes
// everything else through.
//
// specFile (JSON, 0600, in the same TMPDIR as runCli, removed at the end):
// { command, args, windowsVerbatimArguments, timeoutMs, resultFile } — the
// arguments never pass through a command-line reparse.
//
// The helper prints nothing on its streams in normal operation. Failure
// paths (bad spec, no resultFile written) end without the result file, so
// runCli returns the wrapper error instead of inventing a status.
//
// Test hook (read only by the helper process): HERDR_SOHO_TREEKILL_TEST_KILLER,
// when set, replaces taskkill with that executable, called with the child
// pid as its only argument — the portable (macOS/Linux) tests use it to
// prove the killer receives the right pid without a real taskkill.
import fs from 'node:fs';
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
const { command, args, windowsVerbatimArguments, timeoutMs, resultFile } = spec;
if (
  typeof command !== 'string'
  || !Array.isArray(args)
  || typeof windowsVerbatimArguments !== 'boolean'
  || !Number.isFinite(timeoutMs) || timeoutMs <= 0
  || typeof resultFile !== 'string'
) process.exit(1);

// killTree <pid>: taskkill /T /F on the tree root while it is alive reaches
// every descendant. Its own failure never changes the result — the helper
// still waits for the child's exit, and runCli's outer safety cap
// (timeoutMs + 15 s) bounds the whole call.
function killTree(pid) {
  const killer = process.env.HERDR_SOHO_TREEKILL_TEST_KILLER;
  if (killer !== undefined && killer !== '') {
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
  // 'inherit': the child takes the helper's stdio, i.e. the mode's fds
  // (the merged fd, the two file fds, or the pipes) — runCli sees the
  // command's streams exactly as today.
  child = spawn(command, args, { stdio: 'inherit', windowsVerbatimArguments });
} catch {
  process.exit(1); // synchronous spawn throw: no resultFile, no invented status
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
  try {
    fs.writeFileSync(resultFile, JSON.stringify(result), { mode: 0o600 });
  } catch {
    // no resultFile: runCli returns the wrapper error, no invented status
  }
  process.exit(0);
}

child.on('error', (err) => {
  // The command never started (ENOENT, EACCES, …): report the spawn error
  // code like spawnSync does today, with no status.
  finish({ status: null, signal: null, timedOut: false, error: err?.code ?? null });
});
child.on('exit', (code, signal) => {
  finish({ status: code, signal, timedOut, error: null });
});
