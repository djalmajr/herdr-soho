# Brief — Revisão R-B4b: correções da revisão do lint pt-BR

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `984e039` (branch `fix/brief-lint-ptbr`) fecha os três
achados da revisão anterior do B4 sem abrir outro. Seu cwd está em
`984e039`; revise `git diff HEAD~1 HEAD` (e `git diff HEAD~2 HEAD` para o
B4 inteiro). Revisão anterior, com as sondas:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T174500.md`.
Emenda (o contrato da correção):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/b4b-fix-r12.md`.
Relatório: `/tmp/herdr-soho/w14/reports/build-2-20260928T180117.md`.

## O que verificar com atenção

1. Repita as sondas da revisão anterior: `no-arquivo`, `sem-mudar`,
   `fora-testes` agora mantêm o caminho; `nenhum` exclui;
   `arquivos-proibidos-*`, `escopo-fora`, `metadados`/`metadata` não
   passam; `## Meta`, `## Arquivos — donos`, `## Escopo:` passam.
2. A regra da fronteira com acentos (`## Relatório`, `## Relatorio`,
   `## Critérios de aceitação`) e com títulos que continuam por
   pontuação diferente (`## Arquivos (donos)`, `## Arquivos/escopo`).
3. Os títulos em inglês e os aliases configurados mantiveram o prefixo
   simples.
4. Testes e mutações (rode duas).

## Forbidden

- Editar qualquer arquivo. Nada ao Herdr real que escreva.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois os achados com severidade, `arquivo:linha`, cenário, evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
