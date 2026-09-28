import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { writeFakeCli } from './fakes.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');
const H12 = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';
const BRIEF = `# Goal\n\nDo the slice.\n\n# Expected result\n\nThe slice is done.\n\n# Owned files\n\nscripts/x.mjs\n\n# Forbidden\n\nDo not commit, push, or open a PR.\n\n# Report\n\nDone.\n`;
const AMEND = '# Change\n\nUpdate the slice.\n';
const HERDR = `import fs from 'node:fs';
const a = process.argv.slice(2);
const cmd = a.slice(0, 2).join(' ');
if (cmd === 'agent list') process.stdout.write('{"result":{"agents":[{"name":"build","pane_id":"p-build"}]}}\\n');
else if (cmd === 'agent prompt') {
  if (process.env.FAKE_PROMPT_FAIL && fs.existsSync(process.env.FAKE_PROMPT_FAIL)) { process.stderr.write('refused prompt\\n'); process.exit(1); }
  process.stdout.write('{"result":{"submitted":true}}\\n');
} else if (cmd === 'agent get') process.stdout.write('{"result":{"agent":{"name":"build","agent_status":"idle"}}}\\n');
else { process.stderr.write('unexpected fake command: ' + a.join(' ') + '\\n'); process.exit(1); }
`;

function makeFix(prefix) {
  const root = fs.realpathSync(fs.mkdtempSync(path.join(os.tmpdir(), prefix)));
  const repo = path.join(root, 'repo');
  const stateRoot = path.join(root, 'state');
  const ws = path.join(stateRoot, 'ws');
  const bin = path.join(root, 'bin');
  const tmp = path.join(root, 'tmp');
  for (const d of [repo, ws, path.join(ws, 'briefs'), path.join(ws, 'reports'), path.join(ws, 'wait'), bin, tmp,
    path.join(root, 'home'), path.join(root, 'conf')]) fs.mkdirSync(d, { recursive: true });
  spawnSync('git', ['init', '-q'], { cwd: repo, stdio: 'ignore', timeout: 30_000 });
  writeFakeCli(bin, 'herdr', HERDR);
  const fail = path.join(root, 'fail-prompt');
  const env = {
    HOME: path.join(root, 'home'), XDG_CONFIG_HOME: path.join(root, 'conf'), TMPDIR: tmp,
    HERDR_SOHO_DIR: stateRoot, HERDR_WORKSPACE_ID: 'ws', HERDR_ENV: '1',
    HERDR_SOHO_PROMPT_CHECK_SECONDS: '0', HERDR_SOHO_WAIT_POLL_MS: '20',
    FAKE_PROMPT_FAIL: fail, PATH: `${bin}${path.delimiter}${process.env.PATH}`,
  };
  fs.writeFileSync(path.join(ws, 'agents.tsv'), H12 + `build\tp-build\tclaude\timplementer\tanthropic\t1\t${repo}\tnow\tclaude-3\tfull\timplementer\t\n`);
  const stub = path.join(root, 'deny-symlink.mjs');
  fs.writeFileSync(stub, `import fs from 'node:fs';\nconst deny = () => { const e = new Error('symlink denied'); e.code = 'EPERM'; throw e; };\nfs.symlinkSync = deny;\nObject.defineProperty(fs.promises, 'symlink', { value: deny, configurable: true });\n`);
  const brief = path.join(root, 'brief.md');
  const amend = path.join(root, 'amend.md');
  fs.writeFileSync(brief, BRIEF);
  fs.writeFileSync(amend, AMEND);
  const run = (args, { noSymlink = false } = {}) => {
    const preload = noSymlink ? (process.versions.bun ? ['--preload', stub] : ['--import', stub]) : [];
    return spawnSync(process.execPath, [...preload, ENTRY, ...args], { cwd: repo, env, encoding: 'utf8', timeout: 60_000 });
  };
  return { root, repo, ws, env, fail, stub, brief, amend, run, cleanup() { fs.rmSync(root, { recursive: true, force: true }); } };
}

