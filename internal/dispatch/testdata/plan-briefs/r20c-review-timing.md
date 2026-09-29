# Brief — Revisão R20c: margens de tempo dos testes (#20)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `b3223ea` (branch `fix/win-cmd-timeout-tree`). Confirme
com `git log --oneline -1` e revise só `git diff HEAD~1 HEAD` (código do
orquestrador, só testes).

## Goal

Confirmar que as margens novas continuam provando o que cada teste diz.

## Contexto decidido

Numa rodada completa com agentes rodando na mesma máquina, três asserts
de tempo passaram do limite (5,6 s contra 5 s; o fake do neto nem saiu em
800 ms; `task_s` 106 contra 105). As mudanças: testes do treekill com
limite de 10 s (os tetos que eles provam evitar são 15,8 s ou mais); o
teste do neto com `timeoutMs` 5000 e limite 12 s (teto 20 s); a janela
de `task_s` em `status-ages` passa a 99–130 s.

## O que verificar

1. Cada limite novo continua abaixo do teto que o teste prova evitar, com
   folga para distinguir "voltou no timeout" de "voltou no teto".
2. As mutações declaradas ainda falham os testes com os limites novos
   (rode ao menos a do neto: o helper antigo com `inherit` no modo padrão).
3. A janela de `task_s` ainda pega um cálculo errado (ex.: fim em
   `Date.now()` com relatório presente, ou início errado).

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`treekill.test.mjs` e `status-ages.test.mjs`; mutações em cópias em `/tmp`.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
