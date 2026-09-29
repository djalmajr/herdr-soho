# Brief — G1c: `RunCli` com o status real no timeout, saída UTF-8 como o Node, e os P3 da revisão

Role: implementer · Agent: build · Report language: pt-BR

Continuação da sua GW2, mesmo worktree e branch.

## Goal

A revisão da G1b passou, mas deixou 3 P2 e 3 P3 no `internal/platform`. O P2 do Windows você já
está fechando na GW2. Fechar os outros.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/gw2`.

Leia antes: `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T212113.md`
(F1 a F6, com as sondas e as correções validadas pelo revisor).

## Decisions already made

1. **F1.** No timeout, o resultado vem do `cmd.ProcessState` quando ele existe (o filho que trata o
   SIGTERM e sai com N devolve `status: N`, `signal: null`, `timedOut: false`, `error: 'ETIMEDOUT'`,
   como o JS), e o `WaitDelay` não produz erro para um filho que já saiu com 0. Os casos `trap-exit-0`,
   `trap-exit-3` e `daemonize-no-timeout` entram no gerador `gen_runcli.mjs` e no diferencial.
2. **F2.** `Stdout`/`Stderr` do `RunCli` são decodificados como o `utf8` do Node (a mesma regra WHATWG
   do `ReadTextFile`, reaproveitando a função), com diferencial de bytes inválidos na saída.
3. **F4.** A tabela de mutações da revisão refeita linha por linha, com o código de saída de cada uma
   colado; todas pegas. Mutações suas no lugar das da revisão não contam.
4. **F5.** `WaitDelay` também no Windows.
5. **F6.** `resolveFrom` trata caminho enraizado sem unidade (`\foo`) no Windows como o `path.win32`
   do Node trata (compare com `path.win32.resolve` no gerador).

## Expected result

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` (cole).
2. As sondas do F1 e do F2 da revisão refeitas contra o JS: iguais (cole).
3. A tabela de mutações (decisão 3).
4. Os binários de teste Windows recompilados (roteiro da GW2).

## Owned files

- `internal/platform/**`

## Forbidden

- Todo o resto; os mesmos limites da GW2.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
