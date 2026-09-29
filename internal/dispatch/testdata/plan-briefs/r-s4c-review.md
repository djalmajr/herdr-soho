# Brief — Revisão R-S4c: corpo citado e testes isolados do `send`

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `d5acbc5` (branch `feat/peer-send`). Confirme com
`git log --oneline -1` e revise `git diff 2c0bf13 HEAD`. Brief da fatia:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4c-quote-and-isolate.md`.

## Por que esta fatia existe

Uma sonda de revisão anterior rodou `herdr-soho send` contra o Herdr real
e mandou duas mensagens a um painel vivo de outra equipe. Esta revisão
**não pode** repetir isso.

## O que verificar

1. Corpo citado (`> ` por linha, linha vazia `>`), quarta linha do
   cabeçalho exata, cabeçalho forjado no corpo sai citado.
2. Nenhum id real nos testes do `send`; `HERDR_SOCKET_PATH` isolado em
   todo teste; o teste de guarda prova que o `herdr` real com socket
   isolado não envia nada.
3. As mutações declaradas falham os testes.
4. Nada mais mudou no `send`.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `setup*.test.mjs`, `parity-setup.test.mjs`,
`parity-entry.test.mjs`. Sondas **só** com `herdr` falso no `PATH` **e**
`HERDR_SOCKET_PATH` apontando para um caminho inexistente.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma chamada ao Herdr real que escreva** e nenhum `herdr-soho send`
  sem as duas proteções acima; nada de `agent prompt`, `send-keys`,
  painéis ou notificações reais.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
