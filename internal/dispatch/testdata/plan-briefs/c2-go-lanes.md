# Brief — C2: `roles`, `resolve` e `lanes` completos em Go, e os comandos `roles`, `role`, `explain`

Role: implementer · Agent: build-2 · Report language: pt-BR

Continuação das suas C1b e C1c, mesmo pacote `internal/core`.

## Goal

Terminar o núcleo: a resolução de papéis (arquivo do papel, frontmatter, kind/model/effort/approvals
por camada), as lanes (presets por número de painéis, capacidade, reuso, família, `kind-mismatch`) e
os comandos que só dependem disso. É o que o `spawn` e o `dispatch` vão usar para decidir quem roda o
quê.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/c2` (criado
de `go/port`, commit `db09469`, com a sua C1c).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. `internal/core`: `roles.go` ← `lib/roles.mjs` completo, `resolve.go` ← `lib/resolve.mjs`,
   `lanes.go` ← `lib/lanes.mjs` completo (as partes parciais que a C1b criou viram o porte inteiro).
2. Comandos em `internal/cli`, saindo do fallback: `roles`, `role <name>`, `explain`.
3. O `explain` imprime texto em inglês montado pelo JS: byte a byte igual.
4. Testes:
   - os casos de `roles.test.mjs`, `lanes.test.mjs` e `resolve` (onde houver), um `t.Run` por caso com
     `// JS: "<título>"`; os probes de `parity-lanes.test.mjs` (que chamam funções de `lanes.mjs` por
     `node -e`) viram testes Go que leem o mesmo golden `parity-lanes.json`;
   - `parity-config.json` completo (a C1b deixou de fora `project-roles` e `roles-role`);
   - os casos de `explain` onde houver.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-2/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/roles.test.mjs scripts/test/lanes.test.mjs scripts/test/parity-lanes.test.mjs scripts/test/parity-config.test.mjs scripts/test/config.test.mjs scripts/test/state.test.mjs`
   → 0 fail (cole). Não edite nada no worktree `h1`.
3. O golden `parity-lanes.json` lido pelo teste Go: todos os cenários iguais.
4. Uma mutação por regra (precedência de kind/model por camada, descarte do modelo quando o kind vem
   de outra camada, capacidade de lane, `locked` para papel de revisão, preset por número de
   painéis), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes), com
   o teste vermelho colado.
5. API nova no relatório.

## Owned files

- `internal/core/**`, `internal/cli/{roles,role,explain}*.go` e o registro em `internal/cli/cli.go`

## Forbidden

- Os outros pacotes (se precisar mudar algo neles, descreva no relatório), `go.mod`, `skills/`,
  `plugin/`, `docs/`, o worktree `h1`.
- Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
