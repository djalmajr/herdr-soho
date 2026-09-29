# Revisão R-C1c-b — `roster`, `clean`, `friction`, `title` e `feedback` depois da correção

Role: reviewer · Report language: pt-BR

## Goal

Conferir a correção da C1c. A revisão anterior achou um P1: o `clean` do Go apagava do roster
agentes vivos, com o relatório e os arquivos de espera deles, e o `roster` os mostrava como `gone`.
Achou também um P2 (os `die` fora do `friction.log`) e nove P3. Confirmar que tudo fechou, sem
abrir outra coisa. O `clean` apaga estado: um erro aqui perde trabalho de um worker.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-c1cb`, em
  `7bf861f` (HEAD destacado; pai `d3cdf1a`). Diff: `git -C <worktree> show 7bf861f`.
- Revisão anterior (achados F1–F11, sondas e mutantes M01–M17):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T225957.md`.
- Brief da correção, com as divergências que o orquestrador aceitou:
  - F3: `workspace_id` inválido sai com código 2;
  - F8: `wait/` ilegível sai com código 4 sem apagar nada.

  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/c1c-b-state-fix.md`.
- Relatório do implementador:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-4-20260928T233021.current.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/state.mjs`, `lib/tasks.mjs` e
  `lib/commands/{roster,clean,friction,title,feedback}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada achado F1–F11: fechado, parcial ou aberto, com a sonda reexecutada contra o `7bf861f` e
   contra o JS, lado a lado.
2. O `clean` com o `agent list` no formato real do Herdr: agentes vivos com pane, sem pane, com
   `pane_id` nulo e com `pane_id` numérico, mortos, e dois agentes no mesmo pane. Nenhum vivo pode
   perder linha, relatório ou `wait/`.
3. `clean` e `roster` concorrentes (dois processos, e Go + JS juntos) sobre o mesmo estado: nenhuma
   linha perdida ou duplicada.
4. Mutações suas no código novo (pelo menos cinco, uma no `field`), com o teste e o código de saída.
5. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/c1cb/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/c1cb/none.sock`;
herdr falso quando precisar).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real, rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
