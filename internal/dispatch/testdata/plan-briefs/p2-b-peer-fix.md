# Brief — P2-b: correções da revisão do P2 (`send` e `find`)

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão do P2 passou, com 6 achados P3 de paridade com o JS. Um deles faz o `send` recusar uma tela
que o JS aceita (`truſt this workspace` com ſ longo). Os outros são diferenças de borda no
`--timeout`, no log, na busca e na formatação de números do `find`. Fechar os 6, com testes que os
prendam.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/p2-b` (criado de `go/port`, commit `cf7850d`).

- Revisão (cada achado tem arquivo, linha, a prova e a correção proposta):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T232718.md`.
  O harness do revisor fica em `/tmp/hs-go/review/p2/` (`diff.mjs`, `find-diff.mjs`), se ainda
  existir.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3: `/i`
  sem `u`, `toLowerCase`, números JS).

## Decisions already made

1. **Achado 1**: os padrões de diálogo casam contra uma cópia com só A–Z em minúsculas
   (`text.ASCIILower`), sem `(?i)`.
2. **Achado 2**: `--timeout` aceita o que `Number()` do JS aceita e exige inteiro positivo
   (`1e3`, `12.0` valem; `0x10` também, se o `Number()` aceitar — confira no Node).
3. **Achado 3**: o log troca cada corrida de `[\r\n\t]` por um espaço, como o JS.
4. **Achado 4**: a busca do `find` e o `questionDialog` dobram como o `toLowerCase()` do JS (İ vira
   `i` + U+0307, Σ final vira ς). Se não houver uma função pronta em `internal/text`, crie
   `text.JSLower` nela, com teste diferencial gerado pelo Node.
5. **Achado 5**: números do snapshot formatados como `String(n)` do JS (a formatação JS de
   `internal/jsonjs`), antes de gravar o campo e antes da busca.
6. **Achado 6**: um elemento que não é objeto na lista de máquinas é ignorado, como o JS; o resto da
   lista segue.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Cada achado com a sonda da revisão reexecutada contra o binário novo e contra o JS, lado a lado:
   `SAME` (cole).
3. Portão: binário em `/tmp/hs-go/build-2/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/send.test.mjs scripts/test/find.test.mjs scripts/test/sessionref.test.mjs`
   → 0 fail (cole).
4. Uma mutação por achado, cada uma com o teste vermelho e o código de saída colados, uma linha
   por mutação. Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/peer/**`, e `internal/text/**` só para acrescentar `JSLower` (e o gerador/teste dele).

## Forbidden

- Os outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e `h1`.
- Herdr real: todo teste usa o `fakecli`, `HERDR_SOCKET_PATH` para um caminho inexistente e ids
  impossíveis como `w0test:p0a`. Rede. Cache e build só em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
