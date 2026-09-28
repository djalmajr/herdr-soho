// herdr client (slice 3): the agentState classification of spec 4.1
// (agent_not_found → gone; every other failure → unavailable with a
// sanitized cause; success without agent_status → unavailable; normal
// status; the result's state_change_seq as `seq` when it is an integer,
// else `''`), paneTitle (pane id before the options; never throws) and the
// small read helpers. A fake `herdr` on PATH answers per target; the real
// CLI is never used.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { nodeBin } from './parity.mjs';
import { writeFakeCli, sleepingFake } from './fakes.mjs';
import { agentState, paneTitle, requireEnv, liveAgents, paneList, tabList, agentRead } from '../lib/herdr.mjs';

const HERDR_MJS = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'lib', 'herdr.mjs');
const HERDR_URL = pathToFileURL(HERDR_MJS).href;
const CONFIG_URL = pathToFileURL(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', 'lib', 'config.mjs')).href;

// Bash variables stay plain in the template; only `${` (a bash parameter
// expansion) is escaped for the JS template literal.
const NL = '\\n'; // a literal \n in the generated bash (printf interprets it)
const ESC = '\\033'; // a literal \033 (bash printf octal escape)

// The fake logs every call ("$*") and answers `agent get` per target.
function makeFake(root) {
  const bin = path.join(root, 'bin');
  fs.mkdirSync(bin, { recursive: true });
  const source = `
import fs from 'node:fs';
import path from 'node:path';
const root = process.env.FAKE_HERDR_ROOT;
const argv = process.argv.slice(2);
fs.appendFileSync(path.join(root, 'herdr.log'), argv.join(' ') + '\\n');
const target = argv[2] ?? '';
const out = (value) => process.stdout.write(JSON.stringify(value) + '\\n');
const fail = (value) => { process.stderr.write(JSON.stringify(value) + '\\n'); process.exit(1); };
if (argv[0] === 'agent' && argv[1] === 'get') {
  switch (target) {
    case 'notfound': fail({ error: { code: 'agent_not_found', message: 'agent target notfound not found' }, id: 'cli:agent:get' }); break;
    case 'serverdown': fail({ id: 'cli:agent:get', error: { code: 'server_not_running', message: 'no herdr server is running' } }); break;
    case 'nostatus': out({ result: { agent: { name: 'nostatus' } } }); break;
    case 'messy': process.stderr.write('Error: Os { code: 13, kind: PermissionDenied, message: "Permission denied" }\\nsecond-line\\t\\u001b[31mred\\n'); process.exit(1); break;
    case 'flaky': {
      const counter = path.join(root, 'flaky.count');
      const n = Number(fs.existsSync(counter) ? fs.readFileSync(counter, 'utf8') : 0) + 1;
      fs.writeFileSync(counter, String(n));
      if (n <= 2) process.exit(137);
      out({ result: { agent: { name: 'flaky', agent_status: 'working' } } });
      break;
    }
    case 'killed': process.exit(137); break;
    case 'termed': process.exit(143); break;
    case 'slow': setTimeout(() => {}, 5000); break;
    case 'ok': out({ result: { agent: { name: 'ok', agent_status: 'working' } } }); break;
    case 'okseq': out({ result: { agent: { name: 'okseq', agent_status: 'idle', state_change_seq: 3 } } }); break;
    case 'okseqstr': out({ result: { agent: { name: 'okseqstr', agent_status: 'idle', state_change_seq: '7' } } }); break;
    case 'failmeta': process.stderr.write('meta boom'); process.exit(1); break;
    default: process.stderr.write('unexpected target ' + target + '\\n'); process.exit(1);
  }
} else if (argv[0] === 'agent' && argv[1] === 'list') {
  out({ result: { agents: [{ name: 'a', pane_id: 'p1', agent_status: 'working', agent: 'grok' }] } });
} else if (argv[0] === 'pane' && argv[1] === 'list') {
  if (fs.existsSync(path.join(root, 'panefail'))) { process.stderr.write('pane fail'); process.exit(1); }
  out({ result: { panes: [{ pane_id: 'p1', tab_id: 't1' }] } });
} else if (argv[0] === 'tab' && argv[1] === 'list') {
  out({ result: { tabs: [{ tab_id: 't1', label: 'herd-1' }] } });
} else if (argv[0] === 'agent' && argv[1] === 'read') {
  process.stdout.write('screen text\\n');
} else if (argv[0] === 'pane' && argv[1] === 'report-metadata') {
  if (fs.existsSync(path.join(root, 'metafail'))) { process.stderr.write('meta boom'); process.exit(1); }
} else {
  process.stderr.write('unexpected: ' + argv.join(' ') + '\\n');
  process.exit(1);
}
`;
  writeFakeCli(bin, 'herdr', source);
  return {
    bin,
    env: { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}`, USERPROFILE: root, FAKE_HERDR_ROOT: root },
    log: path.join(root, 'herdr.log'),
  };
}

function tmp(prefix) {
  let root = fs.mkdtempSync(path.join(os.tmpdir(), prefix));
  return fs.realpathSync(root);
}

test('agentState: agent_not_found is the only gone', () => {
  const root = tmp('ha-herdr-gone-');
  try {
    const fake = makeFake(root);
    assert.deepEqual(agentState('notfound', fake.env), { state: 'gone', cause: '', seq: '' });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: another error code is unavailable with "<code>: <message>"', () => {
  const root = tmp('ha-herdr-code-');
  try {
    const fake = makeFake(root);
    assert.deepEqual(agentState('serverdown', fake.env), { state: 'unavailable', cause: 'server_not_running: no herdr server is running', seq: '' });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: success without agent_status is unavailable', () => {
  const root = tmp('ha-herdr-nostatus-');
  try {
    const fake = makeFake(root);
    assert.deepEqual(agentState('nostatus', fake.env), { state: 'unavailable', cause: 'agent get returned no agent_status', seq: '' });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: failure without JSON sanitizes the cause (tabs, control chars)', () => {
  const root = tmp('ha-herdr-messy-');
  try {
    const fake = makeFake(root);
    const out = agentState('messy', fake.env);
    assert.equal(out.state, 'unavailable');
    assert.equal(out.cause, 'Error: Os { code: 13, kind: PermissionDenied, message: "Permission denied" } second-line [31mred');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: normal success returns the agent_status', () => {
  const root = tmp('ha-herdr-ok-');
  try {
    const fake = makeFake(root);
    assert.deepEqual(agentState('ok', fake.env), { state: 'working', cause: '', seq: '' });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: state_change_seq is returned when it is an integer, else seq is empty', () => {
  const root = tmp('ha-herdr-seq-');
  try {
    const fake = makeFake(root);
    // Mutation captured: not parsing state_change_seq (or accepting a
    // non-integer one) fails the seq asserts below.
    assert.deepEqual(agentState('okseq', fake.env), { state: 'idle', cause: '', seq: 3 }, 'integer seq is returned');
    assert.deepEqual(agentState('okseqstr', fake.env), { state: 'idle', cause: '', seq: '' }, 'a string seq is not an integer');
    assert.deepEqual(agentState('ok', fake.env), { state: 'working', cause: '', seq: '' }, 'no field: empty seq');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('agentState: never throws when herdr is missing (unavailable, exit 127)', () => {
  const env = { ...process.env, PATH: '/nonexistent' };
  assert.deepEqual(agentState('any', env), { state: 'unavailable', cause: 'herdr agent get failed (exit 127)', seq: '' });
});

test('paneTitle: pane id before the options, title and clear recorded verbatim', () => {
  const root = tmp('ha-herdr-title-');
  try {
    const fake = makeFake(root);
    paneTitle('p9', 'implementer: porte da config', fake.env);
    paneTitle('p9', null, fake.env);
    const lines = fs.readFileSync(fake.log, 'utf8').trim().split('\n');
    assert.deepEqual(lines, [
      'pane report-metadata p9 --source herdr-soho --title implementer: porte da config',
      'pane report-metadata p9 --source herdr-soho --clear-title',
    ]);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('paneTitle: a failing herdr changes nothing (best effort, no throw)', () => {
  const root = tmp('ha-herdr-metafail-');
  try {
    const fake = makeFake(root);
    fs.writeFileSync(path.join(root, 'metafail'), '');
    assert.doesNotThrow(() => paneTitle('p9', 't', fake.env));
    assert.doesNotThrow(() => paneTitle('p9', null, fake.env));
    const lines = fs.readFileSync(fake.log, 'utf8').trim().split('\n');
    assert.equal(lines.length, 2);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

test('requireEnv: outside Herdr dies 2; herdr missing from PATH dies 2', () => {
  const code = `import { requireEnv } from '${HERDR_URL}'; requireEnv(process.env); console.log('ok');`;
  const r1 = spawnSync(nodeBin(), ['--input-type=module', '-e', code], {
    env: { ...process.env, HERDR_ENV: '' },
    encoding: 'utf8',
  });
  assert.equal(r1.status, 2);
  assert.equal(r1.stderr, 'herdr-soho: not running inside Herdr (HERDR_ENV != 1); refusing to control a session from outside\n');
  assert.ok(!r1.stdout.includes('ok'));
  const r2 = spawnSync(nodeBin(), ['--input-type=module', '-e', code], {
    env: { ...process.env, HERDR_ENV: '1', PATH: '/nonexistent' },
    encoding: 'utf8',
  });
  assert.equal(r2.status, 2);
  assert.equal(r2.stderr, 'herdr-soho: herdr CLI not found in PATH\n');
});

test('liveAgents / paneList / tabList / agentRead: the JSON fields the roster reads', () => {
  const root = tmp('ha-herdr-lists-');
  try {
    const fake = makeFake(root);
    const agents = liveAgents(fake.env);
    assert.deepEqual(agents, [{ name: 'a', pane_id: 'p1', agent_status: 'working', agent: 'grok' }]);
    assert.deepEqual(paneList(fake.env, 'ws'), [{ pane_id: 'p1', tab_id: 't1' }]);
    assert.deepEqual(tabList(fake.env, 'ws'), [{ tab_id: 't1', label: 'herd-1' }]);
    assert.equal(agentRead(fake.env, 'a', { source: 'visible', lines: 20 }), 'screen text\n');
    const calls = fs.readFileSync(fake.log, 'utf8').trim().split('\n');
    assert.ok(calls.includes('agent read a --source visible --lines 20'));
    // A failing pane list falls back to [] (bash `|| echo '[]'`).
    fs.writeFileSync(path.join(root, 'panefail'), '');
    assert.deepEqual(paneList(fake.env, 'ws'), []);
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A stuck herdr (no answer within the ceiling) is a herdr failure, never a
// hang: agentState reports it as unavailable. Decision 6: liveAgents does
// not exit — it throws a DieError (message → the entry dies with code 4);
// a herdr failure passes the CLI output through and throws a DieError with
// an empty message and herdr's code (the entry exits with the code only).
test('herdr timeout: agentState is unavailable and liveAgents throws DieError 4', { timeout: 60000 }, () => {
  const root = tmp('ha-herdr-timeout-');
  try {
    const bin = path.join(root, 'bin');
    fs.mkdirSync(bin);
    writeFakeCli(bin, 'herdr', sleepingFake(10000));
    const env = { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}` };
    assert.deepEqual(agentState('any', env, 300), { state: 'unavailable', cause: 'herdr agent get timed out after 0.3s', seq: '' });
    const code = `import { liveAgents } from '${HERDR_URL}';
import { DieError } from '${CONFIG_URL}';
try { liveAgents(process.env, 300); console.log('returned'); }
catch (e) { if (e instanceof DieError) { process.stderr.write('die ' + e.code + ' ' + e.message); process.exit(e.code); } throw e; }`;
    const r = spawnSync(nodeBin(), ['--input-type=module', '-e', code], { env, encoding: 'utf8', timeout: 30_000 });
    assert.equal(r.status, 4, `rc=${r.status} stderr=${r.stderr}`);
    assert.equal(r.stderr, 'die 4 herdr agent list timed out after 0.3s');
    assert.ok(!r.stdout.includes('returned'));
    // A failing herdr: output passed through, empty-message DieError with
    // herdr's code (the entry then exits with that code and no message).
    // The fake answers at once, so the cap is generous (30 s) to stay
    // stable under load; it is never reached here.
    const code2 = `import { liveAgents } from '${HERDR_URL}';
import { DieError } from '${CONFIG_URL}';
try { liveAgents(process.env, 30000); console.log('returned'); }
catch (e) { if (e instanceof DieError) { process.stderr.write('die ' + e.code + '|' + e.message); process.exit(e.code); } throw e; }`;
    const r2 = spawnSync(nodeBin(), ['--input-type=module', '-e', code2], { env: failListEnv(root), encoding: 'utf8', timeout: 30_000 });
    assert.equal(r2.status, 3, `rc=${r2.status} stderr=${r2.stderr}`);
    assert.ok(r2.stderr.startsWith('boom'), `passthrough first, then the DieError: ${r2.stderr}`);
    assert.ok(r2.stderr.endsWith('die 3|'), r2.stderr);
    assert.ok(!r2.stderr.includes('herdr-soho:'), 'no die message of its own');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});

