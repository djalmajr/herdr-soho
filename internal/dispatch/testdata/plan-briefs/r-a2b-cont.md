# Revisão R-A2b (continuação) — correções do `queued`

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Terminar uma revisão que um revisor Claude começou e parou por falta de cota. Ele deixou uma passagem
de bastão com 2 achados P2 já provados, o que foi verificado e o que falta. Confira os dois achados
(reproduza pelo menos um), faça os itens que faltam e entregue o relatório completo.

- Brief original da revisão (escopo, worktree, commit, critérios):
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/r-a2b-review.md`.
- Passagem de bastão (achados, itens feitos, itens que faltam, sondas em `/tmp` para reaproveitar):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T225746.md`.

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
