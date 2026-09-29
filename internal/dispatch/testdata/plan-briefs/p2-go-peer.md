# Brief — P2: `peer` e `sessions` em Go, e os comandos `send` e `find`

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar a mensagem entre agentes (`send`: cabeçalho fixo, corpo citado, espera por alvo ocupado,
recusa por diálogo e por `inbound=off`, prova de entrega sem reenvio) e a listagem de sessões
(`find`, local e máquinas habilitadas). O `send` fala com agentes de outros projetos: a prova de
entrega e as recusas são proteções, e um desvio do JS aqui manda texto para onde não devia.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/p2` (criado de `go/port`, commit `aaccd4e`: fundação, pacotes
puros, `herdr`, `fakecli`, núcleo de config e estado).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
O pacote `internal/core` está recebendo agora `roles`/`resolve`/`lanes` em outro worktree: não mexa
nele.

## Decisions already made

1. `internal/peer`: `peer.go` ← `lib/peer.mjs`, `sessions.go` ← `lib/sessions.mjs`.
2. Comandos em `internal/cli`, saindo do fallback: `send <ref|name> <mensagem…> | --file <caminho>
   [--now] [--timeout MS]` e `find [palavras…] [--machine <rótulo>]… [--all] [--json]`.
3. Todo prazo, janela e intervalo de sondagem igual ao JS (inclusive as variáveis
   `HERDR_SOHO_SEND_WINDOW_MS`, `HERDR_SOHO_SEND_POLL_MS` e o teto de 15000). O texto do cabeçalho,
   das linhas fixas e da linha de fim byte a byte.
4. Testes com o `fakecli` no lugar do `herdr` (nunca o real), ids impossíveis (`w0test:p0a`),
   `HERDR_SOCKET_PATH` para um caminho inexistente: os casos de `send.test.mjs`, `peer.test.mjs`,
   `find.test.mjs`, um `t.Run` por caso com `// JS: "<título>"`. Os casos de diálogo usam as
   fixtures de tela do JS (`scripts/test/fixtures/`).

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-4/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-4/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-4/herdr-soho node --test scripts/test/send.test.mjs scripts/test/peer.test.mjs scripts/test/find.test.mjs scripts/test/sessionref.test.mjs`
   → 0 fail (cole). Não edite nada no worktree `h1`.
3. Uma mutação por regra (prova (a) por mudança de estado, prova (b) pelo id na tela, Enter só com o
   id nas últimas 15 linhas, nunca reenviar, diálogo nas últimas 10/20 linhas, `inbound=off`,
   saneamento do corpo hostil), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes), com
   o teste vermelho colado.
4. API nova no relatório.

## Owned files

- `internal/peer/**`, `internal/cli/{send,find}*.go` e o registro em `internal/cli/cli.go`

## Forbidden

- `internal/core/**` (outra fatia agora; se precisar de algo novo nele, descreva no relatório), os
  outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`.
- Herdr real: nenhum teste manda nada a um painel de verdade. Módulos de terceiros, `go.sum`, rede.
  Cache e build só em `/tmp/hs-go/build-4/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
