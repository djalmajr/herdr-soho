# Brief — C1a: `internal/herdr`, `internal/codexenv` e o CLI falso dos testes Go

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Portar o cliente do `herdr` (todas as chamadas da skill ao Herdr passam por ele) e o diagnóstico do
ambiente do Codex, e criar o CLI falso que os testes Go de todas as próximas fatias vão usar no lugar
do `herdr` real, no Mac e no Windows.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/c1a` (criado de
`go/port` com o `main` atual integrado, commit `bbf39cd`; o JS de referência é o deste worktree).
Outra fatia (G1b) corrige agora `internal/platform` e `internal/cli/cli.go` em outro worktree: use a
API de `internal/platform` como está; se precisar de algo novo nela, descreva no relatório.

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5; D6
agora tem `internal/herdr` como pacote próprio).

## Decisions already made

1. Pacotes: `internal/codexenv` ← `lib/codex-env.mjs`; `internal/herdr` ← `lib/herdr.mjs`. O
   `DieError` que o `herdr.mjs` importa do `config` é o `platform.ExitError`/`platform.Die`.
2. Onde o JS devolve o JSON do herdr (objeto), o Go devolve o valor de `jsonjs.Parse` (objeto
   ordenado), para que uma saída futura que reescreva esses dados mantenha a ordem do JS. Campos que
   o JS extrai e usa como valor simples viram campos tipados de uma struct de resultado.
3. `internal/testutil/fakecli` (novo): instala um CLI falso com um nome dado (`herdr`, `ps`, …) numa
   pasta temporária que o teste põe no `PATH`. O falso é o próprio binário de teste reexecutado: o
   `TestMain` do pacote chama `fakecli.Main()` primeiro, que assume o papel quando uma variável de
   ambiente própria está definida. O comportamento vem de um script declarativo (por exemplo JSON
   com regras "argv casa → stdout, stderr, código, atraso") e toda chamada fica registrada num
   arquivo que o teste lê. No Windows o falso é uma cópia do binário como `<nome>.exe` (sem `.cmd`);
   no Unix, um symlink ou cópia sem extensão. Documente a API no próprio pacote.
4. Testes portados:
   - `codex-env.test.mjs` e os casos de `herdr.test.mjs` que chamam funções de `herdr.mjs`/`codex-env.mjs`
     (hoje por `node -e`), um `t.Run` por caso com `// JS: "<título>"`;
   - os casos que dependem de `ps`/`/proc` usam o `fakecli` para `ps` no lugar do sistema.
   Os casos JS que chamam a CLI inteira (`requireEnv` pelo comando) ficam para quando o comando for
   portado; liste-os.
5. `requireEnv` fica em `internal/herdr` com o mesmo texto de erro e o diagnóstico do Codex. O
   fallback da CLI não muda nesta fatia.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` limpos,
   `go test ./...` ok (cole).
2. `GOOS=windows GOARCH=amd64 go test -c -o /tmp/hs-go/build/herdr_test.exe ./internal/herdr` e o
   mesmo para `./internal/codexenv` compilam (o orquestrador roda no Windows).
3. Diferencial: `parseCodexPolicy`, `evaluateCodexPolicy`, `globMatch` e `stripTomlComment` contra o
   JS num corpus gerado pelo JS (os casos dos testes e hostis: TOML com aspas, escapes, tabelas em
   linha de várias linhas, comentários, CRLF, acento). Cole o resumo.
4. Uma mutação por função com regra (`AgentState` com as pausas de nova tentativa, `RequireEnv`,
   `ParseCodexPolicy`, `EvaluateCodexPolicy`, `GetProcessAncestors`), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes),
   mostrando o teste vermelho.
5. API nova no relatório, incluindo a do `fakecli`.

## Owned files

- `internal/herdr/**`, `internal/codexenv/**`, `internal/testutil/fakecli/**`

## Forbidden

- `internal/platform/**`, `internal/cli/**` e os pacotes do G2 (outras fatias os mudam agora),
  `go.mod`, tudo em `skills/`, `plugin/`, `docs/`.
- Herdr real: nenhum teste chama o `herdr` de verdade (`HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock`
  e o `fakecli` no `PATH`).
- Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos criados, API, casos JS não portados com
motivo, divergências do JS e perguntas abertas.
