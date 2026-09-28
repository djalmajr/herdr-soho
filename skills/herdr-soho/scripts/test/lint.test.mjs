// `lint`: CLI parity with dispatch diagnostics, optional failure-matrix
// markers, read-only roles, and exit codes. Every child has a timeout and
// gets an isolated HOME/config/state tree.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { loadConfig } from '../lib/config.mjs';
import { lintBrief } from '../lib/dispatch.mjs';
import { nodeBin, fixtureEnv } from './parity.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');
const BRIEFS = path.join(SCRIPTS, 'test', 'fixtures', 'briefs');

function makeFix() {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), 'ha-lint-')));
  const state = path.join(root, 'state');
  fs.mkdirSync(state);
  const env = fixtureEnv({
    HOME: path.join(root, 'home'),
    XDG_CONFIG_HOME: path.join(root, 'config'),
    HERDR_SOHO_DIR: state,
    HERDR_WORKSPACE_ID: 'ws',
    TMPDIR: path.join(root, 'tmp'),
  });
  for (const d of [env.HOME, env.XDG_CONFIG_HOME, env.TMPDIR]) fs.mkdirSync(d, { recursive: true });
  return {
    root, state, env,
    brief(name, body) {
      const file = path.join(root, name);
      fs.writeFileSync(file, body);
      return file;
    },
    fixture(name) {
      const file = path.join(root, name);
      fs.copyFileSync(path.join(BRIEFS, name), file);
      return file;
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
}

function run(fix, args, extraEnv = {}) {
  const result = spawnSync(nodeBin(), [ENTRY, 'lint', ...args], {
    cwd: fix.root,
    env: { ...fix.env, ...extraEnv },
    encoding: 'utf8',
    timeout: 60_000,
  });
  return { status: result.status, stdout: result.stdout ?? '', stderr: result.stderr ?? '', error: result.error };
}

function stateFiles(root) {
  return fs.readdirSync(root, { withFileTypes: true }).flatMap((entry) => {
    const file = path.join(root, entry.name);
    return entry.isDirectory() ? stateFiles(file).map((child) => path.join(entry.name, child)) : [entry.name];
  }).sort();
}

// Mutation captured: dropping the empty-inline-code warning or its ordering
// makes the CLI diverge from the independently asserted dispatch diagnostic.
test('lint: emits dispatch warnings unchanged and creates no state', { timeout: 120_000 }, () => {
  const fix = makeFix();
  try {
    const brief = fix.brief('brief with spaces.md', [
      '# Goal', 'Do the slice.', '# Owned files', '- one file', '# Forbidden', 'Do not commit or push.', '# Report',
      '``',
    ].join('\n'));
    const env = { ...fix.env, HERDR_SOHO_BRIEF_LINT_ALIASES: 'not-an-alias' };
    const ctx = loadConfig(env, fix.root);
    const originalWrite = process.stderr.write;
    let dispatchStderr = '';
    process.stderr.write = (chunk) => { dispatchStderr += chunk; return true; };
    try { lintBrief(brief, ctx, env); }
    finally { process.stderr.write = originalWrite; }

    const expected = [
      "herdr-soho: warning: brief_lint_aliases: ignored 'not-an-alias' (use Section=Heading|Heading)\n",
      `herdr-soho: warning: brief ${brief} line 8 has empty inline code (\`\`): a shell heredoc without quotes may have run the backticks\n`,
      `herdr-soho: warning: brief ${brief} is missing sections: [Expected result] — nothing says when the slice is done\n`,
    ].join('');
    assert.equal(dispatchStderr, expected);

    const result = run(fix, [brief], { HERDR_SOHO_BRIEF_LINT_ALIASES: 'not-an-alias' });
    assert.equal(result.error, undefined);
    assert.equal(result.status, 1, result.stderr);
    assert.equal(result.stdout, '');
    assert.equal(result.stderr, dispatchStderr);
    assert.deepEqual(stateFiles(fix.state), []);
    assert.equal(fs.existsSync(path.join(fix.state, 'friction.log')), false);
  } finally { fix.cleanup(); }
});

// Mutation captured: counting markers outside the section or failing to
// require [clock] makes one of these independent boundary/completeness checks fail.
test('lint: checks only the optional failure matrix and explains missing markers in order', { timeout: 120_000 }, () => {
  const fix = makeFix();
  try {
    const incomplete = fix.fixture('retention.md');
    const missingClock = run(fix, [incomplete]);
    assert.equal(missingClock.status, 1, missingClock.stderr);
    assert.equal(missingClock.stdout, '');
    assert.equal(missingClock.stderr,
      `herdr-soho: warning: brief ${incomplete} failure matrix is missing: [clock] — a clock that goes backwards is not covered\n`);

    const complete = fix.brief('retention-complete.md',
      fs.readFileSync(incomplete, 'utf8').replace('- [retry]', '- [retry]\n- [clock] o relógio recua entre execuções e nenhum backup necessário é removido'));
    const allMarkers = run(fix, [complete]);
    assert.equal(allMarkers.status, 0, allMarkers.stderr);
    assert.equal(allMarkers.stdout, `brief ${complete}: ok\n`);
    assert.equal(allMarkers.stderr, '');

    const outside = fix.brief('marker-outside.md',
      fs.readFileSync(incomplete, 'utf8') + '\n# Depois\n[clock] O relógio recua.\n');
    const outsideMarker = run(fix, [outside]);
    assert.equal(outsideMarker.status, 1, outsideMarker.stderr);
    assert.match(outsideMarker.stderr, /failure matrix is missing: \[clock\]/);

    const noMarkers = fix.brief('matrix-empty.md',
      fs.readFileSync(incomplete, 'utf8').replace(/^- \[(crash|retry)\].*\n/gm, ''));
    const allMissing = run(fix, [noMarkers]);
    assert.equal(allMissing.status, 1, allMissing.stderr);
    assert.equal(allMissing.stderr,
      `herdr-soho: warning: brief ${noMarkers} failure matrix is missing: [crash] [retry] [clock] — a crash between publish and prune/delete is not covered; a repeated or retried step is not covered; a clock that goes backwards is not covered\n`);

    const plain = fix.fixture('plain.md');
    const ordinary = run(fix, [plain]);
    assert.equal(ordinary.status, 0, ordinary.stderr);
    assert.equal(ordinary.stdout, `brief ${plain}: ok\n`);
    assert.equal(ordinary.stderr, '');
  } finally { fix.cleanup(); }
});

// Mutation captured: requiring Owned files for a reviewer or returning the
// wrong status for strict/off/invalid arguments changes this observable contract.
test('lint: supports read-only roles, modes, and the defined error exits', { timeout: 120_000 }, () => {
  const fix = makeFix();
  try {
    const missingOwned = fix.brief('review.md', [
      '# Goal', 'Review.', '# Expected result', 'Findings reported.', '# Forbidden', 'Do not commit or push.', '# Report',
    ].join('\n'));
    const reviewer = run(fix, [missingOwned, '--role', 'reviewer']);
    assert.equal(reviewer.status, 0, reviewer.stderr);
    assert.equal(reviewer.stdout, `brief ${missingOwned}: ok\n`);
    assert.equal(reviewer.stderr, '');

    const incomplete = fix.fixture('retention.md');
    const off = run(fix, [incomplete], { HERDR_SOHO_BRIEF_LINT: 'off' });
    assert.equal(off.status, 0, off.stderr);
    assert.equal(off.stdout, `brief ${incomplete}: lint off (brief_lint=off)\n`);
    assert.equal(off.stderr, '');

    const strictMatrix = run(fix, [incomplete], { HERDR_SOHO_BRIEF_LINT: 'strict' });
    assert.equal(strictMatrix.status, 1, strictMatrix.stderr);
    assert.match(strictMatrix.stderr, /failure matrix is missing: \[clock\]/);
    const strict = run(fix, [missingOwned], { HERDR_SOHO_BRIEF_LINT: 'strict' });
    assert.equal(strict.status, 2, strict.stderr);
    assert.equal(strict.stderr,
      `herdr-soho: brief ${missingOwned} is missing sections: [Owned files] — workers without owned files collide (brief_lint=strict)\n`);

    const usage = run(fix, []);
    assert.equal(usage.status, 2);
    assert.equal(usage.stderr, 'herdr-soho: usage: lint <brief.md> [--role <role>]\n');
    for (const args of [[incomplete, '--role'], [incomplete, '--role', '--bogus'], [incomplete, '--bogus']]) {
      const invalid = run(fix, args);
      assert.equal(invalid.status, 2, invalid.stderr);
      assert.equal(invalid.stderr, 'herdr-soho: usage: lint <brief.md> [--role <role>]\n');
    }
    const missingFile = path.join(fix.root, 'absent brief.md');
    const absent = run(fix, [missingFile]);
    assert.equal(absent.status, 2);
    assert.equal(absent.stderr, `herdr-soho: lint: brief not found: ${missingFile}\n`);

    const unknown = run(fix, [incomplete, '--role', 'unknown-role']);
    assert.equal(unknown.status, 3);
    assert.equal(unknown.stderr, "herdr-soho: lint: unknown role 'unknown-role'\n");
    assert.deepEqual(stateFiles(fix.state), []);
  } finally { fix.cleanup(); }
});
