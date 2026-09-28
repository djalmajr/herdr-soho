#!/usr/bin/env node
// herdr-soho plugin — session picker (the pane opened by the `pick`
// action, slice S3).
//
// An overlay pane over the focused pane: it lists every Herdr pane —
// local server first, then, in parallel and appended as they arrive, each
// enabled machine from `herdr machine list --json` — with a
// type-to-filter. Each row shows
// `ref  name  kind  status  workspace/tab  cwd` (truncated to the
// terminal width). Enter copies the selection to the clipboard
// (clipboard.mjs), shows a `herdr-soho` notification and closes; Esc or
// Ctrl-C close without copying.
//
// Data source: the skill CLI `find --json` (one JSON line per pane; the
// S2 contract), e.g.
//   {"ref":"local/w12:p1","machine":"local","workspace_id":"w12",
//    "workspace_label":"appliance","tab_id":"w12:t1","tab_label":"1",
//    "pane_id":"w12:p1","name":"orchestrator-10","kind":"claude",
//    "status":"working","cwd":"/Users/…","title":"…","focused":false}
// where `name` and `kind` may be null.
//
// The terminal I/O (raw mode, redraws, spawns) is separated from the
// state/filter/render logic — createState, feedChunk, applyKey,
// filterEntries, render, copyPayload — so the tests drive the picker
// without a TTY.
import { spawn, spawnSync } from 'node:child_process';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { copyText } from './clipboard.mjs';
import { defaultCliScript } from './bridge.mjs';
import { cmdInvocation } from '../skills/herdr-soho/scripts/lib/platform.mjs';

// Ceiling for direct herdr calls (machine list, notification), mirroring
// the skill CLI's herdr ceiling (scripts/lib/herdr.mjs HERDR_TIMEOUT_MS)
// the way bridge.mjs does. The `find` loads get their own, higher
// ceiling (remote machines measure 5–20 s): a find that overruns it is
// killed and becomes that machine's status line; Esc/SIGTERM always
// abort the rest (main kills the load children).
export const HERDR_CALL_TIMEOUT_MS = 30_000;

// Per-find ceiling (per machine, local included).
export const FIND_TIMEOUT_MS = 60_000;

// Filtered over these fields, case-insensitively. `name` and `kind` may
// be null; the missing values simply do not match.
export const FILTER_FIELDS = ['ref', 'name', 'kind', 'status', 'workspace_label', 'tab_label', 'cwd', 'machine'];

// A find line must carry these (the rest may be null).
const REQUIRED_FIELDS = ['ref', 'machine', 'workspace_id', 'tab_id', 'pane_id'];

// One bad line rejects the whole load (a malformed external response is
// not silently filtered line by line).
export class FindError extends Error {
  constructor(message) {
    super(message);
    this.name = 'FindError';
  }
}

export function parseFindOutput(text, { label = 'local' } = {}) {
  const entries = [];
  for (const line of String(text ?? '').split('\n').map((l) => l.trim()).filter((l) => l !== '')) {
    let obj;
    try { obj = JSON.parse(line); } catch {
      throw new FindError(`${label}: invalid JSON line`);
    }
    if (typeof obj !== 'object' || obj === null || Array.isArray(obj)) {
      throw new FindError(`${label}: invalid JSON line (expected an object)`);
    }
    for (const f of REQUIRED_FIELDS) {
      if (typeof obj[f] !== 'string' || obj[f] === '') {
        throw new FindError(`${label}: line without '${f}'`);
      }
    }
    entries.push(obj);
  }
  return entries;
}

