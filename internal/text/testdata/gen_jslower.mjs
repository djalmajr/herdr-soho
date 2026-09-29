import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const output = path.join(path.dirname(fileURLToPath(import.meta.url)), 'jslower.json');
const cases = [];
const caseIgnorable = /\p{Case_Ignorable}/u;
for (let codePoint = 0; codePoint <= 0x10ffff; codePoint += 1) {
  if (codePoint >= 0xd800 && codePoint <= 0xdfff) continue;
  const input = String.fromCodePoint(codePoint);
  const lower = input.toLowerCase();
  if (lower !== input) cases.push({ input, lower });
  if (caseIgnorable.test(input)) {
    for (const contextual of [`A${input}Σ`, `AΣ${input}`, `AΣ${input}B`]) {
      cases.push({ input: contextual, lower: contextual.toLowerCase() });
    }
  }
}
for (const input of [
  'ΟΣ', 'ΟΣΑ', 'AΣ', "AΣ'", "AΣ'B", 'AΣ\u0301', 'AΣ\u0301B',
  'İstanbul', 'İ', 'Ο.Σ', 'ΟΣ\u2019', 'ΟΣ\u2019Α', 'ος', 'οσ',
]) {
  cases.push({ input, lower: input.toLowerCase() });
}
fs.writeFileSync(output, `${JSON.stringify(cases)}\n`);
process.stdout.write(`wrote ${cases.length} Node String.toLowerCase cases to ${output}\n`);
