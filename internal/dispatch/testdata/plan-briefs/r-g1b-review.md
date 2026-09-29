# Revisão R-G1b — correções da fundação Go (RunCli, fallback, testes)

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar a rodada que fecha os 10 achados da revisão do G1 (feita por outro revisor Sonnet) e porta o
`taskreport` e a raiz de estado da B2c. As próximas fatias usam `RunCli` para todo processo externo
(herdr, git, as CLIs dos agentes), então um desvio dele contra o JS aparece em todo comando.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2`, agora em
  `3ecd2c8` (HEAD destacado; pai `f361360`). Diff: `git -C <worktree> show 3ecd2c8 --stat` e
  `git show 3ecd2c8`.
- Revisão do G1, com as sondas: `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T195624.md`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g1b-go-foundation-fix.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-2-20260928T201348.md`.
- Plano, seções 2 a 5: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.
- O orquestrador roda os binários de teste no Windows; o resultado sai em
  `/tmp/hs-win-logs/hs-gowin.utf8.txt` (use se existir).

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada um dos 10 achados da revisão do G1: fechado, com as sondas daquela revisão refeitas contra o
   JS (RunCli nos três modos, sinal, errno, timeout com neto segurando o pipe, `PATHEXT`, fallback
   com SIGTERM e com legado, `HomeDir` sem `HOME`).
2. `RunCli`: paridade com o `runCli` do JS num diferencial seu (stdin, saídas grandes, UTF-8
   inválido na saída, código alto, processo que fecha stdout cedo, `Cwd` inexistente).
3. `syscall.Exec` no fallback Unix: o que se perde (defer, flush de stdout, arquivos abertos) e se o
   ambiente e o `argv[0]` batem com o launcher.
4. `taskreport` e a raiz da B2c: paridade com o JS e os testes de `worktree-state.test.mjs` do commit
   `4461a88`.
5. Mutações suas (pelo menos cinco) nos pontos novos; `go vet`, `go test ./...`, `gofmt -l`,
   `GOOS=windows go vet`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOCACHE=/tmp/hs-go/review/cache GOPATH=/tmp/hs-go/review/gopath GOTMPDIR=/tmp/hs-go/review/tmp`.
Nenhum Herdr real.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
