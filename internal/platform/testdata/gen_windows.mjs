import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { cmdInvocation, findExecutable } from '../../../skills/herdr-soho/scripts/lib/platform.mjs';

const root = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-platform-win32-'));
const first = path.join(root, 'first');
const second = path.join(root, 'second');
fs.mkdirSync(first);
fs.mkdirSync(second);
fs.writeFileSync(path.join(first, 'npm'), 'extensionless', { mode: 0o600 });
fs.writeFileSync(path.join(second, 'tool.CMD'), 'batch', { mode: 0o600 });
const find = [];
const invocations = [];
const resolve = [];
try {
  for (const pathext of ['.EXE;.CMD', '.EXE;.CMD;', ';.CMD', '.CMD;;.EXE']) {
    const resolved = findExecutable('npm', { PATH: first, PATHEXT: pathext }, 'win32');
    find.push({ name: 'npm', pathSpec: 'first', pathext, basename: resolved ? path.basename(resolved) : null });
  }
  const resolved = findExecutable('tool', { PATH: second, PATHEXT: '.CMD' }, 'win32');
  find.push({ name: 'tool', pathSpec: 'second', pathext: '.CMD', basename: resolved ? path.basename(resolved) : null });
  const cases = [
    { resolved: 'C:\\tools\\grok.CMD', args: ['models', 'has space', 'x& echo INJECTED', '50%', 'a"b'], env: { COMSPEC: 'C:\\Windows\\system32\\cmd.exe' } },
    { resolved: 'C:\\p\\node_modules\\.bin\\tool.cmd', args: ['a&b'], env: {} },
  ];
  for (const fixture of cases) invocations.push({ ...fixture, expected: cmdInvocation(fixture.resolved, fixture.args, fixture.env) });
  for (const value of ['/repo/.git', '\\foo', 'C:\\absolute\\path']) {
    resolve.push({ base: 'C:\\x\\worker', value, expected: path.win32.resolve('C:\\x\\worker', value) });
  }
  const dir = path.dirname(fileURLToPath(import.meta.url));
  fs.writeFileSync(path.join(dir, 'windows.json'), `${JSON.stringify({ version: 1, find, invocations, resolve }, null, 2)}\n`);
} finally { fs.rmSync(root, { recursive: true, force: true }); }
