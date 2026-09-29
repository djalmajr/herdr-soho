# Brief — Revisão R5b: delta das correções da R5 (kit de avaliação)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `feat/eval-protocol`. Revise só `git diff HEAD~1 HEAD`.

## Goal

Confirmar, com execução, que os seis achados da R5 foram resolvidos sem
regressão.

## O que verificar

1. Repita as suas variações `duplicate-unlink` e `delete-tmp-and-symlink`
   contra as sondas novas (devem reprovar) e a referência (6/6).
2. Cenários do gabarito executáveis como escritos (repita a sua sonda).
3. Rubricas de reviewer e specialist sem resultado sem escore.
4. Registros do piloto: escore 2 do revisor, escore 3 dos implementers como
   inferência, a nova observação de reexecução das sondas.

## Checks you may run

Leitura, `git diff/log/show`; `node --test evals/test/`, `bun test evals/test/`;
`node evals/prepare.mjs …`, `node evals/run-probes.mjs …` em `/tmp`. Antes de
afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois `[done]`/`[partial]` por ponto e achados novos com `arquivo:linha` e
evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
