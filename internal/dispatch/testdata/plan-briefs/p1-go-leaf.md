# Brief — P1: reportscan, taskreport e o comando `mutation-guard` em Go

Role: implementer · Agent: build · Report language: pt-BR

Continuação direta do G1, no mesmo worktree.

## Goal

Portar os dois módulos JS que só dependem de `platform` e o primeiro comando de verdade, o
`mutation-guard`, que passa a ser atendido pelo binário Go em vez do fallback para o JS.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/p1-leaf`, criado a
partir do seu G1 já commitado (`475d6cf`). O G1 está em revisão agora; se a revisão pedir mudança
no `internal/platform`, ela chega como emenda depois.

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5, como
no G1).

## Decisions already made

1. Pacotes: `internal/reportscan` ← `lib/reportscan.mjs`; `internal/taskreport` ←
   `lib/taskreport.mjs`. O comando: `internal/cli/mutation_guard.go` ← `lib/commands/mutation-guard.mjs`,
   registrado em `cli.Run` como comando portado (sai do fallback).
2. Um comando portado reproduz o que a entrada JS faz em volta dele (`herdr-soho.mjs`): se o
   `mutation-guard` não é comando vivo, não há friction; confira na entrada e reproduza.
3. O `JSON`, se algum destes módulos escrever, sai por `internal/jsonjs` — que ainda não existe neste
   branch (está na fatia G2, em outro worktree). Se precisar dele, pare nesse item, marque
   `[partial]` e diga onde; não crie outro JSON.
4. Testes:
   - `reportscan`: casos de `scripts/test/reportscan.test.mjs`, um `t.Run` por caso com
     `// JS: "<título>"`, mais diferencial gerado pelo JS para `partialCount` e `reviewHeader`
     (corpus com os casos do teste e hostis: CRLF, blocos de código, `[partial]` dentro de crases,
     acento, emoji).
   - `taskreport`: testes Go das quatro funções exportadas (ponteiro ausente, válido, corrompido,
     escrita atômica, `syncTaskReport` com e sem relatório).
   - `mutation-guard`: cada caso de `scripts/test/mutation-guard.test.mjs` reproduzido como teste Go
     que chama `cli.Run` com os mesmos argumentos, ambiente e árvore de fixture e confere stdout,
     stderr e código; mais um roteiro de comparação que roda o binário Go e o JS nos mesmos cenários
     e mostra `diff` vazio (cole a saída).

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir de
`/work/herdr-soho/.worktrees/s4`:

1. `gofmt -l cmd internal` vazio, `go vet ./...` limpo, `go test ./...` ok,
   `GOOS=windows GOARCH=amd64 go vet ./...` limpo (cole).
2. Comparação Go × JS do `mutation-guard` nos cenários do teste JS: `diff` vazio e códigos iguais.
3. Uma mutação por função com regra (`partialCount`, `reviewHeader`, a checagem de symlink e a de
   `CARGO_TARGET_DIR` do `mutation-guard`), numa cópia fora do repositório com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes,
   mostrando o teste vermelho.
4. API nova no relatório (nomes exportados e assinaturas).

## Owned files

- `internal/reportscan/**`, `internal/taskreport/**`
- `internal/cli/mutation_guard.go`, `internal/cli/mutation_guard_test.go` e o registro do comando em
  `internal/cli/cli.go`

## Forbidden

- Tudo em `skills/`, `plugin/`, `docs/`; `internal/platform/**` (se precisar mudar algo nele,
  descreva no relatório); `go.mod`.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos criados, API, divergências do JS e
perguntas abertas.
