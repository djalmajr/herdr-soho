// Tests for Codex environment policy (Decision 1) and runtime ancestry check (Decision 2).
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { nodeBin } from './parity.mjs';
import { writeFakeCli } from './fakes.mjs';
import {
  parseCodexPolicy,
  evaluateCodexPolicy,
  globMatch,
  doctorCodexPolicyWarnings,
  diagnoseOutsideHerdr,
  getProcessAncestors,
} from '../lib/codex-env.mjs';
import { requireEnv } from '../lib/herdr.mjs';
import { DoctorSay, doctorCheck } from '../lib/commands/doctor.mjs';
import { loadConfig } from '../lib/config.mjs';

const HERDR_MJS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'lib', 'herdr.mjs');
const HERDR_URL = pathToFileURL(HERDR_MJS).href;

function tmp(prefix) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  return fs.realpathSync(root);
}

// ---------------------------------------------------------------------------
// Decision 1: Checagem estática (doctor)
// ---------------------------------------------------------------------------

test('globMatch: case-insensitive with *', () => {
  assert.equal(globMatch('HERDR_*', 'HERDR_ENV'), true);
  assert.equal(globMatch('herdr_*', 'HERDR_ENV'), true);
  assert.equal(globMatch('*', 'HERDR_ENV'), true);
  assert.equal(globMatch('HERDR_ENV', 'herdr_env'), true);
  assert.equal(globMatch('PATH', 'HERDR_ENV'), false);
  assert.equal(globMatch('*ENV', 'HERDR_ENV'), true);
});

