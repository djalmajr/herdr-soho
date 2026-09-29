import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { runCli } from '../../../skills/herdr-soho/scripts/lib/platform.mjs';

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-runcli-'));
const bin = path.join(root, 'bin');
fs.mkdirSync(bin);
const cases = [
  { name: 'default', mode: 'default', script: "#!/bin/sh\nprintf 'out-line\\n'\nprintf 'err-line\\n' >&2\nexit 5\n" },
  { name: 'merge', mode: 'mergeOutput', script: "#!/bin/sh\nprintf 'out-line\\n'\nprintf 'err-line\\n' >&2\nexit 5\n" },
  { name: 'files', mode: 'outputFiles', script: "#!/bin/sh\nprintf 'out-line\\n'\nprintf 'err-line\\n' >&2\nexit 5\n" },
  { name: 'bad-shebang', mode: 'default', script: '#!/missing/interpreter\n' },
  { name: 'no-shebang', mode: 'default', script: 'exit 1\n' },
  { name: 'signal', mode: 'default', script: '#!/bin/sh\nkill -KILL $$\n' },
  { name: 'timeout', mode: 'timeout', script: '#!/bin/sh\n/bin/sleep 5\n' },
  { name: 'trap-exit-0', command: 'node', args: ['-e', "process.on('SIGTERM', () => { console.log('caught'); setTimeout(() => process.exit(0), 20); }); setInterval(() => {}, 50);"], mode: 'mergeOutput', timeoutMs: 300, script: '' },
  { name: 'trap-exit-3', command: 'node', args: ['-e', "process.on('SIGTERM', () => { console.log('caught'); setTimeout(() => process.exit(3), 20); }); setInterval(() => {}, 50);"], mode: 'mergeOutput', timeoutMs: 300, script: '' },
  { name: 'daemonize-no-timeout', mode: 'default', script: "#!/bin/sh\n/bin/sleep 0.3 &\nprintf 'daemonized\\n'\nexit 0\n" },
  { name: 'badutf', mode: 'default', script: "#!/bin/sh\nprintf 'a\\377b\\300\\200c\\355\\240\\200d\\360\\220\\200e\\342\\202'\nprintf 'x\\376y\\362\\200' >&2\n" },
  { name: 'missing', mode: 'missing', script: '' },
];
try {
  const expected = [];
  for (const fixture of cases) {
    const executable = path.join(bin, fixture.name);
    if (fixture.mode !== 'missing' && !fixture.command) fs.writeFileSync(executable, fixture.script, { mode: 0o700 });
    const options = { env: { PATH: `${bin}${path.delimiter}${path.dirname(process.execPath)}${path.delimiter}${process.env.PATH ?? ''}`, TMPDIR: root } };
    if (fixture.mode === 'mergeOutput') options.mergeOutput = true;
    if (fixture.mode === 'outputFiles') options.outputFiles = true;
    if (fixture.timeoutMs) options.timeoutMs = fixture.timeoutMs;
    else if (fixture.mode === 'timeout') options.timeoutMs = 100;
    const result = runCli(fixture.command ?? fixture.name, fixture.args ?? [], options);
    expected.push({ ...fixture, result: {
      notFound: result.notFound,
      status: result.status,
      signal: result.signal,
      stdout: result.stdout,
      stderr: result.stderr,
      timedOut: result.timedOut,
      error: result.error,
    } });
  }
  const dir = path.dirname(fileURLToPath(import.meta.url));
  fs.writeFileSync(path.join(dir, 'runcli.json'), `${JSON.stringify({ version: 1, cases: expected }, null, 2)}\n`);
} finally { fs.rmSync(root, { recursive: true, force: true }); }
