# Brief — S6a-b: `setup --plan --local` e o legado no Go

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A S6a passou a servir `setup --plan`, `--detect` e `--probe` em Go. O portão de paridade da suíte
JS inteira achou 20 falhas que o portão da própria fatia não cobria:

- 19 em `setup-local.test.mjs`: o `--local` do Go é uma versão simplificada. Faltam o plano das
  exclusões do git, as recusas de segurança (symlink no `CLAUDE.local.md`, no `exclude`, no
  `.git/info`, estado dentro do repositório por alias, `HERDR_SOHO_DIR` inseguro, estado na raiz
  do worktree, espaço no fim), o worktree ligado e o `--dry-run`.
- 1 em `legacy.test.mjs`: o lado "antes" do plano não parte do arquivo de projeto legado quando o
  novo não existe.

Essas recusas são proteções: um `--plan` que mostra um plano onde o JS recusaria esconde do usuário
que o `setup` de verdade vai falhar ou escrever num lugar perigoso.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/s6a-b` (criado
de `go/port`, commit `0743095`).

- Log do portão (procure `✖`):
  `/tmp/hs-go/orch/parity-5144665.log`.
- Brief e relatório da S6a:
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/s6a-go-setup-read.md` e
  `/work/herdr-soho/.herdr-soho/w14/reports/build-2-20260928T231246.current.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/setuplocal.mjs` e o que
  `lib/commands/setup-plan.mjs` usa dele, e `lib/legacy.mjs`.

## Decisions already made

1. O que `setup-plan.mjs` usa de `setuplocal.mjs` vira `internal/setup/local.go`. São estas funções:
   `localRels`, `localTarget`, `planLocalExcludes`, `preflightLocalExcludes`, `refuseTrackedLocal`,
   `refuseUnignorableStateDir`, `resolveSetupMode` e o que elas chamarem. Mesmos textos, mesmos
   códigos (rc 4 nas recusas) e a mesma ordem: recusar antes de mostrar qualquer plano. Não escreva
   nada em disco.
2. O `--dry-run` que o `setup-local.test.mjs` usa: se ele é do `setup` que escreve (ainda no JS),
   continua no fallback. Só o `--plan` é seu. Diga no relatório quais testes passam pelo fallback.
3. O legado segue `lib/legacy.mjs` (`effectiveConfigFile`, `legacyProjectConfigPath`): o lado
   "antes" do plano lê o arquivo legado quando o novo não existe, e o diff mostra o arquivo novo.
4. Os testes Go ficam no `internal/setup`. Cada regra de recusa tem pelo menos um caso Go. Onde o
   caso precisa de symlink no Windows, só aquele subteste faz `t.Skip`.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho` (a árvore
   `go/port` + harness H1, só leitura para você),
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/setup-local.test.mjs scripts/test/legacy.test.mjs scripts/test/setup-plan.test.mjs scripts/test/parity-setup-plan.test.mjs scripts/test/setup-legacy.test.mjs`
   → 0 fail (cole o resumo).
3. Uma mutação por regra de recusa (symlink do alvo, symlink do exclude, ancestral `.git/info`,
   estado dentro do repositório por alias, estado na raiz do worktree, espaço no fim) e uma no
   legado. Cada mutação vai numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes. Cole
   o teste vermelho e o código de saída, uma linha por mutação.

## Owned files

- `internal/setup/**` e `internal/cli/setup*_test.go`.

## Forbidden

- Os outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e `h1`.
- O `setup` que escreve, `doctor` e `init`.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, testes que passam pelo fallback,
divergências e perguntas abertas.
