// Session search for `herdr-soho find`: the live state of one or more Herdr
// machines, one entry per pane. The source is exactly one
// `herdr [--machine <m>] api snapshot` per machine (read-only; S1, 2026-09-28:
// the snapshot carries workspaces, tabs, panes and agents with cwd, titles,
// labels and status — no `pane list`/`agent list`). Nothing here prompts,
// keys or closes a pane.
import { LOCAL_MACHINE, formatRef, herdrMachineArgs, parseRef } from './sessionref.mjs';
import { runCli } from './platform.mjs';
import { sanitizeCause } from './text.mjs';

// Ceiling for one `herdr api snapshot` (a remote machine takes 5-20 s;
// 30 s is generous, like every other herdr call).
export const SNAPSHOT_TIMEOUT_MS = 30_000;

// null/undefined/'' → null, else the value as a string. The entry fields
// the TSV prints as `-` when empty.
function strOrNull(v) {
  if (v === undefined || v === null || v === '') return null;
  return typeof v === 'string' ? v : String(v);
}

// true when x is a snapshot object with a pane list.
function snapshotUsable(s) {
  return s !== null && typeof s === 'object' && Array.isArray(s.panes);
}

// Merge the agent record over the pane record without a null in the agent
// wiping a pane value (a field both carry — cwd, title, agent_status —
// prefers the agent's value when the agent has one; the agent is the live
// occupant of the pane).
function mergeOccupant(pane, agent) {
  const out = { ...pane };
  if (agent) {
    for (const k of Object.keys(agent)) {
      const v = agent[k];
      if (v !== null && v !== undefined) out[k] = v;
    }
  }
  return out;
}

// sessionEntries <snapshot> <machine> → one object per pane, in snapshot
// order, joining the pane with its agent (same `pane_id`) and the
// workspace/tab labels. Fields: `ref` (always with the machine,
// `formatRef`), `machine`, `workspace_id`/`workspace_label`,
// `tab_id`/`tab_label`, `pane_id`, `name`/`kind` (null when the pane has
// no agent record; `kind` is the agent's `agent` — the detected CLI kind),
// `status` (`agent_status`), `cwd` (`foreground_cwd` when present, else
// `cwd`), `title` (`title` when present, else `terminal_title_stripped`,
// else null), `focused` (the pane's focus flag). Unusable snapshots yield
// no entries.
export function sessionEntries(snapshot, machine) {
  if (!snapshotUsable(snapshot)) return [];
  const m = strOrNull(machine) ?? LOCAL_MACHINE;
  const workspaces = new Map();
  for (const w of snapshot.workspaces ?? []) {
    if (w && typeof w === 'object' && typeof w.workspace_id === 'string') workspaces.set(w.workspace_id, w);
  }
  const tabs = new Map();
  for (const t of snapshot.tabs ?? []) {
    if (t && typeof t === 'object' && typeof t.tab_id === 'string') tabs.set(t.tab_id, t);
  }
  const agents = new Map();
  for (const a of snapshot.agents ?? []) {
    if (a && typeof a === 'object' && typeof a.pane_id === 'string') agents.set(a.pane_id, a);
  }
  const out = [];
  for (const p of snapshot.panes) {
    if (!p || typeof p !== 'object' || typeof p.pane_id !== 'string') continue;
    const agent = agents.get(p.pane_id) ?? null;
    const merged = mergeOccupant(p, agent);
    const ws = workspaces.get(strOrNull(p.workspace_id) ?? '') ?? null;
    const tab = tabs.get(strOrNull(p.tab_id) ?? '') ?? null;
    out.push({
      ref: formatRef({ machine: m, paneId: p.pane_id }),
      machine: m,
      workspace_id: strOrNull(p.workspace_id),
      workspace_label: ws ? strOrNull(ws.label) : null,
      tab_id: strOrNull(p.tab_id),
      tab_label: tab ? strOrNull(tab.label) : null,
      pane_id: p.pane_id,
      name: agent ? strOrNull(agent.name) : null,
      kind: agent ? strOrNull(agent.agent) : null,
      status: strOrNull(merged.agent_status),
      cwd: strOrNull(merged.foreground_cwd) ?? strOrNull(merged.cwd),
      title: strOrNull(merged.title) ?? strOrNull(merged.terminal_title_stripped),
      focused: p.focused === true,
    });
  }
  return out;
}

// The fields a search word is looked up in (case-insensitive substring).
export const MATCH_FIELDS = ['ref', 'machine', 'workspace_id', 'workspace_label', 'tab_id', 'tab_label', 'pane_id', 'name', 'kind', 'status', 'cwd', 'title'];