// `herdr machine list --json` → the labels of the enabled machines.
// The real output is a bare JSON array of
// { id, label, target, session, enabled, selected }; a {machines:[...]}
// wrapper is accepted. Only enabled === true machines are listed
// (a string such as "false" is not enabled). Any malformed item
// rejects the whole response.
export function parseMachineList(text) {
  let j;
  try { j = JSON.parse(String(text ?? '').trim()); } catch {
    throw new FindError('machine list: invalid JSON');
  }
  const list = Array.isArray(j) ? j
    : (j && typeof j === 'object' && Array.isArray(j.machines) ? j.machines : null);
  if (list === null) throw new FindError('machine list: no machine array');
  const out = [];
  for (const m of list) {
    if (typeof m !== 'object' || m === null) throw new FindError('machine list: invalid item');
    const label = typeof m.label === 'string' && m.label !== '' ? m.label
      : typeof m.id === 'string' && m.id !== '' ? m.id : null;
    if (label === null) throw new FindError('machine list: item without label');
    if (m.enabled === true) out.push(label);
  }
  return out;
}

// ---------- picker state, filter, render (pure; the tests drive this) ----------

export function createState() {
  return {
    entries: [],   // merged, arrival order (local, then remotes as they arrive)
    query: '',
    selected: 0,
    loading: 0,    // in-flight loads
    loadingLocal: false, // the local find is the load in flight ("carregando local…")
    failures: [],  // { label, cause } per failed load (status lines, not errors)
    lastEntry: null, // the entry Enter selected
    copied: null,  // the text Enter copied
    exit: null,    // null | 'copy' | 'esc'
    _esc: false,   // feedChunk: a lone ESC just arrived
    _csi: false,   // feedChunk: inside an ESC[ sequence
  };
}

// The lowercased search text of an entry: the fields joined with a
// space, so a word may span two adjacent fields. Null values contribute
// nothing.
export function entrySearchText(entry) {
  return FILTER_FIELDS.map((f) => {
    const v = entry[f];
    return typeof v === 'string' ? v : '';
  }).join(' ').toLowerCase();
}

// Case-insensitive word filter: every whitespace-separated word of the
// query must occur (as a substring) in the entry's search text. An
// empty query shows everything.
export function filterEntries(entries, query) {
  const terms = String(query ?? '').toLowerCase().split(/\s+/).filter((t) => t !== '');
  if (terms.length === 0) return entries;
  return entries.filter((entry) => {
    const text = entrySearchText(entry);
    return terms.every((t) => text.includes(t));
  });
}

function visible(state) {
  return filterEntries(state.entries, state.query);
}

function clampSelection(state) {
  state.selected = Math.min(state.selected, Math.max(0, visible(state).length - 1));
}

// Pane fields (and a failure cause, the stderr of a subprocess) are
// third-party text: a name, label or cwd may carry C0/C1 controls or
// ANSI sequences (CSI, OSC 52) from whatever produced it. One filter,
// applied to every field before drawing (entryLine, the failure line)
// and before building the copied text (copyPayload):
//   - C0 (U+0000–U+001F) is removed, except \t and \n, which become a
//     space (a field must stay on one line);
//   - C1 (U+0080–U+009F) and DEL are removed;
//   - complete ESC sequences are removed: a CSI (ESC[ … final byte)
//     and an OSC (ESC] … BEL or ST);
//   - a lone ESC that opens no sequence is a C0 control and goes away.
export function stripControls(text) {
  const s = String(text ?? '');
  let out = '';
  let i = 0;
  while (i < s.length) {
    const ch = s[i];
    if (ch === '\x1b') {
      const nxt = s[i + 1];
      if (nxt === '[') { // CSI: skip to the final byte (0x40–0x7e)
        i += 2;
        while (i < s.length && !(s.charCodeAt(i) >= 0x40 && s.charCodeAt(i) <= 0x7e)) i += 1;
        if (i < s.length) i += 1;
        continue;
      }
      if (nxt === ']') { // OSC: skip to BEL or ST
        i += 2;
        while (i < s.length && s[i] !== '\x07' && !(s[i] === '\x1b' && s[i + 1] === '\\')) i += 1;
        if (i < s.length) i += s[i] === '\x07' ? 1 : 2; // BEL or the ST
        continue;
      }
      i += 1; // a lone ESC: C0 control, removed
      continue;
    }
    const cp = s.charCodeAt(i);
    if (cp < 0x20) {
      if (cp === 0x09 || cp === 0x0a) out += ' '; // \t / \n → space
      i += 1;
      continue;
    }
    if (cp === 0x7f || (cp >= 0x80 && cp <= 0x9f)) { i += 1; continue; }
    out += ch;
    i += 1;
  }
  return out;
}

