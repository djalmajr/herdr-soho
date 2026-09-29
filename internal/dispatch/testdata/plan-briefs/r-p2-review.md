# Revisão R-P2 — `peer` e `sessions` em Go, e os comandos `send` e `find`

Role: reviewer · Report language: pt-BR

## Goal

Revisar o porte do `send` (mensagem entre orquestradores de workspaces diferentes) e do `find`
(busca de sessões em todas as máquinas). O `send` escreve no painel de outro agente: um cabeçalho
forjável, um corpo que escapa da citação, uma política `inbound` que não recusa ou um reenvio depois
de `agent_prompt_stalled` viram injeção de instrução ou mensagem duplicada noutro projeto.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-p2`, em `d469af1`
  (HEAD destacado; pai `aaccd4e`). Diff: `git -C <worktree> show d469af1`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/p2-go-peer.md`.
- Relatório do implementador:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-4-20260928T223654.current.md`
  (ele marcou `[partial]` a cobertura Go: os testes Go não espelham cada um dos 75 casos Node, que
  passam contra o binário. Diga se a cobertura basta e quais casos faltam de verdade).
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{peer,sessions}.mjs` e
  `lib/commands/{send,find}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3: erros, JSON, texto em UTF-16, regex RE2 × JS, `(?i)`).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS do `send` com o `herdr` falso: corpo hostil (linha igual ao marcador de fim,
   cabeçalho forjado no corpo, CR, CRLF, ` `, escapes ANSI, NUL, corpo vazio, corpo muito
   longo, texto com emoji e surrogates), remetente sem nome, `inbound=off`, destino remoto e por
   nome, tela com diálogo, `--now`, timeout. Compare stdout, stderr, código, as chamadas ao `herdr`
   falso e o log de tentativas.
2. A prova de entrega: as duas rotas, o id que some da tela, `agent_prompt_stalled` sem reenvio.
3. Diferencial do `find`: filtros, máquina indisponível, continuação depois da falha, saída TSV e
   JSON (ordem das chaves, números, texto não ASCII).
4. Regex e texto: cada `regexp` do Go contra a regex JS de origem (`\s`, `.`, `^`/`$`, `/i` sem
   `u`), e cada corte de texto contra o corte JS em UTF-16.
5. Mutações suas (pelo menos cinco); `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`,
   `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/p2/`. Suíte Node contra o binário, se quiser: a partir de
`/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
`HERDR_SOCKET_PATH=/tmp/hs-go/review/p2/none.sock HERDR_SOHO_TEST_BIN=<binário> node --test scripts/test/send.test.mjs scripts/test/find.test.mjs`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real: todo teste usa o `herdr` falso ou `HERDR_SOCKET_PATH` para um caminho inexistente, e
  ids impossíveis como `w0test:p0a`. Nunca mande nada a um painel de verdade. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
