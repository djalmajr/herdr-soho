// Tests for the read-only plugin bridge (slice 1).
//
// Every test is hermetic: a fake `herdr` binary (for `pane get`) and a
// fake skill CLI (for `doctor`/`roster`) live in a temp dir, and every
// spawnSync the bridge performs against them carries an explicit
// timeout. The fakes record argv, cwd and the effective HERDR_* env so
// the tests assert the observable effects (target resolution, no wrong
// target, no CLI invocation on failure) instead of internal state.
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import {
  run,
  runPick,
  BridgeError,
  EXIT_INVALID_TARGET,
  EXIT_HERDR_FAILURE,
  ACTIONS,
  PICK_OPEN_ARGS,
} from '../bridge.mjs';

// ---------- fakes ----------

// A JS fake `<name>` behind the platform's launcher: `<name>.fake.mjs` plus
// a POSIX sh wrapper `<name>`, or `<name>.cmd` on Windows. Returns the
// launcher path.
function writeLauncherFake(dir, name, script) {
  const js = path.join(dir, `${name}.fake.mjs`);
  fs.writeFileSync(js, `${script}\n`);
  if (process.platform === 'win32') {
    const cmd = path.join(dir, `${name}.cmd`);
    fs.writeFileSync(cmd, `@"${process.execPath}" "%~dp0${name}.fake.mjs" %*\r\n`);
    return cmd;
  }
  const sh = path.join(dir, name);
  const q = (v) => `'${String(v).replace(/'/g, "'\\''")}'`;
  fs.writeFileSync(sh, `#!/bin/sh\nexec ${q(process.execPath)} ${q(js)} "$@"\n`, { mode: 0o755 });
  return sh;
}

// A fake `herdr` executable: optional sleep (for the timeout cases, the
// exit path is delayed so the process actually stays alive), optional
// argv recording, then stdout/stderr and exit code.
function writeFakeHerdr(dir, name, { exit = 0, stdout = '', stderr = '', sleepMs = 0, argvFile = null } = {}) {
  const actions = [
    argvFile ? `fs.writeFileSync(${JSON.stringify(argvFile)}, JSON.stringify(process.argv));` : '',
    `if (${JSON.stringify(stdout)} !== '') process.stdout.write(${JSON.stringify(stdout)});`,
    `if (${JSON.stringify(stderr)} !== '') process.stderr.write(${JSON.stringify(stderr)});`,
    `process.exit(${exit});`,
  ].filter(Boolean).join('\n');
  const script = sleepMs
    ? `import fs from 'node:fs';\nsetTimeout(() => {\n${actions}\n}, ${sleepMs});`
    : `import fs from 'node:fs';\n${actions}`;
  // A JS fake behind the platform's launcher (sh on POSIX, .cmd on
  // Windows, where a script cannot be spawned directly).
  return writeLauncherFake(dir, `herdr-${name}`, script);
}

// A fake skill CLI: records argv, cwd and the effective HERDR_* env (plus
// the PATH prefix the bridge built) in the marker file, then prints and
// exits with the configured code (delayed as a whole when sleepMs is set,
// so the timeout cases actually hang).
function writeFakeCli(dir, name, { exit = 0, stdout = '', stderr = '', sleepMs = 0 } = {}) {
  const actions = [
    `const marker = process.env.HERDR_FAKE_CLI_MARKER;`,
    `if (marker) fs.writeFileSync(marker, JSON.stringify({`,
    `  argv: process.argv,`,
    `  cwd: process.cwd(),`,
    `  env: {`,
    `    HERDR_WORKSPACE_ID: process.env.HERDR_WORKSPACE_ID ?? null,`,
    `    HERDR_TAB_ID: process.env.HERDR_TAB_ID ?? null,`,
    `    HERDR_PANE_ID: process.env.HERDR_PANE_ID ?? null,`,
    `    HERDR_SOHO_NOWRITE: process.env.HERDR_SOHO_NOWRITE ?? null,`,
    `  },`,
    `  pathPrefix: (process.env.PATH ?? '').slice(0, 4096),`,
    `}));`,
    `if (${JSON.stringify(stdout)} !== '') process.stdout.write(${JSON.stringify(stdout)});`,
    `if (${JSON.stringify(stderr)} !== '') process.stderr.write(${JSON.stringify(stderr)});`,
    `process.exit(${exit});`,
  ].filter(Boolean).join('\n');
  const script = sleepMs
    ? `import fs from 'node:fs';\nsetTimeout(() => {\n${actions}\n}, ${sleepMs});`
    : `import fs from 'node:fs';\n${actions}`;
  const file = path.join(dir, `cli-${name}.mjs`);
  fs.writeFileSync(file, `#!/usr/bin/env node\n${script}\n`, { mode: 0o755 });
  return file;
}

