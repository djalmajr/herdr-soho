import test from 'node:test';
import assert from 'node:assert/strict';
import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { fixtureEnv, nodeBin } from './parity.mjs';
import { canSymlink } from './tools.mjs';

const ENTRY = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'herdr-soho.mjs');

function setup() {
  const prefix = process.platform === 'win32' ? 'ha mutation guard ' : 'ha mutation "guard" ';
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), prefix)));
  const source = path.join(root, 'source');
  const copy = path.join(root, 'copy');
  fs.mkdirSync(path.join(source, 'target'), { recursive: true });
  fs.mkdirSync(copy, { recursive: true });
  fs.writeFileSync(path.join(source, 'source.txt'), 'source stays unchanged\n');
  fs.writeFileSync(path.join(source, 'target', 'existing.o'), 'build artifact\n');
  fs.mkdirSync(path.join(source, '.git'), { recursive: true });
  const cargoHome = path.join(root, 'cargo-home');
  fs.mkdirSync(cargoHome, { recursive: true });
  const env = fixtureEnv({
    HOME: path.join(root, 'home'),
    XDG_CONFIG_HOME: path.join(root, 'config'),
    CARGO_HOME: cargoHome,
    RUSTUP_HOME: process.env.RUSTUP_HOME ?? path.join(os.homedir(), '.rustup'),
  });
  delete env.CARGO_TARGET_DIR;
  delete env.CARGO_BUILD_TARGET_DIR;
  const run = (args, over = {}, workDir = source) => {
    const r = spawnSync(nodeBin(), [ENTRY, 'mutation-guard', ...args], {
      cwd: workDir,
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

function cargoAvailable(env) {
  const result = spawnSync('cargo', ['--version'], { env, encoding: 'utf8', timeout: 5_000 });
  return !result.error && result.status === 0;
}

function writeCargoManifest(copy) {
  fs.writeFileSync(path.join(copy, 'Cargo.toml'), '[package]\nname = "mutation-guard-fixture"\nversion = "0.1.0"\nedition = "2021"\n');
  fs.mkdirSync(path.join(copy, 'src'), { recursive: true });
  fs.writeFileSync(path.join(copy, 'src', 'lib.rs'), 'pub fn fixture() {}\n');
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
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
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

// Mutation captured: localeCompare reverses `B` and `a`, changing which
// source-linked directory the guard reports first.
test('reports source symlinks in reverse code-unit directory order', (t) => {
  const s = setup();
  try {
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
    for (const name of ['B', 'a']) {
      fs.mkdirSync(path.join(s.source, name));
      fs.mkdirSync(path.join(s.copy, name));
      fs.symlinkSync(path.join(s.source, name), path.join(s.copy, name, 'target'), 'dir');
    }
    const result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail no-symlink-into-source: symlink B\/target -> /);
    assert.doesNotMatch(result.out, /symlink a\/target/);
  } finally { s.cleanup(); }
});

test('rejects build output environment paths inside the source without echoing values', (t) => {
  const s = setup();
  try {
    const secretPath = path.join(s.source, 'target');
    const result = s.run([s.copy, '--source', s.source, '--env', 'MUTATION_TARGET'], {
      CARGO_TARGET_DIR: secretPath,
      CARGO_BUILD_TARGET_DIR: secretPath,
      MUTATION_TARGET: secretPath,
    });
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail build-env: CARGO_TARGET_DIR points into the source tree/);
    assert.match(result.out, /CARGO_BUILD_TARGET_DIR points into the source tree/);
    assert.match(result.out, /MUTATION_TARGET points into the source tree/);
    assert.equal(result.out.includes(secretPath), false, 'environment values must not be printed');
    assert.equal(result.err, '');
    const empty = s.run([s.copy, '--source', s.source], { CARGO_TARGET_DIR: '' });
    assert.equal(empty.rc, 0, empty.out);
    assert.match(empty.out, /ok build-env/);
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
    const sourceLink = path.join(s.copy, 'source-link');
    fs.symlinkSync(s.source, sourceLink, 'dir');
    const linked = s.run([s.copy, '--source', s.source], { CARGO_TARGET_DIR: path.join(sourceLink, 'target') });
    assert.equal(linked.rc, 1, linked.err);
    assert.match(linked.out, /fail build-env: CARGO_TARGET_DIR points into the source tree/);
  } finally { s.cleanup(); }
});

test('rejects absolute cargo target-dir values inside the source', (t) => {
  const s = setup();
  try {
    writeCargoManifest(s.copy);
    const noCargoPath = path.join(s.root, 'no-cargo');
    fs.mkdirSync(noCargoPath);
    fs.mkdirSync(path.join(s.copy, '.cargo'));
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), `[build]\ntarget-dir = ${JSON.stringify(path.join(s.source, 'target'))}\n`);
    const result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail cargo-config: \.cargo[\\/]config\.toml sets target-dir inside the source tree/);
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('relative and single-quoted build destinations are resolved against the copy', (t) => {
  const s = setup();
  try {
    writeCargoManifest(s.copy);
    const noCargoPath = path.join(s.root, 'no-cargo');
    fs.mkdirSync(noCargoPath);
    // Mutation captured: skipping a relative value (or a TOML literal
    // string in single quotes) passes a copy whose build output lands in the
    // source tree once cargo runs in the copy.
    fs.mkdirSync(path.join(s.copy, '.cargo'));
    const cfg = path.join(s.copy, '.cargo', 'config.toml');
    fs.writeFileSync(cfg, '[build]\ntarget-dir = "../source/target"\n');
    let r = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(r.rc, 1, r.out);
    assert.match(r.out, /fail cargo-config: \.cargo[\\/]config\.toml sets target-dir inside the source tree/);
    fs.writeFileSync(cfg, `[build]\ntarget-dir = '${path.join(s.source, 'target')}'\n`);
    r = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(r.rc, 1, r.out);
    assert.match(r.out, /fail cargo-config/);
    fs.writeFileSync(cfg, "[build]\ntarget-dir = 'target'\n");
    r = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_TARGET_DIR: '../source/target' });
    assert.equal(r.rc, 1, r.out);
    assert.match(r.out, /ok cargo-config/);
    assert.match(r.out, /fail build-env: CARGO_TARGET_DIR points into the source tree/);
    assert.ok(!r.out.includes('../source/target'), 'the value is never printed');
    r = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_TARGET_DIR: 'target' });
    assert.equal(r.rc, 0, r.out);
  } finally { s.cleanup(); }
});

