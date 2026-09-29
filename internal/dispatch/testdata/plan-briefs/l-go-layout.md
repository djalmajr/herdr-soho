# Brief — L: `layout`, `herdtabs` e `regrid` em Go, e os comandos `layout-plan`, `regrid`, `tab-label`

Role: implementer · Report language: pt-BR

## Goal

Portar a disposição dos painéis — onde um worker novo cai (dividir o maior painel, transbordar para
uma aba de rebanho), a grade exata de cada aba, os rótulos automáticos das abas — e servir pelo Go os
comandos que só dependem disso. O `spawn` vai chamar isto a cada worker aberto.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/l` (criado de `go/port`, commit `254d79e`: fundação, pacotes
puros, `herdr`, núcleo completo com lanes, kinds, `send` e `find`).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. `internal/layout`: `layout.go` ← `lib/layout.mjs`, `herdtabs.go` ← `lib/herdtabs.mjs`,
   `regrid.go` ← `lib/regrid.mjs`.
2. Comandos em `internal/cli`, saindo do fallback: `layout-plan` (inclusive `--layout -` pela
   entrada padrão e `--me`), `regrid`, `tab-label [rótulo] [--tab ID] [--auto]`.
3. Toda chamada ao Herdr passa por `internal/herdr`; nos testes, o `fakecli` (nunca o Herdr real).
   A sequência de chamadas (dividir, mover, redimensionar, estacionar em `herd-park`, focar de volta)
   é a mesma do JS, na mesma ordem: os testes conferem o registro de chamadas do falso contra o do
   JS.
4. Testes: os casos de `layout.test.mjs`, `herdtabs.test.mjs`, `regrid.test.mjs` e os goldens
   `parity-layout.json` e `parity-regrid.json`, um `t.Run` por caso com `// JS: "<título>"`.
5. Os testes seguem a regra "Testes que rodam no Windows" da seção 3 do plano: arquivos do
   repositório por caminho relativo à pasta do pacote, nunca `runtime.Caller`; o orquestrador roda
   os binários de teste no Windows.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/<seu nome>/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/<seu nome>/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/<seu nome>/herdr-soho node --test scripts/test/layout.test.mjs scripts/test/herdtabs.test.mjs scripts/test/regrid.test.mjs scripts/test/parity-layout.test.mjs scripts/test/parity-regrid.test.mjs`
   → 0 fail (cole). Não edite nada no worktree `h1`.
3. Uma mutação por regra (`gridSizes` para 1 a 9 painéis, o lado mais longo decide a direção,
   `split_min_pane`, transbordo quando a aba está cheia, rótulo automático cortado em
   `herd_label_max`, rótulo manual nunca sobrescrito), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes), com
   o teste vermelho colado. Refaça cada mutação e cole o código de saída.
4. API nova no relatório.

## Owned files

- `internal/layout/**`, `internal/cli/{layout_plan,regrid,tab_label}*.go` e o registro em
  `internal/cli/cli.go`

## Forbidden

- `internal/core/**` e os outros pacotes (se precisar de algo novo neles, descreva no relatório),
  `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`.
- Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/<seu nome>/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
