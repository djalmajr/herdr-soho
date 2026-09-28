import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { pathToFileURL } from 'node:url';

const target = process.env.EVAL_TARGET;
if (!target) throw new Error('EVAL_TARGET is required');
const { pruneBackups } = await import(`${pathToFileURL(target).href}?probe=${Date.now()}`);

async function sandbox(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'prune-probe-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}
function backup(dir, sequence, timestamp = '20260101T000000000Z') {
  const name = `backup-${String(sequence).padStart(16, '0')}-${timestamp}.json`;
  fs.writeFileSync(path.join(dir, name), `opaque ${sequence}`);
  return name;
}
function names(dir) { return fs.readdirSync(dir).sort(); }
const now = new Date('2026-09-27T21:19:50.000Z');

test('keeps highest sequences, not latest timestamp or mtime', async (t) => {
  // Mutation captured: sorting by timestamp or mtime deletes the wrong file.
  const dir = await sandbox(t);
  const old = backup(dir, 1, '20261231T235959999Z');
  const middle = backup(dir, 2, '20260101T000000000Z');
  const newest = backup(dir, 3, '20260101T000001000Z');
  fs.utimesSync(path.join(dir, newest), new Date(0), new Date(0));
  assert.equal(await pruneBackups({ dir, keep: 2, now }), 1);
  assert.deepEqual(names(dir), [middle, newest].sort());
});

test('a crash after deleting older files can be retried without losing the newest', async (t) => {
  // Mutation captured: retry after a partially completed prune removes a retained backup.
  const dir = await sandbox(t);
  const retained = [backup(dir, 2), backup(dir, 3)];
  backup(dir, 1);
  fs.unlinkSync(path.join(dir, 'backup-0000000000000001-20260101T000000000Z.json'));
  assert.equal(await pruneBackups({ dir, keep: 2, now }), 0);
  assert.deepEqual(names(dir), retained.sort());
});

test('published backup survives the publish-before-prune boundary and retry converges', async (t) => {
  // Mutation captured: pruning to keep=1 removes the newly published highest sequence.
  const dir = await sandbox(t);
  backup(dir, 1);
  const published = backup(dir, 2);
  assert.equal(await pruneBackups({ dir, keep: 1, now }), 1);
  assert.deepEqual(names(dir), [published]);
  assert.equal(await pruneBackups({ dir, keep: 1, now }), 0);
  assert.deepEqual(names(dir), [published]);
});

test('clock rollback cannot change sequence ordering', async (t) => {
  // Mutation captured: sorting by now-relative age prunes a needed high sequence.
  const dir = await sandbox(t);
  backup(dir, 7, '20261231T235959999Z');
  const latest = backup(dir, 8, '20200101T000000000Z');
  assert.equal(await pruneBackups({ dir, keep: 1, now: new Date('2020-01-01T00:00:00Z') }), 1);
  assert.deepEqual(names(dir), [latest]);
});

test('ignores malformed names, temporary files, directories, and symlinks', async (t) => {
  // Mutation captured: broad matching or recursive deletion removes unrelated entries.
  const dir = await sandbox(t);
  const older = backup(dir, 1);
  const latest = backup(dir, 2);
  fs.writeFileSync(path.join(dir, 'backup-final.json'), 'leave');
  fs.writeFileSync(path.join(dir, 'backup-0000000000000003-20260101T000000000Z.json.tmp'), 'leave');
  fs.mkdirSync(path.join(dir, 'backup-0000000000000004-20260101T000000000Z.json'));
  const outside = path.join(dir, 'outside');
  fs.writeFileSync(outside, 'leave');
  fs.symlinkSync(outside, path.join(dir, 'backup-0000000000000005-20260101T000000000Z.json'));
  assert.equal(await pruneBackups({ dir, keep: 1, now }), 1);
  assert.equal(fs.existsSync(path.join(dir, older)), false);
  assert.equal(fs.existsSync(path.join(dir, latest)), true);
  assert.equal(fs.readFileSync(outside, 'utf8'), 'leave');
  assert.equal(fs.existsSync(path.join(dir, 'backup-final.json')), true);
  assert.equal(fs.existsSync(path.join(dir, 'backup-0000000000000004-20260101T000000000Z.json')), true);
});

test('invalid arguments and duplicate sequence fail before mutation', async (t) => {
  // Mutation captured: deleting before validation or guessing a duplicate order loses data.
  const dir = await sandbox(t);
  const first = backup(dir, 1);
  const duplicate = 'backup-0000000000000001-20260102T000000000Z.json';
  fs.writeFileSync(path.join(dir, duplicate), 'duplicate');
  for (const args of [
    { dir, keep: 0, now },
    { dir, keep: 1.5, now },
    { dir, keep: 1, now: new Date(Number.NaN) },
    { dir: path.join(dir, 'missing'), keep: 1, now },
  ]) {
    await assert.rejects(async () => pruneBackups(args));
    assert.deepEqual(names(dir), [first, duplicate].sort());
  }
});
