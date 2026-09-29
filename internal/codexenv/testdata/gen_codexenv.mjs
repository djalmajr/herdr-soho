import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { parseCodexPolicy, evaluateCodexPolicy, globMatch, stripTomlComment } from '../../../skills/herdr-soho/scripts/lib/codex-env.mjs';

const root = path.dirname(fileURLToPath(import.meta.url));
const manualCases = JSON.parse(fs.readFileSync(path.join(root, 'manual_cases.json'), 'utf8'));
const retainedCases = [
  '[shell_environment_policy]\ninherit = "core"\ninclude_only = [\n "HOME", # keep\n "HERDR_*"\n]\n',
  "[shell_environment_policy]\r\ninherit = 'none' # café\r\n[shell_environment_policy.set]\r\nHERDR_ENV = \"1\"\r\n",
  '[shell_environment_policy]\nset = { HERDR_ENV = "brace } # \\"quoted\\"",\n HERDR_PANE_ID = { a = [1, 2] } }\n',
  '[shell_environment_policy]\ninherit = "all"\nexclude = ["HERDR_EN?"]\n',
  '[shell_environment_policy]\n"include_only" = ["PATH", "café"]\n[other]\ntoken = "private#value"\n',
  '[shell_environment_policy]\ninherit = "all"\ninherit = "none"\n',
  '[shell_environment_policy.set]\nHERDR_ENV = "1"\nHERDR_ENV = "2"\n',
  '[shell_environment_policy]\nset = { HERDR_ENV = "secret", "HERDR_PANE_ID" = "x" }\n',
  '[shell_environment_policy]\ninclude_only = [\n "HERDR_*",\n "PATH"\n]\n',
  '[shell_environment_policy]\nset = { HERDR_ENV = "contains \\"quote\\" and }", HERDR_PANE_ID = "x" }\n',
].map((input, index) => ({ name: `retained-${index}`, input }));

const fragments = [
  '[shell_environment_policy]', '[shell_environment_policy.set]', 'inherit = "core"',
  'inherit = "none"', 'inherit = all', "inherit = 'none'", 'include_only = ["HERDR_*"]',
  'include_only = ["PATH", "HOME"]', 'exclude = ["HERDR_ENV"]',
  'exclude = ["HERDR_*", "X"]', 'exclude = [', 'include_only = [', '"HERDR_ENV",', ']',
  'set = {', 'set = { HERDR_ENV = "1" }', 'set = { A = "1",', 'HERDR_PANE_ID = "2",', '}',
  'HERDR_ENV = "1"', 'HERDR_WORKSPACE_ID = "x"', 'A = "}"', '[other]', 'k = "v" # c',
  '# comment', '', ' ', '\t', 'key = ["a]b"]', '"q" = 1', "'s' = 2", 'x = "a#b"',
  "y = 'c#d'", 'z = "e\\"f"', '\ufeff', '\u00a0', '[[t]]', '[a.b]', 'set = 5',
  'inherit = "core" junk', 'exclude = "HERDR_ENV"', 'include_only = "PATH"',
];
let seed = 20260928;
function random() {
  seed ^= seed << 13;
  seed ^= seed >>> 17;
  seed ^= seed << 5;
  return (seed >>> 0) / 0x100000000;
}
function pick(values) { return values[Math.floor(random() * values.length)]; }
const generatedCases = [];
for (let index = 0; index < 3097; index += 1) {
  const count = 1 + Math.floor(random() * 9);
  const eol = pick(['\n', '\n', '\n', '\r\n', '\r']);
  const lines = Array.from({ length: count }, () => pick(fragments));
  if (random() < 0.7) lines.unshift('[shell_environment_policy]');
  generatedCases.push({ name: `seed-20260928-${index}`, input: lines.join(eol) + pick(['', eol]) });
}

const inputs = [...manualCases, ...retainedCases, ...generatedCases];
const rows = inputs.map(({ name, input }) => {
  const policy = parseCodexPolicy(input);
  return { name, input, policy, evaluation: evaluateCodexPolicy(policy) };
});
const strips = ['value = "a#b" # tail', "value = 'a#b' # tail", 'x = "escaped \\" # still string" # end', 'café # comentário', 'x = "a\\\\" # tail'];
const globs = [['HERDR_*', 'HERDR_ENV'], ['herdr_en?', 'HERDR_ENV'], ['a+b', 'A+B'], ['[x]', '[x]'], ['s', 'ſ'], ['k', 'K'], ['*', 'café']];
const out = path.join(root, 'codexenv.json');
const output = {
  manual_count: manualCases.length,
  generated_count: generatedCases.length,
  rows,
  strips: strips.map(input => ({ input, output: stripTomlComment(input) })),
  globs: globs.map(([pattern, value]) => ({ pattern, value, output: globMatch(pattern, value) })),
};
fs.writeFileSync(out, JSON.stringify(output, null, 2) + '\n');
console.log(`manual=${manualCases.length} retained=${retainedCases.length} generated=${generatedCases.length} total=${rows.length}`);
