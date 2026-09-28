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

export function syncTaskReport(sd, agent) {
  const pointer = readTaskReportPointer(sd, agent);
  if (pointer === null) return null;
  const stable = pointer.task_report;
  let content;
  try {
    content = fs.readFileSync(pointer.current);
    if (content.length === 0) content = null;
  } catch { content = null; }
  if (content === null) {
    fs.rmSync(stable, { force: true });
    return null;
  }
  let same = false;
  try { same = fs.readFileSync(stable).equals(content); } catch { /* missing */ }
  if (!same) atomicWrite(stable, content);
  return stable;
}
