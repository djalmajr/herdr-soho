# Brief — S6a: `setup --plan`, `--detect` e `--probe` em Go

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar as três leituras do `setup`, que mostram e sondam sem escrever nada: `--plan` (o diff que o
setup faria), `--detect` (o JSON de assistentes, modelos e revisor recomendado) e `--probe` (a
sondagem de um assistente com timeout). O `setup` que escreve continua no JS (fallback) nesta fatia.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/s6a` (criado
de `go/port`, commit `b5ffdcb`: fundação, pacotes puros, `herdr`, núcleo de config/estado,
`roles`/`resolve`/`lanes`, `kinds`/`models`, `peer`).

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. `internal/setup`: `plan.go` ← `lib/commands/setup-plan.mjs`, `detect.go` ←
   `lib/commands/setup-detect.mjs`, `probe.go` ← `lib/commands/setup-probe.mjs`. O que eles importam
   de `setup.mjs` (`setupTargetExisting`) e de `setuplocal.mjs` entra em `internal/setup` só na
   medida do que as três leituras usam.
2. `lib/ownproviders.mjs` vira `internal/kinds/ownproviders.go` (pi e opencode: providers, modelos
   próprios, orçamentos de thinking). `ownProviderDoctorLines` fica de fora (é do `doctor`).
3. `resolvedRoleKind` (hoje em `lib/spawn.mjs:95`) entra em `internal/core/resolve.go`, com o mesmo
   comentário de origem. É a única mudança permitida em `internal/core`.
4. Roteamento em `internal/cli`: `setup` com `--plan`, `--detect` ou `--probe` roda em Go; qualquer
   outra forma de `setup` segue para o fallback JS sem mudar nada. A combinação de flags inválida
   sai com o mesmo texto e código do JS.
5. O `--probe` executa a CLI do assistente: nos testes, sempre o `fakecli` no lugar dela, nunca um
   assistente real. Timeout, texto de "not authenticated" e ordem das linhas iguais ao JS.
6. Testes: os casos de `setup-plan.test.mjs`, `setup-detect.test.mjs`, `setup-probe.test.mjs`, um
   `t.Run` por caso com `// JS: "<título>"`. Os goldens `parity-setup-{plan,detect,probe}` valem
   para o Go.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build-2/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/h1/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/setup-plan.test.mjs scripts/test/setup-detect.test.mjs scripts/test/setup-probe.test.mjs scripts/test/parity-setup-plan.test.mjs scripts/test/parity-setup-detect.test.mjs scripts/test/parity-setup-probe.test.mjs`
   → 0 fail (cole o resumo). Não edite nada no worktree `h1`. Se um teste passar só pelo fallback JS
   (e não pelo Go), diga qual.
3. Uma mutação por regra (recomendação de revisor de outra família, ordem dos modelos mais novos
   primeiro, timeout do probe, linha de não autenticado, diff unificado do plan), numa cópia fora do
   repositório (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>`
   antes), com o teste vermelho e o código de saída colados, uma linha por mutação.
4. API nova no relatório.

## Owned files

- `internal/setup/**`, `internal/kinds/ownproviders.go` e seu teste, `resolvedRoleKind` em
  `internal/core/resolve.go` (e seu teste), o registro em `internal/cli/cli.go` e
  `internal/cli/setup*_test.go`.

## Forbidden

- O resto de `internal/core/**`, os outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, o
  worktree `h1`.
- O `setup` que escreve (sem `--plan`/`--detect`/`--probe`), `doctor`, `init`: outra fatia.
- Executar um assistente de verdade (`claude`, `codex`, `grok`, `agy`, `cursor-agent`, `pi`,
  `opencode`) em teste ou sonda. Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só
  em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, casos JS não portados com motivo,
divergências e perguntas abertas.
