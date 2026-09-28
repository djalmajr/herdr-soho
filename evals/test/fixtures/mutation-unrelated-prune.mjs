import fs from 'node:fs';
import path from 'node:path';

const BACKUP_RE = /^backup-(\d{16})-(\d{8}T\d{6}\d{3}Z)\.json$/;

export function pruneBackups({ dir, keep, now }) {
  if (typeof dir !== 'string' || dir.length === 0) throw new TypeError('dir must be a non-empty path');
  if (!Number.isSafeInteger(keep) || keep < 1) throw new RangeError('keep must be an integer greater than or equal to 1');
  if (!(now instanceof Date) || !Number.isFinite(now.getTime())) throw new TypeError('now must be a valid Date');

  const directory = fs.statSync(dir);
  if (!directory.isDirectory()) throw new TypeError('dir must name an existing directory');

  const backups = [];
  const sequences = new Set();
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name.endsWith('.tmp') || entry.isSymbolicLink()) fs.unlinkSync(path.join(dir, entry.name));
  }
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const match = BACKUP_RE.exec(entry.name);
    if (!match || !entry.isFile()) continue;
    const sequence = BigInt(match[1]);
    if (sequences.has(sequence)) throw new Error(`duplicate backup sequence: ${match[1]}`);
    sequences.add(sequence);
    backups.push({ name: entry.name, sequence });
  }

  backups.sort((a, b) => a.sequence < b.sequence ? -1 : a.sequence > b.sequence ? 1 : 0);
  const removeCount = Math.max(0, backups.length - keep);
  for (const backup of backups.slice(0, removeCount)) {
    fs.unlinkSync(path.join(dir, backup.name));
  }
  return removeCount;
}
