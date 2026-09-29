# Brief — C1c-b: correções da revisão da C1c (roster, clean, friction, title, feedback)

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão da C1c achou que o `clean` do Go apaga do roster agentes vivos, com o relatório e os
arquivos de espera deles, e que o `roster` os mostra como `gone`: `field` não lê o `*jsonjs.Object`
que o `internal/herdr` devolve. Corrigir isso e os outros dez achados, com testes que os prendam.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/c1c-b` (criado de `go/port`, commit `d3cdf1a`).

- Revisão (leia inteira; cada achado tem o arquivo, a linha e a prova):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T225957.md`
- Passagem anterior da mesma revisão (sondas e casos já varridos):
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T225748.md`
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. **F1 (P1)**: `field` lê `*jsonjs.Object` (e continua aceitando `map[string]any`). Todo lugar que
   lê um agente, pane ou aba vindo do `herdr` passa por ele.
2. **F2**: o `recover` de `cli.Run` grava no `friction.log` como o `dieFriction` do JS
   (`herdr-soho.mjs:242-243`) toda saída com mensagem, quando o caminho do log é conhecido.
3. **F3**: igual ao JS: o pane de uma linha sem nome entra em `rosterPanes`; o `task-<agente>` é lido
   cru (o `\r` fica); o rótulo numérico usa a formatação de número JS (`jsonjs`). Para
   `herdr pane current` que não é objeto, e `workspace_id` objeto ou lista, o Go **não** copia o JS
   (que cria `.herdr-soho/null` ou `[object Object]`): sai com diagnóstico e código 2, sem criar
   diretório. Divergência aceita; o teste JS aceita a forma do Go sob `HERDR_SOHO_TEST_BIN` só se
   algum teste JS cobrir isso (se cobrir, descreva no relatório; não edite o `h1`).
4. **F4 e F11**: comparação estrita como o `===` do JS: `pane_id` nulo não é pane vazio, número não é
   string.
5. **F5**: diretórios `0o777` e o log `0o666`, sujeitos ao umask, como o JS.
6. **F6**: `title` corta só o espaço que o `String.prototype.trim` do JS corta (U+0085 fica);
   `feedback send` troca por unidade UTF-16 no nome do projeto e relê o relatório como UTF-8
   (substituição WHATWG, `text`); o erro de colisão diz `(EEXIST)`, como o JS.
7. **F7**: lock que é arquivo regular não é removido: sai com o mesmo erro e código do JS, lock e
   linha preservados.
8. **F8**: `clean` que não consegue ler `wait/` não apaga nada: sai com diagnóstico e código 4
   (o JS quebra com stack e código 1; divergência aceita, a mesma da C1b-b). `friction add` com o log
   sendo diretório: código 4 com diagnóstico (divergência aceita).
9. **F9**: os cabeçalhos de contrato de `BriefTask` casam contra `text.ASCIILower` da linha, sem
   `(?i)`, para o ſ longo não virar `s`.
10. **F10**: `MarkTaskDone` devolve `error`; quem chamar decide (o JS lança).
11. **Mutantes sobreviventes da revisão (M02, M06, M14, M15, M16)**: um teste que mate cada um.
12. Testes Go de `roster` e `clean` com agente vivo com pane, sem pane e com `pane_id` nulo, e um
    `agent list` no formato real do `herdr` (objeto em `result.agents`).

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Cada achado F1–F11 com a sonda da revisão reexecutada contra o binário novo e contra o JS, lado a
   lado, com a saída colada: `SAME`, ou a divergência aceita acima.
3. A tabela de mutantes da revisão linha por linha (M01–M17), reexecutada contra o código novo, com o
   teste que falha e o código de saída de cada um. Um mutante que você não conseguiu rodar é
   `[partial]` com o motivo, não "morto".
4. Portão: binário em `/tmp/hs-go/<slot>/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/<slot>/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/<slot>/herdr-soho node --test scripts/test/roster.test.mjs scripts/test/clean.test.mjs scripts/test/friction.test.mjs scripts/test/title.test.mjs scripts/test/feedback.test.mjs`
   (os arquivos que existirem) → 0 fail (cole). Não edite nada no worktree `h1`.

## Owned files

- `internal/cli/{roster,clean,friction,feedback,title}*.go` e seus testes, o `recover` em
  `internal/cli/cli.go`, `internal/core/state.go`, `internal/core/tasks.go` e seus testes.

## Forbidden

- Outros arquivos de `internal/core/**` e `internal/cli/**`, os outros pacotes, `go.mod`, `skills/`,
  `plugin/`, `docs/`, o worktree `h1`.
- Herdr real: todo teste usa o `fakecli`, `HERDR_SOCKET_PATH` para um caminho inexistente e ids
  impossíveis como `w0test:p0a`. Módulos de terceiros, `go.sum`, rede. Cache e build só em
  `/tmp/hs-go/<slot>/`. Mutações só em cópia fora do repositório, com
  `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API mudada, divergências e perguntas
abertas.
