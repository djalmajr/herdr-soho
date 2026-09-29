# Brief — B4 (retomada no Codex): gates, mutações e relatório

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Fechar a fatia B4 (lint e posse do brief com títulos pt-BR e itens de
exclusão), que já tem o código e os testes novos, e entregar o relatório.

Worktree `/work/herdr-soho/.worktrees/build-2`,
branch `fix/brief-lint-ptbr`, base `main` `6b764d9` + diff não commitado
(4 arquivos). Caminhos relativos a essa raiz.

Leia, nesta ordem:
1. O contrato (decisões, critérios, arquivos): `/work/herdr-soho/.herdr-soho/w14/plan-briefs/b4-brief-lint.md`
2. A passagem de bastão do implementador anterior: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/handoff-b4.md`

## Expected result

- Confira o `git diff` contra cada decisão e critério do contrato;
  complete o que faltar por edição. Nunca use `git checkout`/`git restore`.
- Execute as mutações de cada `// Mutation captured` em cópia fora do
  worktree checada com `herdr-soho mutation-guard`; cole a saída vermelha.
- `node --test` e `bun test --timeout 60000` dos arquivos de teste que
  cobrem `dispatch.mjs` (lint, owned, aliases) → 0 fail (cole). Goldens que
  mudarem: diff decodificado.
- O relatório vai no caminho do **seu** contrato (o do dispatch), não no
  caminho antigo citado na passagem.

## Owned files

Os do contrato: `skills/herdr-soho/scripts/lib/dispatch.mjs` (só as
funções citadas e auxiliares novas), os testes delas, os goldens que
mudarem por isso, `skills/herdr-soho/SKILL.md` e `docs/guide.md` (só as
frases do item 3).

## Forbidden

- Todo o resto e qualquer outro worktree. Nenhuma chamada ao Herdr real
  que escreva: `herdr` falso no `PATH` e `HERDR_SOCKET_PATH` inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por decisão e critério
`[done]` / `[partial]` / `[skipped]` + motivo, com saídas coladas e as
mutações executadas; diga o que mudou em relação ao diff herdado.
