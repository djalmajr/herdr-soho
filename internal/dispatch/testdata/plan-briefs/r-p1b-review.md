# Revisão R-P1b — `mutation-guard` sem falso "ok" e ordens reproduzíveis (JS e Go)

Role: reviewer · Agent: review-2 · Report language: pt-BR

Continuação da sua revisão do P1.

## Goal

Revisar a rodada que fecha os 5 achados da sua revisão do P1. O `mutation-guard` é uma proteção:
confirme que o "ok" falso sumiu nos dois lados e que nenhuma forma nova deixa passar um
`target-dir` dentro do source. A rodada também muda o JS (`mutation-guard.mjs`, e os `localeCompare`
de `dispatch.mjs` e `stats.mjs`).

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g1`, agora em
  `0ce4cab` (HEAD destacado; pai `f361360`, o `go/port`). Diff: `git -C <worktree> show 0ce4cab`.
- Sua revisão anterior: `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T200756.md`.
- Brief: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/p1b-guard-fix.md`.
- Relatório do implementador:
  `/tmp/herdr-soho/w14/reports/build-20260928T202629.md`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

1. Seus cenários do achado 1 (formas pontilhada, em linha, chave entre aspas, ancestral,
   `CARGO_HOME`) → `fail` nos dois lados; mais os que você achar (comentário `#` na mesma linha,
   `[build]` repetido, `target-dir` em outra tabela como `[profile.x]`, arquivo `config` e
   `config.toml` na mesma pasta, valor com `~`, `CARGO_TARGET_DIR` e config ao mesmo tempo — qual o
   cargo usa?). Compare com `cargo metadata --offline --no-deps` quando der.
2. Achados 2, 3 e 4 fechados, Go × JS iguais; a troca do `localeCompare` em `dispatch`/`stats` não
   muda a saída para nomes ASCII (compare com o pai).
3. Sua tabela de mutações refeita: todas pegas.
4. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows go vet ./...`, e
   `node --test`/`bun test --timeout 60000` de `mutation-guard`, `dispatch`, `stats`.

Ambiente Go como antes. Nenhum Herdr real.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item de
"Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
