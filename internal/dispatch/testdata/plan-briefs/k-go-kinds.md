# Brief — K: `kinds` e `models` em Go, e os comandos `kinds`, `models`, `model`, `env`

Role: implementer · Agent: build-3 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar o mapa de kinds (executável, família, teto de esforço, tradução das flags de modelo, esforço e
aprovações para cada CLI) e a resolução de modelos (aliases, regex contra a lista da CLI, o mais novo
que casa), e servir pelo Go os comandos que só dependem disso. O `spawn` vai montar a linha de
comando de cada agente a partir daqui.

Worktree: `/work/herdr-soho/.worktrees/build-3`, branch `go/k` (criado de `go/port`, commit `aaccd4e`: fundação, pacotes
puros, `herdr`, núcleo de config e estado).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
O pacote `internal/core` está recebendo agora `roles`/`resolve`/`lanes` em outro worktree: não mexa
nele; use o que já existe (`Config`, `Cfg`, `CfgSource`, state).

## Decisions already made

1. `internal/kinds`: `kinds.go` ← `lib/kinds.mjs`, `models.go` ← `lib/models.mjs`.
2. Comandos em `internal/cli`, saindo do fallback: `kinds`, `models <kind>`, `model <kind> <spec>
   [effort]`, `env`.
3. A lista de modelos de cada CLI vem de rodar a CLI (`cursor-agent --list-models`, `agy models`,
   `grok models`) ou do cache do Codex (`~/.codex/models_cache.json`), como no JS, com o mesmo prazo
   (`HERDR_SOHO_MODELS_TIMEOUT`). Nos testes, essas CLIs são o `fakecli`; nunca a CLI real.
4. A regex de modelo do usuário (config) é compilada no Go: uma regex JS que o RE2 não aceita
   (lookaround, backreference) tem de dar o mesmo resultado que o JS dá quando a regex é inválida
   para ele — leia o JS e reproduza; se o JS aceita e o RE2 não, a divergência vai para o relatório
   como pergunta aberta, com o caso.
5. Testes: os casos de `kinds.test.mjs`, `models.test.mjs`, `env.test.mjs` e o golden
   `parity-kinds.json`, um `t.Run` por caso com `// JS: "<título>"`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-3/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-3/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-3/herdr-soho node --test scripts/test/kinds.test.mjs scripts/test/models.test.mjs scripts/test/env.test.mjs scripts/test/parity-kinds.test.mjs`
   → 0 fail (cole). Não edite nada no worktree `h1`.
3. Uma mutação por regra (teto de esforço por kind e por modelo do Codex, sufixo de esforço do
   Cursor, regex que escolhe o mais novo, alias, família por modelo no pi/opencode), numa cópia fora
   do repositório (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard
   <cópia>` antes), com o teste vermelho colado.
4. API nova no relatório.

## Owned files

- `internal/kinds/**`, `internal/cli/{kinds,models,model,env}*.go` e o registro em
  `internal/cli/cli.go`

## Forbidden

- `internal/core/**` (outra fatia agora; se precisar de algo novo nele, descreva no relatório), os
  outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, o worktree `h1`.
- CLIs de agente reais. Módulos de terceiros, `go.sum`, rede. Cache e build só em
  `/tmp/hs-go/build-3/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
