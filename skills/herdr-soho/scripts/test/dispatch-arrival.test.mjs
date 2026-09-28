import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { writeFakeCli } from './fakes.mjs';
import { nodeBin } from './parity.mjs';

const SCRIPTS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const JS_ENTRY = path.join(SCRIPTS, 'herdr-soho.mjs');

function sidecarForPrompt(prompt) {
  const basename = path.basename(prompt);
  const suffix = basename.endsWith('.brief.md') ? '.brief.md' : '.md';
  return path.join(path.dirname(prompt), `${basename.slice(0, -suffix.length)}.dispatch.json`);
}

const HERDR_FAKE = `
import fs from 'node:fs';
const argv = process.argv.slice(2);
if (process.env.FAKE_LOG) fs.appendFileSync(process.env.FAKE_LOG, argv.join(' ') + '\\n');
const cmd = (argv[0] ?? '') + ' ' + (argv[1] ?? '');
const target = argv[2] ?? '';

if (cmd === 'agent get') {
  let mode = 'idle';
  try { mode = fs.readFileSync(process.env.FAKE_MODE, 'utf8').trim(); } catch {}
  let seqJson = '';
  try {
    const s = fs.readFileSync(process.env.FAKE_SEQ, 'utf8').trim();
    if (s !== '') seqJson = ', "state_change_seq": ' + s;
  } catch {}
  let readyJson = '';
  try {
    const r = fs.readFileSync(process.env.FAKE_READY, 'utf8').trim();
    if (r === 'true') readyJson = ', "interactive_ready": true';
    else if (r === 'false') readyJson = ', "interactive_ready": false';
  } catch {}
  process.stdout.write('{"result":{"agent":{"name":"' + target + '","agent_status":"' + mode + '"' + readyJson + seqJson + '}}}\\n');
} else if (cmd === 'agent read') {
  const isRecent = argv.includes('recent-unwrapped');
  if (isRecent && process.env.FAKE_RECENT) {
    try {
      process.stdout.write(fs.readFileSync(process.env.FAKE_RECENT, 'utf8'));
      process.exit(0);
    } catch {}
  }
  let screen = '';
  try { screen = fs.readFileSync(process.env.FAKE_SCREEN, 'utf8'); } catch {}
  process.stdout.write(screen);
} else if (cmd === 'agent prompt') {
  let count = 0;
  try { count = parseInt(fs.readFileSync(process.env.FAKE_PROMPT_COUNT, 'utf8').trim(), 10) || 0; } catch {}
  count += 1;
  try { fs.writeFileSync(process.env.FAKE_PROMPT_COUNT, String(count) + '\\n'); } catch {}

  const hook = process.env.FAKE_PROMPT_HOOK;
  if (hook && fs.existsSync(hook)) {
    try {
      const code = fs.readFileSync(hook, 'utf8');
      const fn = new Function('argv', 'count', 'process', 'fs', code);
      fn(argv, count, process, fs);
    } catch (e) {
      process.stderr.write('hook error: ' + e + '\\n');
    }
  }
  process.stdout.write('{"result":{"submitted":true}}\\n');
} else if (cmd === 'agent send-keys') {
  process.stdout.write('{"result":{"keys_sent":true}}\\n');
} else if (cmd === 'pane report-metadata' || cmd === 'pane rename') {
  process.stdout.write('{"result":{}}\\n');
} else {
  process.stdout.write('{"result":{}}\\n');
}
`;