// The text Enter copies: `<ref> (<name>, <kind>, <status>) <cwd>`, with
// `-` where a field is null. The fields are third-party text, so each
// one goes through stripControls before the text is built. The first
// token is always the reference.
export function copyPayload(entry) {
  const d = (v) => {
    const s = stripControls(typeof v === 'string' ? v : '');
    return s !== '' ? s : '-';
  };
  return `${d(entry.ref)} (${d(entry.name)}, ${d(entry.kind)}, ${d(entry.status)}) ${d(entry.cwd)}`;
}

// A row: `ref  name  kind  status  workspace/tab  cwd`, truncated to
// `width` (two-space columns; the cwd absorbs the cut with a '…'). The
// columns are third-party text, so each one goes through stripControls
// before it is drawn.
export function entryLine(entry, width = 80) {
  const d = (v) => {
    const s = stripControls(typeof v === 'string' ? v : '');
    return s !== '' ? s : '-';
  };
  const cols = [d(entry.ref), d(entry.name), d(entry.kind), d(entry.status), `${d(entry.workspace_label)}/${d(entry.tab_label)}`, d(entry.cwd)];
  const sep = '  ';
  const lead = cols.slice(0, -1);
  const leadStr = lead.join(sep);
  const budget = Math.max(0, width - leadStr.length - (lead.length > 0 ? sep.length : 0));
  let last = cols[cols.length - 1];
  if (last.length > budget) last = budget > 0 ? `${last.slice(0, budget - 1)}…` : '';
  const line = lead.length > 0 ? `${leadStr}${sep}${last}` : last;
  return line.length > width ? line.slice(0, width) : line;
}

// The screen: the query line, the rows ('*' marks the selection), the
// "carregando local…" line while the local find is in flight, the
// "carregando windows…" line while the machine list or a remote load is
// in flight, one status line per failed load, and the count.
export function render(state, width = 80) {
  const list = visible(state);
  const lines = [`> ${state.query}`];
  list.forEach((e, i) => {
    lines.push(`${i === state.selected ? '*' : ' '} ${entryLine(e, Math.max(2, width - 2))}`);
  });
  if (list.length === 0 && state.entries.length > 0) {
    lines.push(`nenhum resultado para "${state.query}"`);
  }
  if (state.loading > 0) {
    lines.push(state.loadingLocal ? 'carregando local…' : 'carregando windows…');
  }
  for (const f of state.failures) {
    // label and cause are third-party text (machine label, stderr of a
    // subprocess): through stripControls before the line is drawn.
    lines.push(`máquina ${stripControls(f.label)}: falhou (${stripControls(f.cause)})`);
  }
  if (state.entries.length === 0 && state.loading === 0 && state.failures.length === 0) {
    lines.push('nenhum pane');
  }
  lines.push(`${list.length} pane${list.length === 1 ? '' : 's'}`);
  return lines.join('\n') + '\n';
}

// One key. Returns the exit action ('copy' | 'esc') or null.
export function applyKey(state, key) {
  switch (key) {
    case 'enter': {
      if (state.exit) return state.exit;
      const list = visible(state);
      if (list.length === 0) return null;
      const entry = list[Math.min(state.selected, list.length - 1)];
      state.lastEntry = entry;
      state.copied = copyPayload(entry);
      state.exit = 'copy';
      return 'copy';
    }
    case 'esc':
    case 'ctrl-c':
      if (state.exit === null) state.exit = 'esc';
      return state.exit;
    case 'backspace':
      state.query = state.query.slice(0, -1);
      clampSelection(state);
      return null;
    case 'up': {
      const n = visible(state).length;
      if (n > 0) state.selected = Math.max(0, state.selected - 1);
      return null;
    }
    case 'down': {
      const n = visible(state).length;
      if (n > 1) state.selected = Math.min(n - 1, state.selected + 1);
      return null;
    }
    default:
      // letters, digits, space and punctuation (any printable char):
      // appended to the query.
      if (typeof key === 'string' && key.length === 1) {
        const cp = key.codePointAt(0);
        if (cp >= 0x20 && cp !== 0x7f) {
          state.query += key;
          clampSelection(state);
          return null;
        }
      }
      return null;
  }
}

