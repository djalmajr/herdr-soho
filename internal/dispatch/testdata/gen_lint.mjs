import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { briefLintFindings } from '../../../skills/herdr-soho/scripts/lib/dispatch.mjs';

const packageDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const dataDir = path.join(packageDir, 'testdata');
const sourcesDir = path.join(dataDir, 'plan-briefs');
const variantsDir = path.join(dataDir, 'variants');
process.chdir(packageDir);

const fullBrief = [
  '# Goal', 'Do the slice.',
  '# Expected result', 'The slice is done.',
  '# Owned files', '- internal/dispatch/lint.go',
  '# Forbidden', '- Do not commit or push.',
  '# Report', 'Write the report.',
].join('\n') + '\n';

const variants = [
  { name: 'sections-missing.md', body: 'Just do it.\n' },
  {
    name: 'aliases-ptbr.md',
    body: [
      '## Contexto', 'Fatia.',
      '## Entrega', 'Feita.',
      '## Arquivos', 'internal/dispatch/lint.go',
      '## Proibido', 'Sem commit/push.',
      '## Relatório', 'Pronto.',
    ].join('\n') + '\n',
    aliases: 'Goal=Contexto,Expected result=Entrega',
  },
  {
    name: 'case-and-accent.md',
    body: [
      '## OBJETIVO', 'Fatia.',
      '## RESULTADO ESPERADO', 'Feita.',
      '## ARQUIVOS', 'internal/dispatch/lint.go',
      '## PROIBIDO', 'Sem commit/push.',
      '## relatorio', 'Pronto.',
    ].join('\n') + '\n',
  },
  {
    name: 'long-s.md',
    body: fullBrief.replace('Do not commit or push.', 'No coſmit or puſh.')
      + '\n# Failure matrix\n[crash] [retry] [cloſk]\n',
  },
  {
    name: 'crlf.md',
    body: fullBrief.replace('# Report\n', '# Failure matrix\n[crash] [retry]\n# Report\n').replaceAll('\n', '\r\n'),
  },
  { name: 'bom.md', body: `\uFEFF${fullBrief}` },
];

fs.mkdirSync(variantsDir, { recursive: true });
for (const variant of variants) {
  fs.writeFileSync(path.join(variantsDir, variant.name), variant.body);
}

const cases = [];
for (const name of fs.readdirSync(sourcesDir).filter((entry) => entry.endsWith('.md')).sort()) {
  cases.push({ path: `testdata/plan-briefs/${name}` });
}
for (const variant of variants) {
  cases.push({ path: `testdata/variants/${variant.name}`, aliases: variant.aliases ?? '' });
}

const ctx = { entries: new Map() };
const rows = cases.map((item) => {
  const env = {
    ...process.env,
    HERDR_SOHO_BRIEF_LINT: 'warn',
    HERDR_SOHO_BRIEF_LINT_ALIASES: item.aliases ?? '',
  };
  const findings = briefLintFindings(item.path, ctx, env);
  return {
    path: item.path,
    ...(item.aliases ? { aliases: item.aliases } : {}),
    mode: findings.mode,
    warnings: findings.warnings,
    missingMessage: findings.missingMessage,
  };
});
fs.writeFileSync(path.join(dataDir, 'lint_corpus.json'), `${JSON.stringify(rows, null, 2)}\n`);
const warned = rows.filter((row) => row.warnings.length > 0 || row.missingMessage !== '').length;
console.log(`JS corpus: ${warned} briefs with warnings; ${rows.length - warned} clean; ${rows.length} total`);
