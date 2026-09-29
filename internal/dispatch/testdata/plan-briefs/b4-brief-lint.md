# Brief — B4: lint e posse do brief sem falso alarme, com seções em pt-BR

Role: implementer · Agent: build · Report language: pt-BR

Worktree `/work/herdr-soho/.worktrees/build-2`,
branch `fix/brief-lint-ptbr`, base `main` `6b764d9`. Caminhos relativos a
essa raiz. Código em `skills/herdr-soho/scripts/lib/dispatch.mjs`
(`briefMissingSections`, `ownedSection`, `ownedPaths`,
`parseBriefLintAliases`).

## Goal

O `dispatch` não acusa sobreposição por arquivos que o brief **exclui** da
posse, e aceita briefs escritos com os títulos de seção em português sem
precisar de `brief_lint_aliases`.

## Caso real que motiva

Um brief do projeto Cinzel tinha, dentro de `## Owned files — Arquivos
donos`, o item:
`- Nenhum arquivo de \`src/server/commands/artifact.ts\`, \`src/server/fns.ts\` ou catálogo i18n: outro implementador atua nessa área.`
O `dispatch` avisou `owns files that '<outro>' is still editing:
src/server/commands/artifact.ts, src/server/fns.ts`. Esses caminhos estão
excluídos, não são do worker. E um brief de revisão com os títulos
`Objetivo`, `Escopo`, `Relatório` foi acusado de não ter Goal/Report.

## Decisions already made

1. **Item de exclusão não dá posse.** Na seção de posse, uma linha (item
   de lista ou texto) cujo texto, depois do marcador de lista e de
   espaços/ênfase (`*`, `_`), começa por uma palavra de negação ou
   exclusão não contribui caminhos. Palavras (sem diferenciar maiúsculas
   nem acentos): `nenhum`, `nenhuma`, `não`, `nunca`, `exceto`, `fora`,
   `sem`, `no`, `not`, `never`, `none`, `except`, `excluding`,
   `outside`. As outras linhas continuam como hoje.
2. **Títulos pt-BR aceitos por padrão.** Além dos títulos de hoje, um
   cabeçalho de nível 1–3 que **comece** por um destes (sem diferenciar
   maiúsculas nem acentos) satisfaz a seção, tanto no lint quanto na
   leitura da seção de posse:
   - Goal: `Objetivo`, `Meta`
   - Expected result: `Resultado esperado`, `Critérios de aceitação`,
     `Critérios de aceite`, `Pronto quando`
   - Owned files: `Arquivos`, `Escopo`
   - Forbidden: `Proibido`, `Fora do escopo`, `Restrições`
   - Report: `Relatório`
   `brief_lint_aliases` continua valendo e soma a estes. A comparação sem
   acentos vale também para os aliases configurados.
3. **Docs.** `SKILL.md` e `docs/guide.md`: onde descrevem as seções do
   brief e `brief_lint_aliases`, cite os títulos pt-BR aceitos e a regra do
   item de exclusão, em uma frase cada.

## Acceptance criteria

1. Testes, cada um com `// Mutation captured: …` executado:
   - o brief do caso real (reproduza a seção de posse acima) → `ownedPaths`
     sem `artifact.ts`/`fns.ts`, com os caminhos dos outros itens;
     `dispatch` com outro worker editando `src/server/fns.ts` → nenhum
     aviso de sobreposição;
   - um item `- Not \`x/y.ts\`` e `- **Exceto** \`a/b.ts\`` → fora;
     `- \`c/d.ts\` (não mexa no resto)` → `c/d.ts` continua dentro (a
     negação no meio da linha não conta);
   - brief só com `## Objetivo`, `## Resultado esperado`, `## Arquivos`,
     `## Proibido`, `## Relatório` e uma linha "sem commit/push" → lint
     sem faltas; `## Relatorio` (sem acento) também passa; `## Escopo` é
     lido como seção de posse;
   - `brief_lint_aliases` continua funcionando (teste existente verde).
2. `node --test` e `bun test --timeout 60000` dos arquivos de teste que
   cobrem `dispatch.mjs` (lint, owned, aliases) e dos novos → 0 fail
   (cole). Goldens que mudarem: diff decodificado e o motivo.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/lib/dispatch.mjs` (só as funções citadas e
  auxiliares novas), testes em `skills/herdr-soho/scripts/test/` para
  elas, os goldens que mudarem por isso
- `skills/herdr-soho/SKILL.md`, `docs/guide.md` (só as frases do item 3)

## Forbidden

- Todo o resto, e qualquer arquivo de outro worktree. Nenhuma chamada ao
  Herdr real que escreva: sondas só com `herdr` falso no `PATH` e
  `HERDR_SOCKET_PATH` apontando para um caminho inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
