#!/usr/bin/env node
// herdr-soho optional plugin — read-only bridge (slice 1).
//
// Herdr launches one action command per invocation
// (`node bridge.mjs doctor` / `node bridge.mjs roster`) with the plugin
// directory as cwd, injecting HERDR_BIN_PATH, HERDR_ENV=1 and
// HERDR_PLUGIN_CONTEXT_JSON (herdr.dev/docs/plugins/, "Commands and
// environment"). The bridge resolves the effective target from that
// context JSON — the UI focus, which can differ from the invoking
// shell's HERDR_* env — and only then runs the skill CLI:
//
//   1. parse HERDR_PLUGIN_CONTEXT_JSON (workspace_id + focused_pane_id,
//      tab_id when present);
//   2. validate the focused pane with `HERDR_BIN_PATH pane get <id>`
//      (JSON) and refuse when the call fails, the output is malformed,
//      or the pane's workspace diverges from the context's;
//   3. run `node ../skills/herdr-soho/scripts/herdr-soho.mjs
//      <doctor|roster>` with the pane's cwd, the context ids kept in the
//      CLI environment and HERDR_SOHO_NOWRITE=1 (read-only: the CLI
//      inspects the focused project without writing .gitignore or its
//      state tree there), printing the target and the CLI result.
//
// Read-only slice: no worker, regrid, setup or layout change. Every
// failure before step 3 exits non-zero without invoking the CLI.
// Exit codes follow the skill CLI conventions: 2 = invalid
// invocation/target, 4 = Herdr call failure; the CLI's own exit code
// passes through after a successful invocation.
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { cmdInvocation } from '../skills/herdr-soho/scripts/lib/platform.mjs';

// Timeouts (ms). HERDR_PANE_GET_TIMEOUT_MS mirrors the skill CLI's herdr
// ceiling (scripts/lib/herdr.mjs HERDR_TIMEOUT_MS); CLI_TIMEOUT_MS is a
// last-resort bound over the whole CLI run (the CLI times out each of its
// own herdr calls as well).
export const HERDR_PANE_GET_TIMEOUT_MS = 30_000;
export const CLI_TIMEOUT_MS = 300_000;

// Exit codes (skill CLI conventions, see the header).
export const EXIT_INVALID_TARGET = 2;
export const EXIT_HERDR_FAILURE = 4;

// The read-only slice exposes exactly these subcommands.
export const ACTIONS = new Set(['doctor', 'roster']);

// Pre-CLI failure: code + operator message (English).
export class BridgeError extends Error {
  constructor(code, message) {
    super(message);
    this.name = 'BridgeError';
    this.code = code;
  }
}

// Context ids: the focused UI target, from HERDR_PLUGIN_CONTEXT_JSON.
// The invoking shell's HERDR_* env can point at a different session
// (plugin actions follow the UI focus), so it is never a target source.
export function parseContext(env) {
  const raw = env.HERDR_PLUGIN_CONTEXT_JSON;
  if (raw === undefined || raw === null || raw === '') {
    throw new BridgeError(EXIT_INVALID_TARGET, 'missing context (empty HERDR_PLUGIN_CONTEXT_JSON); the action must run inside Herdr');
  }
  let ctx;
  try { ctx = JSON.parse(raw); } catch {
    throw new BridgeError(EXIT_INVALID_TARGET, 'invalid context (malformed JSON in HERDR_PLUGIN_CONTEXT_JSON)');
  }
  if (typeof ctx !== 'object' || ctx === null || Array.isArray(ctx)) {
    throw new BridgeError(EXIT_INVALID_TARGET, 'invalid context (expected a JSON object in HERDR_PLUGIN_CONTEXT_JSON)');
  }
  const ws = ctx.workspace_id;
  if (typeof ws !== 'string' || ws === '') {
    throw new BridgeError(EXIT_INVALID_TARGET, 'context has no workspace_id; unknown target');
  }
  const pane = ctx.focused_pane_id;
  if (typeof pane !== 'string' || pane === '') {
    throw new BridgeError(EXIT_INVALID_TARGET, 'context has no focused_pane_id; unknown target');
  }
  // tab_id is optional in the context; a non-string value is treated as
  // absent rather than passed to the CLI as a stale id.
  const tab = typeof ctx.tab_id === 'string' && ctx.tab_id !== '' ? ctx.tab_id : '';
  return { workspaceId: ws, tabId: tab, paneId: pane };
}