function makeTmp(t) {
  // realpath base: /var is a symlink on macOS and the child's
  // process.cwd() reports the resolved path.
  const dir = fs.mkdtempSync(path.join(fs.realpathSync(os.tmpdir()), 'herdr-soho-bridge-'));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

// node:test has no test.each: a plain loop with one test per row. A row
// is either a value array (spread into body) or a scalar (single value).
function each(rows, makeName, body) {
  for (const row of rows) {
    const values = Array.isArray(row) ? row : [row];
    test(makeName(values[0]), (t) => body(t, ...values));
  }
}

// The context JSON Herdr injects for a focused pane (the real shape from
// the socket API schema: workspace_id, tab_id, focused_pane_id, ...).
function contextJson(overrides = {}) {
  return JSON.stringify({
    workspace_id: 'wJ',
    tab_id: 'wJ:t1',
    focused_pane_id: 'wJ:p24',
    ...overrides,
  });
}

// Base env for a bridge run: HERDR_BIN_PATH + context, plus STALE shell
// HERDR_* ids on purpose — the context (UI focus) must win over them.
function baseEnv(herdrBin, { context = contextJson(), withBinPath = true, bin = '/opt/fake/herdr' } = {}) {
  const env = {
    HERDR_ENV: '1',
    HERDR_WORKSPACE_ID: 'wShell',
    HERDR_TAB_ID: 'wShell:t9',
    HERDR_PANE_ID: 'wShell:p9',
    PATH: '/usr/bin:/bin',
  };
  if (withBinPath) env.HERDR_BIN_PATH = herdrBin || bin;
  if (context !== undefined) env.HERDR_PLUGIN_CONTEXT_JSON = context;
  if (process.platform === 'win32') {
    // What Herdr hands a Windows action anyway: cmd.exe and its lookup
    // (a .cmd herdr runs through it), System32 on PATH, the temp dirs.
    for (const k of ['SystemRoot', 'ComSpec', 'PATHEXT', 'TEMP', 'TMP']) {
      if (process.env[k]) env[k] = process.env[k];
    }
    env.PATH = path.join(process.env.SystemRoot || 'C:\\Windows', 'System32');
  }
  return env;
}

// A well-formed `pane get` success for the focused pane, with the pane
// cwd inside the given dir.
function paneOk(dir, { workspaceId = 'wJ', paneId = 'wJ:p24', cwd = null } = {}) {
  return JSON.stringify({
    id: 'cli:pane:get',
    result: {
      pane: {
        pane_id: paneId,
        tab_id: 'wJ:t1',
        workspace_id: workspaceId,
        cwd: cwd ?? dir,
      },
    },
    type: 'pane_info',
  });
}

// Generous ceilings so a loaded machine cannot flake the happy paths;
// the timeout tests below use their own short values, so every fake
// subprocess still has an explicit timeout.
const TIME = { paneGetMs: 10_000, cliMs: 15_000 };

// assert.throws(fn, constructor) returns undefined on this Node version,
// so catch manually and check the class.
function bridgeErr(fn) {
  let e;
  try { fn(); } catch (err) { e = err; }
  assert.ok(e instanceof BridgeError, `expected BridgeError, got: ${e ? `${e.name}: ${e.message}` : 'nothing'}`);
  return e;
}

function escapeRegExp(s) {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

// ---------- happy paths: right target, CLI invoked in the pane cwd ----------

test('doctor: valid context invokes the CLI in the pane cwd with the context ids', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'ok', { stdout: paneOk(dir) });
  const cli = writeFakeCli(dir, 'ok', { stdout: 'CLI-DOCTOR-OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const r = run('doctor', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 0);
  assert.equal(r.err, '');
  assert.match(r.out, new RegExp(`herdr-soho plugin: target workspace=wJ pane=wJ:p24 cwd=${escapeRegExp(dir)}`));
  assert.match(r.out, /CLI-DOCTOR-OK/);
  const rec = JSON.parse(fs.readFileSync(marker, 'utf8'));
  assert.equal(rec.argv[rec.argv.length - 2], cli);
  assert.equal(rec.argv[rec.argv.length - 1], 'doctor');
  assert.equal(rec.cwd, dir); // the pane cwd, never the plugin cwd
  // The context ids win over the stale shell HERDR_* env.
  assert.equal(rec.env.HERDR_WORKSPACE_ID, 'wJ');
  assert.equal(rec.env.HERDR_PANE_ID, 'wJ:p24');
  assert.equal(rec.env.HERDR_TAB_ID, 'wJ:t1');
  // Read-only: the CLI runs in no-write mode (no .gitignore, no state tree).
  assert.equal(rec.env.HERDR_SOHO_NOWRITE, '1');
  // The herdr binary dir is prepended to the CLI's PATH.
  assert.equal(rec.pathPrefix.split(path.delimiter)[0], dir, rec.pathPrefix);
});

test('roster: valid context invokes the CLI with the roster subcommand', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'roster', { stdout: paneOk(dir) });
  const cli = writeFakeCli(dir, 'roster', { stdout: 'CLI-ROSTER-OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const r = run('roster', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 0);
  assert.match(r.out, /CLI-ROSTER-OK/);
  const rec = JSON.parse(fs.readFileSync(marker, 'utf8'));
  assert.equal(rec.argv[rec.argv.length - 1], 'roster');
  assert.equal(rec.cwd, dir);
  assert.equal(rec.env.HERDR_WORKSPACE_ID, 'wJ');
});

test('context without tab_id: the shell HERDR_TAB_ID is not leaked to the CLI', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'notab', { stdout: paneOk(dir) });
  const cli = writeFakeCli(dir, 'notab', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = {
    ...baseEnv(herdrBin, { context: contextJson({ tab_id: null }) }),
    HERDR_FAKE_CLI_MARKER: marker,
  };

  const r = run('doctor', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 0);
  const rec = JSON.parse(fs.readFileSync(marker, 'utf8'));
  assert.equal(rec.env.HERDR_TAB_ID, null); // dropped, not the shell's wShell:t9
  assert.equal(rec.env.HERDR_WORKSPACE_ID, 'wJ');
  assert.equal(rec.env.HERDR_PANE_ID, 'wJ:p24');
});

test('hostile pane id: passed as a single argument to pane get', (t) => {
  const dir = makeTmp(t);
  const hostile = 'wJ:p 24"; rm -rf / #';
  const argvFile = path.join(dir, 'herdr-argv.json');
  const herdrBin = writeFakeHerdr(dir, 'hostile', {
    stdout: paneOk(dir, { paneId: hostile }),
    argvFile,
  });
  const cli = writeFakeCli(dir, 'hostile', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = {
    ...baseEnv(herdrBin, { context: contextJson({ focused_pane_id: hostile }) }),
    HERDR_FAKE_CLI_MARKER: marker,
  };

  const r = run('doctor', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 0);
  const argv = JSON.parse(fs.readFileSync(argvFile, 'utf8'));
  assert.equal(argv[argv.length - 3], 'pane');
  assert.equal(argv[argv.length - 2], 'get');
  assert.equal(argv[argv.length - 1], hostile); // one argv element, no shell split
});

// ---------- closed failure: no context / bad context, no CLI ----------

each(
  [
    ['missing', undefined],
    ['empty', ''],
    ['malformed', '{"workspace_id": "wJ",'],
    ['json null', 'null'],
    ['json array', '["wJ"]'],
    ['json number', '42'],
    ['no workspace_id', contextJson({ workspace_id: undefined })],
    ['empty workspace_id', contextJson({ workspace_id: '' })],
    ['numeric workspace_id', contextJson({ workspace_id: 7 })],
    ['no focused_pane_id', contextJson({ focused_pane_id: undefined })],
    ['empty focused_pane_id', contextJson({ focused_pane_id: '' })],
  ],
  (label) => `context ${label}: fails without invoking the CLI`,
  (t, _label, context) => {
    const dir = makeTmp(t);
    const herdrBin = writeFakeHerdr(dir, 'ctx', { stdout: paneOk(dir) });
    const cli = writeFakeCli(dir, 'ctx', { stdout: 'OK\n' });
    const marker = path.join(dir, 'marker.json');
    const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };
    if (context === undefined) delete env.HERDR_PLUGIN_CONTEXT_JSON;
    else env.HERDR_PLUGIN_CONTEXT_JSON = context;

    const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
    assert.equal(e.code, EXIT_INVALID_TARGET);
    assert.ok(!fs.existsSync(marker), 'the CLI must not run on a bad context');
  },
);

test('missing HERDR_BIN_PATH: fails without invoking the CLI', (t) => {
  const dir = makeTmp(t);
  const cli = writeFakeCli(dir, 'nobin', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv('', { withBinPath: false }), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.match(e.message, /HERDR_BIN_PATH/);
  assert.ok(!fs.existsSync(marker));
});

// ---------- closed failure: pane get failures (herdr failures) ----------

each(
  [
    ['non-zero exit with JSON error', {
      exit: 1,
      stdout: JSON.stringify({ error: { code: 'pane_not_found', message: 'pane wJ:p99 not found' }, id: 'cli:pane:get' }),
    }],
    ['non-zero exit without JSON', { exit: 2, stderr: 'boom' }],
    ['zero exit with malformed JSON', { exit: 0, stdout: 'not json at all' }],
    ['zero exit without .result.pane', { exit: 0, stdout: JSON.stringify({ id: 'x', result: {} }) }],
    ['zero exit with .result.pane missing cwd', {
      exit: 0,
      stdout: JSON.stringify({ result: { pane: { pane_id: 'wJ:p24', workspace_id: 'wJ' } } }),
    }],
    ['zero exit with .result.pane missing workspace_id', {
      exit: 0,
      stdout: JSON.stringify({ result: { pane: { pane_id: 'wJ:p24', cwd: '/tmp' } } }),
    }],
    ['zero exit with non-object .result', { exit: 0, stdout: JSON.stringify({ result: 'pane' }) }],
  ],
  (label) => `pane get ${label}: fails without invoking the CLI`,
  (t, _label, behavior) => {
    const dir = makeTmp(t);
    const herdrBin = writeFakeHerdr(dir, 'badpane', behavior);
    const cli = writeFakeCli(dir, 'badpane', { stdout: 'OK\n' });
    const marker = path.join(dir, 'marker.json');
    const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

    const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
    assert.equal(e.code, EXIT_HERDR_FAILURE);
    assert.ok(!fs.existsSync(marker), 'the CLI must not run when the pane is unverifiable');
  },
);

test('pane get error: the herdr error cause is surfaced to the operator', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'cause', {
    exit: 1,
    stderr: JSON.stringify({ error: { code: 'pane_not_found', message: 'pane wJ:p99 not found' }, id: 'cli:pane:get' }),
  });
  const cli = writeFakeCli(dir, 'cause', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_HERDR_FAILURE);
  assert.match(e.message, /pane_not_found/);
  assert.ok(!fs.existsSync(marker));
});

