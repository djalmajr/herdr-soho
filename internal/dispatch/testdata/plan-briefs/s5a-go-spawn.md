# Brief — S5a: `spawn` em Go

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar o `spawn`: abrir um painel para um papel e iniciar a CLI do assistente com o kind, o modelo,
o esforço e as aprovações resolvidos pela config. Também:

- reusar um worker ocioso compatível;
- nomear o agente no Herdr;
- escolher onde dividir;
- esperar a CLI ficar pronta, detectando diálogo, erro de provider e cota;
- refazer a grade e os rótulos das abas.

O `spawn` decide que processo roda no painel do usuário e com que permissões. Um `--approvals full`
mapeado errado, um modelo trocado, ou um reuso de um worker aberto com outros argumentos dá a um
agente mais poder que o configurado, ou o modelo errado.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/s5a` (criado
de `go/port`, commit `2cf39a8`: tudo até agora, inclusive `internal/layout` com a âncora, a grade e
os rótulos, `internal/kinds` com modelos e esforço, e `internal/core` com `resolve`, `lanes` e
`roles`).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/spawn.mjs` e o que ele importa.

## Decisions already made

1. `internal/spawn` ← `lib/spawn.mjs`. Outra fatia (D6, `doctor`/`init`), em outro worktree agora,
   está criando `internal/spawn/native.go` só com `configNativeArgs` e `ensureOrchestratorName`.
   Ponha essas duas funções também em `internal/spawn/native.go`, tradução direta do JS, e o resto
   em outros arquivos do pacote. O orquestrador resolve o merge dos dois `native.go`.
2. `pollIntervalMs` (hoje em `lib/wait.mjs`) entra no mínimo em `internal/spawn` (ou em
   `internal/core`, se for só leitura de config). O `wait` é outra fatia.
3. Toda chamada ao Herdr passa por `internal/herdr`, na mesma ordem do JS. Nos testes, o `fakecli`
   faz o papel do Herdr e dos assistentes (nunca os reais) e grava cada chamada, com os argumentos.
4. A linha de comando de cada kind, com o modelo, o esforço, `--approvals` e os `args.<kind>`, sai
   byte a byte igual à do JS. Os testes comparam o argv gravado pelo `fakecli`.
5. Roteamento: `spawn` sai do fallback.
6. Testes: os casos de `spawn.test.mjs` e `parity-spawn.test.mjs`, com `// JS: "<título>"`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-4`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-4/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`. Essa árvore é
   só leitura para você: rodar os testes nela é permitido, editar não.
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-4/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-4/herdr-soho node --test scripts/test/spawn.test.mjs scripts/test/parity-spawn.test.mjs scripts/test/regrid.test.mjs scripts/test/herdtabs.test.mjs`
   → 0 fail (cole). Diga quais testes chamam o JS direto (sem a CLI) e, portanto, não provam o Go.
3. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - `--approvals full` por kind;
   - modelo da lane de camada inferior descartado;
   - `--kind` sem `--model` descarta o modelo configurado;
   - esforço limitado ao teto do kind;
   - reuso recusado quando os args mudaram;
   - nome tomado recebe o sufixo;
   - diálogo de confiança deixado para um humano (exit 7);
   - regrid só com `regrid=on`.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/spawn/**`, o roteamento de `spawn` em `internal/cli/cli.go` e `internal/cli/spawn*_test.go`.

## Forbidden

- O resto de `internal/**` (se precisar de algo novo fora do `spawn`, descreva no relatório),
  `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e `h1`.
- Herdr real e os assistentes reais: nunca abra um painel de verdade nem rode `claude`, `codex`,
  `grok`, `agy`, `cursor-agent`, `pi` ou `opencode`. Rede, módulos de terceiros, `go.sum`. Cache e
  build só em `/tmp/hs-go/build-4/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, testes que não passam pelo Go,
divergências e perguntas abertas.
