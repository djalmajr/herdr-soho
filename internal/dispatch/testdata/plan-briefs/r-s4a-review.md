# Revisão R-S4a — `stats` e `collect` em Go

Role: reviewer · Report language: pt-BR

## Goal

Revisar o porte do `stats` e do `collect`.

- O `stats` agrega briefs, sidecars e relatórios por kind, papel, modelo ou agente, em texto e JSON.
  É dele que saem os números para decidir que assistente usar em cada papel.
- O `collect` junta o relatório de um agente, com o ponteiro de tarefa, e confere o hash com
  `--verify`.

Uma emenda contada como tarefa nova, um `lost` que é `pending`, uma mediana errada ou um hash que não
confere levam a decisões de time erradas, ou a um relatório trocado.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-s4a`, em `d598e91`
  (HEAD destacado; pai `15ad833`). Diff: `git -C <worktree> show d598e91`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s4a-go-stats-collect.md`.
- Relatório do implementador:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-3-20260928T231249.current.md`.
  O item `[partial]` é o espelho Go de cada caso JS. O orquestrador aceitou deixar isso para antes
  do corte. Diga só se falta um caso que importa.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/commands/{stats,collect}.mjs` e
  `lib/{reportscan,taskreport}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS do `stats` sobre estados montados. Monte briefs com e sem sidecar, emendas em
   cadeia, reusos, `pending` × `lost`, `not-received` e parciais. Varie os papéis de revisão
   (`ui-reviewer`, `inspector`), o estado vazio, o sidecar inválido e o espelho no `$TMPDIR`.
   Compare texto e `--json` com cada `--by` e cada `--since`, e os fusos (`TZ`). Compare stdout,
   stderr e código.
2. `collect`: com e sem `--verify`, arquivo alterado, ausente, ilegível e sem hash; cwd relativo do
   worker; original no `$TMPDIR` ausente com a cópia do estado. O `herdr` é sempre falso.
3. Datas, mediana, arredondamento e ordem das linhas contra o JS (UTF-16 no `sort`).
4. Pelo menos cinco mutações suas, com o teste e o código de saída de cada uma. Rode também
   `go vet ./...`, `go test ./...`, `gofmt -l cmd internal` e `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/s4a/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/s4a/none.sock`).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real, rede. Ler o estado real do projeto (`.herdr-soho/`) para comparar é permitido;
  escrever nele, não.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