test('rejects Cargo target-dir syntaxes from the copy, its ancestors, and CARGO_HOME', () => {
  // Mutation captured: ignoring a dotted, inline-table, quoted, ancestor, or Cargo-home target-dir lets the build write into source.
  const s = setup();
  try {
    writeCargoManifest(s.copy);
    const noCargoPath = path.join(s.root, 'no-cargo');
    fs.mkdirSync(noCargoPath);
    s.env.PATH = noCargoPath;
    const cargo = path.join(s.copy, '.cargo');
    fs.mkdirSync(cargo);
    const sourceTarget = JSON.stringify(path.join(s.source, 'target'));
    for (const config of [`build.target-dir = ${sourceTarget}\n`, `build = { target-dir = ${sourceTarget} }\n`, `[build]\n"target-dir" = '${path.join(s.source, 'target')}'\n`]) {
      fs.writeFileSync(path.join(cargo, 'config.toml'), config);
      const result = s.run([s.copy, '--source', s.source]);
      assert.equal(result.rc, 1, config);
      assert.match(result.out, /fail cargo-config:/);
    }
    fs.rmSync(cargo, { recursive: true });
    const ancestorCargo = path.join(s.root, '.cargo');
    fs.mkdirSync(ancestorCargo);
    fs.writeFileSync(path.join(ancestorCargo, 'config.toml'), `build.target-dir = ${JSON.stringify(path.join(s.source, 'target'))}\n`);
    let result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /fail cargo-config: \.\.\/\.cargo[\\/]config\.toml target-dir cannot be resolved safely/);
    fs.rmSync(ancestorCargo, { recursive: true });
    const cargoHome = path.join(s.root, 'cargo-home');
    fs.mkdirSync(cargoHome, { recursive: true });
    fs.writeFileSync(path.join(cargoHome, 'config.toml'), `[build]\ntarget-dir = ${sourceTarget}\n`);
    result = s.run([s.copy, '--source', s.source], { CARGO_HOME: cargoHome });
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, new RegExp(`fail cargo-config: ${cargoHome.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}[\\\\/]config\\.toml sets target-dir`));
    const defaultCargoHome = path.join(s.env.HOME, '.cargo');
    fs.mkdirSync(defaultCargoHome, { recursive: true });
    fs.writeFileSync(path.join(defaultCargoHome, 'config.toml'), `[build]\ntarget-dir = ${sourceTarget}\n`);
    result = s.run([s.copy, '--source', s.source], { CARGO_HOME: '' });
    assert.equal(result.rc, 1, result.out);
    fs.mkdirSync(cargo, { recursive: true });
    fs.writeFileSync(path.join(cargo, 'config.toml'), `[build]\ntarget-dir = ${JSON.stringify(path.join(s.copy, 'target'))}\n`);
    result = s.run([s.copy, '--source', s.source], { CARGO_HOME: cargoHome });
    assert.equal(result.rc, 0, result.out);
  } finally { s.cleanup(); }
});