// A fake herdr whose `agent list` prints `boom` to stderr and exits 3.
function failListEnv(root) {
  const bin = path.join(root, 'bin3');
  fs.mkdirSync(bin, { recursive: true });
  writeFakeCli(bin, 'herdr', `if (process.argv[2] === 'agent') { process.stderr.write('boom\\n'); process.exit(3); }
`);
  return { ...process.env, PATH: `${bin}${path.delimiter}${process.env.PATH}` };
}

test('agentState: a kill by a signal (exit 137) is retried before unavailable', { timeout: 30000 }, () => {
  const root = tmp('ha-herdr-137-');
  try {
    const fake = makeFake(root);
    const gets = () => fs.readFileSync(fake.log, 'utf8').split('\n').filter((l) => l.startsWith('agent get')).length;
    // Two kills, then the answer: the third try reports the real state.
    assert.deepEqual(agentState('flaky', fake.env, undefined, [10, 10]), { state: 'working', cause: '', seq: '' });
    assert.equal(gets(), 3, 'three agent get calls');
    // Killed every time: unavailable after the retries, with the signal
    // and the likely cause named (not the bare exit code).
    // Mutation captured: the signal-kill cause reverted to the bare
    // `herdr agent get failed (exit 137)` breaks the exact strings below.
    fs.writeFileSync(fake.log, '');
    assert.deepEqual(agentState('killed', fake.env, undefined, [10, 10]),
      { state: 'unavailable', cause: 'herdr agent get was killed (exit 137, SIGKILL: memory pressure or an external kill)', seq: '' });
    assert.equal(gets(), 3, 'three agent get calls');
    // An external SIGTERM is retried too, and named like the SIGKILL case.
    fs.writeFileSync(fake.log, '');
    assert.deepEqual(agentState('termed', fake.env, undefined, [10, 10]),
      { state: 'unavailable', cause: 'herdr agent get was killed (exit 143, SIGTERM: memory pressure or an external kill)', seq: '' });
    assert.equal(gets(), 3, 'three agent get calls');
    // Our own timeout is not retried.
    fs.writeFileSync(fake.log, '');
    const slow = agentState('slow', fake.env, 2_000, [10, 10]);
    assert.equal(slow.state, 'unavailable');
    assert.match(slow.cause, /timed out/);
    assert.equal(gets(), 1, 'one call for our own timeout');
    // A structured error is never retried.
    fs.writeFileSync(fake.log, '');
    assert.equal(agentState('serverdown', fake.env, undefined, [10, 10]).state, 'unavailable');
    assert.equal(gets(), 1, 'one call for a structured error');
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
});