// `HERDR_BIN_PATH pane get <id>` (JSON) → the pane's { workspaceId, cwd }.
// Every failure is a herdr failure (exit 4): the pane is the target, so an
// unverifiable pane is never substituted with the plugin cwd or another
// workspace. Malformed output is rejected as a whole.
export function getPane(herdrBin, paneId, { timeoutMs = HERDR_PANE_GET_TIMEOUT_MS, platform = process.platform } = {}) {
  // On Windows a .cmd/.bat herdr (a wrapper, or a test fake) cannot be
  // spawned without a shell: it runs through cmd.exe with every argument
  // escaped, by the skill CLI's own rule (cmdInvocation), never `shell: true`.
  const args = ['pane', 'get', paneId];
  const inv = platform === 'win32' && /\.(bat|cmd)$/i.test(herdrBin)
    ? cmdInvocation(herdrBin, args, process.env)
    : { command: herdrBin, args, windowsVerbatimArguments: false };
  let r;
  try {
    r = spawnSync(inv.command, inv.args, {
      encoding: 'utf8',
      timeout: timeoutMs,
      killSignal: 'SIGTERM',
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsVerbatimArguments: inv.windowsVerbatimArguments,
    });
  } catch (e) {
    throw new BridgeError(EXIT_HERDR_FAILURE, `failed to run herdr (${e.message})`);
  }
  if (r.error && r.error.code === 'ETIMEDOUT') {
    throw new BridgeError(EXIT_HERDR_FAILURE, `herdr pane get timed out after ${timeoutMs / 1000}s`);
  }
  if (r.error) {
    throw new BridgeError(EXIT_HERDR_FAILURE, `herdr binary not executable (${herdrBin}): ${r.error.message}`);
  }
  if (r.status === null && r.signal != null) {
    throw new BridgeError(EXIT_HERDR_FAILURE, `herdr pane get timed out after ${timeoutMs / 1000}s`);
  }
  if (r.status !== 0) {
    // herdr reports its JSON errors on stderr; keep both streams so the
    // operator sees the machine-readable cause.
    const out = `${(r.stdout ?? '').trim()} ${(r.stderr ?? '').trim()}`.trim();
    throw new BridgeError(EXIT_HERDR_FAILURE, `herdr pane get failed (exit ${r.status})${out ? `: ${out}` : ''}`);
  }
  let j;
  try { j = JSON.parse(r.stdout ?? ''); } catch {
    throw new BridgeError(EXIT_HERDR_FAILURE, 'malformed herdr output (invalid JSON from pane get)');
  }
  const pane = j && typeof j === 'object' ? (j.result && typeof j.result === 'object' ? j.result.pane : undefined) : undefined;
  if (typeof pane !== 'object' || pane === null) {
    throw new BridgeError(EXIT_HERDR_FAILURE, 'malformed herdr output (pane get without .result.pane)');
  }
  if (typeof pane.workspace_id !== 'string' || pane.workspace_id === '') {
    throw new BridgeError(EXIT_HERDR_FAILURE, 'malformed herdr output (pane without workspace_id)');
  }
  if (typeof pane.cwd !== 'string' || pane.cwd === '') {
    throw new BridgeError(EXIT_HERDR_FAILURE, 'malformed herdr output (pane without cwd)');
  }
  return { workspaceId: pane.workspace_id, cwd: pane.cwd };
}

// The skill CLI entry, resolved relative to this file: the plugin never
// targets its own cwd and never copies the CLI's rules.
export function defaultCliScript(bridgeFile = fileURLToPath(import.meta.url)) {
  return path.resolve(path.dirname(bridgeFile), '../skills/herdr-soho/scripts/herdr-soho.mjs');
}

