# Brief — Revisão R3b: delta da correção do P2 da R3 (mutation-guard)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está na branch `fix/mutation-guard` (topo: a correção). Revise só
`git diff HEAD~1 HEAD`.

## Goal

Confirmar, com execução, que o P2 da R3 foi resolvido sem regressão.

## Mudança decidida (verifique que o código cumpre)

- `CARGO_TARGET_DIR`, `CARGO_BUILD_TARGET_DIR` e `--env NAME` relativos
  resolvem contra a cópia; valor vazio é ignorado; o valor nunca é impresso.
- `target-dir` em `.cargo/config[.toml]` aceita string básica TOML
  (`"…"`, com escapes) e literal (`'…'`); relativo resolve contra a cópia
  (onde o Cargo o resolve); erro de resolução falha fechado.

## O que verificar

1. Repita as suas quatro sondas da R3 (relativo, aspas simples absoluto,
   `CARGO_TARGET_DIR` relativo, snapshot sem escrita).
2. Falso positivo novo: cópia isolada com `target-dir = 'target'` ou
   `CARGO_TARGET_DIR=target` precisa sair 0.
3. Texto da SKILL.md sobre o guard bate com o código.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test` em
`skills/herdr-soho/scripts/test/`; sondas em `/tmp` com `HOME` temporário.
Antes de afirmar que algo falha, rode e cite a saída.

## Forbidden

- Editar qualquer arquivo. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.
- Imprimir variáveis de ambiente ou linhas de comando de processos.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois `[done]`/`[partial]` por ponto e achados novos com `arquivo:linha`
e evidência.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
