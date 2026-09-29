import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { markerSeq, markerSeqChanged, lastNonEmptyLines, promptSitsInInput } from '../../../skills/herdr-soho/scripts/lib/arrival.mjs';
import { dialogKind, questionText } from '../../../skills/herdr-soho/scripts/lib/dialog.mjs';
import { providerDetect } from '../../../skills/herdr-soho/scripts/lib/provider.mjs';
import { quotaDetect, quotaLineIsCode, quotaPhraseQuoted, renewalValue } from '../../../skills/herdr-soho/scripts/lib/quota.mjs';

const hostiles = ['\r', '\u001b', '\u2028', '\u00a0', '😀'];
const rows = [];
const providerSamples = [
  '{"type":"model_capacity"}', 'overloaded', 'at capacity', '529',
  'Request timed out', 'Connection error', 'ECONNREFUSED', 'ECONNRESET', 'econnrefused', 'Econnreset', 'connection refused', 'connection reset',
  'Retry failed after 3 attempts', '503: unavailable', 'Internal Server Error', 'Bad Gateway', 'Service Unavailable',
  'Gateway Timeout', 'unexpected status 503', 'stream disconnected before completion', 'socket hang up', 'fetch failed',
  '401 Unauthorized', 'unexpected status 401', 'Incorrect API key', 'refresh token was revoked', 'Failed to refresh access token',
  'retrying', 'retry in 5 seconds', 'will retry', 'reconnecting',
];
for (const sample of providerSamples) {
  for (const hostile of hostiles) {
    const screen = `Error: ${sample}${hostile}`;
    rows.push({ kind: 'provider', state: 'idle', screen, expected: providerDetect('idle', screen) });
  }
}
for (const prefix of ['■ ', '✗ ', '✘ ', '× ', '⚠ ', '● ', '• ', '⎿ ']) {
  for (const hostile of hostiles) {
    const screen = `${prefix}Error: Request timed out${hostile}`;
    rows.push({ kind: 'provider', state: 'idle', screen, expected: providerDetect('idle', screen) });
  }
}
for (const bullet of ['•', '●', '⏺', '✓', '✔']) {
  for (const hostile of hostiles) {
    const screen = `${bullet} Running local checks${hostile}`;
    rows.push({ kind: 'provider', state: 'idle', screen, expected: providerDetect('idle', screen) });
  }
}
const quotaSamples = [
  'hit your usage limit', 'Individual quota reached', 'You exceeded your current quota', 'quota exceeded',
  'RESOURCE_EXHAUSTED', '429 Too Many Requests', 'rate limit exceeded', "You've hit your daily limit",
  'You have hit your usage limit', 'You have reached your API usage limits', "You've reached your workspace API usage limits",
];
for (const sample of quotaSamples) {
  for (const hostile of hostiles) {
    const screen = `${sample}${hostile}`;
    rows.push({ kind: 'quota', state: 'idle', screen, expected: quotaDetect('idle', screen) });
  }
}
for (const line of [
  '# quota exceeded', ' # quota exceeded', 'x\u2028# 429', 'func\u00a0limit()', 'return\u000b429',
  'const value\u00a0= 1', 'message="quota exceeded"', 'token=secret quota exceeded', '😀 rate limit exceeded',
]) rows.push({ kind: 'quota-code', line, expected: quotaLineIsCode(line) });
for (const hostile of hostiles) {
  for (const line of [`${hostile}# quota exceeded`, `x${hostile}# 429`, `${hostile}return${hostile}x`, `${hostile}function${hostile}x`, `func${hostile}`, `x${hostile}= 1`, `message="quota exceeded"${hostile}`]) {
    rows.push({ kind: 'quota-code', line, expected: quotaLineIsCode(line) });
  }
}
for (const line of ['Resets at 5:00pm', 'Resets at 5:00a.p.', 'try again in 5 minutes', '2026-09-28\u202814:30', '14:30😀']) {
  rows.push({ kind: 'renewal', line, expected: renewalValue(line) });
}
for (const line of ['Try again in 5 Minutes', 'resets in 2 HOURS', 'Resets in 10 Seconds', 'retry after 3 Days']) {
  rows.push({ kind: 'renewal', line, expected: renewalValue(line) });
}
for (const screen of ['{"error":{"code":"insufficient_quota"}} "rate limit exceeded"', 'İ"quota exceeded"', '"You have reached your worKspace API usage limit" You have reached your API usage limit']) {
  rows.push({ kind: 'quota', state: 'idle', screen, expected: quotaDetect('idle', screen) });
}
for (const hostile of hostiles) {
  for (const line of [`Resets at 5:00${hostile}`, `Try again in 5 minutes${hostile}`, `Available again at 6:00${hostile}`, `Retry after 2 hours${hostile}`, `in 15 seconds${hostile}`, `5:00${hostile}`, `2026-09-28${hostile}`, `12 minutes${hostile}`]) {
    rows.push({ kind: 'renewal', line, expected: renewalValue(line) });
  }
}
for (const line of [
  '"rate limit exceeded"', "'rate limit exceeded'", '`rate limit exceeded`',
  '😀 "rate limit exceeded"', '{"message":"rate limit exceeded"}',
  '{"error":{"code":"insufficient_quota"}} "rate limit exceeded"',
  'İ"rate limit exceeded"', 'á😀"rate limit exceeded"',
]) rows.push({ kind: 'quoted', line, expected: quotaPhraseQuoted(line, /rate limit exceeded/i) });
for (const hostile of hostiles) {
  const line = `"rate limit exceeded"${hostile}`;
  rows.push({ kind: 'quoted', line, expected: quotaPhraseQuoted(line, /rate limit exceeded/i) });
}
const questionCases = [
  ['codex', 'Enter to submit answer'], ['codex', 'Enter to submit all'], ['codex', 'Press enter to confirm'],
  ['claude', '↑↓ to navigate · Enter to select'], ['claude', 'Submit answers · Enter to select'],
  ['claude', 'Do you want to proceed? ↑↓ to navigate · Enter to select'],
  ['opencode', 'Enter submit · Esc dismiss'], ['opencode', 'Enter toggle · Esc dismiss'],
  ['opencode', 'Permission required · Enter submit · Esc dismiss'], ['grok', 'Enter to submit answer'],
  ['codex', 'ENTER TO SUBMİT ANSWER'], ['codex', 'ENTER TO SUBMIT ANSWER ſ'], ['codex', 'ENTER TO SUBMIT ANSWER K'],
];
for (const [kind, sample] of questionCases) {
  for (const hostile of hostiles) {
    const screen = `${sample}${hostile}`;
    rows.push({ kind: 'dialog', provider: kind, screen, expected: dialogKind(kind, screen) });
  }
}
for (const screen of [
  'a   \n\nb\t\nc\n', '😀\u00a0\u2028\r\nnext\ufeff\n',
  `${'x'.repeat(500)}\n${'á😀'.repeat(250)}\n${'y'.repeat(500)}\n`,
  `${'x'.repeat(1199)}😀trailing`,
  `${'x'.repeat(1198)}á😀trailing`,
  `${'x'.repeat(1199)}😀`,
  'token=secret\nkey sk_live_abcdefghijklmno here\nBearer abc123.~+/\n',
]) rows.push({ kind: 'question-text', screen, expected: questionText(screen) });
for (const [screen, n] of [[' a\r\nb\n\u00a0\n😀', 2], ['a\nb\nc', 0], ['a\nb\nc', -1], ['one\nRead the file brief.md\n', 15], ['\u2028', 15], [Array.from({ length: 13 }, (_, i) => `row-${i}`).join('\n') + '\nRead the file x', 15], [Array.from({ length: 14 }, (_, i) => `row-${i}`).join('\n') + '\nRead the file x', 15]]) {
  rows.push({ kind: 'last-lines', screen, n, expected: lastNonEmptyLines(screen, n) });
  rows.push({ kind: 'prompt', screen, expected: promptSitsInInput(screen) });
}
for (const screen of [
  ['Read the file x', ...Array.from({ length: 14 }, (_, i) => `row-${i}`)].join('\n'),
  ['Read the file x', ...Array.from({ length: 15 }, (_, i) => `row-${i}`)].join('\n'),
]) rows.push({ kind: 'prompt', screen, expected: promptSitsInInput(screen) });
for (const marker of ['  123  456\n', 'epoch', '\u00a0123\u2028456\ufeff', '', ...hostiles.map(h => `${h}123${h}456${h}`)]) rows.push({ kind: 'marker-seq', marker, expected: markerSeq(marker) });
for (const [marker, current] of [['123 4', '5'], ['123 4', '4'], ['123', '4'], ['123 4', ''], ['', '4']]) {
  rows.push({ kind: 'marker-changed', marker, current, expected: markerSeqChanged(marker, current) });
}

