# Brief — S6a-c: correções da revisão das leituras do `setup` (4 P1, 2 P2)

Role: implementer · Agent: build-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A revisão da S6a/S6a-b reprovou com 4 P1 e 2 P2, todos executados contra o JS:

1. `--plan` com só `CLAUDE.md` planeja no `AGENTS.md`;
2. estado que atravessa um symlink de dentro do repositório não é recusado;
3. `\` no nome do estado vira `/` no POSIX em vez de ser recusado;
4. diretório do exclude sem permissão de escrita para o processo passa;
5. `setup --detect` com opção inválida sai 0;
6. `role.<nome>.kind` com ponto no nome do papel.

O `setup` que escreve (S6b, já integrado) usa o mesmo `internal/setup/local.go`. As correções valem
para os dois lados, e as recusas têm de disparar também no `setup --local` que escreve, antes de
qualquer escrita.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/s6a-c`
(criado de `go/port`, commit `4b9634f`, que já tem S6a, S6a-b e S6b).

- Revisão, com a prova, o vizinho que não deve disparar e a correção proposta de cada achado. Os
  casos do harness (`diffcheck.mjs`) ficam em `/tmp/hs-go/review/s6a/`, se ainda existirem:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260929T003513.md`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5).

## Decisions already made

1. **Achado 1**: sem marcador em nenhum dos dois, o alvo é `AGENTS.md` se ele é arquivo regular,
   senão `CLAUDE.md` se é arquivo regular e não symlink, senão `AGENTS.md`, como o JS
   (`setup-plan.mjs:449-455` e o equivalente em `setup.mjs`). A mesma regra no `setup` que escreve.
2. **Achado 2**: estado externo alcançado por um symlink abaixo da raiz é recusado (código 4, texto
   do JS), antes do `return` de "external".
3. **Achado 3**: a troca de `\` por `/` só no `win32`. No POSIX, `\` é metacaractere e recusa.
4. **Achado 4**: o teste de escrita usa `access(2)` com `W_OK` (`unix.Access` ou
   `syscall.Access`), no mesmo ancestral que o JS usa. No Windows, siga o que o `fs.accessSync` do
   Node faz lá: só o atributo de somente leitura.
5. **Achado 5**: o argv do `setup` passa pelo mesmo analisador do JS antes de qualquer
   `--detect`/`--probe`/`--plan`. Opção desconhecida ou flag sem valor sai com código 2 e o texto do
   JS.
6. **Achado 6**: a chave de config troca `.` e `-` por `_`, como o JS.
7. Cada achado vira um teste Go, com o vizinho que não deve disparar. As recusas também têm teste
   no `setup --local` que escreve: nada muda no disco.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Cada achado com a sonda da revisão (e o vizinho) reexecutada contra o binário novo e o JS, lado a
   lado: `SAME` (cole).
3. Portão: binário em `/tmp/hs-go/build-2/herdr-soho`. Rode a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`. Essa árvore é
   só leitura para você: rodar os testes nela é permitido, editar não.
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-2/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-2/herdr-soho node --test scripts/test/setup.test.mjs scripts/test/setup-local.test.mjs scripts/test/setup-plan.test.mjs scripts/test/setup-detect.test.mjs scripts/test/setup-legacy.test.mjs scripts/test/parity-setup.test.mjs scripts/test/parity-setup-plan.test.mjs scripts/test/parity-setup-detect.test.mjs`
   → 0 fail (cole).
4. Uma mutação por achado, cada uma com o teste vermelho e o código de saída colados, uma linha
   por mutação. Faça as mutações numa cópia fora do repositório e rode
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/setup/**`, o ramo do `setup` em `internal/cli/cli.go` e `internal/cli/setup*_test.go`.

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e
  `h1`.
- Herdr real, os assistentes reais, rede, módulos de terceiros (`golang.org/x/sys` inclusive: use o
  `syscall` da biblioteca padrão), `go.sum`. Cache e build só em `/tmp/hs-go/build-2/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, divergências e perguntas abertas.
