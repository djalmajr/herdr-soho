import fs from 'node:fs';
import path from 'node:path';

const symlinkSupport = new Map();

export function canSymlink(dir) {
  if (symlinkSupport.has(dir)) return symlinkSupport.get(dir);
  const target = path.join(dir, '.symlink-probe-target');
  const link = path.join(dir, '.symlink-probe-link');
  let supported = false;
  try {
    fs.writeFileSync(target, '');
    fs.symlinkSync(target, link);
    supported = true;
  } catch {
    supported = false;
  } finally {
    fs.rmSync(link, { force: true });
    fs.rmSync(target, { force: true });
  }
  symlinkSupport.set(dir, supported);
  return supported;
}

export function linkTool(binDir, name, target) {
  const absoluteTarget = path.resolve(target);
  if (process.platform === 'win32') {
    const launcher = path.join(binDir, `${name}.cmd`);
    fs.writeFileSync(launcher, `@"${absoluteTarget}" %*\r\n`);
    return launcher;
  }
  const link = path.join(binDir, name);
  fs.symlinkSync(absoluteTarget, link);
  return link;
}
