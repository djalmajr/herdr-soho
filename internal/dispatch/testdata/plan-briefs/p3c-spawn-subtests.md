# Brief — P3c: subtestes não aguardados em `spawn.test.mjs`

Role: implementer · Agent: build · Report language: pt-BR

Worktree própria, branch `fix/test-portability-linux` (base:
`fix/test-portability` `08324e6`). Caminhos relativos à raiz dela. Issue:
https://github.com/djalmajr/herdr-soho/issues/17.

## Goal

Nenhum subteste da suíte da skill é cancelado por terminar depois do pai,
em máquina mais lenta.

## Decisions already made

- Numa máquina Linux (Node 22.23, usuário comum), a suíte deu 977/980 e
  uma falha:
  ```
  not ok 815 - spawn: a session set after the spawn blocks the reuse
    location: '.../skills/herdr-soho/scripts/test/spawn.test.mjs:2062:1'
    error: '2 subtests failed'
      not ok 2 - cross-role
        error: 'test did not finish before its parent and was cancelled'
      not ok 3 - lane
        error: 'test did not finish before its parent and was cancelled'
  ```
  No macOS passa. Causa provável: `t.test(...)` sem `await` (ou o pai não
  espera os filhos); confirme lendo o código.
- Corrija esse teste e **qualquer outro** `t.test(` não aguardado em
  `skills/herdr-soho/scripts/test/*.test.mjs` e `plugin/test/*.test.mjs`
  (procure com `grep -n "t\.test(" …` e confira cada um). A correção é
  aguardar os subtestes; o que cada um prova não muda.

## Expected result

Todos os `t.test(` aguardados; a lista no relatório (arquivo:linha).

## Acceptance criteria

1. `grep -n` de todos os `t.test(` com a indicação de cada um aguardado
   (cole).
2. Prova de que o cancelamento acontecia e não acontece mais: rode o teste
   com a máquina lenta simulada (ex.: um subteste com `await new
   Promise(r => setTimeout(r, 200))` numa cópia em `/tmp`) antes e depois;
   cole as duas saídas.
3. `node --test` e `bun test` dos arquivos tocados → 0 fail (cole).

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `skills/herdr-soho/scripts/test/*.test.mjs` e `plugin/test/*.test.mjs` —
  só para aguardar subtestes.

## Forbidden

- Código de produção; goldens; qualquer outra mudança nos testes.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera. Não rode a
suíte completa.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