// Raw stdin chunk → keys. Handles the ESC sequences: a lone ESC is the
// exit key; ESC[A/ESC[B are up/down. Parameter bytes of a CSI sequence
// (e.g. ESC[1;5B, a modified arrow) are skipped, never leaked into the
// query. A pending ESC or ESC[ at a chunk boundary is resolved by the
// next chunk; the TTY layer (main) resolves a trailing lone ESC with a
// short timer (flushEsc), since a real Esc keypress arrives alone.
// Returns the exit action or null.
export function feedChunk(state, chunk) {
  for (const ch of String(chunk ?? '')) {
    if (state._csi) {
      const cp = ch.codePointAt(0);
      if (cp >= 0x40 && cp <= 0x7e) { // the final byte ends the sequence
        state._csi = false;
        if (ch === 'A') applyKey(state, 'up');
        else if (ch === 'B') applyKey(state, 'down');
      } // parameter bytes (0x30–0x3f, ';', ':') are skipped
      if (state.exit) return state.exit;
      continue;
    }
    if (state._esc) {
      state._esc = false;
      if (ch === '[') { state._csi = true; continue; }
      applyKey(state, 'esc');
      if (state.exit) return state.exit;
      continue;
    }
    if (ch === '\x1b') { state._esc = true; continue; }
    if (ch === '\r' || ch === '\n') { applyKey(state, 'enter'); if (state.exit) return state.exit; continue; }
    if (ch === '\x7f' || ch === '\x08') { applyKey(state, 'backspace'); continue; }
    if (ch === '\x03') { applyKey(state, 'ctrl-c'); if (state.exit) return state.exit; continue; }
    const cp = ch.codePointAt(0);
    if (cp >= 0x20 && cp !== 0x7f) applyKey(state, ch);
    // other control characters: ignored
  }
  return state.exit;
}

// Resolve a pending lone ESC as the Esc key (the TTY layer calls this
// when the next chunk never comes). Returns the exit action or null.
export function flushEsc(state) {
  if (!state._esc) return null;
  state._esc = false;
  applyKey(state, 'esc');
  return state.exit;
}

// How long a trailing lone ESC waits for a possible split sequence
// before it is resolved as the Esc key. Real terminals deliver a
// keypress in one chunk, so the window only covers pathological splits.
export const ESC_RESOLVE_MS = 50;

// ---------- data loading (CLI + herdr spawns, injectable) ----------

function runCliJsonLines(nodeBin, cliScript, args, { env, children, timeoutMs } = {}) {
  return new Promise((resolve, reject) => {
    const child = spawn(nodeBin, [cliScript, ...args], { env, stdio: ['ignore', 'pipe', 'pipe'] });
    const stdout = [];
    const stderr = [];
    let timedOut = false;
    const timer = timeoutMs ? setTimeout(() => {
      timedOut = true;
      try { child.kill(); } catch { /* already gone */ }
    }, timeoutMs) : null;
    if (children) { children.add(child); child.once('close', () => children.delete(child)); }
    child.stdout.on('data', (d) => stdout.push(d));
    child.stderr.on('data', (d) => stderr.push(d));
    child.on('error', (e) => {
      if (timer) clearTimeout(timer);
      reject(new FindError(`spawn failed: ${e.message}`));
    });
    child.on('close', (code) => {
      if (timer) clearTimeout(timer);
      // Release the pipe handles: a closed-but-referenced child's pipe
      // sockets keep the event loop alive until they are destroyed.
      try { child.stdout.destroy(); } catch { /* already gone */ }
      try { child.stderr.destroy(); } catch { /* already gone */ }
      if (timedOut) { reject(new FindError(`find timed out after ${timeoutMs / 1000}s`)); return; }
      if (code === 0) resolve(Buffer.concat(stdout).toString('utf8'));
      else {
        const err = code === null ? 'killed'
          : `exit ${code}${stderr.length ? `: ${Buffer.concat(stderr).toString('utf8').trim().slice(0, 120)}` : ''}`;
        reject(new FindError(err));
      }
    });
  });
}

