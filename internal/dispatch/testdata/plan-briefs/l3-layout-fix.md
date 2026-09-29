# Brief — L3: correções da revisão de L/L2 (3 P1, 5 P2, 1 P3)

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do layout em Go reprovou com 3 P1, 5 P2 e 1 P3, todos executados contra o JS com um herdr
falso:

1. O rótulo da aba vira `+impl` quando o primeiro papel está vazio.
2. O `layout-plan` sem área imprime só uma quebra de linha (erro do `json.Marshal` descartado).
3. Um retângulo ausente vira 0 e estaciona o painel, onde o JS pula o candidato.
4. O corte do rótulo automático usa um `\s` diferente do JS.
5. A raiz da aba não é usada quando o painel não tem id.
6. O `-` gravado não aparece no `tab-label --auto`.
7. Um pânico no relabel do `regrid` passa em silêncio.
8. O escape do JSON do `layout-plan` é diferente do `JSON.stringify`.
9. `tab_id: false` não é tratado como estacionamento.

Esses comandos mexem nos painéis e nas abas de verdade do usuário.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/l3` (criado de `go/port`, commit `1da49be`).

- Revisão, com a prova e a correção proposta de cada achado. O herdr falso e os casos ficam em
  `/tmp/hs-go/review/l2/`, se ainda existirem:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-20260929T003818.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3: `\s` do
  JS, números JS, `jsonjs`).

## Decisions already made

1. Cada achado segue a correção proposta pela revisão. Onde a revisão deixa escolha, siga o JS de
   referência (`skills/herdr-soho/scripts/lib/{layout,herdtabs,regrid}.mjs`).
2. Todo JSON que um comando imprime passa por `jsonjs`, nunca por `json.Marshal` com o erro
   descartado.
3. Número não finito (NaN, ±Inf, ausente) segue a regra JS de `Number(...)` e `Number.isFinite`.
4. Cada achado vira um teste Go, com o vizinho que não deve mudar. A sequência de chamadas ao herdr
   falso é comparada com a do JS.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Cada achado com a sonda da revisão reexecutada contra o binário novo e o JS, lado a lado:
   `SAME` (cole).
3. Portão: binário em `/tmp/hs-go/build/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`. Essa árvore é
   só leitura para você: rodar os testes nela é permitido, editar não.
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/layout.test.mjs scripts/test/herdtabs.test.mjs scripts/test/regrid.test.mjs scripts/test/parity-layout.test.mjs scripts/test/parity-regrid.test.mjs scripts/test/release.test.mjs`
   → 0 fail (cole).
4. Uma mutação por achado, com o teste vermelho e o código de saída colados, uma linha por
   mutação. Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/layout/**`, `internal/cli/{layout_plan,regrid,tab_label}*.go`, e em `internal/herdr` só o
  tratamento de `tab_id` do achado 9 (e o teste).

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e
  `h1`.
- Herdr real: nunca mova, divida, renomeie ou feche um painel ou aba de verdade. Rede, módulos de
  terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, divergências e perguntas abertas.