test('cargo metadata rejects every Cargo-supported target-dir form inside source', (t) => {
  const s = setup();
  try {
    if (!cargoAvailable(s.env)) return t.skip('cargo is not available on PATH');
    writeCargoManifest(s.copy);
    const target = JSON.stringify(path.join(s.source, 'target-x'));
    const cases = [
      ['spaced dotted key', `build . target-dir = ${target}\n`],
      ['quoted dotted table', `"build"."target-dir" = ${target}\n`],
      ['quoted dotted table value', `build."target-dir" = ${target}\n`],
      ['unicode escaped key', `[build]\n"target\\u002ddir" = ${target}\n`],
      ['multiline basic', `[build]\ntarget-dir = """${path.join(s.source, 'target-x')}"""\n`],
      ['multiline literal', `[build]\ntarget-dir = '''${path.join(s.source, 'target-x')}'''\n`],
      ['inline table key first', `build = { jobs = 2, target-dir = ${target} }\n`],
      ['inline table key last', `build = { target-dir = ${target}, jobs = 2 }\n`],
      ['other table before build', `[profile.x]\ntarget-dir = "ignored"\n[build]\ntarget-dir = ${target}\n`],
      ['foo table before build', `[foo]\ntarget-dir = "ignored"\n[build]\ntarget-dir = ${target}\n`],
      ['CARGO_HOME parent relative', ''],
      ['CARGO_HOME explicit relative', ''],
      ['config takes precedence over config.toml', ''],
    ];
    for (const [name, config] of cases) {
      fs.rmSync(path.join(s.copy, '.cargo'), { recursive: true, force: true });
      if (name === 'CARGO_HOME parent relative' || name === 'CARGO_HOME explicit relative') {
        const cargoHome = path.join(s.root, name.includes('parent') ? 'home/.cargo' : 'ch');
        fs.mkdirSync(cargoHome, { recursive: true });
        fs.writeFileSync(path.join(cargoHome, 'config.toml'), `[build]\ntarget-dir = ${JSON.stringify(name.includes('parent') ? '../source/target-x' : 'source/target-x')}\n`);
        const env = { CARGO_HOME: cargoHome };
        const result = s.run([s.copy, '--source', s.source], env);
        assert.equal(result.rc, 1, `${name}: ${result.out}`);
        assert.match(result.out, /fail cargo-config: cargo metadata puts target_directory inside the source tree/);
        continue;
      }
      if (name === 'config takes precedence over config.toml') {
        const cargo = path.join(s.copy, '.cargo');
        fs.mkdirSync(cargo, { recursive: true });
        fs.writeFileSync(path.join(cargo, 'config'), `[build]\ntarget-dir = ${target}\n`);
        fs.writeFileSync(path.join(cargo, 'config.toml'), '[build]\ntarget-dir = "isolated-target"\n');
      } else {
        const cargo = path.join(s.copy, '.cargo');
        fs.mkdirSync(cargo, { recursive: true });
        fs.writeFileSync(path.join(cargo, 'config.toml'), config);
      }
      const result = s.run([s.copy, '--source', s.source]);
      assert.equal(result.rc, 1, `${name}: ${result.out}`);
      assert.match(result.out, /fail cargo-config: cargo metadata puts target_directory inside the source tree/, result.err);
    }
    fs.rmSync(path.join(s.copy, '.cargo'), { recursive: true, force: true });
    const ancestor = path.join(s.root, '.cargo');
    fs.mkdirSync(ancestor);
    fs.writeFileSync(path.join(ancestor, 'config.toml'), `[build]\ntarget-dir = ${target}\n`);
    const result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /fail cargo-config: cargo metadata puts target_directory inside the source tree/);
  } finally { s.cleanup(); }
});

