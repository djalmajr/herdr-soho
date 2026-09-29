# Revisão R-G1 — fundação Go (módulo, platform, entrada com fallback)

Role: reviewer · Agent: review-2 · Report language: pt-BR

Este brief não tem relação com o anterior deste painel.

## Goal

Revisar a primeira fatia da migração do herdr-soho para Go antes do merge. O código Go tem que se
comportar byte a byte como o JS de referência. As fatias seguintes constroem em cima desta API, então
um erro aqui se multiplica.

- Worktree de revisão: `/work/herdr-soho/.worktrees/r-g1`, branch
  `go/g1-foundation`, commit `475d6cf` (pai `02910bb`). Diff: `git -C <worktree> show 475d6cf`.
  Rode tudo nesse worktree; o worktree `s4` já está em outra fatia.
- Brief da fatia: `/work/herdr-soho/.herdr-soho/w14/plan-briefs/g1-go-foundation.md`.
- Relatório do implementador: `/tmp/herdr-soho/w14/reports/build-20260928T192650.md`.
- Plano (decisões e convenções, seções 2 a 5):
  `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`.
- JS de referência: `<worktree>/skills/herdr-soho/scripts/lib/platform.mjs`, `herdr-soho.mjs`,
  `lib/usage.mjs`, launcher `scripts/herdr-soho`.

## Expected result

Relatório com a primeira linha `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail` e, por
achado: prioridade, confiança, arquivo:linha, se foi executado ou só lido, e a prova colada.

Verifique, executando sempre que der:

1. Paridade de cada função de `internal/platform` com a do JS, lado a lado: mesmos ramos, mesmos
   padrões, mesmos erros. Atenção a `FindExecutable` (PATHEXT, extensões, caminho com separador),
   `CmdInvocation` (aspas do `cmd.exe`), `AtomicWrite` (arquivo temporário, rename, modo),
   `StateProjectRoot` (bare, submódulo, worktree ligado, fora de git), `ReadTextFile` (U+FFFD pela
   regra WHATWG, CRLF), `RunCli` (timeout, código de saída, sinal).
2. Ajuda e comando desconhecido: rode o binário e o JS e compare stdout, stderr e código.
3. Fallback: comandos conhecidos repassados ao JS com os mesmos argumentos, ambiente e códigos;
   sem node nem bun, a mensagem e o código 2. Teste um comando que lê stdin e um que devolve código
   diferente de 0.
4. Convenções da seção 3 do plano: `Die`/`ExitError`/`recover` só em `cli.Run`, nenhum `os.Exit`
   fora do `main`, `Env` explícito, nomes do Node para plataforma, `Stdout`/`Stderr` trocáveis.
5. Testes: os casos portados de `platform.test.mjs` e `worktree-state.test.mjs` existem e testam o
   que o título JS diz; o diferencial de `ReadTextFile` é gerado pelo JS de verdade. Rode uma ou
   duas mutações suas, numa cópia fora do repositório, e diga se os testes pegam.
6. `go vet ./...`, `go test ./...`, `gofmt -l cmd internal`, `GOOS=windows go vet ./...`.

Rode o Go com `GOTOOLCHAIN=local GOPROXY=off GOCACHE=/tmp/hs-go/review/cache GOPATH=/tmp/hs-go/review/gopath GOTMPDIR=/tmp/hs-go/review/tmp`
(crie as pastas). Nenhum Herdr real: herdr falso no `PATH` e
`HERDR_SOCKET_PATH=/tmp/hs-go/review/none.sock` quando algum comando precisar.

## Forbidden

- Editar qualquer arquivo do repositório ou dos worktrees. Mutações só em cópia fora do repositório
  (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes).
- Herdr real que escreva. Rede.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, primeira linha no formato acima, depois os achados e, por item
de "Expected result", `[done]` / `[partial]` / `[skipped]` + motivo com saídas coladas.
