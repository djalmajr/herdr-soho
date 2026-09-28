// Mutation captured: removing the runner's jq preflight lets Bash suites
// start without jq instead of exiting 2 with the actionable dependency error.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const RUNNER = path.join(SCRIPTS, 'run-tests.sh');
const SKIP = process.platform === 'win32' ? 'run-tests.sh requires a POSIX shell' : false;

test('run-tests: missing jq exits 2 with Node-only guidance', { skip: SKIP }, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-run-tests-no-jq-'));
  try {
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin);
    fs.symlinkSync('/bin/bash', path.join(bin, 'bash'));
    fs.symlinkSync('/usr/bin/dirname', path.join(bin, 'dirname'));
    fs.symlinkSync('/bin/cat', path.join(bin, 'cat'));
    const r = spawnSync('/bin/bash', [RUNNER, '--env', 'outside', 'test-status.sh', 'test-setup.sh'], {
      encoding: 'utf8',
      env: { PATH: bin },
      timeout: 30_000,
    });
    assert.equal(r.status, 2, `${r.stdout}${r.stderr}`);
    assert.equal(r.stdout, '');
    assert.equal(r.stderr, 'run-tests.sh: needs jq (the bash suites use it); install it or run the Node suites only\n');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('Bash suite: missing jq exits 2 with a direct dependency message', { skip: SKIP }, () => {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-bash-suite-no-jq-'));
  try {
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin);
    const r = spawnSync('/bin/bash', [path.join(SCRIPTS, 'test-status.sh')], {
      encoding: 'utf8',
      env: { PATH: bin },
      timeout: 30_000,
    });
    assert.equal(r.status, 2, `${r.stdout}${r.stderr}`);
    assert.equal(r.stdout, '');
    assert.equal(r.stderr, 'test-status.sh: needs jq\n');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
