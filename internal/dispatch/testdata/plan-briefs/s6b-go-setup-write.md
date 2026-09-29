# Brief — S6b: o `setup` que escreve, em Go

Role: implementer · Agent: build · Report language: pt-BR

Continuação da sua S6a-b, mesmo assunto.

## Goal

Na S6a e na S6a-b, o Go passou a servir as leituras do `setup` (`--plan`, `--detect`, `--probe`,
`--plan --local`). Agora é a vez do `setup` que escreve:

- o bloco no `AGENTS.md`/`CLAUDE.md`/`CLAUDE.local.md`;
- os hooks;
- as exclusões do git no `--local`;
- `--panes`, `--lane`, `--set`/`--user-set`/`--session-set`;
- `--no-hooks`, `--target` e `--dry-run`.

O `setup` mexe em arquivos do projeto do usuário. Uma escrita pela metade, um bloco duplicado ou um
exclude no lugar errado estraga o repositório de outra pessoa. Por isso as recusas que você portou
no `--plan --local` valem também aqui, antes de qualquer escrita.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/s6b` (criado de
`go/port`, commit `46a4865`, que já tem a sua S6a-b).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).
- JS de referência: `skills/herdr-soho/scripts/lib/commands/setup.mjs`, o resto de
  `lib/setuplocal.mjs` (o lado que escreve) e o que eles usam de `lib/setuptext.mjs` (já portado em
  `internal/setuptext`).

## Decisions already made

1. `internal/setup/write.go` ← `setup.mjs` (`setupTargetExisting`, `setupWriteBlock`,
   `setupWriteHooks`, `projectNeedsConfigPrompt`, `writeHooksSection`, `cmdSetup`), e o lado que
   escreve de `setuplocal.mjs` em `internal/setup/local.go`.
2. Toda escrita passa por `platform.AtomicWrite`, como o JS. As recusas rodam antes da primeira
   escrita: se uma recusa acontece, nenhum arquivo muda.
3. `--dry-run` não escreve nada e mostra o mesmo texto do JS.
4. Roteamento: todo `setup` passa a rodar em Go. O fallback JS do `setup` sai do `cli.go`.
5. Testes: os casos de `setup.test.mjs`, `setup-local.test.mjs` (o lado que escreve),
   `setup-legacy.test.mjs` e o golden `parity-setup`, com `// JS: "<título>"`. O que for symlink no
   Windows faz `t.Skip` só naquele subteste.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Portão: binário em `/tmp/hs-go/build/herdr-soho` e, a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build/herdr-soho node --test scripts/test/setup.test.mjs scripts/test/setup-local.test.mjs scripts/test/setup-legacy.test.mjs scripts/test/parity-setup.test.mjs scripts/test/setup-plan.test.mjs scripts/test/parity-setup-plan.test.mjs scripts/test/parity-doctor.test.mjs scripts/test/legacy.test.mjs`
   → 0 fail (cole). Diga quais testes chamam o JS direto (sem passar pela CLI) e, portanto, não
   provam o Go.
3. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - recusa antes da escrita (nada muda);
   - bloco substituído, não duplicado;
   - bloco legado migrado;
   - hooks mesclados sem perder os do usuário;
   - exclude escrito no `.git/info/exclude` comum de um worktree ligado;
   - `--dry-run` sem escrita;
   - modo do arquivo preservado.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.
4. API nova no relatório.

## Owned files

- `internal/setup/**`, o roteamento do `setup` em `internal/cli/cli.go` e `internal/cli/setup*_test.go`.

## Forbidden

- Os outros pacotes, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e `h1`.
- `doctor` e `init`: outra fatia.
- Escrever fora de diretórios temporários dos testes. Herdr real, rede, módulos de terceiros,
  `go.sum`. Cache e build só em `/tmp/hs-go/build/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, testes que não passam pelo Go,
divergências e perguntas abertas.
