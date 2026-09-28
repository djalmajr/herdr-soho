import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { forSpecFamily } from '../lib/dispatch.mjs';
import { DieError } from '../lib/config.mjs';
import { writeFakeCli } from './fakes.mjs';

const scripts = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const ENTRY = path.join(scripts, 'herdr-soho.mjs');
const H12 = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';
function fixture() {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-for-released-'));
  const sd = path.join(root, 'state', 'ws');
  const tmp = path.join(root, 'tmp', 'herdr-soho', 'ws', 'reports');
  const bin = path.join(root, 'bin');
  fs.mkdirSync(path.join(sd, 'briefs'), { recursive: true });
  fs.mkdirSync(path.join(sd, 'reports'), { recursive: true });
  fs.mkdirSync(tmp, { recursive: true });
  fs.mkdirSync(bin, { recursive: true });
  writeFakeCli(bin, 'herdr', '');
  const env = {
    HERDR_WORKSPACE_ID: 'ws', HERDR_SOHO_DIR: path.dirname(sd), TMPDIR: path.join(root, 'tmp'),
    PATH: `${bin}${path.delimiter}${path.dirname(process.execPath)}`,
  };
  const ctx = {};
  const add = (agent, ts, kind, model, submission = 'accepted', dir = path.join(sd, 'briefs')) => {
    const stem = `${agent}-${ts}`;
    const filename = `${stem}${dir === tmp ? '.brief.md' : '.md'}`;
    const prompt = path.join(dir, filename);
    fs.writeFileSync(prompt, '# prompt\n');
    fs.writeFileSync(path.join(dir, `${stem}.dispatch.json`), JSON.stringify({ version: 1, kind, model, effort: '', submission }));
  };
  return { root, sd, tmp, env, ctx, add, roster(...rows) { fs.writeFileSync(path.join(sd, 'agents.tsv'), H12 + rows.join('\n') + '\n'); }, cleanup() { fs.rmSync(root, { recursive: true, force: true }); } };
}
const ts = (n) => `20260927T1010${String(n).padStart(2, '0')}`;
const check = (fix, spec) => forSpecFamily(spec, fix.sd, fix.env, fix.root, fix.ctx);

 test('released author with coherent accepted sidecars resolves its family', () => {
  const f = fixture();
  try {
    f.roster();
    f.add('build', ts(1), 'pi', 'anthropic/claude-3');
    f.add('build', ts(2), 'cursor', 'claude-opus-4', 'accepted', f.tmp);
    // Mutation captured: omitting either routed metadata directory loses one accepted record.
    assert.equal(check(f, 'build'), 'anthropic');
  } finally { f.cleanup(); }
});

test('released author with no accepted sidecar gets the usage diagnostic', () => {
  const f = fixture();
  try {
    f.roster();
    f.add('build', ts(1), 'claude', '', 'failed');
    // Mutation captured: counting failed/attempted sidecars would resolve this author instead of erroring.
    assert.throws(() => check(f, 'build'), (e) => e instanceof DieError && e.code === 2
      && e.message === "dispatch: --for 'build': not an agent in the roster, a family (anthropic|openai|xai|google), a kind with a fixed family, or an agent with an accepted dispatch recorded in this workspace");
  } finally { f.cleanup(); }
});

test('released author rejects disagreeing or unknown recorded families with counts', () => {
  const f = fixture();
  try {
    f.roster();
    f.add('build', ts(1), 'pi', 'anthropic/claude-3');
    f.add('build', ts(2), 'codex', '', 'accepted', f.tmp);
    // Mutation captured: accepting the most recent family or skipping the TMPDIR scan hides the historical disagreement.
    assert.throws(() => check(f, 'build'), (e) => e instanceof DieError && e.code === 2
      && e.message === "dispatch: --for 'build': the recorded dispatches of this released agent do not agree on one known model family (anthropic ×1, openai ×1); pass the family instead (anthropic|openai|xai|google)");
    f.add('build', ts(3), 'pi', '');
    assert.throws(() => check(f, 'build'), (e) => e instanceof DieError && e.code === 2 && e.message.includes('(anthropic ×1, openai ×1, unknown ×1)'));
  } finally { f.cleanup(); }
});

test('a reviewer is refused when a released author shares its family', () => {
  const f = fixture();
  try {
    const reviewer = 'reviewer\tp-review\tclaude\treviewer\tanthropic\t1\t/tmp\tnow\tclaude-3\tfull\treviewer\t';
    f.roster(reviewer);
    f.add('build', ts(1), 'pi', 'anthropic/claude-3');
    const brief = path.join(f.root, 'review.md');
    fs.writeFileSync(brief, '# Goal\n\nReview this.\n\n# Expected result\n\nReviewed.\n\n# Forbidden\n\nDo not commit or push.\n\n# Report\n\nDone.\n');
    const r = spawnSync(process.execPath, [ENTRY, 'dispatch', 'reviewer', brief, '--for', 'build', '--no-wait'], {
      cwd: f.root, env: { ...f.env, HERDR_ENV: '1' }, encoding: 'utf8', timeout: 30_000,
    });
    // Mutation captured: bypassing released-author metadata lets a same-family review dispatch through instead of exit 5.
    assert.equal(r.status, 5, `${r.stdout}${r.stderr}`);
    assert.match(r.stderr, /shares a model family with the slice's author\(s\): build \(anthropic\)/);
  } finally { f.cleanup(); }
});

test('--for prefers a live roster entry over its recorded dispatches', () => {
  const f = fixture();
  try {
    f.roster('build\tp-build\tcodex\timplementer\topenai\t1\t/tmp\tnow\t\tfull\timplementer\t');
    f.add('build', ts(1), 'claude', '');
    // Mutation captured: consulting released metadata before the live roster changes openai to anthropic.
    assert.equal(check(f, 'build'), 'openai');
  } finally { f.cleanup(); }
});

test('released agent names match exactly before the timestamp', () => {
  const f = fixture();
  try {
    f.roster();
    f.add('build-2', ts(1), 'claude', '');
    // Mutation captured: prefix matching would let build inherit build-2's accepted dispatch.
    assert.throws(() => check(f, 'build'), (e) => e instanceof DieError && e.code === 2 && e.message.includes('not an agent in the roster'));
  } finally { f.cleanup(); }
});
