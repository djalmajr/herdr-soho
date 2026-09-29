import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { partialCount, reviewHeader } from '../../../skills/herdr-soho/scripts/lib/reportscan.mjs';

const partial = [
  ['table-list-header', '# Report\n\n| item | state |\n| --- | --- |\n| read | [done] |\n| fact A | [partial] |\n| fact B | [partial] |\n'],
  ['case-and-two', '[PARTIAL]: café\n[Partial] — emoji 😀\n[partial] again\n'],
  ['prose', 'For every item: `done` / `partial` / `skipped`\nNo item is partial except one.\npartially done, impartial\n- fact: [partial] — não verificado 😀\n'],
  ['quote', '`[done]`, `[partial]` or `[skipped]`\nEstados: [done] / [partial] / [skipped] + reason\n| 2 | fato | [partial] | reason [done] |\n'],
  ['fences', 'before: [partial]\n```js\n[partial] inside\n~~~\n[partial] still inside\n```\nafter: [partial]\n'],
  ['suffixed-fence', '```\n[partial] fenced\n```js\n`[partial] still fenced\n```\n[partial] outside\n'],
  ['positions', 'soma de `[partial]` no texto\na mention [partial] in prose\n- [partial] item\n| 3 | `[partial]` | … |\n**Estado:** [partial] — ✓\n### B.3 — **[partial]**\n1. [partial]\n> [partial]\n[ ] [partial]\n'],
  ['hostile-crlf', '## Relatório\r\n- `[partial]` — ação pendente 😀\r\n```\r\n[partial] oculto\r\n```\r\n'],
  ['hostile-inline-code', 'texto [partial] mencionado\n- estado: `[partial]` e ```[partial]```\n'],
  ['unicode-whitespace', '\u00a0- [partial] Unicode NBSP\n\uFEFF[partial] BOM-leading\n'],
  ['short-fence-does-not-close-long-fence', '````\n[partial] remains fenced\n```\n[partial] remains fenced\n````\n'],
  ['quoted-state-list-at-line-start', '- [partial] [done] [skipped]\n'],
  ['table-cell-may-end-at-line-end', '| fact | [partial]\n'],
  ['four-space-fence-is-not-an-opener', '    ```\n[partial] visible\n    ```\n'],
  ['empty', ''],
];
const reviews = [
  ['empty', ''],
  ['blanks', '   \n\n  \n'],
  ['plain', '# Relatório\n\ndone.\n'],
  ['header-deeper', '# Report\n\nfindings: 1 (P0 1, P1 0, P2 0, P3 0) | verdict: fail\n'],
  ['header-first', 'findings: 3 (P0 0, P1 1, P2 2, P3 0) | verdict: FAIL\nrest\n'],
  ['strict-spacing', 'findings: 2  (P0 1, P1 1, P2 0, P3 0) | verdict: pass\n'],
  ['bad-verdict', 'findings: 2 (P0 1, P1 1, P2 0, P3 0) | verdict: blocked\n'],
  ['hostile-crlf-unicode', '\uFEFF Findings: 3 (P0 0, P1 1, P2 2, P3 0) | Verdict: FAIL \r\n😀 review\r\n'],
];
let seed = 0x51f15e;
function random() {
  seed = (seed * 1664525 + 1013904223) >>> 0;
  return seed / 0x100000000;
}
const padCase = (value, selector) => [...value].map((char, i) => {
  if (char < 'a' || char > 'z') return char;
  return (selector >> (i % 16)) & 1 ? char.toUpperCase() : char;
}).join('');
const numbers = ['0', '00', '007', '42', '12345678901234567890', '9007199254740993', '12345678901234567890123456789012'];
for (let i = 0; i < 4000; i++) {
  const values = Array.from({ length: 5 }, () => numbers[Math.floor(random() * numbers.length)]);
  const verdict = random() < 0.5 ? 'pass' : 'fail';
  let text = `findings: ${values[0]} (P0 ${values[1]}, P1 ${values[2]}, P2 ${values[3]}, P3 ${values[4]}) | verdict: ${verdict}`;
  text = padCase(text, i * 31);
  if (random() < 0.5) text = `${random() < 0.5 ? ' ' : '\t'}${text}${random() < 0.5 ? ' ' : '\u00a0'}`;
  if (i % 5 === 0) text = text.replace(/ \| verdict:/i, '  | verdict:');
  if (i % 5 === 1) text = text.replace(/\) \| verdict: (pass|fail)$/i, ') | verdict: blocked');
  if (i % 5 === 2) text = `${text} trailing`;
  if (i % 5 === 3) text = text.replace(/P2 [0-9]+/i, 'P2 1e3');
  reviews.push([`generated-${i}`, text]);
}
const partialMarkers = ['[partial]', '[PARTIAL]', '`[partial]`', '**[partial]**', '`**[partial]**`'];
const partialPrefixes = ['- ', '* ', '+ ', '1. ', '12. ', '# ', '### ', '> ', '[ ] ', '[x] ', '', '| facts | ', 'Estado: ', 'B.3 — '];
const partialSuffixes = [' — pending', ' reason', ' |', ' | reason', ' and [done]', '\r\n'];
for (let i = 0; i < 6000; i++) {
  const marker = partialMarkers[Math.floor(random() * partialMarkers.length)];
  const prefix = partialPrefixes[Math.floor(random() * partialPrefixes.length)];
  const suffix = partialSuffixes[Math.floor(random() * partialSuffixes.length)];
  let text = `${prefix}${marker}${suffix}\n`;
  if (i % 4 === 0) text = `~~~\n${text}~~~\n`;
  if (i % 4 === 1) text = `- [done] / ${marker} / [skipped]\n${text}`;
  if (i % 4 === 2) text = `    ~~~\n${text}    ~~~\n`;
  partial.push([`generated-${i}`, text]);
}
const expected = {
  partial: partial.map(([name, text]) => ({ name, text, want: partialCount(text) })),
  review: reviews.map(([name, text]) => ({ name, text, want: reviewHeader(text) })),
};
const dir = path.dirname(fileURLToPath(import.meta.url));
fs.writeFileSync(path.join(dir, 'reportscan.json'), JSON.stringify(expected, null, 2) + '\n');
console.log(`generated review non-null ${expected.review.filter((entry) => entry.name.startsWith('generated-') && entry.want !== null).length}; partial cases ${expected.partial.length}`);
