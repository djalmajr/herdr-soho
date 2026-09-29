import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { cmdStats } from '../../../skills/herdr-soho/scripts/lib/commands/stats.mjs';

const here = path.dirname(fileURLToPath(import.meta.url));
const root = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-stats-gen-'));
const state = path.join(root, 'state', 'ws');
const briefs = path.join(state, 'briefs');
const reports = path.join(state, 'reports');
const env = { HERDR_SOHO_DIR: path.join(root, 'state'), HERDR_WORKSPACE_ID: 'ws', HERDR_SOHO_NOWRITE: '1', TMPDIR: path.join(root, 'tmp') };
fs.mkdirSync(briefs, { recursive: true });
fs.mkdirSync(reports, { recursive: true });
fs.mkdirSync(path.join(env.TMPDIR, 'herdr-soho', 'ws', 'reports'), { recursive: true });
const stamp = '20260928T120000';
const prompt = path.join(briefs, `alice-${stamp}.md`);
const report = path.join(reports, `alice-${stamp}.md`);
fs.writeFileSync(prompt, 'You are running as the `reviewer` role\n');
fs.writeFileSync(report, 'findings: 1 (p0 0, p1 0, p2 1, p3 0) | verdict: pass\n- [partial] fixture\n');
fs.writeFileSync(path.join(briefs, `alice-${stamp}.dispatch.json`), JSON.stringify({ version: 1, submission: 'accepted', kind: 'claude', model: 'sonnet', effort: 'high' }));
const amendment = path.join(briefs, 'alice-20260928T120001.md');
const amendmentReport = path.join(reports, 'alice-20260928T120001.md');
fs.writeFileSync(amendment, '# Amendment to your current brief\n');
fs.writeFileSync(amendmentReport, 'amended report\n');
const time = new Date('2026-09-28T12:00:00Z');
for (const file of [prompt, amendment, report, amendmentReport]) fs.utimesSync(file, time, time);
fs.writeFileSync(path.join(state, 'agents.tsv'), '# name\tpane\n');

const capture = (args) => {
  let text = '';
  const original = process.stdout.write;
  process.stdout.write = (chunk) => { text += chunk; return true; };
  try { cmdStats(args, { entries: new Map() }, env, root); }
  finally { process.stdout.write = original; }
  return text;
};
const fixture = { text: capture([]), json: capture(['--json']) };
fs.writeFileSync(path.join(here, 'stats.json'), `${JSON.stringify(fixture, null, 2)}\n`);
fs.rmSync(root, { recursive: true, force: true });
