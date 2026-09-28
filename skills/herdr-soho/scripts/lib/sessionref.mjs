// Session references: the text form that names one Herdr pane on one
// machine, `[<machine>/]<pane id>` (e.g. `windows/w3:p1`, or `w12:p1` on
// the local server). `find` prints it, the plugin picker copies it, and
// `send` and the orchestrator read it back. `local` names the local Herdr
// server; any other machine is a label (or id) from `herdr machine list`,
// passed to `herdr --machine <label>`.

export const LOCAL_MACHINE = 'local';

const PANE_RE = /^w[0-9A-Za-z]+:p[0-9A-Za-z]+$/;
const MACHINE_RE = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

// parseRef <text>: { machine, paneId } for `[machine/]<pane id>`, or null
// when the text is not a reference (surrounding whitespace ignored).
export function parseRef(text) {
  if (typeof text !== 'string') return null;
  const t = text.trim();
  const slash = t.indexOf('/');
  const machine = slash === -1 ? LOCAL_MACHINE : t.slice(0, slash);
  const paneId = slash === -1 ? t : t.slice(slash + 1);
  if (!MACHINE_RE.test(machine) || !PANE_RE.test(paneId)) return null;
  return { machine, paneId };
}

// formatRef { machine, paneId }: the canonical text, always with the
// machine (`local/w12:p1`).
export function formatRef({ machine, paneId }) {
  return `${machine || LOCAL_MACHINE}/${paneId}`;
}

// herdrMachineArgs <machine>: the leading herdr arguments that target it
// (none for the local server).
export function herdrMachineArgs(machine) {
  return !machine || machine === LOCAL_MACHINE ? [] : ['--machine', machine];
}