function dispatch(fix, amendment = false, noSymlink = false) {
  const r = fix.run(['dispatch', 'build', amendment ? fix.amend : fix.brief, '--no-wait', ...(amendment ? ['--amend'] : [])], { noSymlink });
  assert.equal(r.status, 0, `${r.stdout}${r.stderr}`);
  const value = JSON.parse(r.stdout.trim());
  const existing = fs.existsSync(pointer(fix)) ? readPointer(fix).task_report : null;
  const expected = existing ?? path.join(fix.ws, 'reports', `${path.basename(value.report, '.md')}.current.md`);
  assert.equal(value.task_report, expected);
  return value;
}

function pointer(fix) { return path.join(fix.ws, 'task-report-build.json'); }
function readPointer(fix) { return JSON.parse(fs.readFileSync(pointer(fix), 'utf8')); }
function writeReport(p, text) { fs.writeFileSync(p, text); }
function waitDone(fix, noSymlink = false) {
  const r = fix.run(['wait', 'build', '--timeout', '10000'], { noSymlink });
  assert.equal(r.status, 0, `${r.stdout}${r.stderr}`);
  return JSON.parse(r.stdout.trim().split('\n').at(-1));
}

// Paths are checked separately below; this helper's public-contract assertion only checks key order.
function assertTaskReportAfterReport(value) {
  const keys = Object.keys(value);
  assert.equal(keys.indexOf('task_report'), keys.indexOf('report') + 1, keys.join(', '));
}

test('task report is stable across two amendments, wait and collect; clean and stats ignore the copy and pointer', { timeout: 60_000 }, () => {
  const f = makeFix('ha-task-report-');
  try {
    const first = dispatch(f);
    assertTaskReportAfterReport(first);
    const stable = first.task_report;
    assert.equal(fs.existsSync(stable), false, 'the stable copy is absent while the task is active');
    assert.deepEqual(readPointer(f), { version: 1, task_report: stable, current: first.report, history: [] });
    const body1 = '# first report\n';
    writeReport(first.report, body1);
    const done1 = waitDone(f);
    assert.equal(done1.task_report, stable);
    // Mutation captured: skipping the done-time sync leaves no authoritative task copy.
    assert.equal(fs.readFileSync(stable, 'utf8'), body1);

    const second = dispatch(f, true);
    assert.equal(second.task_report, stable);
    // Mutation captured: retaining the completed snapshot during an amendment leaves stale task content visible.
    assert.equal(fs.existsSync(stable), false, 'an amendment in flight removes the stable copy');
    writeReport(second.report, '# amendment one\n');
    const third = dispatch(f, true);
    assert.equal(third.task_report, stable);
    assert.deepEqual(readPointer(f).history, [first.report, second.report]);
    writeReport(third.report, '# amendment two\n');
    const done3 = waitDone(f);
    assertTaskReportAfterReport(done3);
    assert.equal(done3.report, third.report);
    assert.equal(done3.task_report, stable);
    assert.equal(fs.readFileSync(stable, 'utf8'), '# amendment two\n');
    assert.deepEqual(readPointer(f), { version: 1, task_report: stable, current: third.report, history: [first.report, second.report] });
    for (const p of [first.report, second.report, third.report]) assert.equal(fs.existsSync(p), true, `versioned report remains: ${p}`);
    assert.equal(fs.lstatSync(stable).isSymbolicLink(), false);
    const stableMtime = fs.statSync(stable).mtimeMs;
    const collected = f.run(['collect', 'build']);
    assert.equal(collected.status, 0, collected.stderr);
    assert.ok(collected.stdout.startsWith(`<!-- report: ${third.report} -->\n<!-- task report: ${stable} -->\n`), collected.stdout);
    assert.equal(fs.readFileSync(stable, 'utf8'), '# amendment two\n');
    assert.equal(fs.statSync(stable).mtimeMs, stableMtime, 'sync does not rewrite identical content');

    const old = new Date(Date.now() - 3 * 86400_000);
    fs.utimesSync(stable, old, old);
    const clean = f.run(['clean', '--older-than', '0']);
    assert.equal(clean.status, 0, `${clean.stdout}${clean.stderr}`);
    assert.equal(fs.existsSync(stable), true, 'clean preserves task snapshots');
    assert.equal(fs.existsSync(pointer(f)), true, 'clean preserves the task pointer');
    const stats = f.run(['stats', '--json']);
    assert.equal(stats.status, 0, stats.stderr);
    const report = JSON.parse(stats.stdout);
    assert.equal(report.roles.implementer.tasks, 1);
    assert.equal(report.roles.implementer.amendments, 2);
    // Mutation captured: admitting .current.md as a report/prompt pair changes stats or clean's preservation assertion.
  } finally { f.cleanup(); }
});

