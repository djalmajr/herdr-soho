# Brief — P1c: `cargo-config` pelo próprio cargo, e fail-closed sem ele (JS e Go)

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A segunda revisão do `mutation-guard` achou seis formas TOML válidas em que o cargo escreve dentro do
source e o guard diz `ok`, nos dois lados. Um conjunto de regex por linha não fecha isso. Trocar a
abordagem: perguntar ao cargo quando ele existe, e falhar fechado quando não dá para decidir.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/p1b` (commit `0ce4cab`, o P1b).

Leia antes:
- Revisão, com a tabela de formas e as provas com `cargo metadata`:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T205814.md`.
- Revisão anterior (a tabela de mutações sobreviventes):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T200756.md`.

## Decisions already made

1. **Com `Cargo.toml` na cópia e `cargo` no `PATH`:** o check `cargo-config` roda
   `cargo metadata --offline --no-deps --format-version 1` na cópia (com o ambiente recebido, prazo
   de 30 s) e compara o `target_directory` com o source. Dentro do source → `fail cargo-config:
   cargo metadata puts target_directory inside the source tree`. Falha do comando (código ≠ 0,
   timeout, JSON sem `target_directory`) → `fail cargo-config: cargo metadata failed; target-dir
   cannot be resolved safely`.
2. **Sem `Cargo.toml` na cópia:** `ok cargo-config` (nada vai compilar Rust ali; um
   `~/.cargo/config.toml` do usuário com `target-dir` global não deve reprovar uma cópia de projeto
   que não é Rust).
3. **Com `Cargo.toml` e sem `cargo` no `PATH`:** o check textual de hoje, com as correções da revisão
   — só a chave nua `target-dir` dentro de `[build]` (o arquivo lido inteiro antes de decidir);
   `config` antes de `config.toml` na mesma pasta, parando no primeiro que existir; relativo do
   `CARGO_HOME` resolvido contra o pai do `CARGO_HOME` — e **fail-closed**: se o texto tem
   `target-dir` (ou `target\u`, `"target`) fora de comentário e o parse não achou um valor que ele
   entenda, `fail cargo-config: <arquivo> target-dir cannot be resolved safely`.
4. **Espaços (achado 3).** No Go, toda posição em que o JS usa `\s` usa a classe de espaços do JS.
5. **Mutações (achado 2).** Refaça a tabela de mutações das duas revisões **linha por linha**: para
   cada mutação listada lá (G3, G5, G7, G10, G12, G13, G14, as de `reportscan` R1, R2, R3, R7 e as
   demais), aplique numa cópia fora do repositório, rode, e cole o código de saída do Go e do JS. Toda
   linha tem que terminar "pega". Uma tabela com mutações suas no lugar das da revisão não conta.
6. **`reportscan` (achado 4).** O gerador produz cabeçalhos válidos com variações (números
   grandes, zeros à esquerda, espaços, veredito em caixa diferente) além dos inválidos, e mostra
   quantos resultados não nulos gerou; o corpus de `partialCount` também cresce.
7. JS e Go mudam juntos e dão a mesma saída; os testes JS são de caixa-preta pela CLI, e o mesmo
   cenário vira teste Go. Os testes que dependem do `cargo` real pulam com o motivo quando ele não
   está no `PATH`; os de fail-closed e de "sem `Cargo.toml`" não dependem dele.

## Expected result

1. A tabela da revisão (as 13 formas) → `fail` no JS e no Go com `cargo` no `PATH`; e, com um `PATH`
   sem `cargo`, `fail` pelo fail-closed ou pelo parse corrigido. Cole as duas tabelas.
2. A tabela de mutações refeita linha por linha (decisão 5). Cole.
3. Go: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` (cole). JS: `node --test` e `bun test --timeout 60000` de
   `mutation-guard.test.mjs` → 0 fail (cole).

## Owned files

- `skills/herdr-soho/scripts/lib/commands/mutation-guard.mjs`,
  `skills/herdr-soho/scripts/test/mutation-guard.test.mjs`
- `internal/cli/mutation_guard.go`, `internal/cli/mutation_guard_test.go`, `internal/reportscan/**`
- A frase do `SKILL.md` que descreve o `mutation-guard` (o check do cargo muda)

## Forbidden

- Todo o resto; nenhum outro worktree.
- Módulos de terceiros, `go.sum`, rede (o `cargo metadata` roda com `--offline`). Cache e build só em
  `/tmp/hs-go/<seu nome>/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
