import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { randomUUID } from 'node:crypto';

const testDir = path.dirname(fileURLToPath(import.meta.url));
const root = path.resolve(testDir, '../..');
const fixture = path.join(root, 'evals/fixtures/implementer-prune');
const prepareScript = path.join(root, 'evals/prepare.mjs');
const probeScript = path.join(root, 'evals/run-probes.mjs');
const reference = path.join(testDir, 'fixtures/reference-prune.mjs');
const duplicateMutation = path.join(testDir, 'fixtures/mutation-duplicate-prune.mjs');
const unrelatedMutation = path.join(testDir, 'fixtures/mutation-unrelated-prune.mjs');

function run(args) {
  return spawnSync(process.execPath, args, {
    cwd: root,
    encoding: 'utf8',
    timeout: 15_000,
    maxBuffer: 10 * 1024 * 1024,
  });
}

function tempRoot(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-eval-kit-test-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

function prepare(t, source = fixture, destinationName = 'worker-copy') {
  const parent = tempRoot(t);
  const destination = path.join(parent, destinationName);
  const result = run([prepareScript, source, destination]);
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 0, result.stderr);
  return { parent, destination };
}

function measure(source, destination) {
  const result = run([probeScript, source, destination]);
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 0, result.stderr);
  return JSON.parse(result.stdout);
}

test('prepare refuses a destination inside the repository', () => {
  // Mutation captured: dropping the repository boundary check allows worker state into the checkout.
  const destination = path.join(root, 'evals', 'test', `.inside-${randomUUID()}`);
  const result = run([prepareScript, fixture, destination]);
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 2);
  assert.match(result.stderr, /outside the repository/);
  assert.equal(fs.existsSync(destination), false);
});

test('prepare omits hidden probes and the reviewer answer key', (t) => {
  // Mutation captured: copying the fixture tree directly exposes evaluator-only files to the worker.
  const { destination } = prepare(t);
  const entries = fs.readdirSync(destination);
  assert.equal(entries.includes('probes'), false);
  assert.equal(entries.includes('ANSWER-KEY.md'), false);
  assert.deepEqual(entries.sort(), ['.eval-run.json', 'brief.md', 'src'].sort());
  const metadata = JSON.parse(fs.readFileSync(path.join(destination, '.eval-run.json'), 'utf8'));
  assert.deepEqual(Object.keys(metadata).sort(), ['fixture', 'manifest_ok', 'prepared_at']);
  assert.equal(metadata.fixture, 'implementer-prune');
  assert.equal(metadata.manifest_ok, true);
  assert.equal(Number.isNaN(Date.parse(metadata.prepared_at)), false);

  const reviewerFixture = path.join(root, 'evals/fixtures/reviewer-seeded');
  const { destination: reviewerCopy } = prepare(t, reviewerFixture);
  assert.equal(fs.existsSync(path.join(reviewerCopy, 'ANSWER-KEY.md')), false);
  assert.equal(fs.existsSync(path.join(reviewerCopy, 'base', 'transfer.mjs')), true);
});

test('prepare rejects a fixture whose manifest was adulterated', (t) => {
  // Mutation captured: skipping digest verification accepts altered evaluation input as frozen.
  const parent = tempRoot(t);
  const altered = path.join(parent, 'altered-fixture');
  fs.cpSync(fixture, altered, { recursive: true });
  const manifestPath = path.join(altered, 'MANIFEST.sha256');
  const manifest = fs.readFileSync(manifestPath, 'utf8');
  fs.writeFileSync(manifestPath, manifest.replace(/^[a-f0-9]{64}/, '0'.repeat(64)));
  const destination = path.join(parent, 'worker-copy');
  const result = run([prepareScript, altered, destination]);
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 2);
  assert.match(result.stderr, /manifest checksum mismatch/);
  assert.equal(fs.existsSync(destination), false);
});

test('run-probes passes every hidden probe on the reference implementation', (t) => {
  // Mutation captured: a reference implementation that drops crash, retry, or clock handling fails a probe.
  const { destination } = prepare(t);
  fs.copyFileSync(reference, path.join(destination, 'src', 'prune.mjs'));
  const result = measure(fixture, destination);
  assert.equal(result.fixture, 'implementer-prune');
  assert.equal(result.probes.passed, result.probes.total);
  assert.ok(result.probes.total > 0);
  assert.deepEqual(result.probes.failed, []);
  assert.deepEqual(result.scope_violations, []);
  assert.equal(result.manifest_ok, true);
});

test('run-probes rejects duplicate backups before pruning with valid arguments', (t) => {
  // Mutation captured: removing duplicate-sequence rejection lets a valid prune delete one duplicate.
  const { destination } = prepare(t);
  fs.copyFileSync(duplicateMutation, path.join(destination, 'src', 'prune.mjs'));
  const result = measure(fixture, destination);
  assert.ok(result.probes.passed < result.probes.total);
  assert.ok(result.probes.failed.includes('invalid arguments and duplicate sequence fail before mutation'));
});

test('run-probes rejects pruning temporary files and symlinks', (t) => {
  // Mutation captured: deleting .tmp entries and symlinks before selecting regular backups removes unrelated directory entries.
  const { destination } = prepare(t);
  fs.copyFileSync(unrelatedMutation, path.join(destination, 'src', 'prune.mjs'));
  const result = measure(fixture, destination);
  assert.ok(result.probes.passed < result.probes.total);
  assert.ok(result.probes.failed.includes('ignores malformed names, temporary files, directories, and symlinks'));
});

test('run-probes reports skeleton failures and preserves quoted destination paths', (t) => {
  // Mutation captured: replacing behavior with the skeleton fails probes; splitting a path with spaces or quotes breaks this real CLI call.
  const { destination } = prepare(t, fixture, "worker copy 'quoted'");
  const result = measure(fixture, destination);
  assert.ok(result.probes.passed < result.probes.total);
  assert.ok(result.probes.failed.length > 0);
  assert.equal(result.manifest_ok, true);
});

test('run-probes rejects invalid usage with exit status 2', () => {
  // Mutation captured: accepting a missing destination can proceed with an undefined path.
  const result = run([probeScript]);
  assert.equal(result.error, undefined, result.error?.message);
  assert.equal(result.status, 2);
  assert.equal(result.stdout, '');
  assert.match(result.stderr, /usage: node evals\/run-probes\.mjs/);
});

test('run-probes identifies changed files outside Owned files', (t) => {
  // Mutation captured: omitting manifest-to-copy comparison hides forbidden worker edits.
  const { destination } = prepare(t);
  fs.copyFileSync(reference, path.join(destination, 'src', 'prune.mjs'));
  fs.appendFileSync(path.join(destination, 'brief.md'), '\nunauthorized change');
  fs.writeFileSync(path.join(destination, 'outside owned.txt'), 'unauthorized addition');
  const result = measure(fixture, destination);
  assert.deepEqual(result.scope_violations, ['brief.md', 'outside owned.txt']);
  assert.equal(result.probes.passed, result.probes.total);
});