test('cargo config fallback fails closed and follows Cargo config precedence without cargo', () => {
  const s = setup();
  try {
    writeCargoManifest(s.copy);
    const noCargoPath = path.join(s.root, 'no-cargo-bin');
    fs.mkdirSync(noCargoPath);
    const target = JSON.stringify(path.join(s.source, 'target'));
    fs.mkdirSync(path.join(s.copy, '.cargo'));
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), `build.target-dir = ${target}\n`);
    let dotted = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(dotted.rc, 1, dotted.out);
    assert.match(dotted.out, /fail cargo-config: \.cargo[\/]config\.toml target-dir cannot be resolved safely/);
    for (const config of [
      `[build]\ntarget-dir = '${path.join(s.source, 'target')}'\n`,
      `build = { jobs = 2, target-dir = ${target} }\n`,
      `[build]\ntarget-dir = """${path.join(s.source, 'target')}"""\n`,
      `[foo]\ntarget-dir = "ignored"\n[build]\ntarget-dir = ${target}\n`,
      `[build]\n"target\\u002ddir" = ${target}\n`,
      `[build]\ntarget-dir =\u00a0${target}\n`,
      `[build]\ntarget-dir = ${target}\u3000# comment\n`,
    ]) {
      fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), config);
      const result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
      assert.equal(result.rc, 1, result.out);
      assert.match(result.out, /fail cargo-config: \.cargo[\/]config\.toml (?:target-dir cannot be resolved safely|sets target-dir inside the source tree)/);
      assert.match(result.out, /\.cargo[\/]config\.toml/);
    }
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), "[build]\ntarget-dir = 'isolated-target'\n");
    let result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(result.rc, 0, result.out);
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config'), `[build]\ntarget-dir = ${target}\n`);
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), '[build]\ntarget-dir = "isolated"\n');
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /fail cargo-config: \.cargo[\/]config sets target-dir inside the source tree/);
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config'), '[build]\ntarget-dir = "isolated"\n');
    fs.writeFileSync(path.join(s.copy, '.cargo', 'config.toml'), `[build]\ntarget-dir = ${target}\n`);
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(result.rc, 0, result.out);

    fs.rmSync(path.join(s.copy, '.cargo'), { recursive: true, force: true });
    const ancestorCargo = path.join(s.root, '.cargo');
    fs.mkdirSync(ancestorCargo);
    fs.writeFileSync(path.join(ancestorCargo, 'config'), '[build]\ntarget-dir = "source/target"\n');
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath });
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /\.\.\/[.]cargo[\/]config sets target-dir inside the source tree/);
    fs.rmSync(ancestorCargo, { recursive: true, force: true });

    const cargoHome = path.join(s.root, 'ch');
    fs.mkdirSync(cargoHome);
    fs.writeFileSync(path.join(cargoHome, 'config'), '[build]\ntarget-dir = "source/target"\n');
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_HOME: cargoHome });
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /config sets target-dir inside the source tree/);
    fs.writeFileSync(path.join(cargoHome, 'config.toml'), '[build]\ntarget-dir = "isolated"\n');
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_HOME: cargoHome });
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /config sets target-dir inside the source tree/);
    fs.writeFileSync(path.join(cargoHome, 'config'), '[build]\ntarget-dir = "isolated"\n');
    fs.writeFileSync(path.join(cargoHome, 'config.toml'), '[build]\ntarget-dir = "source/target"\n');
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_HOME: cargoHome });
    assert.equal(result.rc, 0, result.out);

    fs.rmSync(path.join(s.copy, 'Cargo.toml'));
    result = s.run([s.copy, '--source', s.source], { PATH: noCargoPath, CARGO_HOME: cargoHome });
    assert.equal(result.rc, 0, result.out);
    assert.match(result.out, /ok cargo-config/);
  } finally { s.cleanup(); }
});

