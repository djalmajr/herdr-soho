// Read-only preflight for mutation-test copies: reject source-tree build
// artifacts before a worker mutates or builds in the copy.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { compareCodeUnits } from '../text.mjs';
import { projectRoot } from '../platform.mjs';

const USAGE = 'usage: mutation-guard <copy-dir> [--source <dir>] [--env NAME]...';
const BUILD_ENV = ['CARGO_TARGET_DIR', 'CARGO_BUILD_TARGET_DIR'];

function usageError() {
  process.stderr.write(`herdr-soho: ${USAGE}\n`);
  process.exitCode = 2;
}

function inside(parent, candidate) {
  const relative = path.relative(parent, candidate);
  return relative === '' || (relative !== '..' && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative));
}

// Resolve existing symlinked ancestors even when the build destination has
// not been created yet (a common state for Cargo target directories).
function canonicalPath(input) {
  let unresolved = path.resolve(input);
  const tail = [];
  while (true) {
    try {
      const resolved = fs.realpathSync(unresolved);
      return path.join(resolved, ...tail.reverse());
    } catch (error) {
      if (error.code !== 'ENOENT' && error.code !== 'ENOTDIR') throw error;
      const parent = path.dirname(unresolved);
      if (parent === unresolved) throw error;
      tail.push(path.basename(unresolved));
      unresolved = parent;
    }
  }
}

function parseArgs(args) {
  let copy = '';
  let source = '';
  const envNames = [];
  let sourceSeen = false;
  for (let i = 0; i < args.length; i++) {
    const arg = args[i];
    if (arg === '--source' || arg === '--env') {
      const value = args[i + 1];
      if (value === undefined || value === '' || value.startsWith('-')) return null;
      i++;
      if (arg === '--source') {
        if (sourceSeen) return null;
        sourceSeen = true;
        source = value;
      } else {
        if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(value)) return null;
        envNames.push(value);
      }
    } else if (arg.startsWith('--') || copy !== '') {
      return null;
    } else {
      copy = arg;
    }
  }
  if (copy === '') return null;
  return { copy, source, envNames };
}

function symlinkIntoSource(copy, source) {
  const pending = [copy];
  while (pending.length > 0) {
    const dir = pending.pop();
    const entries = fs.readdirSync(dir, { withFileTypes: true }).sort((a, b) => compareCodeUnits(b.name, a.name));
    for (const entry of entries) {
      const full = path.join(dir, entry.name);
      const rel = path.relative(copy, full);
      const stat = fs.lstatSync(full);
      if (stat.isSymbolicLink()) {
        try {
          const target = fs.realpathSync(full);
          if (inside(source, target)) return `symlink ${rel} -> ${target}`;
        } catch (error) {
          if (error.code === 'ENOENT') continue; // broken links are harmless
          throw error;
        }
      } else if (stat.isDirectory() && entry.name !== '.git') {
        pending.push(full);
      }
    }
  }
  return '';
}

// A relative build destination resolves against the copy: that is where
// the role runs the build.
function envPointingIntoSource(env, names, copy, source) {
  const offenders = [];
  for (const name of names) {
    const value = env[name];
    if (value === undefined || value === '') continue;
    try {
      if (inside(source, canonicalPath(path.resolve(copy, value)))) offenders.push(`${name} points into the source tree`);
    } catch {
      // If a configured destination cannot be resolved, do not echo it or
      // assume it is safe to write there.
      offenders.push(`${name} cannot be resolved safely`);
    }
  }
  return offenders.join('; ');
}

function cargoTargetInsideSource(copy, source, env) {
  try { fs.statSync(path.join(copy, 'Cargo.toml')); }
  catch (error) {
    if (error.code === 'ENOENT') return '';
    return 'cargo metadata failed; target-dir cannot be resolved safely';
  }
  if (cargoOnPath(env)) {
    const result = spawnSync('cargo', ['metadata', '--offline', '--no-deps', '--format-version', '1'], {
      cwd: copy, env, encoding: 'utf8', timeout: 30_000,
    });
    if (result.error || result.status !== 0) return 'cargo metadata failed; target-dir cannot be resolved safely';
    let metadata;
    try { metadata = JSON.parse(result.stdout); } catch { return 'cargo metadata failed; target-dir cannot be resolved safely'; }
    if (typeof metadata.target_directory !== 'string' || metadata.target_directory === '') {
      return 'cargo metadata failed; target-dir cannot be resolved safely';
    }
    try {
      return inside(source, canonicalPath(path.resolve(copy, metadata.target_directory)))
        ? 'cargo metadata puts target_directory inside the source tree'
        : '';
    } catch {
      return 'cargo metadata failed; target-dir cannot be resolved safely';
    }
  }
  return textualCargoTargetInsideSource(copy, source, env);
}

function cargoOnPath(env) {
  const dirs = (env.PATH ?? '').split(path.delimiter);
  const extensions = process.platform === 'win32' ? ['', '.exe', '.cmd', '.bat'] : [''];
  for (const dir of dirs) for (const extension of extensions) {
    try { if (fs.statSync(path.join(dir, `cargo${extension}`)).isFile()) return true; }
    catch { /* continue through PATH */ }
  }
  return false;
}

