# Brief — C1c: `state` e `tasks` em Go, e os comandos `roster`, `friction`, `title`, `clean`, `feedback`

Role: implementer · Agent: build-2 · Report language: pt-BR

Continuação da sua C1b, mesmo worktree.

## Goal

Terminar o núcleo de estado — id do workspace, diretório de estado, roster e o lock dele, friction,
ponteiros de relatório, título de tarefa — e servir pelo Go os comandos que só dependem dele e do
cliente do herdr.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/c1c` (criado
de `go/port`, commit `77070c7`, que agora tem tudo: G1/G1b, P1, G2/G2b, a C1a com `internal/herdr`,
`internal/codexenv` e `internal/testutil/fakecli`, a GW1 e a sua C1b).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. **Resposta à sua pergunta da C1b.** O `state.mjs` não importa `herdr.mjs`: ele chama o binário
   `herdr` direto pelo `runCli`. O Go faz o mesmo, `platform.RunCli`, em `internal/core/state.go`.
   Onde um módulo JS importa `herdr.mjs` (como `tasks.mjs` e os comandos), o Go usa
   `internal/herdr`.
2. `internal/core`: `state.go` completo ← `lib/state.mjs` (inclusive `withRosterLock`, que funciona
   no Windows, e o friction log); `tasks.go` ← `lib/tasks.mjs`.
3. Comandos em `internal/cli`, cada um saindo do fallback: `roster`, `friction` (e `friction add`),
   `title`, `clean`, `feedback send`. O que a entrada JS faz em volta de comando vivo (friction log,
   `requireEnv`) o Go reproduz.
4. Testes com o `fakecli` no lugar do `herdr` (nunca o real): os casos de `state.test.mjs`,
   `friction.test.mjs`, `title.test.mjs`, `release.test.mjs` só no que for `state`,
   `feedback.test.mjs`, e os de `roster`/`clean` onde houver, um `t.Run` por caso com
   `// JS: "<título>"`. O lock do roster tem teste de concorrência (dois processos) que roda no Unix
   e no Windows.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão de paridade dos comandos novos: construa o binário
   (`CGO_ENABLED=0 go build -o /tmp/hs-go/build-2/herdr-soho ./cmd/herdr-soho`) e rode, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho` (o harness da
   suíte JS), `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/state.test.mjs scripts/test/friction.test.mjs scripts/test/title.test.mjs scripts/test/feedback.test.mjs scripts/test/release.test.mjs scripts/test/config.test.mjs scripts/test/session.test.mjs scripts/test/parity-config.test.mjs`
   → 0 fail (cole). Não edite nada nesse worktree.
3. Uma mutação por regra (formato da linha do roster, lock, `workspaceId`, `lastReport`, título de
   tarefa), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes),
   com o teste vermelho colado.
4. API nova no relatório.

## Owned files

- `internal/core/**`, `internal/cli/{roster,friction,title,clean,feedback}*.go` e o registro em
  `internal/cli/cli.go`

## Forbidden

- `internal/cli/mutation_guard*.go`, `internal/reportscan/**` (outra fatia), os outros pacotes
  (se precisar mudar algo neles, descreva no relatório), `go.mod`, `skills/`, `plugin/`, `docs/`,
  o worktree `h1`.
- Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
