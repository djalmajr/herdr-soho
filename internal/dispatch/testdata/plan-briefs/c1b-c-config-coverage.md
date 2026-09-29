# Brief — C1b-c: `session clear` com symlink quebrado e a cobertura Go do núcleo de config

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão da C1b-b passou, com um achado P3. Também mostrou que o núcleo de config tem pouca
cobertura Go própria: de 128 mutantes reconstruídos, 47 sobreviveram e 11 não compilaram. O
comportamento está coberto pela suíte JS contra o binário, mas essa suíte sai do repositório no
corte. Até lá, o Go precisa se proteger sozinho.

1. **P3**: `session clear` apaga um `session.conf` que é symlink para um alvo que não existe. O JS
   (`existsSync`, que segue o link) diz `session is empty (nothing to clear)` e não apaga nada.
2. **Cobertura**: um teste Go que mate cada sobrevivente da revisão que não seja equivalente.
   Também o K06: `config` sem subcomando com `HERDR_SOHO_NOWRITE=1`.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/c1b-c` (criado de `go/port`, commit `cc078f6`).

- Revisão (o achado e a prova; a lista de sobreviventes e de inválidos; o harness e o log dos
  mutantes): `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T232858.md`.
- Revisão anterior (onde cada identificador de mutante está descrito):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T220308.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. **P3**: se o `Lstat` mostra symlink, faça `Stat` do alvo. Se o alvo não existe, imprima
   `session is empty (nothing to clear)` e volte sem `Remove`. Um diretório real continua no `Die`
   de código 4. Symlink para arquivo ou para diretório continua removendo o link, como o JS.
2. **Sobreviventes**: para cada um da lista da revisão, um teste que o mate, ou uma linha no
   relatório dizendo por que é equivalente (com a prova). Os 11 inválidos: reescreva cada um para
   compilar e rode.
3. **K06** mexe em `internal/cli/cli.go`, só no ramo do `config` com `HERDR_SOHO_NOWRITE`. Mesmo
   texto e código do JS.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-4`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. O P3 com a sonda da revisão (`DANGLING` e `SYMDIR`) reexecutada contra o binário novo e o JS,
   lado a lado: `SAME` (cole).
3. A tabela dos sobreviventes e dos inválidos, linha por linha: morto (teste e código de saída) ou
   equivalente (prova). Um mutante que você não conseguiu rodar é `[partial]` com o motivo.
4. Portão: binário em `/tmp/hs-go/build-4/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-4/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-4/herdr-soho node --test scripts/test/config.test.mjs scripts/test/session.test.mjs scripts/test/legacy.test.mjs scripts/test/parity-config.test.mjs scripts/test/parity-lanes.test.mjs`
   (os que existirem) → 0 fail (cole).

## Owned files

- `internal/core/{config,session,legacy,lanes}*.go` e seus testes, `internal/cli/config_session_test.go`
  e o ramo do `config`/`HERDR_SOHO_NOWRITE` em `internal/cli/cli.go`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e `h1`.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build-4/`.
  Mutações só em cópia fora do repositório, com
  `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, divergências e perguntas abertas.
