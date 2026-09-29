# Revisão R-C1a-b/c — `herdr` e `codexenv` em Go depois das correções

Role: reviewer · Report language: pt-BR

## Goal

Conferir as duas rodadas de correção da C1a. O pacote `internal/herdr` é por onde todo comando Go
fala com o Herdr, com estado do agente, sinais, retries e leitura do JSON. O `internal/codexenv`
decide que variáveis de ambiente o worker Codex herda. Um erro aqui aparece em todo `wait`/`status`
futuro, ou muda o ambiente do worker em silêncio.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-c1ab`, em `614ed7f`
  (HEAD destacado). Os dois commits a revisar:
  - `9b21c6b` (C1a-b: sinais, coerções JS, `fakecli`);
  - `614ed7f` (C1a-c: parser TOML igual ao JS).

  Diffs: `git -C <worktree> show 9b21c6b` e `git -C <worktree> show 614ed7f`.
- Revisão original da C1a (os achados que as correções fecham):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T215403.md`.
- Relatórios dos implementadores:
  - C1a-b:
    `/work/herdr-soho/.herdr-soho/w14/reports/build-20260928T224605.current.md`;
  - C1a-c:
    `/work/herdr-soho/.herdr-soho/w14/reports/build-20260928T233333.current.md`.
- Pendência que é sua: dez mutações da revisão original **não rodaram**. Nove são de
  `internal/herdr`: `transientkill-ignores-json-error`, `transientkill-checks-code-not-message`,
  `signal-137-unnamed`, `signal-143-unnamed`, `dieerror-signal-1`, `timeout-not-flagged`,
  `seq-noninteger-kept`, `agentprompt-no-trim` e `split-ratio-dropped`. A décima é
  `fakecli-stderr-dropped`, de `internal/testutil/fakecli`. O código foi refatorado. Reescreva
  cada uma para o código atual e rode.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{herdr,codex-env}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada achado da revisão original da C1a: fechado, parcial ou aberto, com a sonda reexecutada
   contra o `614ed7f` e contra o JS, lado a lado.
2. As dez mutações acima, reescritas e rodadas: morta, sobrevivente ou equivalente, com o teste e o
   código de saída. Mais cinco mutações suas no parser TOML novo (`policy_parser.go`).
3. O diferencial TOML: rode o gerador do implementador com outra semente e confira que continua
   com zero divergências. Tente documentos hostis: aspas não fechadas, chaves repetidas, tabelas
   inline aninhadas, BOM, CRLF, `\u` inválido e arquivo enorme.
4. Os PIDs fora de `int`, que o orquestrador aceitou ignorar: confira que nada quebra, nem vira
   pânico.
5. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/c1ab/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/c1ab/none.sock`;
o `fakecli` ou um herdr falso quando precisar).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real, o `codex` real, rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
