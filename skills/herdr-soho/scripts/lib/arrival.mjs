// Prompt-arrival helpers shared by the dispatch arrival check
// (lib/dispatch.mjs) and the wait's follow-up of a not-received dispatch
// (lib/wait.mjs): the exact start of the text dispatch sends to the worker
// and the input-box detection on the visible screen.
//
// The module exists because wait.mjs needs the helpers and dispatch.mjs
// already imports waitFor from wait.mjs; dispatch.mjs re-exports the names
// so its public surface is unchanged.

// The exact start of the text dispatch sends to the worker (the input-box
import fs from 'node:fs';
import path from 'node:path';

// marker below must track it).
export const PROMPT_MARKER = 'Read the file ';

// The last `n` non-empty lines of a screen (CRLF normalized).
export function lastNonEmptyLines(screen, n) {
  return String(screen ?? '')
    .replace(/\r\n/g, '\n')
    .split('\n')
    .filter((l) => l.trim() !== '')
    .slice(-n);
}

// True when one of the last 15 non-empty visible lines carries the start of
// the dispatched text: the prompt is sitting in the CLI's input box (the
// screen changed, the agent is idle, no Enter was ever sent).
export function promptSitsInInput(screen) {
  return lastNonEmptyLines(screen, 15).some((l) => l.includes(PROMPT_MARKER));
}

// The not-received marker holds "<epoch> <seq>" (the state_change_seq read
// when the dispatch ended not-received); an epoch-only marker is an older
// one and stays valid. Returns the seq field ('' when absent) of the
// marker's text.
export function markerSeq(markerText) {
  const parts = String(markerText ?? '').trim().split(/\s+/);
  return parts.length >= 2 && parts[1] !== '-' ? parts[1] : '';
}

// True when the marker holds a seq, the current seq is known, and the two
// differ: the agent changed state since the dispatch ended not-received, so
// a prompt echo in the last lines is no proof the input box is stuck.
export function markerSeqChanged(markerText, curSeq) {
  const seq = markerSeq(markerText);
  return seq !== '' && curSeq !== '' && String(curSeq) !== seq;
}

// A queued marker is "<epoch> <seq> <composed prompt path>". Preserve paths
// with spaces by treating everything after the first two fields as the path.
// Two-field markers predate this format; derive their path from last-report.
export function queuedPromptPath(markerText, sd, agent) {
  const match = String(markerText ?? '').trim().match(/^\S+\s+\S+\s+(.+)$/);
  if (match) return match[1];
  let report = '';
  try { report = fs.readFileSync(path.join(sd, `last-report-${agent}`), 'utf8').trim(); } catch {}
  if (!report) return '';
  const basename = path.basename(report);
  const statePrompt = path.join(sd, 'briefs', basename);
  if (fs.existsSync(statePrompt)) return statePrompt;
  const tmpPrompt = path.join(path.dirname(report), basename.replace(/\.md$/, '.brief.md'));
  return fs.existsSync(tmpPrompt) ? tmpPrompt : statePrompt;
}

export function markerHasPromptPath(markerText) {
  return /^\s*\S+\s+\S+\s+.+\s*$/.test(String(markerText ?? ''));
}

// A queued prompt is still in the input box only when its unique composed
// path is among the final three non-empty visible lines.
export function queuedPromptSitsInInput(markerText, screen, sd, agent) {
  const promptPath = queuedPromptPath(markerText, sd, agent);
  return promptPath !== '' && lastNonEmptyLines(screen, 3).some((line) => line.includes(promptPath));
}