function runHerdr(herdrBin, args, { env, platform, children, timeoutMs }) {
  const inv = platform === 'win32' && /\.(bat|cmd)$/i.test(herdrBin)
    ? cmdInvocation(herdrBin, args, env)
    : { command: herdrBin, args, windowsVerbatimArguments: false };
  return new Promise((resolve, reject) => {
    let child;
    try {
      child = spawn(inv.command, inv.args, {
        env,
        stdio: ['ignore', 'pipe', 'pipe'],
        windowsVerbatimArguments: inv.windowsVerbatimArguments,
      });
    } catch (e) {
      reject(new FindError(`spawn failed: ${e.message}`));
      return;
    }
    const stdout = [];
    const stderr = [];
    const timer = timeoutMs ? setTimeout(() => {
      try { child.kill(); } catch { /* already gone */ }
    }, timeoutMs) : null;
    if (children) { children.add(child); child.once('close', () => children.delete(child)); }
    child.stdout.on('data', (d) => stdout.push(d));
    child.stderr.on('data', (d) => stderr.push(d));
    child.on('error', (e) => {
      if (timer) clearTimeout(timer);
      reject(new FindError(e.code === 'ENOENT' ? 'herdr not found' : `spawn failed: ${e.message}`));
    });
    child.on('close', (code) => {
      if (timer) clearTimeout(timer);
      // Release the pipe handles (see runCliJsonLines).
      try { child.stdout.destroy(); } catch { /* already gone */ }
      try { child.stderr.destroy(); } catch { /* already gone */ }
      if (code === 0) resolve(Buffer.concat(stdout).toString('utf8'));
      else reject(new FindError(code === null ? `timed out after ${timeoutMs / 1000}s` : `exit ${code}${stderr.length ? `: ${Buffer.concat(stderr).toString('utf8').trim().slice(0, 120)}` : ''}`));
    });
  });
}

// Load the entries into `state`: local `find --json` first, then in
// parallel one `find --json --machine <label>` per enabled machine.
// Failures become status lines ({ label, cause }), never errors. Returns
// the final { failures } (state is mutated in place; onChange — called
// after every change — drives the redraw).
export async function loadEntries(state, opts = {}) {
  const env = opts.env ?? process.env;
  const nodeBin = opts.nodeBin ?? process.execPath;
  const cliScript = opts.cliScript ?? defaultCliScript();
  const platform = opts.platform ?? process.platform;
  const herdrBin = opts.herdrBin ?? (typeof env.HERDR_BIN_PATH === 'string' && env.HERDR_BIN_PATH !== '' ? env.HERDR_BIN_PATH : 'herdr');
  const onChange = opts.onChange ?? (() => {});

  const find = (machine) => runCliJsonLines(nodeBin, cliScript,
    machine === null ? ['find', '--json'] : ['find', '--json', '--machine', machine],
    { env, children: opts.children, timeoutMs: opts.findTimeoutMs ?? FIND_TIMEOUT_MS });

  const fail = (label, e) => {
    state.failures.push({ label, cause: e && e.message ? e.message : String(e) });
    state.loading = Math.max(0, state.loading - 1);
    onChange();
  };

  // The local find is marked in flight before it starts: the first frame
  // already says "carregando local…", not "nenhum pane".
  state.loading = 1;
  state.loadingLocal = true;
  onChange();
  let local;
  try {
    local = parseFindOutput(await find(null), { label: 'local' });
  } catch (e) {
    state.loadingLocal = false;
    fail('local', e instanceof FindError ? e : new FindError(String(e)));
    return { failures: state.failures };
  }
  state.loading = 0; // the local find is in
  state.loadingLocal = false;
  state.entries.push(...local);
  onChange();

  let machines = [];
  state.loading = 1; // the machine list
  try {
    machines = parseMachineList(await runHerdr(herdrBin, ['machine', 'list', '--json'], { env, platform, children: opts.children, timeoutMs: HERDR_CALL_TIMEOUT_MS }));
  } catch (e) {
    state.loading = 0;
    state.failures.push({ label: 'máquinas', cause: e && e.message ? e.message : String(e) });
    onChange();
    return { failures: state.failures };
  }
  state.loading = 0; // the machine list is in
  if (machines.length === 0) {
    onChange();
    return { failures: state.failures };
  }
  state.loading = machines.length; // the remote finds, in parallel
  onChange();
  await Promise.all(machines.map(async (m) => {
    try {
      state.entries.push(...parseFindOutput(await find(m), { label: m }));
    } catch (e) {
      state.failures.push({ label: m, cause: e && e.message ? e.message : String(e) });
    } finally {
      state.loading = Math.max(0, state.loading - 1);
      onChange();
    }
  }));
  return { failures: state.failures };
}

