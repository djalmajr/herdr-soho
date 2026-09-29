# Brief — G1b: correções da revisão do G1 (RunCli, fallback, testes)

Role: implementer · Agent: build-2 · Report language: pt-BR

## Goal

Fechar os 10 achados da revisão do G1 e duas pendências que chegaram depois, para que as próximas
fatias do Go construam sobre um `internal/platform` e um fallback que batem com o JS e têm testes que
pegam defeitos.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/port` (commit
`f361360`: o G1 `475d6cf`, o P1 `5c0ba24` e o seu G2 `6cdfc58`, já integrados; `go test ./...` verde).

Leia antes:
- Revisão, com as provas e as correções sugeridas:
  `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T195624.md`.
- Plano, seções 2 a 5: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.
- Brief do G1: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g1-go-foundation.md`.

## Decisions already made

1. **Achado 1 (P1).** `RunCli` com resultado nomeado, para os `defer` que leem os arquivos
   temporários chegarem ao chamador. Testes dos três modos (padrão, `MergeOutput`, `OutputFiles`)
   contra o JS.
2. **Achado 2.** No Windows, `.cmd`/`.bat` passam pelo `SysProcAttr.CmdLine` montado com o resultado
   do `CmdInvocation` (arquivo com build tag `windows`). O orquestrador confirma no Windows.
3. **Achado 3.** O legado (`HERDR_AGENTS_*` → `HERDR_SOHO_*`) é aplicado numa cópia do `Env`, usada
   só nas decisões do próprio Go (ajuda, NOWRITE, comandos portados). O fallback recebe o `Env`
   original.
4. **Achados 4 e 6.** No Unix o fallback usa `syscall.Exec`: o processo Go vira o runtime, como o
   `exec` do launcher (sinais e código de saída passam a ser os do runtime). No Windows, filho com
   `os/exec`, código do filho devolvido, e falha ao iniciar escreve `herdr-soho: <erro>` e sai 127.
5. **Achado 5.** `RunCli` devolve `Signal`, `TimedOut` e o `error` como o JS: nomes `SIGxxx` por
   tabela, códigos `ENOENT`/`ENOEXEC`/`EACCES`/… a partir do `syscall.Errno`, e a regra de
   `timedOut` que o JS usa (leia em `lib/platform.mjs`, não invente).
6. **Achado 7.** `HomeDir`: `HOME` (win32: `USERPROFILE`) do `Env`; senão `os/user.Current()`;
   senão `Die` com código 2 e a mensagem `cannot resolve the home directory; set HOME` (no win32,
   `USERPROFILE`). Divergência aceita e registrada: o JS acha o home pelo passwd onde o Go sem cgo
   não acha (macOS com `env -i`).
7. **Achado 8.** `PATHEXT` e `PATH` divididos como o JS divide (leia `findExecutable`), descartando
   segmentos vazios onde o JS descarta.
8. **Achado 9.** Timeout: o filho morre como no JS, e `WaitDelay` de 100 ms. Não mate o grupo de
   processos (o JS não mata).
9. **Achado 10.** Testes que matam as mutações da tabela da revisão (M1, M2, M3, M4, M5, M8, M11):
   - `ReadTextFile`: diferencial com pelo menos 2000 entradas aleatórias de 1 a 8 bytes, geradas e
     lidas pelo JS (gerador com semente fixa, JSON versionado), incluindo `E0 80`, `F0 80`, `F4 90`;
   - `FindExecutable`: bit de execução, win32 com `PATHEXT` (portar `models.test.mjs:265-295`, que
     também cobre `cmdInvocation`);
   - `AtomicWrite`: arquivo novo 0600, erro com código `ELOOP`, links continuam links, cadeia resolve;
   - `StateProjectRoot`: o ramo de `core.worktree`;
   - NOWRITE com argumentos extras;
   - fallback: node < 20 com bun, stdin, descoberta da raiz da skill.
10. **Runtime do fallback.** `HERDR_SOHO_JS_RUNTIME` (caminho absoluto de node ou bun), quando
    definida e executável, é o runtime do fallback, sem checar versão; senão a regra do launcher.
    Veja a seção 2 (D5) do plano. O harness da suíte JS define essa variável.
11. **B2c no Go.** O JS de `stateProjectRoot`/`projectRoot` mudou no branch
    `fix/state-across-worktrees`, commit `4461a88` (`git -C /work/herdr-soho
    show 4461a88 -- skills/herdr-soho/scripts/lib/platform.mjs`): raiz com segmento `.git` recusada
    (POSIX e Win32) e cache por processo com a mesma chave. Porte as duas coisas, com os testes novos
    de `worktree-state.test.mjs` daquele commit.

12. **`taskreport` (sobra do P1).** Porte `lib/taskreport.mjs` para `internal/taskreport`, agora que
    `internal/jsonjs` está no branch: as quatro funções exportadas, a escrita JSON pelo `jsonjs` e
    testes (ponteiro ausente, válido, corrompido, escrita atômica, `syncTaskReport` com e sem
    relatório), mais um diferencial do JSON gravado contra o JS.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = seu nome), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `GOOS=linux go build ./...` limpos, `go test ./...` ok (cole).
2. Por achado: a correção e o teste que prova, com a saída vermelha antes (mutação ou código antigo
   numa cópia fora do repositório,
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
3. As sondas da revisão (achados 1, 3, 4, 5, 6, 8, 9) rodadas de novo contra o JS: iguais (cole).
4. A tabela de mutações da revisão refeita: todas pegas.
5. API alterada ou nova no relatório.

## Owned files

- `internal/platform/**`, `internal/cli/cli.go`, `internal/cli/cli_test.go`, `internal/cli/usage.go`
- `internal/taskreport/**` (novo)

## Forbidden

- `internal/cli/mutation_guard*.go`, `internal/reportscan/**` (P1, em revisão à parte), os pacotes do G2
  (`internal/text`, `jsonjs`, `provider`, `sessionref`, `setuptext`, em revisão à parte), `go.mod`,
  tudo em `skills/`, `plugin/`, `docs/`.
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/<seu nome>/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por achado e por entrada de "Expected result", com `[done]`,
`[partial]` ou `[skipped]` e o motivo, saídas coladas. Depois: API alterada, divergências aceitas e
perguntas abertas.
