# Brief — D6: `doctor` e `init` em Go

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Fechar a fase 6. O `doctor` é o diagnóstico que o usuário lê antes de abrir um time: Herdr dentro
ou fora, versões, skill oficial, assistentes no `PATH`, estado gravável, config, lanes, hooks,
sandbox do Codex, providers próprios. O `doctor --fix` alinha a config. O `init` roda o `doctor`,
renomeia o agente que chama para `orchestrator` e imprime o contexto. Um aviso que some, ou um
`--fix` que escreve a mais, deixa o usuário abrir um time com a config errada.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/d6` (criado de
`go/port`, commit `5a3a2e0`: tudo até agora, inclusive o `setup` inteiro em Go).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/commands/doctor.mjs` e `lib/commands/init.mjs`, e
  o que eles importam.

## Decisions already made

1. `internal/doctor` ← `doctor.mjs`; `init` em `internal/doctor/init.go` (ou `internal/cli`).
2. O que o `doctor`/`init` usam de módulos ainda não portados entra no mínimo necessário:
   - `configNativeArgs` e `ensureOrchestratorName` (hoje em `lib/spawn.mjs`) vão para um pacote
     novo `internal/spawn`, só essas duas funções e o que elas chamarem. A fase 5 completa o pacote
     depois.
   - `sandboxNotes` (hoje em `lib/dispatch.mjs`) vai para `internal/dispatch`, só ele.
   - `ownProviderDoctorLines` (hoje em `lib/ownproviders.mjs`) entra em `internal/kinds`.
3. Textos, ordem das linhas, `warn`/`ok`, `first_run` e códigos de saída iguais ao JS. O `--fix`
   escreve só o que o JS escreve, por `AtomicWrite`.
4. `init` renomeia o agente por `internal/herdr`. Nos testes, o `fakecli` faz o papel do Herdr
   (nunca o real).
5. Roteamento: `doctor` e `init` saem do fallback.
6. Testes: os casos de `doctor.test.mjs`, `init.test.mjs` e `parity-doctor.test.mjs`, com
   `// JS: "<título>"`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`. Essa árvore
   (`go/port` + harness H1) é **só leitura** para você: rodar os testes nela é permitido, editar
   não.
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/doctor.test.mjs scripts/test/init.test.mjs scripts/test/parity-doctor.test.mjs`
   → 0 fail (cole). Diga quais testes chamam o JS direto (sem a CLI) e, portanto, não provam o Go.
3. Uma mutação por regra, cada uma com o teste vermelho e o código de saída colados, uma linha por
   mutação. As regras:
   - `first_run` verdadeiro e falso;
   - skill oficial diferente de `herdr --skill`;
   - aviso de legado;
   - `split_max_panes` maior que `panes` e o `--fix` que alinha;
   - sandbox sem rede;
   - hook ausente;
   - `init` que não renomeia um nome já tomado.

   Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/doctor/**`, `internal/spawn/**` e `internal/dispatch/**` (só o mínimo da decisão 2),
  `internal/kinds/ownproviders*.go`, o roteamento de `doctor`/`init` em `internal/cli/cli.go` e
  `internal/cli/{doctor,init}*_test.go`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e
  `h1`.
- Herdr real, os assistentes reais (`claude`, `codex`, `grok`, `agy`, `cursor-agent`, `pi`,
  `opencode`), rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, testes que não passam pelo Go,
divergências e perguntas abertas.
