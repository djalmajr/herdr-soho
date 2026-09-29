# Brief — C1a-c: o parser da política Codex igual ao JS

Role: implementer · Agent: build · Report language: pt-BR

Continuação da sua C1a-b, mesmo assunto.

## Goal

Fechar o F7 e o F5 que ficaram `[partial]` na C1a-b. No F7, a sonda mostrou o parser TOML do Go
divergindo do JS em 276 casos estruturais e 155 de avaliação, em 3.097 documentos, e em 18 de 97
casos manuais. O `codex-env` decide que variáveis de ambiente o worker Codex herda. Um parser que
aceita o que o JS recusa, ou o contrário, muda o ambiente do worker em silêncio.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/c1a-c` (criado
de `go/port`, commit `15ad833`, que já tem a sua C1a-b).

- Seu relatório da C1a-b:
  `/work/herdr-soho/.herdr-soho/w14/reports/build-20260928T224605.current.md`.
- Revisão da C1a:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T215403.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. **F7: o JS é a referência, sempre.** O Go reproduz o JS também onde o JS recusa um TOML válido ou
   aceita um inválido: mesma estrutura, mesma avaliação, mesmo erro (texto e código na saída do
   comando). Corrigir o parser é uma função futura, só em Go e depois da paridade. Ela não entra
   nesta fatia.
2. **F5**: `Process.PID` continua `int`. Um PID que não cabe em `int` (como `1e20`) é tratado como
   fora do domínio: o processo é ignorado. Isso é divergência aceita e fica registrada no relatório
   com a sonda.
3. A sonda diferencial vira teste: um `testdata/gen_codexenv_toml.mjs` gera, com o JS, os documentos
   e as saídas (estrutura e avaliação), e o teste Go compara. Use corpus determinístico (semente
   fixa) com pelo menos os 3.097 documentos e os 97 casos manuais da sua sonda.
4. As 13 mutações `PATTERN-MISSING` da C1a-b: reescreva cada uma para o código novo e rode, ou diga
   por que não se aplica mais.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. O diferencial TOML com zero divergências de estrutura e de avaliação (cole o resumo), ou cada
   divergência restante listada com o documento mínimo e o motivo, marcada `[partial]`.
3. As 13 mutações reescritas: morta, sobrevivente ou equivalente, com o teste e o código de saída,
   uma linha cada.
4. Portão: binário em `/tmp/hs-go/build/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/codex-env.test.mjs`
   → 0 fail (cole). Não edite nada no worktree `h1`.

## Owned files

- `internal/codexenv/**` (código, testes e `testdata/`), e o tipo `Process` em `internal/herdr` só
  se o F5 exigir.

## Forbidden

- Os outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`.
- Herdr real, o `codex` real, rede, módulos de terceiros, `go.sum`. Cache e build só em
  `/tmp/hs-go/build/`. Mutações só em cópia fora do repositório, com
  `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, divergências aceitas e perguntas
abertas.
