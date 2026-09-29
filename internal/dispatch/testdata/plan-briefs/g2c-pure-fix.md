# Brief — G2c: a troca de `(?i)` onde o JS não tem `/i`, e as fronteiras soltas

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do G2b reprovou por uma regressão da própria rodada: ao passar todo o bloco de regex do
`provider` para minúsculas, um padrão que no JS diferencia caixa (`ECONNREFUSED`) passou a casar
`econnrefused`. Fechar os 4 achados.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/g2c` (criado de `go/port`, commit `11db892`, que já tem o G2b).

Leia antes: `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T212500.md`
(F1 a F4, com provas, fuzz e os consertos testados pelo revisor).

## Decisions already made

1. **Regra geral (F1).** Cada regex Go segue a flag do seu regex JS, um por um: `/i` sem `u` → cópia
   com minúsculas ASCII; sem `/i` → a linha original, sem nenhuma troca de caixa. Faça a tabela
   regex JS → regex Go → flag → linha usada, para todos os regex de `provider`, `quota` e `dialog`, e
   cole no relatório.
2. **F2.** `RenewalValue`: o grupo am/pm volta a ser `[ap]\.m\.` sobre a cópia minúscula.
3. **F3.** `QuotaPhraseQuoted`: o índice é calculado sobre uma minúscula que dobra o K de Kelvin
   (U+212A) em `k` como o `toLowerCase` do JS (e só as trocas que o JS faz e mudam o comprimento em
   UTF-16 como ele), com o caso da revisão como teste.
4. **F4.** As quatro fronteiras viram casos: `Z` no teste de `ASCIILower`, `bearer sk-abcdefgh12` no
   gerador do `text`, `{"hooks":{"SessionStart":[{"hooks":false}]}}` no do `setuptext`, e frases de
   cota (`hit your usage limit`, `resets in `, `try again in `) nos átomos do fuzz do `provider`,
   com o número de resultados não nulos de cada função no relatório.
5. Refaça a tabela de mutações da revisão linha por linha, com o código de saída colado; todas pegas.

## Expected result

1. `gofmt -l internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` (cole).
2. As sondas da revisão (F1, F2, F3) contra o JS: iguais (cole).
3. A tabela de regex (decisão 1) e a de mutações (decisão 5).

## Owned files

- `internal/provider/**`, `internal/text/**` (exceto `CompareUTF16`), `internal/setuptext/**`

## Forbidden

- Todo o resto. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build-3/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
