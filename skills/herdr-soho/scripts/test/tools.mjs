import fs from 'node:fs';
import path from 'node:path';

let symlinkAvailable;

export function canSymlink(dir) {
  if (symlinkAvailable !== undefined) return symlinkAvailable;
  const source = path.join(dir, `.symlink-probe-${process.pid}-${Date.now()}`);
  const target = `${source}.target`;
  try {
    fs.writeFileSync(target, 'probe');
    fs.symlinkSync(target, source);
    symlinkAvailable = true;
  } catch {
    symlinkAvailable = false;
  } finally {
    fs.rmSync(source, { force: true });
    fs.rmSync(target, { force: true });
  }
  return symlinkAvailable;
}

export function linkTool(binDir, name, target) {
  if (process.platform === 'win32') {
    const launcher = path.join(binDir, `${name}.cmd`);
    fs.writeFileSync(launcher, `@"${path.resolve(target)}" %*\r\n`);
    return launcher;
  }
  const launcher = path.join(binDir, name);
  fs.symlinkSync(target, launcher);
  return launcher;
}
