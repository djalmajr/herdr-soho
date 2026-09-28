import fs from 'node:fs';
import path from 'node:path';
import { assertOutsideRepository, readFixtureManifest } from './lib.mjs';

function main(argv) {
  if (argv.length !== 2) throw new Error('usage: node evals/prepare.mjs <fixture> <dest>');
  const [fixtureArg, destArg] = argv;
  const { root, manifest } = readFixtureManifest(fixtureArg);
  const dest = assertOutsideRepository(destArg);
  const relative = path.relative(root, dest);
  if (relative === '' || (!relative.startsWith(`..${path.sep}`) && relative !== '..' && !path.isAbsolute(relative))) {
    throw new Error('destination must not be inside the fixture');
  }
  if (!fs.statSync(path.dirname(dest)).isDirectory()) throw new Error(`destination parent is not a directory: ${path.dirname(dest)}`);

  fs.mkdirSync(dest);
  try {
    for (const relativePath of manifest.keys()) {
      const target = path.join(dest, ...relativePath.split('/'));
      fs.mkdirSync(path.dirname(target), { recursive: true });
      fs.copyFileSync(path.join(root, ...relativePath.split('/')), target, fs.constants.COPYFILE_EXCL);
    }
    const preparedAt = new Date().toISOString();
    fs.writeFileSync(path.join(dest, '.eval-run.json'), `${JSON.stringify({
      fixture: path.basename(root),
      prepared_at: preparedAt,
      manifest_ok: true,
    })}\n`, { flag: 'wx' });
    process.stdout.write(`${JSON.stringify({ fixture: path.basename(root), destination: dest, prepared_at: preparedAt, manifest_ok: true })}\n`);
  } catch (error) {
    fs.rmSync(dest, { recursive: true, force: true });
    throw error;
  }
}

try {
  main(process.argv.slice(2));
} catch (error) {
  process.stderr.write(`prepare: ${error.message}\n`);
  process.exitCode = 2;
}
