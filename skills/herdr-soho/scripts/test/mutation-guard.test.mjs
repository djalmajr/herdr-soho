import test from 'node:test';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { fixtureEnv, nodeBin } from './parity.mjs';

const ENTRY = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'herdr-soho.mjs');

function setup() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'ha mutation "guard" ')));
  const source = path.join(root, 'source');
  const copy = path.join(root, 'copy');
  fs.mkdirSync(path.join(source, 'target'), { recursive: true });
  fs.mkdirSync(copy, { recursive: true });
  fs.writeFileSync(path.join(source, 'source.txt'), 'source stays unchanged\n');
  fs.writeFileSync(path.join(source, 'target', 'existing.o'), 'build artifact\n');
  fs.mkdirSync(path.join(source, '.git'), { recursive: true });
  const env = fixtureEnv({ HOME: path.join(root, 'home'), XDG_CONFIG_HOME: path.join(root, 'config') });
  delete env.CARGO_TARGET_DIR;
  delete env.CARGO_BUILD_TARGET_DIR;
  const run = (args, over = {}) => {
    const r = spawnSync(nodeBin(), [ENTRY, 'mutation-guard', ...args], {
      cwd: source,
      env: { ...env, ...over },
      encoding: 'utf8',
      timeout: 30_000,
    });
    return { rc: r.status === null ? -1 : r.status, out: r.stdout ?? '', err: r.stderr ?? '' };
  };
  return {
    root, source, copy, env, run,
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
}

function treeHash(root) {
  const hash = crypto.createHash('sha256');
  function visit(dir, rel = '') {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name.localeCompare(b.name))) {
      if (rel === '' && entry.name === '.git') continue;
      const name = path.join(rel, entry.name);
      const full = path.join(dir, entry.name);
      const stat = fs.lstatSync(full);
      hash.update(`${stat.isDirectory() ? 'd' : stat.isSymbolicLink() ? 'l' : 'f'}:${name}\0`);
      if (stat.isDirectory()) visit(full, name);
      else if (stat.isSymbolicLink()) hash.update(fs.readlinkSync(full));
      else hash.update(fs.readFileSync(full));
    }
  }
  visit(root);
  return hash.digest('hex');
}

// Mutation captured: a source-linked target is refused before the simulated build writes its marker.
test('rejects a target symlink into the source before any mutation', (t) => {
  const s = setup();
  try {
    fs.symlinkSync(path.join(s.source, 'target'), path.join(s.copy, 'target'), 'dir');
    const mutateOnlyAfterGuard = () => {
      const result = s.run([s.copy, '--source', s.source]);
      if (result.rc === 0) fs.writeFileSync(path.join(s.copy, 'target', 'mutation-marker'), 'mutated');
      return result;
    };
    const result = mutateOnlyAfterGuard();
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail no-symlink-into-source: symlink target -> /);
    assert.equal(fs.existsSync(path.join(s.source, 'target', 'mutation-marker')), false, 'the guard must reject before mutation');
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('rejects build output environment paths inside the source without echoing values', (t) => {
  const s = setup();
  try {
    const secretPath = path.join(s.source, 'target');
    const result = s.run([s.copy, '--source', s.source, '--env', 'MUTATION_TARGET'], {
      CARGO_TARGET_DIR: secretPath,
      MUTATION_TARGET: secretPath,
    });
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail build-env: CARGO_TARGET_DIR points into the source tree/);
    assert.match(result.out, /MUTATION_TARGET points into the source tree/);
    assert.equal(result.out.includes(secretPath), false, 'environment values must not be printed');
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('rejects absolute cargo target-dir values inside the source', (t) => {
  const s = setup();
  try {
    fs.mkdirSync(path.join(s.copy, '.cargo'));
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), `target-dir = ${JSON.stringify(path.join(s.source, 'target'))}\n`);
    const result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail cargo-config: \.cargo[\\/]config\.toml sets target-dir inside the source tree/);
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('accepts an isolated copy, ignores broken links and does not descend into .git', (t) => {
  const s = setup();
  try {
    fs.mkdirSync(path.join(s.copy, 'target'), { recursive: true });
    fs.mkdirSync(path.join(s.copy, '.cargo'), { recursive: true });
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), `target-dir = ${JSON.stringify(path.join(s.copy, 'target'))}\n`);
    fs.symlinkSync(path.join(s.root, 'missing-target'), path.join(s.copy, 'broken-link'));
    fs.mkdirSync(path.join(s.copy, '.git'), { recursive: true });
    fs.symlinkSync(path.join(s.source, 'target'), path.join(s.copy, '.git', 'ignored-link'), 'dir');
    const result = s.run([s.copy], { CARGO_TARGET_DIR: path.join(s.copy, 'target') });
    assert.equal(result.rc, 0, result.err);
    assert.equal(result.out, 'ok copy-outside-source\nok no-symlink-into-source\nok build-env\nok cargo-config\n');
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('isolated-copy mutation leaves every source file and build artifact unchanged', (t) => {
  const s = setup();
  try {
    fs.mkdirSync(path.join(s.copy, 'target'), { recursive: true });
    fs.writeFileSync(path.join(s.copy, 'source.txt'), 'source stays unchanged\n');
    const sourceBefore = treeHash(s.source);
    const buildBefore = treeHash(path.join(s.source, 'target'));
    const result = s.run([s.copy], { CARGO_TARGET_DIR: path.join(s.copy, 'target') });
    assert.equal(result.rc, 0, result.err);
    fs.writeFileSync(path.join(s.copy, 'source.txt'), 'temporary mutation\n');
    fs.writeFileSync(path.join(s.copy, 'target', 'build-output.o'), 'isolated build output\n');
    assert.equal(treeHash(s.source), sourceBefore, 'source files must retain their sha256');
    assert.equal(treeHash(path.join(s.source, 'target')), buildBefore, 'source build directory must retain its sha256');
    assert.equal(fs.readFileSync(path.join(s.copy, 'source.txt'), 'utf8'), 'temporary mutation\n');
    assert.equal(fs.existsSync(path.join(s.copy, 'target', 'build-output.o')), true);
  } finally { s.cleanup(); }
});

test('rejects copy/source containment in either direction', (t) => {
  const s = setup();
  try {
    const nested = path.join(s.source, 'nested-copy');
    fs.mkdirSync(nested);
    const inside = s.run([nested, '--source', s.source]);
    assert.equal(inside.rc, 1, inside.err);
    assert.match(inside.out, /fail copy-outside-source:/);
    const contains = s.run([s.root, '--source', s.source]);
    assert.equal(contains.rc, 1, contains.err);
    assert.match(contains.out, /fail copy-outside-source:/);
  } finally { s.cleanup(); }
});

test('invalid arguments and non-directory copies exit 2', (t) => {
  const s = setup();
  try {
    for (const args of [[], ['--source', s.source], [s.copy, '--source'], [s.copy, '--source', '--env', 'X'], [s.copy, '--env'], [s.copy, '--env', '--source', s.source], [path.join(s.root, 'missing-copy')], [path.join(s.source, 'source.txt')], [s.copy, '--unknown']]) {
      const result = s.run(args);
      assert.equal(result.rc, 2, `${JSON.stringify(args)}: ${result.err}`);
      assert.equal(result.out, '', `${JSON.stringify(args)} must not run checks`);
      assert.match(result.err, /usage: mutation-guard/);
    }
  } finally { s.cleanup(); }
});