test('cargo metadata output without target_directory fails closed', (t) => {
  if (process.platform === 'win32') return t.skip('the fake cargo executable uses a POSIX shebang');
  const s = setup();
  try {
    writeCargoManifest(s.copy);
    const fakeBin = path.join(s.root, 'fake-bin');
    fs.mkdirSync(fakeBin);
    fs.writeFileSync(path.join(fakeBin, 'cargo'), `#!${process.execPath}\nprocess.stdout.write('{}');\n`, { mode: 0o755 });
    const result = s.run([s.copy, '--source', s.source], { PATH: fakeBin });
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail cargo-config: cargo metadata failed; target-dir cannot be resolved safely/);
    assert.equal(result.err, '');
  } finally { s.cleanup(); }
});

test('treats ENOTDIR like a missing tail and reports symlinks in UTF-16 order', (t) => {
  // Mutation captured: treating ENOTDIR as fatal or sorting by locale changes guard acceptance or the reported link.
  const s = setup();
  try {
    fs.writeFileSync(path.join(s.copy, 'not-dir'), 'file');
    let result = s.run([s.copy, '--source', s.source], { CARGO_TARGET_DIR: path.join(s.copy, 'not-dir', 'target') });
    assert.equal(result.rc, 0, result.out);
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
    fs.symlinkSync(path.join(s.source, 'target'), path.join(s.copy, 'a'), 'dir');
    fs.symlinkSync(path.join(s.source, 'target'), path.join(s.copy, '😀'), 'dir');
    fs.symlinkSync(path.join(s.source, 'target'), path.join(s.copy, '～'), 'dir');
    result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.out);
    assert.match(result.out, /fail no-symlink-into-source: symlink ～ -> /);
  } finally { s.cleanup(); }
});

test('uses the repository root as default source when called from a subdirectory', (t) => {
  const s = setup();
  try {
    const nestedCwd = path.join(s.source, 'nested', 'work');
    const copyInside = path.join(s.source, 'isolated-copy');
    fs.mkdirSync(nestedCwd, { recursive: true });
    fs.mkdirSync(copyInside);
    const git = spawnSync('git', ['init', '-q'], { cwd: s.source, encoding: 'utf8' });
    if (git.status !== 0) return t.skip('git is not available to create the project-root fixture');
    const result = s.run([copyInside], {}, nestedCwd);
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail copy-outside-source: copy and source directories overlap/);
  } finally { s.cleanup(); }
});

test('finds nested and indirect symlinks into the source', (t) => {
  const s = setup();
  try {
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
    const nested = path.join(s.copy, 'nested', 'deeper');
    fs.mkdirSync(nested, { recursive: true });
    fs.symlinkSync(path.join(s.source, 'target'), path.join(nested, 'nested-link'), 'dir');
    let result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail no-symlink-into-source: symlink nested[\\/]deeper[\\/]nested-link -> /);

    fs.rmSync(path.join(s.copy, 'nested'), { recursive: true });
    const bridge = path.join(s.root, 'bridge');
    fs.symlinkSync(path.join(s.source, 'target'), bridge, 'dir');
    fs.symlinkSync(bridge, path.join(s.copy, 'indirect-link'), 'dir');
    result = s.run([s.copy, '--source', s.source]);
    assert.equal(result.rc, 1, result.err);
    assert.match(result.out, /fail no-symlink-into-source: symlink indirect-link -> /);
  } finally { s.cleanup(); }
});

test('accepts an isolated copy, ignores broken links and does not descend into .git', (t) => {
  const s = setup();
  try {
    if (!canSymlink(s.root)) return t.skip('symlink creation is unavailable on this host');
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
    const same = s.run([s.source, '--source', s.source]);
    assert.equal(same.rc, 1, same.err);
    assert.match(same.out, /fail copy-outside-source:/);
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
