# Revisão R-A2c — confirmação final do `queued` (JS)

Role: reviewer · Report language: pt-BR

## Goal

Última revisão antes do PR do `queued` para a `main`. A revisão anterior (A2b) reprovou com dois P2 e
seis lacunas de teste, e já tinha testado a correção numa cópia. Confirmar que o commit `25e4708`
fecha tudo sem abrir outra coisa. O código roda hoje em todo `dispatch`/`wait`/`status` de quem usa
a skill; um prompt tratado como perdido quando está na fila, ou o contrário, faz o orquestrador
reenviar trabalho ou esperar para sempre.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-a2c`, em `25e4708`
  (HEAD destacado, branch `fix/dispatch-queued`; pai `78c3665`). Diff: `git -C <worktree> show 25e4708`.
  O branch inteiro contra a `main`: `git -C <worktree> diff main...25e4708`.
- Revisão anterior (F1–F8, cenários `n-unavailable` e `n-wrap`, tabela de mutantes A–N15):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T225943.md`.
- Brief da correção: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/a2c-queued-fix.md`.
- Relatório do implementador:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-3-20260928T233505.current.md`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. F1–F8 da revisão anterior: fechado, parcial ou aberto, com a sonda reexecutada contra o `25e4708`.
2. A tabela de mutantes A–N15 reexecutada por você (não a do implementador), com o código de saída.
3. O branch inteiro contra a `main` (A2 + A2b + A2c): um cenário de ponta a ponta com herdr falso
   para cada estado (`queued` que chega, `queued` que se perde, cota, provider, `blocked`, `gone`,
   `unavailable` transitório, painel estreito), dispatch → wait → status.
4. `node --test` e `bun test` em `wait`, `status`, `dispatch-arrival`, `dispatch`, `stats` (cole os
   resumos).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real: todo teste usa herdr falso, `HERDR_SOCKET_PATH` para um caminho inexistente e ids
  impossíveis como `w0test:p0a`. Nunca mande nada a um painel de verdade. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
