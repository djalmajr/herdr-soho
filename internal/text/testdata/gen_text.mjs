import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { hasWord, redactSecrets, sanitizeCause } from '../../../skills/herdr-soho/scripts/lib/text.mjs';

const inputs = [
  'a\nb\tc', 'x\u001b[31my\r\n z', 'a  b   c ', 'a'.repeat(300), '',
  'áéñ 😀\u2028\u00a0\r\u001b', 'Bearer abc123.~+/', 'bearer xyz-123', 'bearer sk-abcdefgh12', 'pk-proj-abcdefgh12',
  'token=sk_live_abcdefghij', 'secret=x', 'secret: sk_test_a1b2c3 rest', 'sk-ant-123',
  'sk-proj-abcDEF123456', 'sk-ant-api01-XYZ12345', 'pk-live12345678', 'key sk_live_987654321',
];
const words = [['one two three', 'two'], ['one two three', 'two three'], ['one  two', 'two'], ['', '']];
for (const hostile of ['\r', '\u001b', '\u2028', '\u00a0', '😀']) {
  inputs.push(`sk_live_abcdefghij${hostile}`, `sk-proj-abcdefgh${hostile}`, `Bearer abc123.${hostile}`, `token=${hostile}abc`);
}
const rows = inputs.map((input) => ({ input, sanitize: sanitizeCause(input), redact: redactSecrets(input) }));
for (const [list, word] of words) rows.push({ list, word, hasWord: hasWord(list, word) });
const out = path.join(path.dirname(fileURLToPath(import.meta.url)), 'text.json');
fs.writeFileSync(out, JSON.stringify(rows, null, 2) + '\n');
