// Stable, regular-file snapshots for a task's authoritative report.
import fs from 'node:fs';
import path from 'node:path';
import { atomicWrite } from './platform.mjs';

export function taskReportPointerPath(sd, agent) {
  return path.join(sd, `task-report-${agent}.json`);
}

export function readTaskReportPointer(sd, agent) {
  try {
    const value = JSON.parse(fs.readFileSync(taskReportPointerPath(sd, agent), 'utf8'));
    if (value && value.version === 1 && typeof value.task_report === 'string'
      && typeof value.current === 'string' && Array.isArray(value.history)
      && value.history.every((p) => typeof p === 'string')) return value;
  } catch { /* absent or invalid pointer */ }
  return null;
}

export function writeTaskReportPointer(sd, agent, pointer) {
  atomicWrite(taskReportPointerPath(sd, agent), `${JSON.stringify(pointer)}\n`);
}

function readNonEmpty(file) {
  try {
    const content = fs.readFileSync(file);
    return content.length === 0 ? null : content;
  } catch { return null; }
}

// The stable copy follows the task's current report. A done report routed
// through $TMPDIR is mirrored by the wait into <state>/reports/ under the
// same name, and the system may reap the original: the mirror then stands
// in. With neither (a pending report or amendment), the copy is removed.
export function syncTaskReport(sd, agent) {
  const pointer = readTaskReportPointer(sd, agent);
  if (pointer === null) return null;
  const stable = pointer.task_report;
  let content = readNonEmpty(pointer.current);
  if (content === null) {
    const mirror = path.join(sd, 'reports', path.basename(pointer.current));
    if (path.resolve(mirror) !== path.resolve(pointer.current)) content = readNonEmpty(mirror);
  }
  if (content === null) {
    fs.rmSync(stable, { force: true });
    return null;
  }
  let same = false;
  try { same = fs.readFileSync(stable).equals(content); } catch { /* missing */ }
  if (!same) atomicWrite(stable, content);
  return stable;
}