// After a copy: `herdr notification show herdr-soho --body "copied <ref>"
// --sound none`. A notification failure is not an error. The ref is
// third-party text: stripControls before mounting the body.
export function notifyCopied(herdrBin, ref, { env = process.env, platform = process.platform, timeoutMs = HERDR_CALL_TIMEOUT_MS } = {}) {
  const bin = typeof herdrBin === 'string' && herdrBin !== '' ? herdrBin : (typeof env.HERDR_BIN_PATH === 'string' && env.HERDR_BIN_PATH !== '' ? env.HERDR_BIN_PATH : 'herdr');
  const args = ['notification', 'show', 'herdr-soho', '--body', `copied ${stripControls(ref)}`, '--sound', 'none'];
  const inv = platform === 'win32' && /\.(bat|cmd)$/i.test(bin)
    ? cmdInvocation(bin, args, env)
    : { command: bin, args, windowsVerbatimArguments: false };
  try {
    const r = spawnSync(inv.command, inv.args, {
      env,
      encoding: 'utf8',
      timeout: timeoutMs,
      killSignal: 'SIGTERM',
      stdio: ['ignore', 'pipe', 'pipe'],
      windowsVerbatimArguments: inv.windowsVerbatimArguments,
    });
    return { ok: r.status === 0, skipped: false };
  } catch {
    return { ok: false, skipped: false };
  }
}

// ---------- the terminal I/O ----------

