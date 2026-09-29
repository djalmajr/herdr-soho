# Brief — Revisão R-S2: `herdr-soho find`

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd agora está em `3ff4148` (branch `feat/session-find`, sobre a base
`c1596eb`, que traz `lib/sessionref.mjs`). Confirme com
`git log --oneline -1` e revise `git diff HEAD~2 HEAD` (a base e o `find`).
Brief da fatia: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s2-find.md`.

## Goal

Achados ancorados em `arquivo:linha`, com execução, antes do push.

## O que verificar

1. O contrato do brief (campos, busca em E, referência casando só o seu
   pane e consultando a sua máquina, `--all` só com máquinas `enabled`,
   TSV/JSON, exit 0/1/2/4, falha remota em stderr).
2. `parseRef` (`lib/sessionref.mjs`): aceita/recusa o que deve? Algum id
   de pane real do Herdr 0.9.1 (`w14:pR`, `w12:p1`, `w6:p1`) ou rótulo de
   máquina salvo fica de fora?
3. Snapshots reais: rode `node skills/herdr-soho/scripts/herdr-soho.mjs
   find soho` e `… find --json --machine windows pinar` (só leitura; a
   `windows` leva ~5 s). Nada de `--all` com a `linux` além de uma vez.
4. Um pane sem agente, um agente sem `cwd`, e um workspace sem rótulo não
   quebram a saída.
5. Os testes pegam as mutações que declaram.

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`find.test.mjs` e `sessionref.test.mjs`; o próprio `find` (só leitura).

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
