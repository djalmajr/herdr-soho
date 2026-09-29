# Brief — Revisão R-A1b: correções da revisão do dispatch arrival

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `6a89a16` fecha os quatro achados da revisão anterior
sem abrir outro, para o branch `fix/dispatch-arrival` ir à `main`.

Seu cwd está em `6a89a16` (branch `fix/dispatch-arrival`). Confirme com
`git log --oneline -1` e revise `git diff HEAD~1 HEAD` (a fatia) e, para
contexto, `git diff HEAD~2 HEAD` (o A1 inteiro). Revisão anterior, com as
sondas que você pode reusar:
`/tmp/herdr-soho/w14/reports/soho-grok-r7-20260928T150930.md`.
Brief da correção:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/a1-fix-r7.md`.
Relatório do implementador:
`/tmp/herdr-soho/w14/reports/soho-agy-a1b-20260928T153451.md`.

## O que verificar com atenção

1. Rode de novo as sondas A–D e K da revisão anterior: A e D agora saem 0
   sem Enter nem reenvio? B e C continuam 0?
2. A regra 4 antes da regra 2 abre um falso "recebido"? Cenário: o texto
   está parado na caixa de entrada **e** o caminho aparece acima das 3
   últimas linhas do `recent-unwrapped` (um prompt longo que quebra em
   várias linhas na caixa), com a tela mudando porque o texto foi
   digitado. Isso é dado como recebido sem ter sido enviado? Se sim, qual
   sinal distingue os dois casos?
3. Os casos (a) e (d) agora pegam as mutações que declaram (repita as
   duas mutações da revisão anterior em cópias em `/tmp`).
4. O `FAKE_CHECK_VISIBLE_EQUAL` novo no falso: só nos testes? Algum teste
   existente enfraquecido?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`dispatch-arrival.test.mjs`, `dispatch.test.mjs`,
`parity-dispatch.test.mjs`, `task-report.test.mjs`,
`for-released.test.mjs`, `parity-config.test.mjs`; mutações em cópias em
`/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhuma chamada ao Herdr real que escreva (`dispatch` real, `agent
  prompt`, `send-keys`, painéis): sondas só com `herdr` falso no `PATH` e
  `HERDR_SOCKET_PATH` apontando para um caminho inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