function makeFix(prefix) {
  let root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  root = fs.realpathSync(root);
  const bin = path.join(root, 'bin');
  const repo = path.join(root, 'repo');
  const state = path.join(root, 'state');
  const ws = path.join(state, 'ws');
  for (const d of [bin, repo, ws, path.join(ws, 'briefs'), path.join(ws, 'reports'), path.join(ws, 'wait'),
    path.join(root, 'home'), path.join(root, 'conf'), path.join(root, 'tmp')]) {
    fs.mkdirSync(d, { recursive: true });
  }
  spawnSync('git', ['init', '-q'], { cwd: repo, stdio: 'ignore', timeout: 30_000 });
  writeFakeCli(bin, 'herdr', HERDR_FAKE);
  const env = {
    HOME: path.join(root, 'home'),
    USERPROFILE: path.join(root, 'home'),
    XDG_CONFIG_HOME: path.join(root, 'conf'),
    TMPDIR: path.join(root, 'tmp'),
    HERDR_SOHO_DIR: state,
    HERDR_WORKSPACE_ID: 'ws',
    HERDR_ENV: '1',
    HERDR_SOHO_REGRID: 'off',
    HERDR_SOHO_WAIT_POLL_MS: '20',
    HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
    HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    FAKE_MODE: path.join(root, 'mode'),
    FAKE_SEQ: path.join(root, 'seq'),
    FAKE_READY: path.join(root, 'ready'),
    FAKE_SCREEN: path.join(root, 'screen'),
    FAKE_RECENT: path.join(root, 'recent'),
    FAKE_LOG: path.join(root, 'herdr.log'),
    FAKE_PROMPT_COUNT: path.join(root, 'prompt-count'),
    FAKE_PROMPT_HOOK: path.join(root, 'prompt-hook.js'),
    PATH: `${bin}${path.delimiter}${process.env.PATH}`,
  };
  fs.writeFileSync(env.FAKE_MODE, 'idle\n');
  fs.writeFileSync(env.FAKE_SEQ, '1\n');
  fs.writeFileSync(env.FAKE_SCREEN, 'screen-initial\n');
  fs.writeFileSync(env.FAKE_PROMPT_COUNT, '0\n');

  const H12 = '# name\tpane\tkind\trole\tfamily\tcreated_pane\tcwd\tstarted\tmodel\tapprovals\troles\tlane\n';
  const ROW = 'build\tp1\tagy\timplementer\tgoogle\t1\t' + repo + '\tnow\tgemini\task\t\tbuild\n';
  fs.writeFileSync(path.join(ws, 'agents.tsv'), H12 + ROW);

  const fix = {
    root, repo, state, ws, env,
    brief(name, body) {
      const p = path.join(root, name);
      fs.writeFileSync(p, body);
      return p;
    },
    log() {
      try { return fs.readFileSync(env.FAKE_LOG, 'utf8'); } catch { return ''; }
    },
    cleanup() { fs.rmSync(root, { recursive: true, force: true }); },
  };
  return fix;
}

function cmd(fix, args, extraEnv = {}) {
  return spawnSync(nodeBin(), [JS_ENTRY, ...args], {
    cwd: fix.repo,
    env: { ...fix.env, ...extraEnv },
    encoding: 'utf8',
    timeout: 60_000,
  });
}

const BRIEF = `# Goal\n\nPerform dispatch arrival verification.\n\n# Expected result\n\nPrompt received.\n\n# Owned files\n\nskills/herdr-soho/scripts/lib/dispatch.mjs\n\n# Forbidden\n\nDo not commit or push.\n\n# Report\n\ndone.\n`;

