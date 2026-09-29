# Brief — Revisão R2b: delta da correção do P2 da R2

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está no commit `046b976`. Revise só `git diff 4b593e0 046b976`.

## Goal

Confirmar, com execução, que o P2 da R2 foi resolvido e que o delta não
reabriu o risco principal (tela estática ou só lida virando checkpoint).

## Mudança decidida (verifique que o código cumpre)

- A comparação de hash e os marcadores (`stuck-hash`, `stuck-since`,
  `activity-at`) acompanham toda sondagem de agente `working`, qualquer que
  seja `stuck_warn_minutes`; só o aviso de tela parada depende de
  `stuck_warn_minutes > 0`.
- Toda sondagem que obteve tela não vazia grava `wait/<agent>.probe-at`.
- Uma mudança observada é datada na sondagem anterior (`probe-at`, senão
  `stuck-since`), o momento mais antigo em que pode ter ocorrido; sem
  nenhum dos dois, não é datada (sem `activity-at`).
- `activityAgeSeconds` com hash diferente: `nowS - probe-at` (senão
  `stuck-since`), `null` sem ambos; o `status` segue só leitura.
- O dispatch limpa `probe-at`.

## O que verificar

1. Repita as suas fixtures `minutes-0-stale-mismatch` e
   `minutes-non-numeric-stale-mismatch`.
2. Lacuna longa entre dois `wait` (hash antigo de outra tela, `probe-at`
   de 30 min) não vira checkpoint ativo.
3. Movimento real entre sondagens do mesmo `wait` continua ativo; tela
   estática, só contador e leitura vazia continuam inativas.
4. Texto novo da SKILL.md (parágrafos do `timeout` e do `status`) bate
   com o código.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; `run-tests.sh --env outside <suite>`;
sondas em `/tmp` com `HOME` temporário. Antes de afirmar que algo falha,
rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois `[done]`/`[partial]` por ponto e qualquer achado novo com
`arquivo:linha` e evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
