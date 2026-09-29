# Brief — G2b: correções da revisão do G2 (diferencial vazio, `(?i)`, cortes)

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do G2 achou que o diferencial do `provider` não compara nada (3358 casos passam sem
afirmar), um regex sem `(?i)`, a dobra de caixa do RE2 diferente da do JS, e fronteiras sem teste.
Fechar tudo, com os pacotes do G2 provados contra o JS de verdade.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/g2b` (criado de `go/port`, commit `f361360`: G1, P1 e G2
integrados). Outras fatias mexem agora em `internal/platform`, `internal/cli`, `internal/reportscan`
e em `internal/text/text.go` (só a função `CompareUTF16`, nova) em outros worktrees; o orquestrador
junta os branches.

Leia antes:
- Revisão: `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T201448.md`
  (F1 a F6, com provas, fuzz e mutações).
- Plano, seção 3 (regras de `(?i)`, `localeCompare`, `.sort()`, UTF-16):
  `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.
- Brief do G2: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g2-go-pure.md`.

## Decisions already made

1. **F1.** O teste diferencial decodifica o `kind` de verdade e falha (`t.Fatal`) em erro de
   decodificação ou `kind` vazio; nenhum `_ =` descarta erro em teste. Depois do conserto, todo
   subteste compara Go × JS. Aplique a mesma checagem aos diferenciais dos outros pacotes do G2.
2. **F2.** `RenewalValue` com a duração insensível a caixa, e o corpus com unidades em maiúscula.
3. **F3.** `LastNonEmptyLines(screen, 0)` devolve tudo, como o `slice(-0)` do JS.
4. **F4.** Nenhum `(?i)` nem `strings.ToLower`/`ToUpper` onde o JS usa `/i` sem `u` ou
   `toLowerCase` em texto de terminal: compare contra uma cópia com só A–Z trocadas (helper em
   `internal/text`, por exemplo `text.ASCIILower`), ou classes explícitas. Os casos da revisão
   (`ſ`, `K` de Kelvin, `İ`) entram no corpus e no teste.
5. **F5.** Testes que matam todas as mutações sobreviventes da tabela da revisão (P3, P4, P5, P9,
   P10, P15, P16, P17, P18, P19, S2, T4, T7, X4, J9), incluindo acento e emoji em toda função que
   corta texto (regra da seção 3).
6. **F6.** O contrato de `QuestionText` fica escrito no código: o valor pode ter surrogate solto
   (WTF-8) e serve o JSON; quem gravar texto em arquivo troca surrogate solto por U+FFFD. Crie a
   função dessa troca em `internal/text` (por exemplo `text.ToWellFormedUTF8`), com teste contra o
   `fs.writeFileSync` do Node (gerador), para a fatia do `wait` usar.
7. **Fuzz.** Um diferencial aleatório com semente fixa, gerado pelo JS, de pelo menos 5000 entradas
   para `ProviderDetect`, `QuotaDetect`, `RenewalValue`, `DialogKind`, `QuestionText` e
   `LastNonEmptyLines`, com os caracteres que a revisão usou (ASCII, `ſ`, `K`, `İ`, U+2028, NBSP,
   ESC, CR, emoji).

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` limpos,
   `go test ./...` ok (cole).
2. A prova do F1: com `ProviderDetect` trocado por "sempre nil" numa cópia, o diferencial sozinho
   (`go test ./internal/provider -run Differential`) falha (cole).
3. A tabela de mutações da revisão refeita: todas pegas (cole).
4. Os geradores rodam duas vezes sem mudar os JSON versionados (`shasum` antes e depois).

Mutações só numa cópia fora do repositório, com
`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/provider/**`, `internal/sessionref/**`, `internal/setuptext/**`, `internal/jsonjs/**`
- `internal/text/**`, exceto a função `CompareUTF16` (outra fatia a cria)

## Forbidden

- `internal/platform/**`, `internal/cli/**`, `internal/reportscan/**`, `internal/taskreport/**`,
  `go.mod`, tudo em `skills/`, `plugin/`, `docs/`.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/<seu nome>/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações.
