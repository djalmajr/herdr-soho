// `find [search words]... [--machine <label>]... [--all] [--json]`: list
// the panes of the local Herdr (and, when asked, other machines), filtered by the
// search words, with a copy-pasteable reference (`<machine>/<ws>:<pane>`)
// per pane. Read-only: one `herdr api snapshot` per machine (30 s each),
// plus `herdr machine list --json` under `--all`. Exit 0 with at least one
// entry, 1 with none (like grep), 2 on bad usage (unknown flag,
// `--machine` without a value), 4 when the local machine is unavailable —
// even when another machine delivered entries.
import { LOCAL_MACHINE, parseRef } from '../sessionref.mjs';
import { DieError } from '../config.mjs';
import { fetchSessions, machineList, matchEntries } from '../sessions.mjs';

const USAGE = 'usage: find [search words] [--machine <label>]... [--all] [--json]';

// TSV cell: null/undefined/'' becomes '-' (the column set of the default
// output: ref, name, kind, status, workspace, tab, cwd).
function tsvCell(v) {
  return v === null || v === undefined || v === '' ? '-' : String(v);
}

export function cmdFind(argv, ctx, env = process.env) {
  const words = [];
  const machines = [LOCAL_MACHINE];
  let all = false;
  let json = false;
  for (let i = 0; i < (argv ?? []).length; i += 1) {
    const a = String(argv[i]);
    if (a === '--all') all = true;
    else if (a === '--json') json = true;
    else if (a === '--machine') {
      const v = String(argv[i + 1] ?? '');
      if (v === '' || v.startsWith('--')) throw new DieError(USAGE, 2);
      i += 1;
      if (!machines.includes(v)) machines.push(v);
    } else if (a.startsWith('--')) throw new DieError(USAGE, 2);
    else words.push(a);
  }
  if (all) {
    const list = machineList({ env });
    if (!list.ok) {
      // The list cannot be read: keep the machines the flags already named
      // (local at least) and say why, instead of refusing the whole
      // lookup.
      process.stderr.write(`herdr-soho: find: machine list failed: ${list.cause}\n`);
    } else {
      for (const m of list.machines) {
        if (m.enabled && !machines.includes(m.label)) machines.push(m.label);
      }
    }
  }
  // A search that is another machine's reference queries that machine even
  // without --machine.
  const single = words.length === 1 ? parseRef(words[0]) : null;
  if (single && single.machine !== LOCAL_MACHINE && !machines.includes(single.machine)) {
    machines.push(single.machine);
  }

  const { entries, failures } = fetchSessions(machines, { env });
  for (const f of failures) {
    process.stderr.write(`herdr-soho: find: machine '${f.machine}' unavailable: ${f.cause}\n`);
  }
  const matched = matchEntries(entries, words);
  const lines = json
    ? matched.map((e) => JSON.stringify(e))
    : matched.map((e) => [e.ref, e.name, e.kind, e.status, e.workspace_label, e.tab_label, e.cwd].map(tsvCell).join('\t'));
  for (const line of lines) process.stdout.write(`${line}\n`);
  if (failures.some((f) => f.machine === LOCAL_MACHINE)) return 4;
  return matched.length > 0 ? 0 : 1;
}
