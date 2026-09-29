# Emenda — L3: a categoria do `friction.log` é o comando que o usuário rodou

Role: implementer · Agent: build · Report language: pt-BR

## Goal

No JS, toda linha do `friction.log` leva na terceira coluna o comando que o usuário rodou: o
`frictionCmd` de `lib/state.mjs:33-54`, definido uma vez pela entrada. No Go, três lugares fixam a
categoria no código:

- o regrid automático em `internal/layout` grava `regrid`, também quando roda dentro do `spawn` ou do
  `release`. O teste JS `spawn: a failed automatic regrid is the bash warning and keeps the spawn code`
  (`scripts/test/regrid.test.mjs:598`) falha: espera `spawn` e recebe `regrid`;
- `internal/spawn` (`Warn(..., "spawn")`, em `native.go` e `spawn.go`);
- o `EnsureOrchestratorName` chamado pelo `init`, que deveria gravar `init`.

Corrija com o mesmo desenho do JS: um valor por processo em `internal/core` (o comando atual),
definido uma vez pelo `cli.Run`. O `core.Warn` e o registro de erro usam esse valor. Nenhum outro
pacote passa a categoria à mão.

## Expected result

1. O teste JS acima passa contra o seu binário. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`, que é só
   leitura para você: rodar os testes nela é permitido, editar não. Um detalhe: o `gate` ainda não
   tem o `spawn` em Go. Faça `git -C <seu worktree> merge go/port` antes; o `go/port` em `253db9b`
   já tem o `spawn`. Comando:
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/regrid.test.mjs scripts/test/spawn.test.mjs scripts/test/init.test.mjs scripts/test/release.test.mjs`
   → 0 fail (cole).
2. `grep -rn '"spawn")\|"regrid")\|"init")' internal` mostra que nenhuma chamada de aviso ou de
   friction fixa mais a categoria. Cole a saída.
3. Um teste Go que prende a categoria (o regrid dentro do `spawn` grava `spawn`), com o mutante que
   ele mata e o código de saída.

## Owned files

Os do brief L3, mais `internal/core/state.go` (só o comando atual e o `Warn`), `internal/spawn/**`
(só as chamadas de `Warn`), o `cli.Run` em `internal/cli/cli.go` (só definir o comando atual) e
`internal/doctor/**` (só as chamadas de `Warn`).

## Forbidden

Os do brief L3. No commit, push, tag, or PR. The orchestrator owns git.

## Report

No mesmo relatório da L3, uma seção "Emenda" com os três itens, `[done]`/`[partial]`/`[skipped]` e
as saídas coladas.
