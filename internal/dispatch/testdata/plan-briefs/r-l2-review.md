# Revisão R-L/L2 — layout, `regrid`, `tab-label` e mover painéis em Go

Role: reviewer · Report language: pt-BR

## Goal

Revisar o porte do layout. As duas fatias trouxeram:

- a grade e a âncora;
- mover painéis no `internal/herdr` (`pane move`);
- refazer a grade, estacionar em `herd-park` e dar rótulo às abas.

Esses comandos mexem nos painéis de verdade do usuário. Uma ordem de chamadas errada, um painel
movido para a aba errada ou um rótulo manual sobrescrito bagunçam a sessão de quem está trabalhando.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-l2`, em `8524a1c`
  (HEAD destacado). Os dois commits a revisar:
  - `2b4d1d7` (L: `layout-plan`);
  - `8524a1c` (L2: `regrid`, `tab-label`, `herdtabs`, `pane move`).

  Diffs: `git -C <worktree> show 2b4d1d7` e `git -C <worktree> show 8524a1c`.
- Briefs: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/l-go-layout.md` e
  `l2-go-regrid-tabs.md`, na mesma pasta.
- Relatórios:
  - L:
    `/work/herdr-soho/.herdr-soho/w14/reports/build-4-20260928T230818.current.md`;
  - L2:
    `/work/herdr-soho/.herdr-soho/w14/reports/build-2-20260928T234705.current.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{layout,herdtabs,regrid,herdr}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Diferencial Go × JS do `regrid`, do `tab-label` e do `layout-plan` com um herdr falso que grava
   cada chamada. Compare a sequência inteira de chamadas (argumentos e ordem), stdout, stderr e
   código. Varie:
   - 0 a 9 painéis, aba cheia e transbordo, `herd-park` já existente;
   - rótulo manual, rótulo automático longo (`herd_label_max`), nomes não ASCII;
   - `herd-tab` com formato antigo;
   - falha do herdr no meio da sequência.
2. `pane move` em `internal/herdr`: cada combinação de `--new-tab`, `--tab`, `--split`,
   `--target-pane`, `--ratio` e `--no-focus` contra a função JS. Confira os argumentos e o
   tratamento de erro.
3. Regex e cortes de texto dos rótulos contra o JS: UTF-16, `/i` sem `u`, `toLowerCase`.
4. Pelo menos seis mutações suas, com o teste e o código de saída de cada uma. Rode também
   `go vet ./...`, `go test ./...`, `gofmt -l cmd internal` e `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/l2/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/l2/none.sock`,
ids impossíveis como `w0test:p0a`; herdr falso para tudo).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real: nunca mova, divida, renomeie ou feche um painel ou aba de verdade. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
