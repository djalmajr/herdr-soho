# Brief — S4a-b: correções da revisão do `stats` e do `collect` (4 P1, 4 P2)

Role: implementer · Agent: AGENT_NAME · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do `stats`/`collect` em Go reprovou com 4 P1 e 4 P2, todos executados contra o JS:

1. Data ISO sem fuso lida como UTC, e não como horário local.
2. Despacho sem relatório contado como revisão sem cabeçalho.
3. `collect` roda fora do Herdr, e o JS recusa.
4. Relatório ilegível devolve sucesso.
5. CRLF do relatório não é preservado na impressão.
6. Os pares não são ordenados por unidade UTF-16.
7. A largura da tabela não é medida em unidades UTF-16.
8. Espaço Unicode na linha `sha256` não é reconhecido.

Os números do `stats` decidem que assistente usar em cada papel, e o `collect` entrega o relatório de
um worker. Erro aqui leva a uma decisão de time errada ou a um relatório trocado.

Worktree: `WORKTREE_PATH`, branch `go/s4a-b` (criado de `go/port`, commit `COMMIT`).

- Revisão, com a prova e a correção proposta de cada achado:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260929T011828.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3: UTF-16,
  `sort`, `\s` do JS).

## Decisions already made

1. Cada achado segue a correção proposta pela revisão. Onde ela deixa escolha, siga o JS
   (`skills/herdr-soho/scripts/lib/commands/{stats,collect}.mjs`).
2. **Achado 3**: o `collect` é um comando "vivo" no JS (`LIVING` em `herdr-soho.mjs`), então chama
   `herdr.RequireEnv` como o `roster`/`release` no `cli.Run`. O `stats` não é vivo e não muda.
3. **Achado 4**: relatório ilegível sai com o código e o texto do JS. Se o JS quebra com stack, o Go
   sai com diagnóstico e código 4, como a divergência já aceita na C1b-b; diga qual dos dois foi.
4. Cada achado vira um teste Go, com o vizinho que não deve mudar. O fuso é fixado por `TZ` no
   teste.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Cada achado com a sonda da revisão reexecutada contra o binário novo e o JS, lado a lado:
   `SAME` ou a divergência aceita (cole).
3. Portão: binário em `/tmp/hs-go/<slot>/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`; ali você só
   roda testes, não edita. Comando:
   `HERDR_SOCKET_PATH=/tmp/hs-go/<slot>/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/<slot>/herdr-soho node --test scripts/test/stats.test.mjs scripts/test/collect.test.mjs`
   → 0 fail (cole).
4. Uma mutação por achado, com o teste vermelho e o código de saída colados, uma linha por mutação.
   Faça as mutações numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/stats/**`, `internal/cli/stats_collect_test.go` e, em `internal/cli/cli.go`, só o
  `RequireEnv` do `collect`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e
  `h1`.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/<slot>/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, divergências e perguntas abertas.
