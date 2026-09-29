# Brief — Revisão R-S4e: prova de chegada do `send` sem reenvio

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `fb9365b` fecha os cinco achados da revisão anterior do
`send` sem abrir outro, para o branch `feat/peer-send` seguir para a
integração.

Seu cwd está em `fb9365b` (branch `feat/peer-send`). Confirme com
`git log --oneline -1` e revise `git diff HEAD~1 HEAD`. Revisão anterior,
com as sondas que você pode reusar:
`/tmp/herdr-soho/w14/reports/soho-grok-r8-20260928T152710.md`.
Brief da correção (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4e-arrival-proof-fix.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-agy-s4e-20260928T154820.md`.

## O que verificar com atenção

1. Rode de novo as sondas da revisão anterior (corpo de 57 linhas,
   leitura `recent` falhando, `[y/N]` citado, leitura visível falhando,
   janela 999999). Algum caminho ainda chama `agent prompt` duas vezes?
2. **Falso "entregue".** Prova (a): o `state_change_seq` pode mudar por
   outro motivo logo depois do prompt (o alvo ainda estava terminando uma
   resposta, ou o texto caiu num diálogo que mudou o estado)? Prova (b):
   uma mensagem de várias linhas parada na caixa de entrada, com a tela
   mudando porque foi digitada, pode ter a linha final fora das últimas 15
   linhas visíveis (caixa alta, mensagem longa) e ser dada como entregue?
   Diga quais são alcançáveis e proponha (sem decidir).
3. **Enter.** O Enter único vai só quando houve alguma leitura bem-
   sucedida? Pode cair num diálogo que apareceu depois do prompt?
4. **Diálogo.** Padrões de confiança nas últimas 20 linhas com qualquer
   status; marcadores de `dialog.mjs` só com `blocked`. O status usado é o
   lido na hora, ou um antigo?
5. Formato: primeira linha começa por `PEER_PREFIX` (constante, não
   literal), linha final exata, bloco de setup inalterado e ainda correto,
   docs coerentes com códigos 4/15/17 e logs `lost`/`unverified`/
   `unreadable`/`dialog`.
6. Testes existentes enfraquecidos? Os novos pegam as mutações que
   declaram (rode ao menos três em cópias em `/tmp`)?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `setup*.test.mjs`, `parity-setup.test.mjs`,
`parity-entry.test.mjs`; mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma execução do `send` fora dos testes**, e nenhuma chamada ao
  Herdr real que escreva (`agent prompt`, `send-keys`, painéis): sondas só
  com `herdr` falso no `PATH` **e** `HERDR_SOCKET_PATH` apontando para um
  caminho inexistente, com ids de painel impossíveis (`w0test:p0a`).
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