// Fixed-seed differential corpus: 5,000 rows for each requested function.
let seed = 0x4b1d2a79;
function random() {
  seed = (Math.imul(seed, 1664525) + 1013904223) >>> 0;
  return seed;
}
const atoms = [
  'Error:', 'API Error:', 'Request timed out', 'Connection error', 'reconnecting', '401', '4011', '529',
  'at capacity', 'overloaded', 'rate limit exceeded', 'hit your usage limit', 'resets in ', 'try again in ', 'insufficient_quota', '2 days', 'Try again in 5 Minutes',
  'ENTER TO SUBMIT ANSWER', 'Enter to select', 'to navigate', 'Esc dismiss', 'permission required',
  'ſ', 'K', 'K', 'İ', '\u2028', '\u00a0', '\u001b', '\r', '😀', 'á', 'x', ' ', '\n',
];
function sample(min = 2, max = 14) {
  const count = min + random() % (max - min + 1);
  let value = '';
  for (let i = 0; i < count; i++) value += atoms[random() % atoms.length];
  return value;
}
for (let i = 0; i < 5000; i++) {
  const state = random() % 5 === 0 ? 'working' : 'idle';
  const screen = `Error: ${sample()}`;
  rows.push({ kind: 'provider', state, screen, expected: providerDetect(state, screen) });
}
for (let i = 0; i < 5000; i++) {
  const screen = sample();
  const state = random() % 5 === 0 ? 'working' : 'idle';
  rows.push({ kind: 'quota', state, screen, expected: quotaDetect(state, screen) });
}
for (let i = 0; i < 5000; i++) {
  const line = sample();
  rows.push({ kind: 'renewal', line, expected: renewalValue(line) });
}
for (let i = 0; i < 5000; i++) {
  const provider = ['codex', 'claude', 'opencode', 'unknown-kind'][random() % 4];
  const screen = sample();
  rows.push({ kind: 'dialog', provider, screen, expected: dialogKind(provider, screen) });
}
for (let i = 0; i < 5000; i++) {
  const screen = i % 100 === 0 ? `${'x'.repeat(1198)}á😀tail` : `${sample()}\n${sample()}\n`;
  rows.push({ kind: 'question-text', screen, expected: questionText(screen) });
}
for (let i = 0; i < 5000; i++) {
  const screen = Array.from({ length: random() % 22 }, () => sample(1, 4)).join('\n');
  const n = i % 50 === 0 ? 15 : i % 53 === 0 ? 0 : random() % 18;
  rows.push({ kind: 'last-lines', screen, n, expected: lastNonEmptyLines(screen, n) });
}
const out = path.join(path.dirname(fileURLToPath(import.meta.url)), 'provider.json');
fs.writeFileSync(out, JSON.stringify(rows, null, 2) + '\n');
