# Brief — Revisão R14: timeouts explícitos para o Bun no Windows (issue #17)

Role: reviewer · Agent: review · Report language: pt-BR

Seu cwd está em `fix/test-portability` `3e1abf0`. Confirme com
`git log --oneline -1` e revise só `git diff HEAD~1 HEAD`.

## Goal

Confirmar que o commit só dá timeout explícito a cinco testes, sem mudar o
que eles provam.

## Contexto decidido

No Windows (Bun 1.4.0), cinco testes passaram do timeout padrão de 5 s do
Bun (`this test timed out after 5000ms`); o `spawn` de um deles voltou
com `status null` porque o Bun matou o filho. No Node (sem limite
padrão) e no Bun do macOS/Linux eles passam. Correção: `{ timeout:
60_000 }` nesses cinco, como vários testes do mesmo arquivo já têm.

## O que verificar

1. Só as opções de timeout mudaram; nenhum corpo de teste.
2. Outro teste com vários spawns e sem timeout explícito corre o mesmo
   risco? (liste arquivo:linha dos que passam de ~2,5 s no `bun test`
   daqui, como candidatos; não é achado se estiver abaixo disso).

## Checks you may run

Leitura, `git diff/log/show`; `bun test` e `node --test` nos três
arquivos tocados. Não rode a suíte inteira.

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