test('--amend creates a task pointer for a legacy task without one', { timeout: 60_000 }, () => {
  const f = makeFix('ha-task-report-legacy-amend-');
  try {
    const oldReport = path.join(f.ws, 'reports', 'build-20260926T101010.md');
    fs.writeFileSync(oldReport, '# legacy version\n');
    fs.writeFileSync(path.join(f.ws, 'last-report-build'), `${oldReport}\n`);
    const amended = dispatch(f, true);
    const stable = path.join(f.ws, 'reports', `${path.basename(amended.report, '.md')}.current.md`);
    // Mutation captured: anchoring an untracked legacy task to its old attempt instead of this first tracked dispatch picks the wrong stable destination.
    assert.equal(amended.task_report, stable);
    assert.deepEqual(readPointer(f), { version: 1, task_report: stable, current: amended.report, history: [oldReport] });
    assert.equal(fs.existsSync(stable), false);
    writeReport(amended.report, '# legacy task amended\n');
    const done = waitDone(f);
    assert.equal(done.task_report, stable);
    assert.equal(fs.readFileSync(stable, 'utf8'), '# legacy task amended\n');
    assert.equal(fs.readFileSync(oldReport, 'utf8'), '# legacy version\n');
  } finally { f.cleanup(); }
});

test('failed transport restores last-report, pointer and stable copy byte-for-byte', { timeout: 60_000 }, () => {
  const f = makeFix('ha-task-report-fail-');
  try {
    const first = dispatch(f);
    writeReport(first.report, '# completed report\n');
    waitDone(f);
    const lastPath = path.join(f.ws, 'last-report-build');
    const before = [fs.readFileSync(lastPath), fs.readFileSync(pointer(f)), fs.readFileSync(first.task_report)];
    fs.writeFileSync(f.fail, '1');
    const failed = f.run(['dispatch', 'build', f.amend, '--amend', '--no-wait']);
    assert.equal(failed.status, 4, `${failed.stdout}${failed.stderr}`);
    assert.deepEqual(fs.readFileSync(lastPath), before[0]);
    assert.deepEqual(fs.readFileSync(pointer(f)), before[1]);
    assert.deepEqual(fs.readFileSync(first.task_report), before[2]);
    // Mutation captured: leaving the new target in last-report or the task pointer after a rejected send fails these byte comparisons.
  } finally { f.cleanup(); }
});

test('dispatch-amend-done-collect works when all symlink creation APIs fail with EPERM', { timeout: 60_000 }, () => {
  const f = makeFix('ha-task-report-no-symlink-');
  try {
    const first = dispatch(f, false, true);
    assert.equal(fs.existsSync(first.task_report), false);
    writeReport(first.report, '# original\n');
    const second = dispatch(f, true, true);
    assert.equal(fs.existsSync(second.task_report), false);
    writeReport(second.report, '# amended\n');
    const done = waitDone(f, true);
    assert.equal(done.task_report, first.task_report);
    const collected = f.run(['collect', 'build'], { noSymlink: true });
    assert.equal(collected.status, 0, `${collected.stdout}${collected.stderr}`);
    assert.equal(fs.readFileSync(first.task_report, 'utf8'), '# amended\n');
    assert.equal(fs.lstatSync(first.task_report).isSymbolicLink(), false);
    // Mutation captured: replacing the regular-file copy with symlink creation fails under this EPERM preload.
  } finally { f.cleanup(); }
});
