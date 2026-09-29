# Brief — Revisão R-D1c: o seu achado P3 (tabela `set` em várias linhas)

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `cd50239` fecha o achado P3 da sua revisão anterior
(`}` dentro de string numa tabela `set` em várias linhas) sem abrir
outro. O código desta fatia foi escrito pelo orquestrador (Claude).

Seu cwd está em `cd50239` (branch `fix/codex-env-diagnostic`). Confirme
com `git log --oneline -1` e revise `git diff HEAD~1 HEAD`. Sua revisão
anterior:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T164604.md`.

## O que verificar

1. Repita as suas provas do achado (`multiline-all-three`,
   `multiline-only-env`, `single-line-brace`, `normal-multiline`) e compare
   com `codex sandbox -- /usr/bin/printenv` com `CODEX_HOME` temporário.
2. `inlineTableClosed`: aspas simples (literal, sem escape) e duplas (com
   `\`), `{` dentro de string, tabela nunca fechada (fim do arquivo ou
   próximo cabeçalho de seção), tabela aninhada.
3. O teste novo pega a mutação que declara.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`codex-env.test.mjs`, `doctor.test.mjs`, `parity-doctor.test.mjs`,
`herdr.test.mjs`; mutações em cópias em `/tmp`; `codex sandbox` com
`CODEX_HOME` temporário.

## Forbidden

- Editar qualquer arquivo do checkout ou `~/.codex/config.toml`. Revisor é
  somente leitura.
- Nenhuma chamada ao Herdr real que escreva e nenhum `herdr pane current`.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
