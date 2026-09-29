# Brief — R4: `release` em Go, e o `agent_status: false` do `herdr`

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar o `release <agente> [--close] [--force]`: soltar um worker, limpar o título da tarefa, e com
`--close` fechar o painel e refazer a grade e os rótulos das abas. O `release` fecha painéis de
verdade. Ele recusa fechar um worker que ainda trabalha sem relatório (código 3), ou cujo estado o
Herdr não informou (código 4). Essas recusas protegem o trabalho de um worker: um desvio do JS aqui
perde trabalho.

Junto, uma correção de uma linha da revisão da C1a-b/c: `agent_status: false` vira o estado
`"false"` no Go, e o JS o trata como ausente.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/r4` (criado
de `go/port`, commit `46a4865`: tudo até agora, inclusive `internal/layout` com `AutoRegrid` e
`HerdTabsRelabel`, e `internal/herdr` com `PaneClose` e `AgentState`).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/commands/release.mjs` e o que ele usa.
- Revisão da C1a-b/c (o P3 e a correção proposta):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260929T000659.md`.

## Decisions already made

1. `release` em `internal/cli/release.go` (ou um pacote `internal/release`, se o relatório justificar).
   Os textos, os códigos de saída (1, 3, 4) e a ordem das chamadas ao Herdr são os do JS:
   estado → fechar → refazer a grade → rótulos. As mensagens de recusa vão ao `friction.log` pelo
   registro geral do `cli.Run`, sem um registro próprio.
2. Só fecha painel que esta skill abriu (a coluna do roster que o JS lê). Um painel de fora nunca é
   fechado.
3. O P3: em `internal/herdr/herdr.go`, a função `str` usada para o `agent_status` devolve `""` para
   `false`; `jsString` continua `"false"` nas mensagens.
4. Testes: os casos de `release.test.mjs` e `for-released.test.mjs`, com `// JS: "<título>"`. Nos
   testes, o `fakecli` faz o papel do Herdr e grava as chamadas.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-3`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-3/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-3/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-3/herdr-soho node --test scripts/test/release.test.mjs scripts/test/for-released.test.mjs scripts/test/regrid.test.mjs scripts/test/herdtabs.test.mjs`
   → 0 fail (cole). Diga quais testes não passam pelo Go.
3. Uma mutação por regra, cada uma com o teste vermelho e o código de saída colados, uma linha por
   mutação. As regras:
   - recusa com o worker trabalhando sem relatório (3);
   - recusa com o estado desconhecido (4);
   - `--force` passa;
   - painel de fora nunca fechado;
   - painel de burst fecha sem `--close`;
   - grade e rótulos só depois de fechar;
   - `agent_status: false`.

   Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/cli/release*.go` (ou `internal/release/**`), o registro em `internal/cli/cli.go`, e em
  `internal/herdr/herdr.go` só a função `str` (e o teste dela).

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e `h1`.
  `spawn` e `wait`/`status`.
- Herdr real: nunca feche, mova ou renomeie um painel de verdade. Rede, módulos de terceiros,
  `go.sum`. Cache e build só em `/tmp/hs-go/build-3/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
