# Brief — L2: `regrid`, `tab-label` e os rótulos das abas em Go

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Terminar o porte do layout. A fatia L portou só o `layout-plan` (`internal/layout/layout.go`: grade
e âncora). Ela parou porque `internal/herdr` não tinha como mover painéis, e o pacote estava em
outra fatia. Agora está livre. Portar o resto: mover painéis, refazer a grade, estacionar em
`herd-park` e dar rótulos às abas.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/l2` (criado
de `go/port`, commit `5144665`: tudo até agora, inclusive `internal/layout` e o `internal/herdr` da
C1a-b).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- Relatório da L (o que ficou de fora):
  `/work/herdr-soho/.herdr-soho/w14/reports/build-4-20260928T230818.current.md`.
- JS de referência: `skills/herdr-soho/scripts/lib/{layout,herdtabs,regrid}.mjs` no worktree.

## Decisions already made

1. `internal/herdr` ganha as chamadas que faltam para o `regrid` e as abas: `pane move` com
   `--new-tab`, `--tab`, `--split`, `--target-pane`, `--ratio` e `--no-focus`, e o que mais
   `layout.mjs`, `herdtabs.mjs` e `regrid.mjs` chamarem. Mesmos argumentos, na mesma ordem, e o
   mesmo tratamento de erro das funções JS em `lib/herdr.mjs`.
2. `internal/layout`: `herdtabs.go` ← `lib/herdtabs.mjs`, `regrid.go` ← `lib/regrid.mjs`, e o resto de
   `lib/layout.mjs` (direção automática com chamadas vivas, foco e restauração, âncora com roster)
   em `layout.go`.
3. Comandos em `internal/cli`, saindo do fallback: `regrid` e `tab-label [rótulo] [--tab ID] [--auto]`.
4. `autoRegrid` e `herdTabsRelabel` ficam exportados para quando `spawn` e `release` forem portados.
   Não mexa em `spawn` nem em `release` (continuam no JS).
5. A sequência de chamadas ao Herdr (dividir, mover, redimensionar, estacionar em `herd-park`, focar
   de volta) é a mesma do JS, na mesma ordem: os testes conferem o registro de chamadas do `fakecli`
   contra o do JS.
6. Testes: os casos de `layout.test.mjs`, `herdtabs.test.mjs`, `regrid.test.mjs` e os goldens
   `parity-layout.json` e `parity-regrid.json`, com `// JS: "<título>"`. Arquivos do repositório por
   caminho relativo à pasta do pacote, nunca `runtime.Caller`; nada de `sh` nos testes.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-2/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/layout.test.mjs scripts/test/herdtabs.test.mjs scripts/test/regrid.test.mjs scripts/test/parity-layout.test.mjs scripts/test/parity-regrid.test.mjs`
   → 0 fail (cole). Diga quais testes ainda passam só pelo fallback JS (devem ser só os de
   `spawn`/`release`). Não edite nada no worktree `h1`.
3. Uma mutação por regra, cada uma com o teste vermelho e o código de saída colados. As regras:
   - a ordem das chamadas do `regrid`;
   - o transbordo quando a aba está cheia;
   - o estacionamento em `herd-park`;
   - o rótulo automático cortado em `herd_label_max`;
   - o rótulo manual nunca sobrescrito;
   - a poda do arquivo `herd-tab`;
   - o foco devolvido ao painel de origem.

   Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/layout/**`, `internal/herdr/**` (só acrescentar; não mude o comportamento do que existe),
  `internal/cli/{regrid,tab_label}*.go` e o registro em `internal/cli/cli.go`.

## Forbidden

- `internal/core/**` (se precisar de algo novo nele, descreva no relatório), os outros pacotes,
  `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`. `spawn` e `release`.
- Herdr real: todo teste usa o `fakecli`, `HERDR_SOCKET_PATH` para um caminho inexistente e ids
  impossíveis como `w0test:p0a`. Módulos de terceiros, `go.sum`, rede. Cache e build só em
  `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