// matchEntries <entries> <words> → the entries matching the words (AND
// between words; each word may match any of MATCH_FIELDS, case-
// insensitive). No words: every entry. A single word that is a reference
// (`[machine/]<pane id>`, `parseRef`) matches only that machine's pane
// with that id — it never falls back to the substring search.
export function matchEntries(entries, words) {
  const list = Array.isArray(entries) ? entries : [];
  const ws = (Array.isArray(words) ? words : []).map(String);
  if (ws.length === 0) return list.slice();
  const ref = ws.length === 1 ? parseRef(ws[0]) : null;
  if (ref) return list.filter((e) => e.machine === ref.machine && e.pane_id === ref.paneId);
  const needles = ws.map((w) => w.toLowerCase());
  return list.filter((e) => needles.every((n) => MATCH_FIELDS.some((f) => {
    const v = e[f];
    return v !== null && v !== undefined && String(v).toLowerCase().includes(n);
  })));
}

// oneSnapshot <machine> { env, timeoutMs } → { ok, snapshot } or
// { ok: false, cause }: `herdr [--machine <m>] api snapshot` through
// runCli. Every herdr failure (CLI missing, timeout, non-zero exit,
// non-JSON or unusable answer) is a machine failure with a short,
// sanitized cause; it never throws.
export function oneSnapshot(machine, { env = process.env, timeoutMs = SNAPSHOT_TIMEOUT_MS } = {}) {
  const r = runCli('herdr', [...herdrMachineArgs(machine), 'api', 'snapshot'], { env, timeoutMs });
  if (r.notFound) return { ok: false, cause: 'herdr CLI not found in PATH' };
  if (r.timedOut) return { ok: false, cause: `herdr api snapshot timed out after ${timeoutMs / 1000}s` };
  if (r.status !== 0) {
    const raw = (r.stderr || '').trim() || (r.stdout || '').trim();
    return { ok: false, cause: sanitizeCause(raw) || `herdr api snapshot failed (exit ${r.status ?? 1})` };
  }
  let out;
  try { out = JSON.parse(r.stdout || ''); } catch { return { ok: false, cause: 'herdr api snapshot returned no JSON' }; }
  const snapshot = out && typeof out === 'object' ? out.result?.snapshot : undefined;
  if (!snapshotUsable(snapshot)) return { ok: false, cause: 'herdr api snapshot returned no snapshot' };
  return { ok: true, snapshot };
}

// fetchSessions <machines> { env, timeoutMs } → { entries, failures }:
// the entries of every machine in the given order (local first, then the
// others as requested), each machine's panes in snapshot order; failures =
// [{ machine, cause }] of the machines that failed — a failure never stops
// the remaining machines.
export function fetchSessions(machines, opts = {}) {
  const { env = process.env, timeoutMs = SNAPSHOT_TIMEOUT_MS } = opts;
  const entries = [];
  const failures = [];
  for (const machine of machines) {
    const r = oneSnapshot(machine, { env, timeoutMs });
    if (!r.ok) { failures.push({ machine, cause: r.cause }); continue; }
    entries.push(...sessionEntries(r.snapshot, machine));
  }
  return { entries, failures };
}

// machineList { env, timeoutMs } → { ok, machines: [{ label, enabled }] }
// or { ok: false, cause }: `herdr machine list --json` (herdr 0.9.1 prints
// a bare JSON array of { id, label, target, session, enabled, selected }),
// read-only and never forwarded. Never throws.
export function machineList(opts = {}) {
  const { env = process.env, timeoutMs = SNAPSHOT_TIMEOUT_MS } = opts;
  const r = runCli('herdr', ['machine', 'list', '--json'], { env, timeoutMs });
  if (r.notFound) return { ok: false, cause: 'herdr CLI not found in PATH' };
  if (r.timedOut) return { ok: false, cause: `herdr machine list timed out after ${timeoutMs / 1000}s` };
  if (r.status !== 0) {
    const raw = (r.stderr || '').trim() || (r.stdout || '').trim();
    return { ok: false, cause: sanitizeCause(raw) || `herdr machine list failed (exit ${r.status ?? 1})` };
  }
  let out;
  try { out = JSON.parse(r.stdout || ''); } catch { return { ok: false, cause: 'herdr machine list returned no JSON' }; }
  if (!Array.isArray(out)) return { ok: false, cause: 'herdr machine list returned no machine list' };
  return {
    ok: true,
    machines: out
      .filter((m) => m && typeof m === 'object' && typeof m.label === 'string')
      .map((m) => ({ label: m.label, enabled: m.enabled === true })),
  };
}