// Run one action: <doctor|roster>. Returns { code, out, err }; a pre-CLI
// failure throws BridgeError. opts (tests): env, cliScript,
// timeouts { paneGetMs, cliMs }, nodeBin.
export function run(subcommand, opts = {}) {
  const env = opts.env ?? process.env;
  const timeouts = opts.timeouts ?? {};
  const paneGetMs = timeouts.paneGetMs ?? HERDR_PANE_GET_TIMEOUT_MS;
  const cliMs = timeouts.cliMs ?? CLI_TIMEOUT_MS;

  if (!ACTIONS.has(subcommand)) {
    throw new BridgeError(EXIT_INVALID_TARGET, `unknown subcommand '${subcommand}' (use 'doctor' or 'roster')`);
  }
  const ctx = parseContext(env);
  const herdrBin = typeof env.HERDR_BIN_PATH === 'string' && env.HERDR_BIN_PATH !== '' ? env.HERDR_BIN_PATH : '';
  if (herdrBin === '') {
    throw new BridgeError(EXIT_INVALID_TARGET, 'HERDR_BIN_PATH missing; the action must run inside Herdr');
  }
  const pane = getPane(herdrBin, ctx.paneId, { timeoutMs: paneGetMs });
  if (pane.workspaceId !== ctx.workspaceId) {
    throw new BridgeError(EXIT_INVALID_TARGET, `workspace divergence: the context points to '${ctx.workspaceId}' and pane ${ctx.paneId} belongs to '${pane.workspaceId}'; target rejected`);
  }
  let st;
  try { st = fs.statSync(pane.cwd); } catch {
    throw new BridgeError(EXIT_INVALID_TARGET, `pane cwd does not exist: ${pane.cwd}`);
  }
  if (!st.isDirectory()) {
    throw new BridgeError(EXIT_INVALID_TARGET, `pane cwd is not a directory: ${pane.cwd}`);
  }
  const cli = opts.cliScript ?? defaultCliScript();
  if (!fs.existsSync(cli)) {
    throw new BridgeError(EXIT_INVALID_TARGET, `CLI script not found: ${cli}`);
  }

  // Child env: the context ids win over the invoking shell's HERDR_*; a
  // context without tab_id drops the shell's stale HERDR_TAB_ID. The
  // herdr binary's directory is prepended to PATH so the CLI's own
  // `herdr` calls resolve the running binary (HERDR_BIN_PATH portability,
  // herdr.dev/docs/plugins/). HERDR_SOHO_NOWRITE=1 makes the CLI
  // read-only: both actions only inspect the focused project, so the CLI
  // must not append .gitignore or create its state tree there.
  const childEnv = { ...env };
  childEnv.HERDR_WORKSPACE_ID = ctx.workspaceId;
  childEnv.HERDR_PANE_ID = ctx.paneId;
  childEnv.HERDR_SOHO_NOWRITE = '1';
  if (ctx.tabId !== '') childEnv.HERDR_TAB_ID = ctx.tabId;
  else delete childEnv.HERDR_TAB_ID;
  const pathKey = Object.keys(childEnv).find((k) => k === 'PATH' || k === 'Path') ?? 'PATH';
  childEnv[pathKey] = `${path.dirname(herdrBin)}${path.delimiter}${childEnv[pathKey] ?? ''}`;

  const out = `herdr-soho plugin: target workspace=${ctx.workspaceId} pane=${ctx.paneId} cwd=${pane.cwd}\n`;
  let r;
  try {
    r = spawnSync(opts.nodeBin ?? process.execPath, [cli, subcommand], {
      cwd: pane.cwd,
      env: childEnv,
      encoding: 'utf8',
      timeout: cliMs,
      killSignal: 'SIGTERM',
      stdio: ['ignore', 'pipe', 'pipe'],
    });
  } catch (e) {
    throw new BridgeError(EXIT_HERDR_FAILURE, `failed to run the CLI (${e.message})`);
  }
  if (r.error && r.error.code === 'ETIMEDOUT') {
    return { code: EXIT_HERDR_FAILURE, out, err: `herdr-soho plugin: CLI ${subcommand} timed out after ${cliMs / 1000}s\n` };
  }
  if (r.error) {
    throw new BridgeError(EXIT_HERDR_FAILURE, `CLI not executable (${cli}): ${r.error.message}`);
  }
  if (r.status === null && r.signal != null) {
    return { code: EXIT_HERDR_FAILURE, out, err: `herdr-soho plugin: CLI ${subcommand} timed out after ${cliMs / 1000}s\n` };
  }
  return { code: r.status ?? 1, out: out + (r.stdout ?? ''), err: r.stderr ?? '' };
}

// Entry: Herdr runs `node bridge.mjs <doctor|roster>`; direct runs behave
// the same.
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  const sub = process.argv[2] ?? '';
  try {
    const r = run(sub);
    if (r.out) process.stdout.write(r.out);
    if (r.err) process.stderr.write(r.err);
    process.exitCode = r.code;
  } catch (e) {
    if (e instanceof BridgeError) {
      process.stderr.write(`herdr-soho plugin: ${e.message}\n`);
      process.exitCode = e.code;
    } else throw e;
  }
}
