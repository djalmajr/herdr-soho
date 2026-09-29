# Brief — GW3: os testes Go de todos os pacotes passam no Windows real

Role: implementer · Agent: build-4 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

A quinta rodada dos binários de teste Go no Windows 11 real (amd64, sem privilégio de symlink) teve
28 testes falhando em 9 dos 17 pacotes: `cli`, `codexenv`, `core`, `herdr`, `kinds`, `peer`,
`platform`, `setup` e `testutil/fakecli`. Uma parte é teste que assume Unix. Outra parte é defeito do
produto no Windows, e o JS de referência medido no próprio Windows já mostra a divergência:

- raiz do estado com `/`;
- `resolveFrom` com caminho sem drive;
- a pasta da skill não encontrada fora do repositório;
- o `.EXE` maiúsculo.

Deixar os 17 pacotes verdes no Windows, sem pular o que só existe lá.

Worktree: `/work/herdr-soho/.worktrees/build-4`, branch `go/gw3` (criado de `go/port`, commit `e8ef75a`).

- Falhas (linhas `--- FAIL` e `_test.go:N`): `/tmp/hs-go/gowin5-failures.txt`. Log inteiro:
  `/tmp/hs-go/gowin5-log.txt`. Rodado em `go/port` `5144665`.
- Briefs das rodadas anteriores (as regras valem de novo):
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/gw1-go-windows-tests.md` e
  `/work/herdr-soho/.herdr-soho/w14/plan-briefs/gw2-go-windows-tests.md`.
- A referência JS medida no Windows: `internal/platform/testdata/windows-roots.json`.
- Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seção 3, "Testes
  que rodam no Windows").

## Decisions already made

1. **`fakecli` em todo pacote que o usa.** "Windows test binary was not prepared by TestMain" em
   `cli`, `kinds`, `peer`, `setup` e `core` (`lanes_test.go`): todo pacote de teste que usa o
   `fakecli` tem o `TestMain` que o prepara. Se o `fakecli` puder se preparar sozinho, com
   preguiça e uma vez por processo, prefira isso a copiar o `TestMain` em cada pacote. Diga qual
   escolheu.
2. **`fakecli` `.EXE` maiúsculo** (`fakecli_test.go:67`, `NotFound:true`): é produto. O
   `platform.FindExecutable` ou o `RunCli` do Windows não acham `probe.EXE`. Corrija pela regra do JS
   (`PATHEXT` sem distinguir caixa) e diga onde estava.
3. **`platform` — `resolveFrom` e a raiz do estado** (`platform_test.go:374`, `:831`, `:857`, `:871`):
   é produto. O JSON de referência vem do JS no Windows, e o Go tem de devolver o mesmo:
   - caminho sem drive resolvido como `path.win32`;
   - a saída do `git` com `/` normalizada para `\`;
   - o caminho relativo do worktree ligado.

   Não mexa no JSON para o teste passar.
4. **Pasta da skill fora do repositório** (`roles_test.go:58` "unknown role 'scouter'",
   `core_test.go:89/133/415`, `config_session_test.go:53/189`, `parity_lanes_test.go:174`,
   `ownproviders_test.go:24`): os binários rodam em `%TEMP%\hs-gowin\<pacote>\`, com o repositório
   copiado para `%TEMP%\hs-gowin\`. O `testdata` e os arquivos do repositório são achados por caminho
   relativo à pasta do pacote (`../../skills/herdr-soho/...`), nunca pelo caminho absoluto do Mac
   gravado no binário (`\Users\djalmajr\...` em `config_session_test.go:53`). Se o produto acha a
   skill pelo executável (`os.Executable`), o teste passa `HERDR_SOHO_SKILL_DIR` explicitamente.
5. **Modos e privilégios**: modo `666` depois de copiar (`core_test.go:333`,
   `config_session_test.go:278`). No Windows o teste afirma o bit de somente leitura, não os bits
   Unix. Symlink sem privilégio: só aquele subteste faz `t.Skip`.
6. **`mutation_guard_test.go`**: os nomes com aspas (`ha mutation "guard"`) são proibidos no Windows.
   O prefixo segue o do JS (`ha mutation guard ` no win32). O `cargo` falso com shebang POSIX vira um
   `fakecli` no Windows.
7. **`codexenv`**: o diagnóstico no win32 devolve a mensagem base por desenho. Os casos de
   `TestDiagnoseDynamicSnapshotNavigation` e `TestDiagnosePaneFiltersAndAncestorCase` que exercitam a
   filtragem passam a plataforma `"linux"` explicitamente. O contrato win32 tem teste próprio.
8. **`herdr`** (`TestLiveAgentsFailureSemantics`, `TestRequireEnv`): leia o log. Se é sinal POSIX,
   caso próprio do Windows ou pular só aquele subteste com o motivo. Se é produto, corrija e explique.
9. Roteiros de compilar e rodar no Windows em `/tmp/hs-go/build-4/`, como na GW1/GW2: cada binário
   roda a partir da pasta do seu pacote, com os argumentos entre aspas simples no PowerShell. O
   orquestrador roda no Windows e devolve o log.

## Expected result

1. No Mac: `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` ok (cole).
2. Os binários de teste Windows dos 17 pacotes compilam (cole a lista).
3. Por falha do log (as 28): causa, correção, e se era teste ou produto. Uma tabela.
4. A suíte JS do portão no Mac continua verde para o que você tocou: a partir de
   `/work/herdr-soho/.worktrees/gate/skills/herdr-soho`,
   `HERDR_SOCKET_PATH=/tmp/hs-go/build-4/none.sock HERDR_SOHO_TEST_BIN=/tmp/hs-go/build-4/herdr-soho node --test scripts/test/platform.test.mjs scripts/test/state.test.mjs scripts/test/mutation-guard.test.mjs`
   (os arquivos que existirem) → 0 fail.

## Owned files

- Todo `*_test.go` e `testdata/` de `internal/**`, `internal/testutil/fakecli/**`, e, no produto, só
  `internal/platform/**` (itens 2 e 3). Outro defeito de produto no Windows: descreva no relatório.

## Forbidden

- Produto fora de `internal/platform`, `go.mod`, `skills/`, `plugin/`, `docs/`, os worktrees `gate` e
  `h1`. Mudar o JSON de referência do Windows.
- Herdr real, rede, módulos de terceiros, `go.sum`. Cache e build só em `/tmp/hs-go/build-4/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos, produto corrigido e por quê, e perguntas
abertas.
