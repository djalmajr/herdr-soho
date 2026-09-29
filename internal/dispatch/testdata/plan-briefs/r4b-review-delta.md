# Brief — Revisão R4b: delta da correção do P1 da R4

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `fix/task-report-for-released` (topo: a correção).
Revise só `git diff HEAD~1 HEAD`.

## Goal

Confirmar, com execução, que o P1 da R4 foi resolvido sem regressão.

## Mudança decidida (verifique que o código cumpre)

- `syncTaskReport`: se `pointer.current` não é legível ou está vazio, usa
  `<state>/reports/<basename(current)>` (o espelho do `wait`) quando é outro
  arquivo; remove a cópia estável só quando os dois faltam.
- `collect` imprime `<!-- task report: … -->` só se a cópia existir depois
  do sync.

## O que verificar

1. Repita a sua sonda `collect-after-tmp-reaped`.
2. Emenda pendente no layout `$TMPDIR` continua sem cópia estável (nenhum
   espelho com o nome da emenda).
3. Nenhum caminho em que o espelho de outra tentativa seja usado por engano.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; sondas em `/tmp` com `HOME`/`TMPDIR`
temporários. Antes de afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois `[done]`/`[partial]` por ponto e achados novos com `arquivo:linha`
e evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
