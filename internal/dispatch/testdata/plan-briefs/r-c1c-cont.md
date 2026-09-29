# Revisão R-C1c (continuação) — `state` e `tasks` em Go, e os cinco comandos

Role: reviewer · Agent: review-2 · Report language: pt-BR

## Goal

Terminar uma revisão que um revisor Claude começou e parou por falta de cota. Ele deixou uma passagem
de bastão com 8 achados (1 P1, 1 P2, 6 P3) até aqui, o que foi verificado e o que falta. Confira os achados
(reproduza o P1 e o P2), faça os itens que faltam e entregue o relatório completo.

- Brief original da revisão (escopo, worktree, commit, critérios):
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/r-c1c-review.md`.
- Passagem de bastão (achados, itens feitos, itens que faltam, sondas em `/tmp` para reaproveitar):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T225748.md`.

## Expected result

O relatório completo, no formato do brief original: primeira linha
`findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`, os achados (os da passagem que você
confirmar, com a sua prova, e os novos), e por item do brief original `[done]` / `[partial]` /
`[skipped]` com o motivo. Um achado da passagem que você não conseguir reproduzir vai com
`unverified` e o motivo, não some.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, no formato acima.