// The interactive loop. opts: env, nodeBin, cliScript, herdrBin,
// stdin/stdout (defaults: the process streams), width (fallback when
// stdout.columns is absent), children (optional Set collecting the load
// children). Resolves { result } with result = { action, copied,
// clipboard, notification } — it never calls process.exit; the entry
// point sets the exit code.
export async function main(opts = {}) {
  const env = opts.env ?? process.env;
  const stdin = opts.stdin ?? process.stdin;
  const stdout = opts.stdout ?? process.stdout;
  const fallbackWidth = typeof opts.width === 'number' && opts.width > 0 ? opts.width : 80;
  const state = createState();
  const children = opts.children ?? new Set();
  const herdrBin = opts.herdrBin ?? herdrBinOf(env);
  const width = () => (typeof stdout.columns === 'number' && stdout.columns > 0 ? stdout.columns : fallbackWidth);

  const redraw = () => {
    if (state.exit) return;
    const lines = render(state, width()).split('\n');
    let out = '\x1b[H';
    for (let i = 0; i < lines.length - 1; i++) out += `${lines[i]}\x1b[K\n`;
    // Erase the rest of the screen: a frame smaller than the previous
    // one must not leave the old lines behind (old count, "carregando",
    // an old selection row).
    out += '\x1b[J';
    stdout.write(out);
  };

  let done = null;
  let escTimer = null;
  // A SIGTERM/SIGHUP (e.g. Herdr closing the pane) goes through the same
  // finish as Esc/Ctrl-C: raw mode off, the find children killed, no
  // copy — and the process only leaves after main resolves.
  const onSignal = () => finish('esc');
  const finish = (action) => {
    if (done) return;
    done = { action };
    if (escTimer) { clearTimeout(escTimer); escTimer = null; }
    process.removeListener('SIGTERM', onSignal);
    process.removeListener('SIGHUP', onSignal);
    stdin.off('data', onData);
    stdin.off('end', onEnd);
    if (stdin.isTTY) { try { stdin.setRawMode(false); } catch { /* not a tty */ } }
    // Release the stdin handle: a pty slave keeps the event loop alive
    // while the handle is open, so the picker must destroy it or the
    // process (and the pane) lingers after the exit.
    try { stdin.destroy(); } catch { /* already gone */ }
    for (const c of children) { try { c.kill(); } catch { /* already gone */ } }
    if (action === 'copy' && state.copied) {
      // Copy (native tool or OSC 52 to this terminal, which Herdr
      // forwards to the user's terminal), then the notification.
      done.clipboard = copyText(state.copied, { env, out: stdout });
      done.notification = state.lastEntry
        ? notifyCopied(herdrBin, state.lastEntry.ref, { env })
        : { ok: false, skipped: true };
    }
  };
  // UTF-8 across chunks: a multi-byte character cut at a chunk boundary
  // (e.g. the c3 of é) must not become U+FFFD — the decoder keeps the
  // partial bytes for the next chunk. TextDecoder with stream: true is
  // the incremental decoder (the stdlib StringDecoder is gone since
  // Node 26).
  const decoder = new TextDecoder('utf-8');
  const onData = (buf) => {
    const text = decoder.decode(buf, { stream: true });
    if (escTimer) { clearTimeout(escTimer); escTimer = null; }
    const action = feedChunk(state, text);
    if (action) { finish(action); return; }
    if (state._esc) {
      // A lone ESC may be the start of a sequence split across chunks;
      // give the next chunk a short window, then resolve it as the Esc
      // key (a real Esc keypress arrives alone and nothing more comes).
      escTimer = setTimeout(() => {
        escTimer = null;
        const a2 = flushEsc(state);
        if (a2) finish(a2);
        else redraw();
      }, ESC_RESOLVE_MS);
    } else {
      redraw();
    }
  };
  const onEnd = () => { finish('esc'); }; // piped stdin ending: close without copying
  if (stdin.isTTY) {
    // Raw mode: keystrokes (letters, Esc, Ctrl-C) arrive as they are
    // typed, without echo or line buffering; the query line is the echo.
    try { stdin.setRawMode(true); } catch { /* not a real tty */ }
  }
  stdin.on('data', onData);
  if (typeof stdin.on === 'function') stdin.on('end', onEnd);
  process.once('SIGTERM', onSignal);
  process.once('SIGHUP', onSignal);
  // The local find is in flight before the first frame (loadEntries also
  // marks it; the two marks are the same value).
  state.loading = 1;
  state.loadingLocal = true;
  redraw();

  // Local first, remotes appended as they arrive; the user can already
  // type and select while the remotes load.
  const load = loadEntries(state, {
    env,
    nodeBin: opts.nodeBin,
    cliScript: opts.cliScript,
    herdrBin,
    platform: opts.platform,
    children,
    onChange: redraw,
  });

  await new Promise((resolve) => {
    const check = () => { if (done) resolve(done); else setTimeout(check, 5); };
    check();
  });
  await load.catch(() => { /* load failures are status lines, not errors */ });
  const result = {
    action: done.action,
    copied: state.copied,
    clipboard: done.clipboard ?? null,
    notification: done.notification ?? null,
    state,
  };
  return result;
}

function herdrBinOf(env) {
  return typeof env.HERDR_BIN_PATH === 'string' && env.HERDR_BIN_PATH !== '' ? env.HERDR_BIN_PATH : 'herdr';
}

// Entry: Herdr runs `node picker.mjs` with the plugin pane env.
if (process.argv[1] && path.resolve(process.argv[1]) === path.resolve(fileURLToPath(import.meta.url))) {
  main().then((r) => {
    process.exitCode = r.action === 'copy' || r.action === 'esc' ? 0 : 1;
  }).catch((e) => {
    process.stderr.write(`herdr-soho picker: ${e && e.message ? e.message : e}\n`);
    process.exitCode = 1;
  });
}
