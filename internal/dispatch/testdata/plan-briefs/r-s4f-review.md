# Brief — Revisão R-S4f: provas de chegada do `send` (a sua revisão anterior)

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `9dd1c13` fecha os quatro achados da sua revisão
anterior do `send` sem abrir outro, para `feat/peer-send` seguir para a
integração.

Seu cwd está em `9dd1c13` (branch `feat/peer-send`). Confirme com
`git log --oneline -1` e revise `git diff HEAD~1 HEAD`. Sua revisão
anterior (reuse as sondas de `/tmp/s4e-r11/probe.test.mjs`, se ainda
existirem):
`/tmp/herdr-soho/w14/reports/soho-grok-r11-20260928T162634.md`.
Brief da correção (o contrato):
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4f-proof-tighten.md`.
Relatório do implementador (Codex, que terminou o que um pi começou):
`/tmp/herdr-soho/w14/reports/build-20260928T173141.md`.

## O que verificar com atenção

1. Suas sondas de antes (`now-working-seq-moves-id-absent`,
   `seq-moves-id-absent-dialog-screen`, `enter-into-dialog-after-prompt`,
   `end-line-above-last-15`, `viewport-clips-end-line`,
   `wrapped-end-line`, `stale-status-question-after-wait`) agora dão o
   resultado que o contrato pede?
2. **Falso negativo.** Com as provas mais estritas, o caso comum ainda sai
   `sent`? Alvo `idle`, mensagem curta de 3 linhas, o agente começa a
   responder (seq muda para `working`): `sent` sem Enter. Alvo cujo CLI o
   Herdr não acompanha bem (seq nunca muda) e mensagem curta que continua
   visível no histórico: o que acontece (Enter? `lost`?)? Isso é
   aceitável? Proponha, sem decidir.
3. Normalização (remoção de espaço e desenho de caixa): pode fazer o
   `#<id>` casar com texto que não é a mensagem?
4. Testes enfraquecidos? Mutações declaradas que não pegam (rode três)?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `setup*.test.mjs`, `parity-setup.test.mjs`,
`parity-entry.test.mjs`; mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- **Nenhuma execução do `send` fora dos testes**, e nenhuma chamada ao
  Herdr real que escreva: `herdr` falso no `PATH` **e**
  `HERDR_SOCKET_PATH` inexistente, ids impossíveis (`w0test:p0a`).
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
