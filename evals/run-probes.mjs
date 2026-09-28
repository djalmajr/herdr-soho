import crypto from 'node:crypto';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { assertOutsideRepository, ownedPaths, readFixtureManifest, workerFiles } from './lib.mjs';

function isWithin(parent, candidate) {
  const relative = path.relative(parent, candidate);
  return relative === '' || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative));
}

function listProbeFiles(root, current = root) {
  const files = [];
  for (const entry of fs.readdirSync(current, { withFileTypes: true })) {
    const absolute = path.join(current, entry.name);
    if (entry.isSymbolicLink()) throw new Error(`probe tree contains a symlink: ${path.relative(root, absolute)}`);
    if (entry.isDirectory()) files.push(...listProbeFiles(root, absolute));
    else if (entry.isFile() && entry.name.endsWith('.test.mjs')) files.push(absolute);
  }
  return files.sort();
}

function compareScope(destination, manifest, owned) {
  const baseline = new Map(manifest);
  const actual = new Map(workerFiles(destination).map((item) => [item.relative, item]));
  const violations = new Set();
  for (const [relative, expectedHash] of baseline) {
    const item = actual.get(relative);
    if (!item || item.type !== 'file' || crypto.createHash('sha256').update(fs.readFileSync(path.join(destination, ...relative.split('/')))).digest('hex') !== expectedHash) {
      if (!owned.has(relative)) violations.add(relative);
    }
  }
  for (const [relative, item] of actual) {
    if (!baseline.has(relative) && !owned.has(relative)) violations.add(relative);
    if (item.type !== 'file' && baseline.has(relative) && !owned.has(relative)) violations.add(relative);
  }
  return [...violations].sort();
}

function readTestResult(stdout, stderr, status) {
  const tests = /^# tests (\d+)$/m.exec(stdout);
  const passed = /^# pass (\d+)$/m.exec(stdout);
  const failedCount = /^# fail (\d+)$/m.exec(stdout);
  if (!tests || !passed || !failedCount) {
    throw new Error(`test runner did not return complete TAP results (status ${status}): ${stderr.trim() || stdout.trim()}`);
  }
  const failed = [...stdout.matchAll(/^not ok \d+ - (.+)$/gm)].map((match) => match[1]);
  const totalFailed = Number(failedCount[1]);
  while (failed.length < totalFailed) failed.push('probe runner failure');
  return {
    passed: Number(passed[1]),
    total: Number(tests[1]),
    failed: failed.slice(0, totalFailed),
  };
}

async function main(argv) {
  if (argv.length !== 2) throw new Error('usage: node evals/run-probes.mjs <fixture> <dest>');
  const [fixtureArg, destArg] = argv;
  const { root: fixtureRoot, manifest } = readFixtureManifest(fixtureArg);
  const destination = assertOutsideRepository(destArg, { mustExist: true });
  const meta = JSON.parse(fs.readFileSync(path.join(destination, '.eval-run.json'), 'utf8'));
  if (meta?.fixture !== path.basename(fixtureRoot) || meta?.manifest_ok !== true || typeof meta?.prepared_at !== 'string') {
    throw new Error('destination is missing valid prepare metadata for this fixture');
  }
  const probeRoot = path.join(fixtureRoot, 'probes');
  const probeFiles = listProbeFiles(probeRoot);
  if (probeFiles.length === 0) throw new Error('fixture has no hidden .test.mjs probes');
  const owned = ownedPaths(path.join(fixtureRoot, 'brief.md'));
  const scopeViolations = compareScope(destination, manifest, owned);
  const completedFiles = new Map(workerFiles(destination).map((item) => [item.relative, item]));

  let temporary;
  let measured;
  try {
    temporary = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-eval-probes-'));
    const tempReal = fs.realpathSync(temporary);
    const destReal = fs.realpathSync(destination);
    if (isWithin(destReal, tempReal)) throw new Error('probe temporary directory must not be inside the worker destination');

    for (const [relative] of manifest) {
      const source = path.join(destination, ...relative.split('/'));
      const entry = completedFiles.get(relative);
      if (!entry) continue;
      if (entry.type !== 'file') throw new Error(`worker path is not a regular file: ${relative}`);
      const target = path.join(temporary, ...relative.split('/'));
      fs.mkdirSync(path.dirname(target), { recursive: true });
      fs.copyFileSync(source, target);
    }
    for (const probeFile of probeFiles) {
      const relative = path.relative(probeRoot, probeFile);
      const target = path.join(temporary, 'probes', relative);
      fs.mkdirSync(path.dirname(target), { recursive: true });
      fs.copyFileSync(probeFile, target);
    }
    const targetSource = path.join(temporary, 'src', 'prune.mjs');
    const testPaths = probeFiles.map((file) => path.join('probes', path.relative(probeRoot, file)));
    const childEnv = { ...process.env, EVAL_TARGET: targetSource };
    delete childEnv.NODE_TEST_CONTEXT;
    const nodeExecutable = process.versions.bun ? 'node' : process.execPath;
    const child = spawnSync(nodeExecutable, ['--test-reporter=tap', '--test', ...testPaths], {
      cwd: temporary,
      env: childEnv,
      encoding: 'utf8',
      timeout: 30_000,
      maxBuffer: 10 * 1024 * 1024,
    });
    if (child.error) throw new Error(`could not complete hidden probes: ${child.error.message}`);
    if (child.status === null) throw new Error('hidden probe process did not exit');
    measured = readTestResult(child.stdout, child.stderr, child.status);
  } finally {
    if (temporary) fs.rmSync(temporary, { recursive: true, force: true });
  }

  process.stdout.write(`${JSON.stringify({
    fixture: path.basename(fixtureRoot),
    probes: measured,
    scope_violations: scopeViolations,
    manifest_ok: true,
  })}\n`);
}

main(process.argv.slice(2)).catch((error) => {
  process.stderr.write(`run-probes: ${error.message}\n`);
  process.exitCode = 2;
});
