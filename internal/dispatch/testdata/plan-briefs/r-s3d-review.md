# Brief — Revisão R-S3d: saída do seletor por sinal e notificação limpa

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `8788b05` (branch `feat/session-picker`). Confirme com
`git log --oneline -1` e revise `git diff e056644 HEAD` (dois commits: a
notificação limpa `e4b37c9` e a saída por sinal `8788b05`). Revisão
anterior (os dois achados que `8788b05` corrige):
`/tmp/herdr-soho/w14/reports/soho-grok-r4-20260928T134422.md`.

## O que verificar

1. Repita as sondas de pty da revisão anterior: `SIGTERM` e `SIGHUP` no
   primeiro quadro, ~350 ms depois e depois de "carregando windows…",
   com `find` falso pendurado que grava o pid — o seletor sai em < 1 s,
   nenhum `find` fica vivo, nenhum quadro depois do sinal.
2. Esc, Ctrl-C e Enter continuam como antes (terminal restaurado, cópia
   só no Enter).
3. O corpo da notificação sai sem controles.
4. As mutações declaradas falham os testes.

## Checks you may run

Leitura, `git diff/log/show`; `node --test plugin/test/` e
`bun test --timeout 60000 plugin/test/`; sondas em `/tmp` com pty.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhuma chamada ao Herdr real que escreva: sondas só com `herdr` falso
  no `PATH` e `HERDR_SOCKET_PATH` apontando para um caminho inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
