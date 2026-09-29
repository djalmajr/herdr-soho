# Brief — P1b: `mutation-guard` sem falso "ok" e ordens que o Go reproduz (JS e Go)

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do P1 achou um buraco que já existe no JS e o Go copiou: o `mutation-guard` diz
`ok cargo-config` quando o cargo vai escrever dentro do source. É uma proteção; um falso "ok" deixa
uma mutação escrever no repositório. Também achou três diferenças Go × JS e testes fracos. Esta
fatia corrige o JS e o Go juntos, no mesmo branch.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/p1b` (criado de `go/port`, commit
`f361360`: G1, P1 e G2 integrados). Outra fatia (G1b) mexe agora em `internal/platform` e
`internal/cli/cli.go` em outro worktree; o orquestrador junta os dois branches.

Leia antes:
- Revisão: `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T200756.md`
  (achados 1 a 5, com provas e cenários).
- Plano, seção 3 (as regras novas de `(?i)`, `localeCompare` e `.sort()`):
  `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.

## Decisions already made

1. **Achado 1, JS e Go.** O check `cargo-config` lê, além de hoje, as formas `build.target-dir = …`,
   `build = { target-dir = … }` e `"target-dir" = …` (aspas simples ou duplas no valor), em
   `.cargo/config.toml` e `.cargo/config` da cópia **e de cada pasta ancestral da cópia** até a raiz,
   e em `$CARGO_HOME/config.toml`/`config` (sem `CARGO_HOME`: `~/.cargo`). O primeiro valor achado na
   ordem de precedência do cargo (a pasta mais próxima da cópia vence; `CARGO_HOME` por último)
   decide. Um valor relativo resolve contra a pasta que contém o `.cargo` onde ele está. A mensagem
   de falha diz qual arquivo: `fail cargo-config: <caminho do arquivo> sets target-dir inside the
   source tree`. Não chame o `cargo`.
2. **Achado 2, Go.** `ENOTDIR` conta como ausência, como no JS.
3. **Achado 3, JS e Go.** A ordem da listagem de symlinks do `mutation-guard` passa a ser por unidade
   de código UTF-16 (decrescente, como hoje), no JS e no Go. Faça o mesmo com os outros dois
   `localeCompare` do JS: `lib/dispatch.mjs:146` e `lib/commands/stats.mjs:299` (só o JS: `dispatch`
   e `stats` ainda não existem em Go). Crie `text.CompareUTF16` em `internal/text`, com teste.
4. **Achado 4, Go.** `reportscan` sem `(?i)`: compare contra uma cópia da linha com só A–Z em
   minúsculas (ou classes explícitas), com os casos de `ſ` e `K` (Kelvin) da revisão.
5. **Achado 5.** Testes que matam as mutações sobreviventes da tabela da revisão, e o diferencial de
   `reportscan` aumentado para pelo menos 500 entradas aleatórias geradas pelo JS.
6. Os testes JS novos são de caixa-preta pela CLI (`mutation-guard.test.mjs`, e o que prova a ordem
   em `dispatch`/`stats`); o mesmo cenário vira teste Go do `mutation-guard`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. Go: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` limpos,
   `go test ./...` ok (cole).
2. JS: `node --test` e `bun test --timeout 60000` de `mutation-guard.test.mjs`, `dispatch.test.mjs`,
   `stats.test.mjs` → 0 fail (cole). Se um golden mudar, regrave com `HERDR_SOHO_GOLDEN=update` e
   mostre o diff decodificado.
3. Os cenários do achado 1 (as três formas, ancestral, `CARGO_HOME`) → `fail` no JS e no Go, com
   `diff` Go × JS vazio (cole). Os cenários dos achados 2 e 3 → iguais nos dois.
4. Uma mutação por correção (cópia fora do repositório,
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes),
   mostrando o teste vermelho.

## Owned files

- `skills/herdr-soho/scripts/lib/commands/mutation-guard.mjs`, `lib/dispatch.mjs` (só a linha do
  `localeCompare`), `lib/commands/stats.mjs` (só a linha do `localeCompare`), e os testes JS
  `mutation-guard.test.mjs`, `dispatch.test.mjs`, `stats.test.mjs`
- `internal/cli/mutation_guard.go`, `internal/cli/mutation_guard_test.go`, `internal/reportscan/**`,
  `internal/text/text.go` e `text_test.go` (só `CompareUTF16`)

## Forbidden

- Todo o resto; `internal/platform/**` e `internal/cli/cli.go` estão em outra fatia agora.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/<seu nome>/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas e as mutações.
