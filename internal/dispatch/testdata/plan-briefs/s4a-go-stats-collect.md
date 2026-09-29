# Brief — S4a: `stats` e `collect` em Go

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar duas leituras da fase 4: `stats` (agregado dos relatórios e briefs por kind, papel, modelo e
período) e `collect` (juntar o relatório de um agente, com o ponteiro de tarefa e o aviso de
parciais). `wait`, `status` e `release` ficam para outra fatia: o `wait`/`status` estão mudando no
JS agora, e o `release` depende do layout.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/s4a` (criado
de `go/port`, commit `b5ffdcb`: fundação, pacotes puros, `herdr`, núcleo de config/estado,
`roles`/`resolve`/`lanes`/`tasks`, `kinds`, `peer`).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. `internal/stats` ← `lib/commands/stats.mjs`; `collect` em `internal/stats/collect.go` ←
   `lib/commands/collect.mjs` (ou `internal/collect`, se o relatório justificar; um dos dois).
2. Porte o `stats.mjs` do `main`. Um branch JS em andamento muda só um comentário nele (a contagem
   de `not-received` não muda); nada a acompanhar.
3. Datas e durações: mesmos arredondamentos, mediana e formatação do JS (`--since`, `--by`, `--json`,
   minutos com uma casa, a ordem das linhas). O fuso é o local, como no JS; os testes fixam `TZ`.
4. `collect` usa o `herdr` só pelo pacote `internal/herdr` (`agentState`), com o `fakecli` nos
   testes. O hash e a cópia do relatório iguais ao JS (`crypto` → `crypto/sha256`, mesmo formato).
5. Roteamento em `internal/cli`: `stats` e `collect` saem do fallback.
6. Testes: os casos de `stats.test.mjs` e `collect.test.mjs`, um `t.Run` por caso com
   `// JS: "<título>"`, mais um diferencial `testdata/gen_stats.mjs` (a saída do JS sobre um estado
   montado com briefs, relatórios, sidecars e emendas, em texto e `--json`).

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-3`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-3/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-3/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-3/herdr-soho node --test scripts/test/stats.test.mjs scripts/test/collect.test.mjs`
   → 0 fail (cole o resumo). Não edite nada no worktree `h1`.
3. Uma mutação por regra (emenda não conta como tarefa nova, `lost` × `pending`, `not-received` só
   com sidecar aceito, mediana, parciais somados, ponteiro de tarefa do `collect`, hash), numa cópia
   fora do repositório (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>`
   antes), com o teste vermelho e o código de saída colados, uma linha por mutação.
4. API nova no relatório.

## Owned files

- `internal/stats/**` (e `internal/collect/**`, se escolher esse pacote), o registro em
  `internal/cli/cli.go` e `internal/cli/{stats,collect}*_test.go`.

## Forbidden

- `internal/core/**` (se precisar de algo novo nele, descreva no relatório), os outros pacotes,
  `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`.
- Mudar o que o `stats` conta (a contagem por cadeia de tarefa é uma feature futura, só em Go,
  depois da paridade).
- Herdr real: nenhum teste fala com um painel de verdade. Módulos de terceiros, `go.sum`, rede. Cache
  e build só em `/tmp/hs-go/build-3/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