// (a) alvo que pisca working com o mesmo state_change_seq e muda a tela sozinho,
// e perde o primeiro prompt -> um reenvio; perde de novo -> not-received exit 15
test('arrival (a): target blinks working with same seq and changing screen, loses both prompts -> 1 resend and not-received exit 15', { timeout: 60000 }, () => {
  const fix = makeFix('ha-arr-a-');
  try {
    const brief = fix.brief('brief.md', BRIEF);
    // Target is working with unchanging seq 1. On prompt, screen redraws itself without prompt path.
    fs.writeFileSync(fix.env.FAKE_MODE, 'working\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      const fs = require('node:fs');
      // Redraws screen with boot messages on each prompt
      fs.writeFileSync(process.env.FAKE_SCREEN, 'boot welcome redraw ' + count + '\\n');
      fs.writeFileSync(process.env.FAKE_RECENT, 'boot welcome redraw ' + count + '\\nline 2\\nline 3\\nline 4\\n');
    `);

    // Mutation captured: accepting working with unchanged state_change_seq, or accepting any screen change
    // without checking composed path outside input box, would exit 0 instead of 15 without resending.
    const r = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r.status, 15, `expected exit 15, got ${r.status}. stderr: ${r.stderr}`);
    const j = JSON.parse(r.stdout.trim().split('\n').pop());
    assert.equal(j.wait_status, 'not-received');
    assert.match(r.stderr, /prompt to 'build' did not arrive \(screen unchanged, agent not working\); sending it once more/);
    assert.match(r.stderr, /prompt to 'build' was not received after one resend/);

    const logLines = fix.log().split('\n').filter((l) => l.startsWith('agent prompt '));
    assert.equal(logLines.length, 2, 'expected exactly 2 prompt attempts (initial + 1 resend)');

    const sc = JSON.parse(fs.readFileSync(sidecarForPrompt(j.composed_prompt), 'utf8'));
    assert.equal(sc.submission, 'accepted');
    assert.equal(sc.arrival, 'not-received');
  } finally { fix.cleanup(); }
});

// (b) o mesmo alvo que recebe o reenvio (seq muda) -> recebido, um reenvio
test('arrival (b): target blinks working with same seq on prompt 1, but receives resend (seq changes) -> received with 1 resend', { timeout: 60000 }, () => {
  const fix = makeFix('ha-arr-b-');
  try {
    const brief = fix.brief('brief.md', BRIEF);
    fs.writeFileSync(fix.env.FAKE_MODE, 'working\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      if (count === 1) {
        // First prompt lost: redraws screen, seq stays 1
        fs.writeFileSync(process.env.FAKE_SCREEN, 'boot welcome redraw 1\\n');
      } else {
        // Second prompt received: state_change_seq increments to 2
        fs.writeFileSync(process.env.FAKE_SEQ, '2\\n');
        fs.writeFileSync(process.env.FAKE_SCREEN, 'thinking...\\n');
      }
    `);

    // Mutation captured: failing to recheck seq on resend or accepting seq=1 on prompt 1 would report 0 resends or exit 15.
    const r = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r.status, 0, `expected exit 0, got ${r.status}. stderr: ${r.stderr}`);
    const j = JSON.parse(r.stdout.trim().split('\n').pop());
    assert.equal(j.wait_status, 'submitted');
    assert.equal(j.resent, true, 'expected resent: true in dispatch JSON');

    const logLines = fix.log().split('\n').filter((l) => l.startsWith('agent prompt '));
    assert.equal(logLines.length, 2, 'expected exactly 2 prompt attempts (initial + 1 resend)');
  } finally { fix.cleanup(); }
});

// (c) alvo normal -> recebido sem reenvio
test('arrival (c): normal target turns working with changed seq on first prompt -> received without resend', { timeout: 60000 }, () => {
  const fix = makeFix('ha-arr-c-');
  try {
    const brief = fix.brief('brief.md', BRIEF);
    fs.writeFileSync(fix.env.FAKE_MODE, 'idle\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      fs.writeFileSync(process.env.FAKE_MODE, 'working\\n');
      fs.writeFileSync(process.env.FAKE_SEQ, '2\\n');
      fs.writeFileSync(process.env.FAKE_SCREEN, 'working on prompt...\\n');
    `);

    // Mutation captured: requiring extra prompts or failing to recognize moved seq would cause resend or exit 15.
    const r = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r.status, 0, `expected exit 0, got ${r.status}. stderr: ${r.stderr}`);
    const j = JSON.parse(r.stdout.trim().split('\n').pop());
    assert.equal(j.wait_status, 'submitted');
    assert.equal('resent' in j, false, 'normal arrival must not have resent key');

    const logLines = fix.log().split('\n').filter((l) => l.startsWith('agent prompt '));
    assert.equal(logLines.length, 1, 'expected exactly 1 prompt attempt');

    // Sub-case c2: target stays idle with unchanged seq, but writes non-empty report -> received via Rule 1 (non-empty report) without resend
    fs.writeFileSync(fix.env.FAKE_MODE, 'idle\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_LOG, '');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      const text = argv[3] || '';
      const m = text.match(/write your report to (\\S+) and reply/);
      if (m) fs.writeFileSync(m[1], '# Report\\n\\ndone.\\n');
    `);

    // Mutation captured: removing the non-empty report check in Rule 1 causes an idle agent with completed report to fail arrival (exit 15).
    const r2 = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r2.status, 0, `expected exit 0, got ${r2.status}. stderr: ${r2.stderr}`);
    const j2 = JSON.parse(r2.stdout.trim().split('\n').pop());
    assert.equal(j2.wait_status, 'submitted');
    assert.equal(j2.report_exists, true);
    assert.equal('resent' in j2, false, 'arrival via existing non-empty report must not resend');

    const logLines2 = fix.log().split('\n').filter((l) => l.startsWith('agent prompt '));
    assert.equal(logLines2.length, 1, 'expected exactly 1 prompt attempt for report arrival');
  } finally { fix.cleanup(); }
});

