# Brief — D5a: o lint de brief e o comando `lint` em Go

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar o lint de brief: as seções obrigatórias (Goal, Expected result, Forbidden, Report, a linha de
no-commit, Owned files para papel que edita), os aliases `brief_lint_aliases` (pt-BR inclusive), os
modos `warn|strict|off` e a matriz de falhas. Também o comando `lint`, que roda esse lint sem mandar
nada. O `dispatch` usa o mesmo lint e fica para outra fatia: ele depende de uma mudança JS que ainda
não chegou à `main`. O lint em si não muda com ela.

Um lint que deixa passar um brief sem Forbidden ou sem a linha de no-commit manda para um worker um
contrato sem limites. Um lint que reprova um brief bom trava o `dispatch`.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/d5a` (criado
de `go/port`, commit `b59e14a`).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/commands/lint.mjs` e, em
  `skills/herdr-soho/scripts/lib/dispatch.mjs`, as funções do lint: `briefLintFindings`,
  `lintBrief`, `parseBriefLintAliases`, `failureMatrixMissing` e as auxiliares delas (mais ou menos
  entre as linhas 200 e 420). Não porte o `cmdDispatch`.

## Decisions already made

1. `internal/dispatch/lint.go` ← as funções de lint de `dispatch.mjs`. O pacote `internal/dispatch` já
   existe, com `sandboxNotes`. O comando `lint` fica em `internal/cli/lint.go` ou em
   `internal/dispatch`, o que for mais simples.
2. Regex: cada `/…/i` sem `u` do JS casa contra `text.ASCIILower` da linha, sem `(?i)`. Cada `\s` usa
   a classe de espaço do JS (seção 3 do plano). A lista de avisos sai na mesma ordem e com o mesmo
   texto do JS.
3. Roteamento: `lint` sai do fallback. Mesmos códigos de saída do JS em cada modo.
4. Testes: os casos de `lint.test.mjs` e os de lint em `dispatch.test.mjs`, com `// JS: "<título>"`.
   Some um diferencial `testdata/gen_lint.mjs`, em que o JS lint um corpus de briefs:
   - os de `.herdr-soho/w14/plan-briefs/` deste projeto, copiados para o `testdata`;
   - variações geradas: seções faltando, aliases pt-BR, cabeçalhos com caixa e acento diferentes, `ſ`
     longo, CRLF e BOM.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. O diferencial do lint com zero divergências. Cole o resumo, com quantos briefs deram aviso e
   quantos passaram limpos.
3. Portão: binário em `/tmp/hs-go/build-2/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`; ali você só lê,
   não edita. Comando:
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/lint.test.mjs`
   → 0 fail (cole).
4. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - seção faltando;
   - no-commit faltando;
   - Owned files só para papel que edita;
   - alias pt-BR;
   - modo `strict` × `warn`;
   - matriz de falhas.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/dispatch/**` (só o lint e os seus testes/`testdata`), o registro de `lint` em
  `internal/cli/cli.go` e `internal/cli/lint*.go`.

## Forbidden

- O `cmdDispatch`, o `wait` e o `status`. O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`,
  `docs/`. Editar os worktrees `gate` e `h1`.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build-2/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