function textualCargoTargetInsideSource(copy, source, env) {
  const unresolvedTarget = /target-dir|target\\u|"target/i;
  const stripComment = (line) => {
    let quote = '';
    let escaped = false;
    for (let i = 0; i < line.length; i++) {
      const char = line[i];
      if (quote === '"' && escaped) { escaped = false; continue; }
      if (quote === '"' && char === '\\') { escaped = true; continue; }
      if (quote) { if (char === quote) quote = ''; continue; }
      if (char === '"' || char === "'") quote = char;
      else if (char === '#') return line.slice(0, i);
    }
    return line;
  };
  const parse = (text) => {
    let inBuild = false;
    let unresolved = false;
    for (const line of text.split(/\r?\n/)) {
      const contents = stripComment(line);
      const space = '[\\s\\v\\u00a0\\u1680\\u2000-\\u200a\\u2028\\u2029\\u202f\\u205f\\u3000\\ufeff]';
      const header = new RegExp(`^${space}*\\[${space}*([^\\]]+)\\]${space}*$`).exec(contents);
      if (header) { inBuild = header[1].trim() === 'build'; continue; }
      if (!inBuild) {
        if (unresolvedTarget.test(contents)) unresolved = true;
        continue;
      }
      const assignment = 'target-dir';
      const basic = new RegExp(`^${space}*${assignment}${space}*=${space}*"((?:\\\\[^\\r\\u2028\\u2029]|[^"\\\\])*)"${space}*$`).exec(contents);
      const literal = new RegExp(`^${space}*${assignment}${space}*=${space}*'([^']*)'${space}*$`).exec(contents);
      if (!basic && !literal && unresolvedTarget.test(contents)) unresolved = true;
      if (basic) { try { return JSON.parse(`"${basic[1]}"`); } catch { return null; } }
      if (literal) return literal[1];
    }
    return unresolved ? null : undefined;
  };
  for (let dir = copy; ; dir = path.dirname(dir)) {
    for (const name of ['config', 'config.toml']) {
      const file = path.join(dir, '.cargo', name);
      const relative = path.relative(copy, file) || path.join('.cargo', name);
      let text;
      try { text = fs.readFileSync(file, 'utf8'); }
      catch (error) { if (error.code === 'ENOENT') continue; return `${relative} cannot be read`; }
      const value = parse(text);
      if (value === undefined || value === '') break;
      if (value === null) return `${relative} target-dir cannot be resolved safely`;
      try { return inside(source, canonicalPath(path.resolve(dir, value))) ? `${relative} sets target-dir inside the source tree` : ''; }
      catch { return `${relative} target-dir cannot be resolved safely`; }
    }
    const parent = path.dirname(dir);
    if (parent === dir) break;
  }
  const cargoHome = env.CARGO_HOME || path.join(env.HOME || os.homedir(), '.cargo');
  for (const name of ['config', 'config.toml']) {
    const file = path.join(cargoHome, name);
    const relative = file;
    let text;
    try { text = fs.readFileSync(file, 'utf8'); }
    catch (error) { if (error.code === 'ENOENT') continue; return `${relative} cannot be read`; }
    const value = parse(text);
    if (value === undefined || value === '') break;
    if (value === null) return `${relative} target-dir cannot be resolved safely`;
    try { return inside(source, canonicalPath(path.resolve(path.dirname(cargoHome), value))) ? `${relative} sets target-dir inside the source tree` : ''; }
    catch { return `${relative} target-dir cannot be resolved safely`; }
  }
  return '';
}

function printCheck(name, detail = '') {
  process.stdout.write(detail ? `fail ${name}: ${detail}\n` : `ok ${name}\n`);
  return detail !== '';
}

export function cmdMutationGuard(args, env = process.env, cwd = process.cwd()) {
  const parsed = parseArgs((args ?? []).map(String));
  if (!parsed) {
    usageError();
    return;
  }

  let copy;
  let source;
  try {
    const copyPath = path.resolve(cwd, parsed.copy);
    if (!fs.statSync(copyPath).isDirectory()) throw new Error('not-directory');
    copy = fs.realpathSync(copyPath);
    const sourcePath = parsed.source === '' ? projectRoot(env, cwd) : path.resolve(cwd, parsed.source);
    if (!fs.statSync(sourcePath).isDirectory()) throw new Error('not-directory');
    source = fs.realpathSync(sourcePath);
  } catch {
    usageError();
    return;
  }

  let failed = printCheck('copy-outside-source', inside(source, copy) || inside(copy, source)
    ? 'copy and source directories overlap'
    : '');

  let symlinkFailure = '';
  try { symlinkFailure = symlinkIntoSource(copy, source); }
  catch { symlinkFailure = 'unable to inspect symlinks safely'; }
  failed = printCheck('no-symlink-into-source', symlinkFailure) || failed;

  const envFailure = envPointingIntoSource(env, [...new Set([...BUILD_ENV, ...parsed.envNames])], copy, source);
  failed = printCheck('build-env', envFailure) || failed;

  const configFailure = cargoTargetInsideSource(copy, source, env);
  failed = printCheck('cargo-config', configFailure) || failed;
  if (failed) process.exitCode = 1;
}
