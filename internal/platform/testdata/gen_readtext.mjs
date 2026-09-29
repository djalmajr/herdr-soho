import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { readTextFile } from '../../../skills/herdr-soho/scripts/lib/platform.mjs';

const dir = path.dirname(fileURLToPath(import.meta.url));
const expected = [];
let state = 0x5eed1234;
function next() {
  state ^= state << 13;
  state ^= state >>> 17;
  state ^= state << 5;
  return state >>> 0;
}
const randomInputs = Array.from({ length: 2500 }, () => {
  const bytes = Buffer.alloc(1 + (next() % 8));
  for (let i = 0; i < bytes.length; i++) bytes[i] = next() & 0xff;
  return bytes;
});
const inputs = [
  Buffer.from([0x61, 0x0d, 0x0a, 0x62, 0x0d, 0x0a]),
  Buffer.from([0xe0, 0x80]),
  Buffer.from([0xf0, 0x80]),
  Buffer.from([0xf4, 0x90]),
  Buffer.from([0x61, 0x80, 0x81, 0x62]),
  Buffer.from([0x61, 0xe2, 0x82, 0x62]),
  Buffer.from([0xc0, 0xaf]),
  Buffer.from([0xed, 0xa0, 0x80]),
  Buffer.from([0xef, 0xbb, 0xbf, 0x0d, 0x0a, 0xf0, 0x9f, 0x92]),
  ...randomInputs,
];
const tempDir = fs.mkdtempSync(path.join(os.tmpdir(), 'herdr-readtext-'));
try {
	for (let index = 0; index < inputs.length; index++) {
		const bytes = inputs[index];
		const file = path.join(tempDir, `case-${String(index).padStart(4, '0')}.bin`);
		fs.writeFileSync(file, bytes);
		expected.push({ hex: bytes.toString('hex'), expected: readTextFile(file) });
	}
	fs.writeFileSync(path.join(dir, 'readtext.json'), JSON.stringify({ version: 1, cases: expected }, null, 2) + '\n');
} finally { fs.rmSync(tempDir, { recursive: true, force: true }); }
