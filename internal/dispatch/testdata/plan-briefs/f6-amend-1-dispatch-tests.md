# Emenda 1 — F6: atualizar as expectativas de `dispatch.test.mjs`

## Goal

Levar `dispatch.test.mjs` ao contrato deste brief, sem mudar código de
produção.

## Decisions already made

- `skills/herdr-soho/scripts/test/dispatch.test.mjs` passa a ser seu
  (Owned files), **só** para expectativas: a chave `task_report` na posição
  decidida (logo depois de `report`, antes de `settled_report` /
  `report_exists`) e os diagnósticos novos de `--for` (as duas mensagens
  exatas do item 2). Não relaxe nem remova asserções: ajuste o valor
  esperado. Se algum teste só passa enfraquecendo a asserção, pare e marque
  `[partial]` com o motivo.
- Nenhum arquivo de produção muda nesta emenda.

## Expected result

`node --test` e `bun test` de
`task-report.test.mjs for-released.test.mjs dispatch.test.mjs collect.test.mjs stats.test.mjs wait.test.mjs parity-dispatch.test.mjs parity-wait.test.mjs`
→ 0 fail (cole os resumos).

## Owned files

- `skills/herdr-soho/scripts/test/dispatch.test.mjs`

## Forbidden

Qualquer outro arquivo. No commit, push, tag, or PR. The orchestrator owns
git.

## Report

O mesmo relatório da F6, cobrindo o brief e esta emenda, por item
`[done]` / `[partial]` / `[skipped]`, com as saídas coladas.
