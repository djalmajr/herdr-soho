# Brief — Revisão R7: teste de `status` com cwd explícito (PR #12)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `16e5dc1` (branch `fix/wait-status-activity`, PR #12).
Confirme com `git log --oneline -1` e revise só `git diff HEAD~1 HEAD`.

## Goal

Confirmar que o teste corrigido prova o formato novo do `status` sem
depender do relógio, antes do merge da PR #12.

## Contexto decidido

A PR #12 acrescentou `task_s` e `activity_s` à linha TSV do `status`. O
teste `status resolves state under its explicit cwd` em `lanes.test.mjs`
ainda esperava a linha de três colunas e falhava na própria branch. A
correção fixa os mtimes do marcador `last-report-build` (1000) e do
relatório (1042) e espera `build\tdone\t<report>\t42\t-`.

## O que verificar

1. O valor `42` sai do cálculo de `status.mjs` (mtime do relatório menos
   mtime do marcador) e não de coincidência; `-` em `activity_s` vale
   porque o estado é `done`.
2. O teste continua provando o que o nome diz (estado resolvido sob o cwd
   pedido, não o do chamador).
3. Uma mutação: trocar a origem do `task_s` (ex.: `Date.now()` como fim)
   faz o teste falhar.

## Checks you may run

Leitura, `git diff/log/show`; `node --test` e `bun test` só em
`skills/herdr-soho/scripts/test/lanes.test.mjs`; mutações numa cópia em
`/tmp`. Não rode a suíte completa.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
