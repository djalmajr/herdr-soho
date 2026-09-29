# Revisão R-C1b-b — correções do núcleo de config em Go

Role: reviewer · Report language: pt-BR

## Goal

Conferir se a rodada de correção da C1b fechou os achados da revisão anterior sem abrir outros. O
núcleo de config é lido por quase todo comando: uma precedência, um comentário perdido ou um nome de
lane casado errado aparece em tudo.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-c1bb`, em
  `d0c8b0a` (HEAD destacado; pai `b779e92`). Diff: `git -C <worktree> show d0c8b0a`.
- Revisão anterior, com os achados, as sondas e a tabela de mutantes:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T220308.md`.
- Brief da correção: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/c1b-b-core-fix.md`.
- Relatório do implementador:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-2-20260928T224648.current.md`.
  Ele marcou `[partial]` três coisas: (a) nove falhas de E/S em que o JS quebra com stack e código 1
  e o Go sai com diagnóstico e código 4, com a mesma árvore final — o orquestrador aceitou isso como
  divergência; confira só que a árvore é mesmo igual e que nada é escrito pela metade; (b) a tabela
  de mutantes da revisão anterior não foi refeita linha por linha, só seis mutações; (c) o K06
  (`config` sem subcomando com `HERDR_SOHO_NOWRITE`) ficou de fora por depender de `cli.go`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{config,session,legacy,lanes}.mjs`.
- Plano e convenções Go: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
  (seção 3).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada achado da revisão anterior: fechado, parcial ou aberto, com a sonda reexecutada contra o
   `d0c8b0a` e contra o JS, lado a lado.
2. A tabela de mutantes da revisão anterior, reexecutada linha por linha contra o `d0c8b0a`: morto,
   sobrevivente ou equivalente, com o teste e o código de saída. Diga quais sobreviventes importam.
3. As nove divergências de E/S: árvore final igual e nenhum arquivo escrito pela metade.
4. Regressões: diferencial Go × JS em volta do que mudou (nomes de lane com prefixo/sufixo comum,
   terminadores de linha, comentários, arquivo legado ilegível).
5. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows GOARCH=amd64 go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOFLAGS=-mod=mod`, `GOCACHE`, `GOPATH` e `GOTMPDIR` em
`/tmp/hs-go/review/c1bb/`. Nenhum Herdr real (`HERDR_SOCKET_PATH=/tmp/hs-go/review/c1bb/none.sock`;
herdr falso quando precisar).

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