// (d) a espera de assentamento espera interactive_ready e duas telas iguais antes do primeiro agent prompt,
// e respeita prompt_settle_seconds=0
test('arrival (d): settle wait waits for interactive_ready and two identical visible screens before prompt 1, respects settle=0', { timeout: 60000 }, () => {
  const fix = makeFix('ha-arr-d-');
  try {
    const brief = fix.brief('brief.md', BRIEF);

    // Sub-case d1: settle wait is active (prompt_settle_seconds=3).
    // Target starts with interactive_ready=false. Screen changes between calls.
    // Fake dynamically changes ready to true and screens to identical after 1 settle probe.
    fs.writeFileSync(fix.env.FAKE_READY, 'false\n');
    fs.writeFileSync(fix.env.FAKE_SCREEN, 'booting-1\n');
    fs.writeFileSync(fix.env.FAKE_MODE, 'working\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');

    // A hook on herdr fake execution: simulate settling after first check
    // We can handle this by having a probe counter in a file
    const probeFile = path.join(fix.root, 'probe-count');
    fs.writeFileSync(probeFile, '0\n');

    // Update FAKE_PROMPT_HOOK for prompt arrival
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      fs.writeFileSync(process.env.FAKE_SEQ, '2\\n');
    `);

    // We can customize the fake herdr to settle after probe 1
    // Let's create an external settle controller in the fake:
    const settleCtrl = path.join(fix.root, 'settle-ctrl.js');
    fs.writeFileSync(settleCtrl, `
      const fs = require('node:fs');
      const pf = '${probeFile}';
      let c = parseInt(fs.readFileSync(pf, 'utf8').trim(), 10) || 0;
      c += 1;
      fs.writeFileSync(pf, String(c) + '\\n');
      if (c >= 2) {
        fs.writeFileSync(process.env.FAKE_READY, 'true\\n');
        fs.writeFileSync(process.env.FAKE_SCREEN, 'booting-settled\\n');
      }
    `);

    // Mutation captured: sending prompt before interactive_ready is true or before two identical screens
    // causes agent prompt to be called before settling.
    const r1 = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '5',
      NODE_OPTIONS: `--require ${settleCtrl}`,
    });

    assert.equal(r1.status, 0, `expected exit 0, got ${r1.status}. stderr: ${r1.stderr}`);
    const log1 = fix.log().split('\n').filter((l) => l !== '');
    const firstPromptIdx1 = log1.findIndex((l) => l.startsWith('agent prompt '));
    assert.ok(firstPromptIdx1 > 0, 'first agent prompt must happen after settle probes');

    // Settle probes before prompt: at least 2 settle reads + 2 pre-send reads = >= 4 reads
    const prePromptCalls = log1.slice(0, firstPromptIdx1);
    assert.ok(prePromptCalls.filter((l) => l.startsWith('agent get ')).length >= 2, 'expected at least 2 agent gets before prompt (settle + preSeq)');
    assert.ok(prePromptCalls.filter((l) => l.startsWith('agent read ')).length >= 4, 'expected at least 4 agent reads before prompt (settle reads + pre-send reads)');

    // Sub-case d2: prompt_settle_seconds=0 disables settle check completely
    fs.writeFileSync(fix.env.FAKE_LOG, '');
    fs.writeFileSync(fix.env.FAKE_READY, 'false\n'); // even if ready is false
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      fs.writeFileSync(process.env.FAKE_SEQ, '2\\n');
    `);

    const r2 = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r2.status, 0, `expected exit 0, got ${r2.status}. stderr: ${r2.stderr}`);
    const log2 = fix.log().split('\n').filter((l) => l !== '');
    // With settle=0, the very first agent prompt has no settle reads before it
    const promptIdx2 = log2.findIndex((l) => l.startsWith('agent prompt '));
    // The only reads before prompt when checkOn=1 are H0 and preAuthCauses
    const readsBefore = log2.slice(0, promptIdx2).filter((l) => l.startsWith('agent read '));
    assert.equal(readsBefore.length, 2, 'only H0 and preAuthCauses read before prompt when settle=0');
    const getsBefore = log2.slice(0, promptIdx2).filter((l) => l.startsWith('agent get '));
    assert.equal(getsBefore.length, 1, 'only preSeq get before prompt when settle=0');

    // Sub-case d3: target does not settle within prompt_settle_seconds -> warns and sends anyway
    fs.writeFileSync(fix.env.FAKE_LOG, '');
    fs.writeFileSync(fix.env.FAKE_READY, 'false\n');
    fs.writeFileSync(fix.env.FAKE_SCREEN, 'never-settling\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      fs.writeFileSync(process.env.FAKE_SEQ, '2\\n');
    `);

    const r3 = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '1',
    });

    assert.equal(r3.status, 0, `expected exit 0, got ${r3.status}. stderr: ${r3.stderr}`);
    assert.match(r3.stderr, /prompt to 'build' did not settle in 1s; sending anyway/);
  } finally { fix.cleanup(); }
});

// (e) tela mudada com o caminho do prompt visível fora das últimas 3 linhas -> recebido sem reenvio
test('arrival (e): screen changed and composed prompt path visible outside last 3 lines -> received without resend', { timeout: 60000 }, () => {
  const fix = makeFix('ha-arr-e-');
  try {
    const brief = fix.brief('brief.md', BRIEF);
    // Target stays idle, seq stays 1 (no rule 1 arrival).
    // Prompt hook extracts composed prompt path and renders it outside the last 3 non-empty lines in recent-unwrapped.
    fs.writeFileSync(fix.env.FAKE_MODE, 'idle\n');
    fs.writeFileSync(fix.env.FAKE_SEQ, '1\n');
    fs.writeFileSync(fix.env.FAKE_SCREEN, 'initial visible screen\n');
    fs.writeFileSync(fix.env.FAKE_PROMPT_HOOK, `
      const text = argv[3] || '';
      const m = text.match(/Read the file (.+?) in full/);
      const composedPath = m ? m[1] : '/composed/not/found';

      // Visible screen changes
      fs.writeFileSync(process.env.FAKE_SCREEN, 'screen after prompt submission\\n');

      // Recent unwrapped screen: composedPath is outside the last 3 non-empty lines!
      const recentContent = [
        'Transcript history header',
        'User prompt arrived:',
        'Read the file ' + composedPath + ' in full and execute it.',
        'Executing prompt instructions...',
        'Line 5 output',
        '> input line 1',
        '> input line 2',
        '> input line 3'
      ].join('\\n') + '\\n';
      fs.writeFileSync(process.env.FAKE_RECENT, recentContent);
    `);

    // Mutation captured: ignoring composed path or requiring rule 1 seq change would cause resend or exit 15.
    const r = cmd(fix, ['dispatch', 'build', brief, '--no-wait'], {
      HERDR_SOHO_PROMPT_CHECK_SECONDS: '1',
      HERDR_SOHO_PROMPT_SETTLE_SECONDS: '0',
    });

    assert.equal(r.status, 0, `expected exit 0, got ${r.status}. stderr: ${r.stderr}`);
    const j = JSON.parse(r.stdout.trim().split('\n').pop());
    assert.equal(j.wait_status, 'submitted');
    assert.equal('resent' in j, false, 'strict rule 4 arrival must not resend');

    const logLines = fix.log().split('\n').filter((l) => l.startsWith('agent prompt '));
    assert.equal(logLines.length, 1, 'expected exactly 1 prompt attempt (no resend)');
  } finally { fix.cleanup(); }
});
