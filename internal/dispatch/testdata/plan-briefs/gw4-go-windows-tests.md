# Brief — GW4: as 12 falhas Go que sobraram no Windows

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A sexta rodada no Windows 11 real, sobre `go/port` `cc078f6` (com a GW3), caiu de 28 para 12
falhas. Nove pacotes passam inteiros. Sobraram duas coisas:

- Produto que a GW3 corrigiu no Mac e **não** passou no Windows: `resolveFrom` com caminho sem
  drive ainda junta com a base (`Go="C:\x\worker\repo\.git" JS="C:\repo\.git"`), e a raiz do worktree
  ligado sai `"."` em vez de `""`.
- Testes das fatias integradas depois da GW3 que ainda assumem Unix.

Deixar os 17 pacotes verdes no Windows.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/gw4` (criado de `go/port`, commit `0c9d683`).

- Log inteiro da rodada: `/tmp/hs-go/gowin6-log.txt`. As linhas de falha:
  `/tmp/hs-go/gowin6-failures.txt`. Os 12 testes que falham:
  - `TestWindowsPlatformMatchesJSFixtures` e `TestStateProjectRoot` (produto, `platform`);
  - `TestRunCliWindows` (o `.exe` e o `café` que chega ao `.cmd` como `caf├⌐`);
  - `TestFakeCLIConcurrentCallOrdinalIsSerialized` (lock do log de chamadas: `open ...`);
  - `TestLiveAgentsFailureSemantics`;
  - `TestSetupReadCommands` (`probe` do pi sai `exit 1`);
  - `TestApplyLaneFileGolden` (as linhas `set lane.*.kind` que o golden espera);
  - `TestParityConfigGolden`;
  - `TestStateCommandParity` (colisão do `feedback send`);
  - `TestMutationGuard` (o texto do `cargo-config` com `\` no Windows);
  - `TestDiagnoseDynamicSnapshotNavigation` e `TestDiagnosePaneFiltersAndAncestorCase`.
- O relatório da GW3 (feita por outro worker) e o brief dela (as regras valem de novo):
  `/tmp/herdr-soho/w14/reports/build-4-20260929T000517.md`
  e `/work/herdr-soho/.herdr-soho/w14/plan-briefs/gw3-go-windows-tests.md`.
- A referência JS medida no Windows: `internal/platform/testdata/windows-roots.json`.

## Decisions already made

1. **`resolveFrom` e a raiz do estado** são produto. No Windows, `filepath.IsAbs(`\repo`)` é falso:
   um caminho que começa com `\` ou `/` e não tem volume é "absoluto sem drive" e recebe o volume de
   `base`, como o `path.win32.resolve`. A raiz do worktree ligado vazia é `""`, não `"."`. O JSON de
   referência não muda.
2. **O `.cmd` e o `café`**: descubra no Windows (pelo roteiro de referência JS, como na GW2) o que o
   JS entrega ao `.cmd`, e faça o Go igual. Se o JS entrega o mesmo `caf├⌐` (página de código do
   console), o teste passa a esperar o que o JS entrega, com o motivo no teste.
3. **`fakecli`**: o lock do log de chamadas no Windows precisa aguentar abertura concorrente
   (compartilhamento de arquivo). Se o `O_EXCL` falha por `ERROR_SHARING_VIOLATION` ou
   `ERROR_ACCESS_DENIED` durante a criação por outro processo, tente de novo como no
   `ERROR_FILE_EXISTS`.
4. **O resto**: leia a linha do log. Um teste que assume Unix passa a afirmar o que o Windows tem ou
   pula só aquele subteste, com o motivo. Um defeito de produto no Windows fora de `platform` e
   `fakecli`: descreva no relatório com a linha do log, sem corrigir.
5. Você não roda no Windows: prepare os binários (`go test -c` dos 17 pacotes) e o roteiro de
   referência JS, se o item 2 precisar dele. O orquestrador roda no Windows e devolve o log.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` e
   `go test ./...` ok (cole).
2. Os 17 binários de teste Windows compilam (cole a lista).
3. Por teste que falhou (os 12): causa, correção, e se era teste ou produto. Uma tabela.
4. Se o item 2 das decisões precisar, o roteiro `/tmp/hs-go/build-2/js-win-ref-cmd.mjs` com a
   instrução de uma linha para rodar.

## Owned files

- Todo `*_test.go` e `testdata/` de `internal/**`, `internal/testutil/fakecli/**`, e no produto só
  `internal/platform/**`.

## Forbidden

- Produto fora de `internal/platform`, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e
  `h1`. Mudar o JSON de referência do Windows.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build-2/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, produto corrigido e por quê, e perguntas
abertas.
