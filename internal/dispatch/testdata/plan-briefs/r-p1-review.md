# Revisão R-P1 — reportscan e `mutation-guard` em Go

Role: reviewer · Agent: review-2 · Report language: pt-BR

Continuação da revisão do G1, mesmo worktree.

## Goal

Revisar a segunda fatia do Go: o porte de `reportscan` e o primeiro comando atendido pelo binário, o
`mutation-guard`. O `taskreport` ficou de fora de propósito (depende do `internal/jsonjs` de outra
fatia). Os achados da sua revisão do G1 (`RunCli`, fallback, `platform`) já estão numa rodada de
correção à parte: não os repita, a menos que este commit os agrave.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g1`, agora em
  `5c0ba24` (HEAD destacado; pai `475d6cf`). Diff: `git -C <worktree> show 5c0ba24`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/p1-go-leaf.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-20260928T194739.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/reportscan.mjs` e
  `lib/commands/mutation-guard.mjs`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. `partialCount`, `partialCountFile` e `reviewHeader`: paridade com o JS, por diferencial seu
   (corpus aleatório e hostil: cercas de código abertas e fechadas, crases, CRLF, cabeçalhos quase
   certos, números grandes, acento, emoji). O `wait` e o `dispatch` decidem com esses números.
2. `mutation-guard`: paridade com o JS em cenários seus além dos do teste JS (symlink em profundidade,
   `.cargo/config` e `config.toml`, `--env` com valor relativo, `--source` explícito, caminhos com
   espaço e acento, cópia que é symlink). O comando é uma proteção: um falso "ok" é P1.
3. Os testes Go matam mutações suas (pelo menos três por módulo).
4. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows go vet ./...`.

Ambiente Go como na sua revisão anterior. Nenhum Herdr real.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
