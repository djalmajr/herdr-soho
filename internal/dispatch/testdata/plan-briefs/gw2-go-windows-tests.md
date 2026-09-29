# Brief — GW2: o que ainda falha nos testes Go no Windows, e o JS do Windows como referência

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel (a GW1, antes da compactação, foi sua).

## Goal

Depois da GW1, os binários de teste de 12 pacotes rodaram no Windows real: 6 passam (`jsonjs`,
`provider`, `reportscan`, `sessionref`, `taskreport`, `text`) e 6 falham (`cli`, `codexenv`,
`herdr`, `platform`, `setuptext`, `testutil/fakecli`). Fechar essas falhas, e montar a referência
que falta: o que o JS faz no Windows, para os casos em que o Go e o JS podem divergir só lá (por
exemplo a raiz do estado com `/` ou `\`).

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/gw2` (criado de
`go/port`, commit `77070c7`: tudo integrado até a C1b, com a GW1).

Leia antes:
- Falhas no Windows: `/tmp/hs-go/gowin2-failures.txt`; log completo em `/tmp/hs-go/gowin2-log.txt`.
- Brief da GW1: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/gw1-go-windows-tests.md`.
- Plano, seções 3 a 5: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.

## Decisions already made

1. **`fakecli` no Windows.** Os testes de `herdr` falham com `herdr agent get timed out after 0.1s`
   e `1s`: um `.exe` recém-copiado demora a iniciar no Windows (o antivírus examina todo executável
   novo). O `fakecli` passa a criar a cópia `.exe` uma vez por pacote de teste (no `TestMain`), e
   não por teste; e os prazos dos testes que usam o falso não descem de 5 s no Windows (os testes de
   timeout usam um falso que dorme, não um prazo curto contra o arranque). O teste próprio do
   `fakecli` que falha (`TestReexecRecordsExactArgumentsAndDispatchesRule`, `Stderr: open …`) tem a
   causa no relatório.
2. **`codexenv`.** No win32 o produto devolve a mensagem base por desenho (sem `ps`). Os testes de
   ancestrais por `ps` passam `platform` `"linux"` explicitamente (a função recebe a plataforma), ou
   pulam no win32 com o motivo; o contrato win32 → mensagem base tem teste próprio.
3. **`setuptext`.** O hook do SessionStart é um comando `sh` por desenho (a skill documenta que, no
   Windows, ele roda via Git Bash ou WSL). O teste que executa o hook pula quando não há `sh` no
   `PATH`, com esse motivo.
4. **`platform` — raiz do estado e `RunCliWindows`.** A raiz volta com `/` (o `git` do Windows
   imprime `C:/Users/…`) e o teste espera `\`. Não decida pelo teste: escreva
   `/tmp/hs-go/build/js-win-ref.mjs`, um roteiro que o orquestrador roda no Windows com o JS real
   (`skills/herdr-soho/scripts/lib/platform.mjs` do worktree copiado para lá), montando os mesmos
   repositórios dos testes Go de `StateProjectRoot`/`ProjectRoot` e imprimindo JSON com o que o JS
   devolve em cada caso. O teste Go passa a comparar com esse JSON (`testdata/windows-roots.json`, que
   o orquestrador traz de volta); até lá, o teste do Windows fica marcado com `t.Skip` citando o
   roteiro. Faça o mesmo para o `.exe` de `TestRunCliWindows` (linha 385 do log: descubra o que o
   teste esperava e confirme pela regra do JS).
5. **`cli`.** O `TestMutationGuard` falha pelos nomes com aspas (`t.Name()`): a fatia P1c terminou de
   mexer no `mutation-guard`? Não: ela ainda está em andamento em outro worktree. Não toque em
   `mutation_guard_test.go`; liste no relatório a linha exata a trocar para o orquestrador repassar.
   O resto do `cli` que falhar no Windows é seu.
6. Os roteiros de execução no Windows (compilar e rodar) ficam em `/tmp/hs-go/build/`, como na GW1,
   rodando cada binário a partir da pasta do seu pacote (o `testdata` é relativo) e com os argumentos
   entre aspas simples no PowerShell.

## Expected result

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Os binários de teste Windows de todos os pacotes compilam (cole a lista).
3. `/tmp/hs-go/build/js-win-ref.mjs` pronto, com instruções de uma linha para rodar.
4. Por falha do log: causa, correção, teste ou produto.

## Owned files

- `internal/**/*_test.go`, `internal/**/testdata/**`, `internal/testutil/**`, exceto
  `internal/cli/mutation_guard_test.go`
- Produto só se a decisão 4 ou o log provarem defeito: `internal/platform/**`

## Forbidden

- `internal/cli/mutation_guard*.go`, `internal/reportscan/**` (P1c), `internal/core/**` e os
  comandos `roster`/`friction`/`title`/`clean`/`feedback` (C1c, em outro worktree), `go.mod`,
  `skills/`, `plugin/`, `docs/`.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result" e por falha do log, com
`[done]`, `[partial]` ou `[skipped]` e o motivo, saídas coladas.
