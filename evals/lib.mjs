import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

export const EVALS_DIR = path.dirname(fileURLToPath(import.meta.url));
export const REPO_ROOT = path.dirname(EVALS_DIR);
const MANIFEST_RE = /^([a-f0-9]{64})  ([^\r\n]+)$/;

function isWithin(parent, candidate) {
  const relative = path.relative(parent, candidate);
  return relative === '' || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative));
}

function physicalCandidate(candidate) {
  let ancestor = path.resolve(candidate);
  const suffix = [];
  while (true) {
    try {
      fs.lstatSync(ancestor);
      break;
    } catch (error) {
      if (error?.code !== 'ENOENT') throw error;
      const parent = path.dirname(ancestor);
      if (parent === ancestor) throw error;
      suffix.unshift(path.basename(ancestor));
      ancestor = parent;
    }
  }
  const realAncestor = fs.realpathSync(ancestor);
  return path.join(realAncestor, ...suffix);
}

export function assertOutsideRepository(candidate, { mustExist = false } = {}) {
  const absolute = path.resolve(candidate);
  const root = fs.realpathSync(REPO_ROOT);
  const physical = physicalCandidate(absolute);
  if (isWithin(root, absolute) || isWithin(root, physical)) {
    throw new Error(`destination must be outside the repository: ${candidate}`);
  }
  if (mustExist) {
    const stat = fs.statSync(absolute);
    if (!stat.isDirectory()) throw new Error(`destination must be an existing directory: ${candidate}`);
  } else if (fs.existsSync(absolute)) {
    throw new Error(`destination already exists: ${candidate}`);
  }
  return absolute;
}

function validateRelativePath(relative) {
  if (relative.length === 0 || relative.includes('\\') || path.posix.isAbsolute(relative)) return false;
  const segments = relative.split('/');
  return segments.every((segment) => segment !== '' && segment !== '.' && segment !== '..');
}

function listFixtureFiles(root, current = root) {
  const files = [];
  for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
    const absolute = path.join(current, entry.name);
    const relative = path.relative(root, absolute).split(path.sep).join('/');
    if (relative === 'MANIFEST.sha256' || relative === 'ANSWER-KEY.md' || relative === '.eval-run.json' || relative === 'probes' || relative.startsWith('probes/')) continue;
    if (entry.isSymbolicLink()) throw new Error(`fixture contains a symlink: ${relative}`);
    if (entry.isDirectory()) files.push(...listFixtureFiles(root, absolute));
    else if (entry.isFile()) files.push(relative);
    else throw new Error(`fixture contains a non-regular file: ${relative}`);
  }
  return files.sort();
}

export function readFixtureManifest(fixture) {
  const root = fs.realpathSync(path.resolve(fixture));
  if (!fs.statSync(root).isDirectory()) throw new Error(`fixture is not a directory: ${fixture}`);
  const manifestPath = path.join(root, 'MANIFEST.sha256');
  const raw = fs.readFileSync(manifestPath, 'utf8');
  const manifest = new Map();
  for (const line of raw.split(/\r?\n/)) {
    if (line === '' && manifest.size > 0) continue;
    const match = MANIFEST_RE.exec(line);
    if (!match || !validateRelativePath(match[2])) throw new Error(`invalid manifest line: ${line}`);
    const [, expected, relative] = match;
    if (relative === 'MANIFEST.sha256' || relative === 'ANSWER-KEY.md' || relative === '.eval-run.json' || relative === 'probes' || relative.startsWith('probes/')) {
      throw new Error(`manifest must not include hidden or generated path: ${relative}`);
    }
    if (manifest.has(relative)) throw new Error(`duplicate manifest path: ${relative}`);
    let pathCursor = root;
    for (const segment of relative.split('/')) {
      pathCursor = path.join(pathCursor, segment);
      if (fs.lstatSync(pathCursor).isSymbolicLink()) throw new Error(`manifest path traverses a symlink: ${relative}`);
    }
    const stat = fs.statSync(path.join(root, relative));
    if (!stat.isFile()) throw new Error(`manifest path is not a regular file: ${relative}`);
    const actual = crypto.createHash('sha256').update(fs.readFileSync(path.join(root, relative))).digest('hex');
    if (actual !== expected) throw new Error(`manifest checksum mismatch: ${relative}`);
    manifest.set(relative, expected);
  }
  if (manifest.size === 0) throw new Error('manifest is empty');
  const actualFiles = listFixtureFiles(root);
  const listedFiles = [...manifest.keys()].sort();
  if (actualFiles.length !== listedFiles.length || actualFiles.some((file, index) => file !== listedFiles[index])) {
    const unlisted = actualFiles.filter((file) => !manifest.has(file));
    const missing = listedFiles.filter((file) => !actualFiles.includes(file));
    throw new Error(`manifest file set mismatch (unlisted: ${unlisted.join(', ') || 'none'}; missing: ${missing.join(', ') || 'none'})`);
  }
  return { root, manifest };
}

export function workerFiles(directory) {
  const files = [];
  function visit(current) {
    for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
      const absolute = path.join(current, entry.name);
      const relative = path.relative(directory, absolute).split(path.sep).join('/');
      if (relative === '.eval-run.json') continue;
      if (entry.isSymbolicLink()) {
        files.push({ relative, type: 'symlink' });
      } else if (entry.isDirectory()) {
        visit(absolute);
      } else if (entry.isFile()) {
        files.push({ relative, type: 'file' });
      } else {
        files.push({ relative, type: 'other' });
      }
    }
  }
  visit(directory);
  return files.sort((a, b) => a.relative.localeCompare(b.relative));
}

export function ownedPaths(briefPath) {
  const text = fs.readFileSync(briefPath, 'utf8');
  const heading = /^## Owned files\s*$/m.exec(text);
  if (!heading) throw new Error('brief is missing an Owned files section');
  const sectionStart = heading.index + heading[0].length;
  const nextHeading = /^## /m.exec(text.slice(sectionStart));
  const sectionEnd = nextHeading ? sectionStart + nextHeading.index : text.length;
  const paths = new Set();
  for (const line of text.slice(sectionStart, sectionEnd).split(/\r?\n/)) {
    if (line.trim() === '') continue;
    const item = /^- `([^`]+)`(?:\s+—.*)?$/.exec(line.trim());
    if (!item || !validateRelativePath(item[1])) throw new Error(`invalid Owned files entry: ${line}`);
    paths.add(item[1]);
  }
  if (paths.size === 0) throw new Error('brief Owned files section is empty');
  return paths;
}

export function sha256File(file) {
  return crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
}
