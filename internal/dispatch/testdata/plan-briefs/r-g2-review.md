# Revisão R-G2 — pacotes Go puros (text, jsonjs, provider, sessionref, setuptext)

Role: reviewer · Agent: review · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar a fatia que porta para Go os módulos JS sem disco nem processo e cria o `internal/jsonjs`, o
JSON que toda saída da CLI Go vai usar. É a base de quase tudo que vem depois: um desvio aqui aparece
em cada comando. A paridade com o JS tem que ser exata.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g2`, branch
  `go/g2-pure`, commit `6cdfc58` (pai `02910bb`). Diff: `git -C <worktree> show 6cdfc58 --stat` e
  `git show 6cdfc58`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g2-go-pure.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-2-20260928T192652.md`.
- Plano, seção 3 (convenções de texto, regex e JSON):
  `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/{text,provider,quota,dialog,arrival,sessionref,setuptext}.mjs`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. `jsonjs` contra o node com um diferencial seu, grande e aleatório: escape de todos os controles,
   U+2028/U+2029, `<>&`, surrogates isolados, números (inteiros grandes, `1e21`, `1e-7`, `-0`,
   `NaN`, `Infinity`, `5e-324`, `0.1+0.2`), ordem de chaves no `Parse` (chaves inteiras, `"01"`,
   `"4294967295"`, `"-1"`), `Undefined` em objeto e em array, indentação aninhada e vazios.
2. Cada regex dos módulos JS e a forma Go: procure as diferenças de RE2 da seção 3 do plano (`\s`,
   `.`, `^`/`$` com `m`, classes com `i`, lookaround reescrito em código) e prove com entradas suas
   que o Go e o JS decidem igual. O `provider`/`quota`/`dialog` decide quota, erro de provedor e
   diálogo a partir da tela de um terminal: um falso positivo para um worker, um falso negativo
   esconde uma cota estourada.
3. Cortes de texto em UTF-16 (`QuestionText` e onde mais houver): paridade com emoji e acento.
4. Os geradores de `testdata` importam os módulos JS de verdade e o corpus cobre os casos dos testes
   JS; os testes Go matam mutações suas (pelo menos duas por pacote).
5. `go vet ./...`, `go test ./...`, `gofmt -l internal`, `GOOS=windows go vet ./...`.

Ambiente Go: `GOTOOLCHAIN=local GOPROXY=off GOCACHE=/tmp/hs-go/review/cache GOPATH=/tmp/hs-go/review/gopath GOTMPDIR=/tmp/hs-go/review/tmp`.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
