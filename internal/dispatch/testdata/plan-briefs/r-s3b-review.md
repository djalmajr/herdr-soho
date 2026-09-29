# Brief — Revisão R-S3b: emenda do seletor (sete achados)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `e056644` (branch `feat/session-picker`). Confirme com
`git log --oneline -1` e revise `git diff 733d50d HEAD` (a emenda sobre o
que a revisão anterior viu). Revisão anterior (os sete achados):
`/tmp/herdr-soho/w14/reports/soho-grok-r3-20260928T115559.md`.
Brief da emenda: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s3-amend-1.md`.

## Goal

Confirmar que os sete achados foram resolvidos sem regressão.

## O que verificar

1. Repita as sondas da revisão anterior (redesenho com `\x1b[J`; campos
   com CSI/OSC/quebra de linha na tela e no texto copiado; `SIGTERM` e
   `SIGHUP` com um `find` falso pendurado — terminal restaurado e filho
   morto; primeiro quadro; `é` cortado entre chunks; argv do PowerShell
   com texto por stdin contendo aspas, `$` e acento; teste de palavras com
   a mutação `includes`).
2. Nada mais mudou (manifesto, ação `pick`, formato copiado, teclas).
3. As mutações declaradas falham os testes.

## Checks you may run

Leitura, `git diff/log/show`; `node --test plugin/test/` e
`bun test --timeout 60000 plugin/test/`; sondas em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma chamada ao Herdr real que escreva**: nada de abrir/fechar
  painéis, ligar o plugin, `agent prompt`, `send-keys`, `notification
  show`, nem rodar `herdr-soho send`. Toda sonda usa um `herdr` falso no
  `PATH` e `HERDR_SOCKET_PATH` apontando para um caminho inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