// Mutation captured: ignoring inherit="core" drops the warning when Codex config omits include_only.
test('Decision 1 case: core (inherit="core" drops HERDR_*)', () => {
  const root = tmp('ha-codex-core-');
  try {
    const conf = path.join(root, 'config.toml');
    fs.writeFileSync(conf, '[shell_environment_policy]\ninherit = "core"\n');
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 1);
    assert.equal(say.okCount, 0);
    // Mutation captured: changing the reason string from inherit="core" breaks expected advisory text.
    const policy = parseCodexPolicy(fs.readFileSync(conf, 'utf8'));
    const evaluation = evaluateCodexPolicy(policy);
    assert.equal(evaluation.drops, true);
    assert.equal(evaluation.reason, 'inherit="core"');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: omitting inherit="none" check allows inherit="none" to silently pass without warning.
test('Decision 1 case: none (inherit="none" drops HERDR_*)', () => {
  const root = tmp('ha-codex-none-');
  try {
    const conf = path.join(root, 'config.toml');
    fs.writeFileSync(conf, '[shell_environment_policy]\ninherit = "none"\n');
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 1);
    const policy = parseCodexPolicy(fs.readFileSync(conf, 'utf8'));
    const evaluation = evaluateCodexPolicy(policy);
    assert.equal(evaluation.drops, true);
    assert.equal(evaluation.reason, 'inherit="none"');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: skipping include_only check fails to warn when HERDR_* is excluded from include_only.
test('Decision 1 case: include_only without HERDR_*', () => {
  const root = tmp('ha-codex-inc-without-');
  try {
    const conf = path.join(root, 'config.toml');
    fs.writeFileSync(conf, '[shell_environment_policy]\ninherit = "all"\ninclude_only = ["HOME", "PATH"]\n');
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 1);
    const policy = parseCodexPolicy(fs.readFileSync(conf, 'utf8'));
    const evaluation = evaluateCodexPolicy(policy);
    assert.equal(evaluation.drops, true);
    assert.equal(evaluation.reason, 'include_only without HERDR_*');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: requiring exact match instead of glob case-insensitivity warns even when HERDR_* is present.
test('Decision 1 case: include_only with HERDR_* (no warning)', () => {
  const root = tmp('ha-codex-inc-with-');
  try {
    const conf = path.join(root, 'config.toml');
    fs.writeFileSync(conf, '[shell_environment_policy]\ninherit = "all"\ninclude_only = ["HOME", "PATH", "herdr_*"]\n');
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 0);
    const policy = parseCodexPolicy(fs.readFileSync(conf, 'utf8'));
    const evaluation = evaluateCodexPolicy(policy);
    assert.equal(evaluation.drops, false);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: ignoring exclude patterns allows HERDR_* to be dropped without warning.
test('Decision 1 case: exclude (exclude matches HERDR_ENV)', () => {
  const root = tmp('ha-codex-exclude-');
  try {
    const conf = path.join(root, 'config.toml');
    fs.writeFileSync(conf, '[shell_environment_policy]\ninherit = "all"\nexclude = ["HERDR_*"]\n');
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 1);
    const policy = parseCodexPolicy(fs.readFileSync(conf, 'utf8'));
    const evaluation = evaluateCodexPolicy(policy);
    assert.equal(evaluation.drops, true);
    assert.equal(evaluation.reason, 'exclude matches HERDR_*');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: throwing or warning on missing config.toml treats absent config as a policy error.
test('Decision 1 case: missing file (produces no warning)', () => {
  const root = tmp('ha-codex-missing-');
  try {
    const say = new DoctorSay();
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(say.warnCount, 0);
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: single-line regex fails to parse multi-line TOML arrays and drops patterns.
test('Decision 1 case: multi-line array parsing', () => {
  const tomlPass = `
[shell_environment_policy]
inherit = "all"
include_only = [
  "HOME",
  "LANG",
  "LOGNAME",
  "PATH",
  "SHELL",
  "USER",
  "USERNAME",
  "TMPDIR",
  "TEMP",
  "TMP",
  "HERDR_*"
]
`;
  const policyPass = parseCodexPolicy(tomlPass);
  assert.equal(policyPass.inherit, 'all');
  assert.ok(policyPass.include_only.includes('HERDR_*'));
  assert.equal(evaluateCodexPolicy(policyPass).drops, false);

  const tomlFail = `
[shell_environment_policy]
inherit = "all"
include_only = [
  "HOME",
  "LANG", # comment
  "PATH"
]
`;
  const policyFail = parseCodexPolicy(tomlFail);
  assert.equal(policyFail.inherit, 'all');
  assert.equal(policyFail.include_only.length, 3);
  assert.equal(evaluateCodexPolicy(policyFail).drops, true);
  assert.equal(evaluateCodexPolicy(policyFail).reason, 'include_only without HERDR_*');
});

// Mutation captured: printing unparsed TOML sections leaks private tokens to stdout.
test('Decision 1 case: other content not printed (tokens preserved privately)', () => {
  const root = tmp('ha-codex-tokens-');
  try {
    const conf = path.join(root, 'config.toml');
    const secretContent = `
[api_tokens]
openai = "sk-super-secret-token-12345"
anthropic = "sk-ant-secret-key-67890"

[shell_environment_policy]
inherit = "core"
`;
    fs.writeFileSync(conf, secretContent);
    const captured = [];
    const say = {
      warn(msg) { captured.push(msg); },
      ok() {},
    };
    doctorCodexPolicyWarnings({ CODEX_HOME: root }, say);
    assert.equal(captured.length, 1);
    assert.equal(captured[0], 'codex: shell_environment_policy drops HERDR_* (inherit="core"): commands Codex runs cannot see Herdr; see the Codex section of docs/guide.md');
    for (const msg of captured) {
      assert.ok(!msg.includes('sk-super-secret-token-12345'), 'must never print secret token');
      assert.ok(!msg.includes('api_tokens'), 'must never print other sections');
    }
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// Decision 2: Checagem em tempo de execução
// ---------------------------------------------------------------------------

function setupFakes(root, { treeTable, snapshotPanes, processInfos }) {
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin, { recursive: true });

  // Fake ps: receives -o ppid=,comm= -p <pid>
  const treeFile = path.join(root, 'ps_tree.json');
  fs.writeFileSync(treeFile, JSON.stringify(treeTable ?? {}));
  const psSource = `
import fs from 'node:fs';
const pIdx = process.argv.indexOf('-p');
const pid = pIdx !== -1 ? process.argv[pIdx + 1] : '';
const treeFile = process.env.FAKE_PS_TREE;
if (treeFile && fs.existsSync(treeFile)) {
  const table = JSON.parse(fs.readFileSync(treeFile, 'utf8'));
  const entry = table[pid];
  if (entry) {
    process.stdout.write(entry.ppid + ' ' + entry.comm + '\\n');
    process.exit(0);
  }
}
process.exit(1);
`;
  writeFakeCli(bin, 'ps', psSource);

  // Fake herdr
  const logFile = path.join(root, 'herdr.log');
  const snapFile = path.join(root, 'snapshot.json');
  fs.writeFileSync(snapFile, JSON.stringify({ result: { snapshot: { panes: snapshotPanes ?? [] } } }));

  const procInfoDir = path.join(root, 'proc_info');
  fs.mkdirSync(procInfoDir, { recursive: true });
  if (processInfos) {
    for (const [paneId, info] of Object.entries(processInfos)) {
      fs.writeFileSync(path.join(procInfoDir, `${paneId}.json`), JSON.stringify({ result: { process_info: info } }));
    }
  }

  const herdrSource = `
import fs from 'node:fs';
import path from 'node:path';
const root = process.env.FAKE_ROOT;
const argv = process.argv.slice(2);
fs.appendFileSync(path.join(root, 'herdr.log'), argv.join(' ') + '\\n');
if (argv[0] === 'api' && argv[1] === 'snapshot') {
  const data = fs.readFileSync(path.join(root, 'snapshot.json'), 'utf8');
  process.stdout.write(data);
  process.exit(0);
} else if (argv[0] === 'pane' && argv[1] === 'process-info') {
  const paneIdx = argv.indexOf('--pane');
  const paneId = paneIdx !== -1 ? argv[paneIdx + 1] : '';
  const file = path.join(root, 'proc_info', paneId + '.json');
  if (fs.existsSync(file)) {
    process.stdout.write(fs.readFileSync(file, 'utf8'));
    process.exit(0);
  }
  process.exit(1);
} else {
  process.stderr.write('unexpected herdr call: ' + argv.join(' ') + '\\n');
  process.exit(1);
}
`;
  writeFakeCli(bin, 'herdr', herdrSource);

  return {
    bin,
    logFile,
    env: {
      ...process.env,
      PATH: `${bin}${path.delimiter}${process.env.PATH}`,
      FAKE_ROOT: root,
      FAKE_PS_TREE: treeFile,
      HERDR_SOCKET_PATH: path.join(root, 'nonexistent.sock'),
      CODEX_HOME: path.join(root, 'codex_home'),
      HERDR_ENV: '',
    },
  };
}

// Mutation captured: invoking Herdr without a Codex ancestor causes unnecessary subprocess execution.
test('Decision 2 case: sem ancestral codex (nenhuma chamada ao Herdr)', () => {
  const root = tmp('ha-d2-nocodex-');
  try {
    const fakes = setupFakes(root, {
      treeTable: {
        '5000': { ppid: 4000, comm: 'node' },
        '4000': { ppid: 1, comm: 'bash' },
      },
    });

    const msg = diagnoseOutsideHerdr(
      'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside',
      fakes.env,
      'darwin',
      { pid: 5000 },
    );
    assert.equal(msg, 'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside');

    // Mutation captured: calling herdr when no codex ancestor exists records calls in herdr.log.
    assert.equal(fs.existsSync(fakes.logFile), false, 'herdr must not be called when no codex ancestor exists');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: omitting the matched pane diagnosis causes requireEnv to report a generic outside-Herdr error.
test('Decision 2 case: um painel casa (local/<pane> e exit 2)', () => {
  const root = tmp('ha-d2-one-match-');
  try {
    const fakes = setupFakes(root, {
      treeTable: {
        '5000': { ppid: 4000, comm: 'node' },
        '4000': { ppid: 3000, comm: 'bash' },
        '3000': { ppid: 1, comm: 'codex' },
      },
      snapshotPanes: [
        { pane_id: 'w14:p1', agent: 'codex' },
        { pane_id: 'w14:p2', agent: 'grok' },
      ],
      processInfos: {
        'w14:p1': {
          foreground_processes: [
            { pid: 3000, name: 'codex', argv: ['--secret-arg'], cmdline: 'codex --secret-token' },
          ],
        },
      },
    });

    // Test requireEnv via subprocess
    const code = `import { requireEnv } from '${HERDR_URL}';
try {
  requireEnv(process.env, { pid: 5000 });
  console.log('UNREACHABLE');
} catch (e) {
  // in normal execution die exits; when imported directly we check DieError or process exit
  console.error(e.message);
  process.exit(e.code ?? 1);
}`;
    const r = spawnSync(nodeBin(), ['--input-type=module', '-e', code], {
      env: fakes.env,
      encoding: 'utf8',
    });
    assert.equal(r.status, 2);
    assert.ok(r.stderr.includes('herdr-soho: this command runs under Codex in Herdr pane local/w14:p1 (matched by process ancestry), but Codex\'s shell_environment_policy does not pass HERDR_*; allow them (see herdr-soho doctor) and restart Codex\n'), r.stderr);
    assert.ok(!r.stdout.includes('UNREACHABLE'));

    // Mutation captured: logging or formatting process argv/cmdline exposes command line secrets.
    assert.ok(!r.stderr.includes('--secret-arg'), 'must never print argv');
    assert.ok(!r.stderr.includes('--secret-token'), 'must never print cmdline');
    assert.ok(!r.stdout.includes('--secret-arg'));
    assert.ok(!r.stdout.includes('--secret-token'));

    // Mutation captured: invoking pane current without HERDR_PANE_ID returns the focused pane instead of the caller.
    const calls = fs.readFileSync(fakes.logFile, 'utf8');
    assert.ok(!calls.includes('pane current'), 'pane current must never be called');
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: choosing an arbitrary pane when multiple match violates unambiguous identification.
test('Decision 2 case: dois paineis casam', () => {
  const root = tmp('ha-d2-two-matches-');
  try {
    const fakes = setupFakes(root, {
      treeTable: {
        '5000': { ppid: 3000, comm: 'node' },
        '3000': { ppid: 1, comm: 'codex' },
      },
      snapshotPanes: [
        { pane_id: 'w14:p1', agent: 'codex' },
        { pane_id: 'w14:p2', agent: 'codex' },
      ],
      processInfos: {
        'w14:p1': { foreground_processes: [{ pid: 3000, name: 'codex' }] },
        'w14:p2': { foreground_processes: [{ pid: 3000, name: 'codex' }] },
      },
    });

    const msg = diagnoseOutsideHerdr(
      'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside',
      fakes.env,
      'darwin',
      { pid: 5000 },
    );
    assert.equal(
      msg,
      'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside (a Codex ancestor was found, but no single Herdr pane matched it)',
    );

    const docMsg = diagnoseOutsideHerdr(
      'HERDR_ENV != 1: not inside a Herdr pane',
      fakes.env,
      'darwin',
      { pid: 5000 },
    );
    assert.equal(
      docMsg,
      'HERDR_ENV != 1: not inside a Herdr pane (a Codex ancestor was found, but no single Herdr pane matched it)',
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});

// Mutation captured: failing to match any pane despite codex ancestor must add the ambiguity suffix.
test('Decision 2 case: nenhum painel casa', () => {
  const root = tmp('ha-d2-no-match-');
  try {
    const fakes = setupFakes(root, {
      treeTable: {
        '5000': { ppid: 3000, comm: 'node' },
        '3000': { ppid: 1, comm: 'codex' },
      },
      snapshotPanes: [
        { pane_id: 'w14:p1', agent: 'codex' },
      ],
      processInfos: {
        'w14:p1': { foreground_processes: [{ pid: 9999, name: 'other' }] },
      },
    });

    const msg = diagnoseOutsideHerdr(
      'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside',
      fakes.env,
      'darwin',
      { pid: 5000 },
    );
    assert.equal(
      msg,
      'not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside (a Codex ancestor was found, but no single Herdr pane matched it)',
    );
  } finally {
    fs.rmSync(root, { recursive: true, force: true });
  }
});
