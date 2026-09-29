# Revisão R-G2b — correções dos pacotes Go puros

Role: reviewer · Agent: review-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar a rodada que fecha os 6 achados da revisão do G2 (feita pelo outro revisor Sonnet). O
achado principal era grave: o diferencial do `provider` não comparava nada. Confirme que agora os
diferenciais comparam de verdade e que a troca de `(?i)` por minúsculas ASCII reproduz o `/i` do JS
sem abrir divergência nova.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2b`, branch
  `go/g2b`, commit `021f540` (pai `f361360`). Diff: `git -C <worktree> show 021f540`.
- Revisão do G2, com provas, fuzz e a tabela de mutações:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T201448.md`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g2b-pure-fix.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-4-20260928T204554.md`.
- Plano, seção 3: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Cada diferencial de cada pacote falha com uma mutação sua na função que ele cobre (prove que não
   há outro diferencial vazio). Os geradores produzem saídas não triviais (conte os resultados não
   nulos por função).
2. A troca de `(?i)`: paridade com o JS num fuzz seu com `ſ`, `K`, `İ`, `ß`, `ﬀ`, maiúsculas gregas e
   turcas, em `ProviderDetect`, `QuotaDetect`, `DialogKind`, `QuestionText`. `QuotaPhraseQuoted` e os
   cortes UTF-16.
3. A tabela de mutações da revisão do G2 refeita linha por linha (o implementador marcou uma, P3,
   como sobrevivente: diga se é equivalente).
4. `text.ToWellFormedUTF8` contra o `fs.writeFileSync` do Node.
5. `go vet ./...`, `go test ./...`, `gofmt -l internal`, `GOOS=windows go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOCACHE=/tmp/hs-go/review2/cache GOPATH=/tmp/hs-go/review2/gopath GOTMPDIR=/tmp/hs-go/review2/tmp`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
