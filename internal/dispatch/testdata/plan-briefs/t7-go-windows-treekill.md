# Brief — T7: timeout de `.cmd`/`.bat` no Windows mata a árvore inteira (Job Objects)

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

No Windows, quando um `.cmd`/`.bat` estoura o timeout, o `spawnSync` do Node mata só o `cmd.exe`, e os
processos que o batch abriu continuam vivos. No caso de uma CLI instalada pelo npm, isso é o
`node …\cli.js` atrás do shim. O JS resolve com um helper (`lib/treekill-run.mjs`) que roda
`taskkill /PID <pid> /T /F` (issue #20). O plano pede outra coisa no Go: o processo filho num **Job
Object**, com `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, e `TerminateJobObject` no timeout. Assim a árvore
inteira morre, sem helper e sem `taskkill`.

O resultado que o `RunCli` devolve continua o do JS: status nulo, sinal `SIGTERM`, `ETIMEDOUT`, e a
saída capturada até o kill.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/t7` (criado
de `go/port`, commit `995640f`).

- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3 e a fase 7).
- JS de referência: `skills/herdr-soho/scripts/lib/platform.mjs`, a partir do `runCli` e do
  `runCliTreeKill` (linha 250 em diante), e `lib/treekill-run.mjs`. Os testes JS:
  `scripts/test/treekill.test.mjs`.
- O Go de hoje: `internal/platform/platform.go` (`RunCli`) e `internal/platform/process_windows.go`.

## Decisions already made

1. Só no Windows (`process_windows.go`, build tag), só para `.cmd`/`.bat` com `TimeoutMs > 0`, o mesmo
   caminho que o JS desvia para o helper. O resto (POSIX, `.exe`, sem timeout) não muda.
2. Job Object por `syscall`/`kernel32` (`CreateJobObjectW`, `SetInformationJobObject` com
   `JOBOBJECT_EXTENDED_LIMIT_INFORMATION`, `AssignProcessToJobObject`, `TerminateJobObject`), sem
   módulos de terceiros. O processo é criado suspenso (`CREATE_SUSPENDED`) e só retomado depois de
   entrar no job, para nenhum filho escapar antes. Se o job falhar (por exemplo, um processo já
   dentro de um job que não permite aninhar), caia no `taskkill /PID <pid> /T /F` como o JS, com o
   mesmo limite de 10 s.
3. O gancho de teste do JS (`HERDR_SOHO_TREEKILL_TEST_KILLER`) tem equivalente no Go para os testes
   portáveis. No Windows real, o teste prova que o neto morre: um `.cmd` que abre um `fakecli` que
   dorme e grava o PID. Depois do timeout, o PID não existe mais.
4. Os testes Windows seguem a regra da seção 3 do plano. O orquestrador roda os binários no Windows.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-4`), a partir do worktree:

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` e
   `go test ./...` ok (cole).
2. O binário de teste Windows de `internal/platform` compila (cole). No relatório, a instrução de uma
   linha para o orquestrador rodar o teste do neto no Windows.
3. O mapa dos casos do `treekill.test.mjs`: para cada um, o teste Go correspondente, ou por que não se
   aplica ao Job Object.
4. Uma mutação por regra, com o teste vermelho e o código de saída colados, uma linha por mutação.
   As regras:
   - só `.cmd`/`.bat` com timeout;
   - processo retomado só depois de entrar no job;
   - o resultado do timeout igual ao JS.

   Faça cada mutação numa cópia fora do repositório, com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes.

## Owned files

- `internal/platform/**` (produto e testes).

## Forbidden

- O resto de `internal/**`, `go.mod`, `skills/`, `plugin/`, `docs/`. Editar os worktrees `gate` e
  `h1`.
- Matar processos que o teste não criou, ou procurar processos por nome ou por linha de comando: só
  por PID que o próprio teste gravou. Herdr real, rede, módulos de terceiros (`golang.org/x/sys`
  inclusive), `go.sum`. Cache e build só em `/tmp/hs-go/build-4/`.
- Subagentes (se você dividir o trabalho) nunca gravam no caminho do relatório do contrato: só você
  escreve o relatório, uma vez, no fim.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, API, divergências e perguntas abertas.