test('unexecutable herdr binary: fails without invoking the CLI', (t) => {
  const dir = makeTmp(t);
  const herdrBin = path.join(dir, 'no-such-herdr');
  const cli = writeFakeCli(dir, 'badbin', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_HERDR_FAILURE);
  assert.match(e.message, /not executable/);
  assert.ok(!fs.existsSync(marker));
});

test('pane get timeout: fails without invoking the CLI', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'hang', { sleepMs: 5000 });
  const cli = writeFakeCli(dir, 'hang', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: { paneGetMs: 300, cliMs: 15_000 } }));
  assert.equal(e.code, EXIT_HERDR_FAILURE);
  assert.match(e.message, /timed out/);
  assert.ok(!fs.existsSync(marker));
});

// ---------- closed failure: wrong target, no CLI ----------

test('workspace divergence: the pane belongs to another workspace', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'div', { stdout: paneOk(dir, { workspaceId: 'wK' }) });
  const cli = writeFakeCli(dir, 'div', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('roster', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.match(e.message, /diverg/i);
  assert.ok(!fs.existsSync(marker), 'no alternate workspace is chosen');
});

test('nonexistent pane cwd: fails without invoking the CLI', (t) => {
  const dir = makeTmp(t);
  const gone = path.join(dir, 'does-not-exist');
  const herdrBin = writeFakeHerdr(dir, 'nocwd', { stdout: paneOk(dir, { cwd: gone }) });
  const cli = writeFakeCli(dir, 'nocwd', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.match(e.message, new RegExp(escapeRegExp(gone)));
  assert.ok(!fs.existsSync(marker));
});

test('pane cwd that is a file: fails without invoking the CLI', (t) => {
  const dir = makeTmp(t);
  const file = path.join(dir, 'a-file');
  fs.writeFileSync(file, 'x');
  const herdrBin = writeFakeHerdr(dir, 'filecwd', { stdout: paneOk(dir, { cwd: file }) });
  const cli = writeFakeCli(dir, 'filecwd', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.ok(!fs.existsSync(marker));
});

test('missing CLI script: fails before spawning the CLI', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'nocli', { stdout: paneOk(dir) });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const e = bridgeErr(() => run('doctor', {
    env,
    cliScript: path.join(dir, 'missing-cli.mjs'),
    timeouts: TIME,
  }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.ok(!fs.existsSync(marker));
});

// ---------- CLI outcome pass-through ----------

test('CLI non-zero exit: the code and stderr pass through, target still printed', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'clierr', { stdout: paneOk(dir) });
  const cli = writeFakeCli(dir, 'clierr', { exit: 3, stdout: 'partial\n', stderr: 'cli said no\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const r = run('doctor', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 3);
  assert.match(r.out, /target workspace=wJ/);
  assert.match(r.out, /partial/);
  assert.match(r.err, /cli said no/);
  assert.ok(fs.existsSync(marker), 'the CLI ran (its failure is passed through)');
});

test('CLI timeout: exit 4 with the timeout cause', (t) => {
  const dir = makeTmp(t);
  const herdrBin = writeFakeHerdr(dir, 'clihang', { stdout: paneOk(dir) });
  const cli = writeFakeCli(dir, 'clihang', { sleepMs: 5000 });
  const env = baseEnv(herdrBin);

  const r = run('roster', { env, cliScript: cli, timeouts: { paneGetMs: 10_000, cliMs: 300 } });

  assert.equal(r.code, EXIT_HERDR_FAILURE);
  assert.match(r.err, /timed out/);
  assert.match(r.out, /target workspace=wJ/);
});

// ---------- unknown subcommands ----------

each(
  ['', 'regrid', 'DOCTOR', 'doctor-x', 'dispatch'],
  (sub) => `unknown subcommand '${sub}': fails without invoking anything`,
  (t, sub) => {
    const dir = makeTmp(t);
    const herdrBin = writeFakeHerdr(dir, 'unk', { stdout: paneOk(dir) });
    const cli = writeFakeCli(dir, 'unk', { stdout: 'OK\n' });
    const marker = path.join(dir, 'marker.json');
    const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

    const e = bridgeErr(() => run(sub, { env, cliScript: cli, timeouts: TIME }));
    assert.equal(e.code, EXIT_INVALID_TARGET);
    assert.ok(!fs.existsSync(marker));
    assert.ok(!ACTIONS.has(sub));
  },
);

// ---------- real CLI: the P2 read-only regression ----------
//
// The plugin's actions inspect the focused project without writing anything
// to it (P2 from the Cursor/Grok review: the CLI used to append
// .herdr-soho/ to .gitignore and create the state tree). These tests run
// the REAL skill CLI (node stays executable, so both actions really run)
// against a fresh `git init` project with a fake `herdr` on PATH.

// A fake `herdr` answering every call the bridge and the real CLI make:
// `pane get` (the bridge), `agent list` / `pane list` / `tab list` (roster)
// and `--version` / `status server` / `--skill` (doctor). The pane's cwd is
// the test's fresh project, embedded in the script (the bridge's pane get
// spawn inherits the bridge process env, not the test env).
function writeRealTestHerdr(dir, project) {
  const script = `const a = process.argv.slice(2);
const out = (v) => process.stdout.write(typeof v === 'string' ? v : JSON.stringify(v) + '\\n');
switch (a[0]) {
  case 'pane':
    if (a[1] === 'get') out({ result: { pane: { pane_id: a[2], tab_id: 'wJ:t1', workspace_id: 'wJ', cwd: ${JSON.stringify(project)} } } });
    else out({ result: { panes: [] } });
    break;
  case 'tab': out({ result: { tabs: [] } }); break;
  case 'agent': out({ result: { agents: [] } }); break;
  case '--version': out('herdr 0.9.1\\n'); break;
  case 'status': out('server 0.9.1\\n'); break;
  case '--skill': out('fake skill marker\\n'); break;
  default: process.stderr.write('unexpected: ' + a.join(' ') + '\\n'); process.exit(1);
}
`;
  // Named `herdr` (herdr.cmd on Windows): the real CLI finds it on PATH.
  return writeLauncherFake(dir, 'herdr', script);
}

test('real CLI: doctor and roster leave a fresh git project untouched (no .gitignore, no state)', { timeout: 120000 }, (t) => {
  const dir = makeTmp(t);
  // The focused project: a fresh git work tree (the pane cwd).
  const project = fs.realpathSync(fs.mkdtempSync(path.join(dir, 'proj-')));
  const g = spawnSync('git', ['init', '--quiet'], { cwd: project, encoding: 'utf8' });
  assert.equal(g.status, 0, g.stderr);
  const herdrBin = writeRealTestHerdr(dir, project);
  const env = baseEnv(herdrBin);
  for (const sub of ['doctor', 'roster']) {
    const before = fs.readdirSync(project).sort();
    const r = run(sub, { env, timeouts: { paneGetMs: 15_000, cliMs: 60_000 } });
    assert.equal(r.code, 0, `${sub}: exit ${r.code}: ${r.out}${r.err}`);
    assert.match(r.out, new RegExp(`herdr-soho plugin: target workspace=wJ pane=wJ:p24 cwd=${escapeRegExp(project)}`));
    // No new file or directory of any kind in the project (no .gitignore,
    // no .herdr-soho/, nothing else).
    const after = fs.readdirSync(project).sort();
    assert.deepEqual(after, before, `${sub}: the project gained entries: ${after.filter((e) => !before.includes(e)).join(', ')}`);
    assert.ok(!fs.existsSync(path.join(project, '.gitignore')), `${sub}: no .gitignore`);
    assert.ok(!fs.existsSync(path.join(project, '.herdr-soho')), `${sub}: no state dir`);
  }
  // Mutation captured: a CLI write in either action (a .gitignore append or
  // a state tree) changes the project's entries and fails the asserts.
});

// ---------- pick: open the picker pane ----------

// A fake `herdr` that answers `pane get` (success) and `plugin pane open`
// (records the exact argv in argvFile, then stdout/exit per the config).
function writePickFakeHerdr(dir, name, { argvFile, open = { exit: 0, stdout: '', stderr: '' } }) {
  const script = `import fs from 'node:fs';
const argvFile = ${JSON.stringify(argvFile)};
const open = ${JSON.stringify(open)};
const cwd = ${JSON.stringify(dir)};
const a = process.argv.slice(2);
if (argvFile) fs.appendFileSync(argvFile, JSON.stringify(a) + '\\n');
if (a[0] === 'pane' && a[1] === 'get') {
  process.stdout.write(JSON.stringify({ id: 'cli:pane:get', result: { pane: { pane_id: a[2], tab_id: 'wJ:t1', workspace_id: 'wJ', cwd } } }));
  process.exit(0);
}
if (a[0] === 'plugin' && a[1] === 'pane' && a[2] === 'open') {
  if (open.stdout) process.stdout.write(open.stdout);
  if (open.stderr) process.stderr.write(open.stderr);
  process.exit(open.exit);
}
process.stderr.write('unexpected: ' + a.join(' ') + '\\n');
process.exit(9);
`;
  return writeLauncherFake(dir, `herdr-${name}`, script);
}

test('pick: opens the picker pane with the exact arguments and does not run the CLI', (t) => {
  const dir = makeTmp(t);
  const argvFile = path.join(dir, 'herdr-argv.jsonl');
  const herdrBin = writePickFakeHerdr(dir, 'ok', {
    argvFile,
    open: { exit: 0, stdout: JSON.stringify({ id: 'cli:plugin:pane:open', result: { pane_id: 'wJ:p40' } }) + '\n' },
  });
  const cli = writeFakeCli(dir, 'pickok', { stdout: 'SHOULD-NOT-RUN\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const r = run('pick', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 0, `exit ${r.code}: ${r.out}${r.err}`);
  assert.match(r.out, /herdr-soho plugin: target workspace=wJ pane=wJ:p24/);
  const argvs = fs.readFileSync(argvFile, 'utf8').trim().split('\n').map((l) => JSON.parse(l));
  assert.equal(argvs.length, 2, 'exactly two herdr calls: pane get, then pane open');
  assert.deepEqual(argvs[0].slice(-3), ['pane', 'get', 'wJ:p24'], 'the context pane is validated first');
  // Pinned against the literal (not only the export): a wrong entrypoint,
  // placement or missing --focus fails this even if the export changes.
  assert.deepEqual(argvs[1], ['plugin', 'pane', 'open', '--plugin', 'djalmajr.herdr-soho', '--entrypoint', 'picker', '--placement', 'overlay', '--focus']);
  assert.deepEqual(argvs[1], PICK_OPEN_ARGS, 'the exact pane open arguments');
  assert.ok(!fs.existsSync(marker), 'pick never runs the CLI');
  // Mutation captured: a different entrypoint id, placement or a missing
  // --focus in the args fails the pinned-literal deepEqual.
});

test('pick: a bad context fails without opening any pane', (t) => {
  const dir = makeTmp(t);
  const argvFile = path.join(dir, 'herdr-argv.jsonl');
  const herdrBin = writePickFakeHerdr(dir, 'badctx', { argvFile });
  const cli = writeFakeCli(dir, 'pickctx', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };
  delete env.HERDR_PLUGIN_CONTEXT_JSON;

  const e = bridgeErr(() => run('pick', { env, cliScript: cli, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.ok(!fs.existsSync(argvFile), 'no pane get, no pane open');
  assert.ok(!fs.existsSync(marker));
  // Mutation captured: opening the pane before the context validation
  // leaves the argv file behind and fails the assert.
});

test('pick: a pane-open failure passes the herdr code and stderr through', (t) => {
  const dir = makeTmp(t);
  const argvFile = path.join(dir, 'herdr-argv.jsonl');
  const herdrBin = writePickFakeHerdr(dir, 'openfail', {
    argvFile,
    open: { exit: 5, stderr: 'ui_busy: a modal is already open\n' },
  });
  const cli = writeFakeCli(dir, 'pickfail', { stdout: 'OK\n' });
  const marker = path.join(dir, 'marker.json');
  const env = { ...baseEnv(herdrBin), HERDR_FAKE_CLI_MARKER: marker };

  const r = run('pick', { env, cliScript: cli, timeouts: TIME });

  assert.equal(r.code, 5, `exit ${r.code}: ${r.out}${r.err}`);
  assert.match(r.err, /ui_busy/);
  assert.match(r.out, /target workspace=wJ/);
  const argvs = fs.readFileSync(argvFile, 'utf8').trim().split('\n').map((l) => JSON.parse(l));
  assert.deepEqual(argvs[argvs.length - 1], PICK_OPEN_ARGS);
  assert.ok(!fs.existsSync(marker));
  // Mutation captured: swallowing a non-zero pane-open exit (returning 0
  // or throwing) fails the code/stderr asserts.
});

test('pick: runPick is callable directly and rejects a workspace divergence', (t) => {
  const dir = makeTmp(t);
  const argvFile = path.join(dir, 'herdr-argv.jsonl');
  const herdrBin = writeLauncherFake(dir, 'herdr-pickdiv', `import fs from 'node:fs';
const a = process.argv.slice(2);
if (a[0] === 'pane' && a[1] === 'get') {
  process.stdout.write(${JSON.stringify(paneOk(dir, { workspaceId: 'wK', paneId: 'wK:p9' }))});
  process.exit(0);
}
fs.appendFileSync(${JSON.stringify(argvFile)}, JSON.stringify(a) + '\\n');
process.exit(0);
`);
  const env = { ...baseEnv(herdrBin), HERDR_PLUGIN_CONTEXT_JSON: contextJson() };

  const e = bridgeErr(() => runPick({ env, timeouts: TIME }));
  assert.equal(e.code, EXIT_INVALID_TARGET);
  assert.match(e.message, /diverg/i);
  assert.ok(!fs.existsSync(argvFile), 'no pane open on a divergence');
  // Mutation captured: skipping the workspace divergence check in runPick
  // opens the pane and fails the assert.
});
