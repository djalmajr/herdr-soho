# Brief — GW1: os testes Go passam no Windows real

Role: implementer · Agent: build · Report language: pt-BR

Continuação da sua C1a, mesmo worktree.

## Goal

Os binários de teste Go, compilados para Windows e executados lá pelo orquestrador, falharam em
`platform`, `cli` e `taskreport`. Quase tudo é teste que assume Unix (symlink sem privilégio, modos
de arquivo, nome de pasta com aspas, fixtures POSIX), mas um caso pode ser defeito real. Deixar
todos os pacotes testáveis no Windows, com cobertura Windows de verdade do que só existe lá (`.cmd`
com `CmdLine` verbatim, `PATHEXT`, `.exe`), para que cada fatia seguinte seja validada no Windows.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/gw1` (criado de
`go/c1a`, commit `3c44819`: tudo do `go/port` e a sua C1a).

Leia antes:
- Falhas no Windows (Windows 11, amd64, sem privilégio de symlink, pasta temporária em
  `C:\Users\<user>\AppData\Local\Temp`): `/tmp/hs-go/gowin-failures.txt` (as linhas de falha) e o log
  inteiro em `/tmp/hs-win-logs/hs-gowin.utf8.txt`.
- Plano, seções 3 a 5: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.

## Decisions already made

1. **Symlink:** um teste que cria symlink e recebe o erro de privilégio do Windows ("A required
   privilege is not held by the client") faz `t.Skip` com esse motivo, só naquele subteste; o resto
   do teste roda. Nada de pular o teste inteiro em `runtime.GOOS == "windows"` quando só uma parte
   depende de symlink.
2. **Modos de arquivo:** no Windows o teste afirma o que o Windows tem (o bit de somente leitura) e
   não os bits Unix; no Unix nada muda.
3. **Nomes de pasta:** nenhum teste monta nome de pasta ou arquivo a partir de `t.Name()` com
   caracteres que o Windows recusa (`"`, `:`, `*`, `?`, `<`, `>`, `|`); use `t.TempDir()` ou saneie.
4. **Fixtures de processo:** o diferencial do `RunCli` que usa comandos POSIX roda no Unix; no Windows
   há um conjunto próprio com o `fakecli` (`.exe`) e um `.cmd` de fixture (argumentos com espaço,
   aspas, `%`, `^`, `!`, acento; código de saída; stdin; saída grande; timeout), com o resultado
   esperado escrito no teste a partir da regra do JS (`cmdInvocation` + `windowsVerbatimArguments`).
   Esse conjunto é a prova do `SysProcAttr.CmdLine` da G1b.
5. **`TestProjectRootCache` e `TestFindExecutable`:** leia o log e o código. Se a causa for o teste,
   corrija o teste; se for o produto (por exemplo caminho curto 8.3, caixa de letra de drive, `\`
   contra `/` que o git devolve no Windows), corrija o produto e explique no relatório com a linha do
   log.
6. `internal/cli/mutation_guard*.go` está em outra fatia agora (P1c): não mexa; liste no relatório o
   que falha nele no Windows e por quê.
7. Um roteiro PowerShell `/tmp/hs-go/build/gowin.ps1` que o orquestrador roda no Windows: executa
   cada binário de teste da pasta do seu pacote (argumentos entre aspas simples — o PowerShell parte
   `-test.v` em dois sem elas), com `-test.count=1 -test.timeout=600s`, e imprime as linhas de falha e
   `exit=<código>` por pacote, e `HS-GOWIN-DONE` no fim. E um roteiro `/tmp/hs-go/build/gowin-build.sh`
   que compila todos os pacotes com testes para `GOOS=windows GOARCH=amd64` em
   `/tmp/hs-go/build/win/<pacote>_test.exe`.

## Expected result

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. `sh /tmp/hs-go/build/gowin-build.sh` compila todos os binários de teste Windows (cole a lista com
   os tamanhos).
3. Por falha do log do Windows: a causa, a correção e se era teste ou produto.

## Owned files

- `internal/**/*_test.go` e `internal/**/testdata/**`, exceto `internal/cli/mutation_guard_test.go`
- Produto só onde a decisão 5 encontrar defeito real: `internal/platform/**`

## Forbidden

- `internal/cli/mutation_guard*.go`, `internal/reportscan/**` (P1c), `internal/core/**` e
  `internal/cli/config*.go`/`session*.go` (C1b, em outro worktree), `go.mod`, `skills/`, `plugin/`,
  `docs/`.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result" e por falha do log, com
`[done]`, `[partial]` ou `[skipped]` e o motivo, saídas coladas.
