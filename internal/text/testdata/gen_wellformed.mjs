import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const inputs = [
  [0xd83d], [0xde00], [0xd83d, 0xde00], [0x00e1, 0xd83d],
  [0x0078, 0xd83d, 0x0079], [0xd800, 0xdc00, 0xdfff],
];
const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-soho-wellformed-'));
try {
  const rows = inputs.map((units, i) => {
    const file = path.join(tempDir, String(i));
    fs.writeFileSync(file, String.fromCharCode(...units));
    return { units, expectedHex: fs.readFileSync(file).toString('hex') };
  });
  const out = path.join(path.dirname(fileURLToPath(import.meta.url)), 'wellformed.json');
  fs.writeFileSync(out, JSON.stringify(rows, null, 2) + '\n');
} finally {
  fs.rmSync(tempDir, { recursive: true, force: true });
}
