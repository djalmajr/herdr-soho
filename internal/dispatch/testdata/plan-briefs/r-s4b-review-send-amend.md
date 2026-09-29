# Brief — Revisão R-S4b: emenda do `send` (timeout, controles, inbound sem cwd)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `2c0bf13` (branch `feat/peer-send`). Confirme com
`git log --oneline -1` e revise `git diff 014d1ee HEAD` (a emenda sobre o
que a revisão anterior viu). Revisão anterior (os três achados que a
emenda corrige):
`/tmp/herdr-soho/w14/reports/soho-grok-r-20260928T113542.md`.

## Goal

Confirmar que os três achados foram resolvidos sem regressão.

## Contrato decidido

1. `code: 'timeout'` do `agent prompt` → exit 15 (`did not take the
   message (timeout)`), sem reenvio, log `timeout`.
2. O corpo e os campos do remetente no cabeçalho perdem todo caractere de
   controle, exceto `\n` e `\t`, e os marcadores de bracketed paste
   (`ESC[200~`, `ESC[201~`); o cabeçalho verdadeiro vem sempre primeiro.
3. Alvo local sem `cwd` ainda tem o `inbound` lido (usuário e padrão,
   nunca o projeto de quem envia).

## O que verificar

1. Cada item com a sonda da revisão anterior (repita-as).
2. Texto legítimo sobrevive: acentos, emoji, `\t`, várias linhas, aspas,
   `$`, crases.
3. Os testes novos pegam as mutações que declaram.
4. Nada mais mudou no `send`.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`send.test.mjs`, `config*.test.mjs`, `setup.test.mjs`,
`parity-setup.test.mjs`, `parity-entry.test.mjs`; sondas em `/tmp` com
`herdr` falso. Não mande mensagem a pane real.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Mandar prompt, teclas ou mensagem a qualquer pane.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
