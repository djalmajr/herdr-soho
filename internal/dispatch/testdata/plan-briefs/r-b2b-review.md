# Brief — Revisão R-B2b: os seus dois achados do estado entre worktrees

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `00d1ca2` (HEAD de `fix/state-across-worktrees`; confirme com
`git log --oneline -1`) fecha os dois achados da sua revisão anterior sem
abrir outro. Revise `git diff HEAD~1 HEAD` (e `git diff HEAD~2 HEAD` para
o B2 inteiro). Sua revisão, com as sondas:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T181350.md`.
Emenda (o contrato da correção):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/b2b-fix-r12.md`.
Relatório: `/tmp/herdr-soho/w14/reports/build-3-20260928T182945.md`.

## O que verificar com atenção

1. Repita as suas sondas (bare com worktree, submódulo com worktree,
   submódulo sem worktree, fora de git) e os três arranjos do aviso.
2. O caminho do `core.worktree` do submódulo: relativo ao diretório comum,
   ausente, apontando para dentro do git. Windows (barras, drive).
3. Custo: quantos processos `git` por comando do `herdr-soho` num worktree
   comum (fora de worktree ligado deve continuar igual a hoje)?
4. Testes e mutações (rode duas).

## Forbidden

- Editar qualquer arquivo do checkout. Nada ao Herdr real que escreva.
- Nenhum comando git que escreva neste repositório (nos temporários de
  `/tmp`, pode). No commit, push, tag, or PR. The orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois os achados com severidade, `arquivo:linha`, cenário, evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
