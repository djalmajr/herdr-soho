# Brief — G1: fundação Go (módulo, platform, entrada com fallback para o JS)

Role: implementer · Agent: build · Report language: pt-BR

Este brief não tem relação com os anteriores deste painel. Começa a migração do herdr-soho para Go.

## Goal

Criar o módulo Go na raiz do repositório com o binário `herdr-soho`, portar `platform.mjs` fielmente
e portar a entrada da CLI (roteamento de `herdr-soho.mjs` e o texto de `usage.mjs`), repassando para
o JS todo comando que ainda não foi portado. As próximas fatias constroem em cima desta API.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/g1-foundation`
(já criado, commit `02910bb`). O JS de referência é o deste mesmo worktree:
`/work/herdr-soho/.worktrees/s4/skills/herdr-soho/scripts/`.

Leia antes, inteiro: o plano `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md`
(seções 2, 3, 4 e 5 valem como decisões deste brief).

## Decisions already made

1. `go.mod`: `module github.com/djalmajr/herdr-soho`, `go 1.24`, sem `require`. Só biblioteca padrão.
2. `cmd/herdr-soho/main.go`: só `os.Exit(cli.Run(os.Args[1:], platform.EnvFromOS()))` (o nome pode
   variar, a forma não).
3. `internal/platform`: porte de todas as exportações de `lib/platform.mjs` — `die`, `homeDir`,
   `userConfigPath`, `readTextFile`, `projectRoot`, `stateProjectRoot`, `findExecutable`,
   `cmdInvocation`, `atomicWrite`, `runCli` — e das funções internas que elas usam. O `runCli` no
   Windows usa `treekill-run.mjs`: nesta fatia o Go não porta o treekill; no Windows o `RunCli`
   executa sem ele e o relatório marca isso como `[partial]` com o motivo (fase 7 do plano).
   Mais o que a seção 3 do plano define aqui: `ExitError`, `Die` (panic), `Env` (+ `EnvFromOS`),
   `Current()` (nomes do Node), `Stdout`, `Stderr`, `Now`.
4. `ReadTextFile` decodifica como o `readFileSync(file, 'utf8')` do Node (bytes inválidos → U+FFFD
   pela regra WHATWG: uma U+FFFD por subparte máxima inválida) e troca CRLF por LF. Prove com teste
   diferencial: gerador `internal/platform/testdata/gen_readtext.mjs` que escreve arquivos com bytes
   inválidos, lê pelo JS e grava o esperado em `testdata/readtext.json`.
5. `internal/cli`: `Run(args []string, env platform.Env) int`, com `recover` do `ExitError`
   (seção 3 do plano). Portado nesta fatia: a ajuda (`help`, `-h`, `--help`, sem comando: texto de
   `usage.mjs` byte a byte, saída 0) e o comando desconhecido (mesma mensagem e código do JS).
   Antes de rotear, leia em `herdr-soho.mjs` o que o JS faz antes do `switch` (legacy env,
   `loadConfig`, `HERDR_SOHO_NOWRITE`, friction dos comandos vivos) e reproduza o que afeta a ajuda
   e o comando desconhecido. Se alguma dessas etapas exigir código de fatias futuras (config), não
   porte: liste no relatório como pergunta aberta, com o caso em que a saída diverge.
6. Fallback: todo comando conhecido pelo JS e não portado executa o JS — `node` do `PATH` com versão
   ≥ 20, senão `bun`, com `<skill>/scripts/herdr-soho.mjs` e os mesmos argumentos, o mesmo ambiente,
   stdin/stdout/stderr herdados, e devolve o código do filho. Sem nenhum dos dois:
   `herdr-soho: needs Node.js 20+ or Bun` e código 2 (a mesma frase do launcher
   `scripts/herdr-soho`). A lista de comandos conhecidos vem do `switch` de `herdr-soho.mjs`.
7. Raiz da skill (D7 do plano): `HERDR_SOHO_SKILL_DIR`, senão subir de `os.Executable()` até a pasta
   com `SKILL.md` e `roles/`, senão `Die` com código 2 e mensagem que diga como definir a variável.
8. Nomes: exportações do JS em PascalCase (`findExecutable` → `FindExecutable`), parâmetros na mesma
   ordem; valores padrão do JS viram parâmetros explícitos.

## Expected result

Cole a saída de cada comando no relatório. Rode tudo com o ambiente da seção 4 do plano
(`<slot>` = `build`), a partir de `/work/herdr-soho/.worktrees/s4`.

1. `gofmt -l cmd internal` → vazio. `go vet ./...` → limpo. `go test ./...` → ok.
2. `GOOS=windows GOARCH=amd64 go vet ./...` e `GOOS=linux GOARCH=amd64 go build ./...` → sem erro.
3. `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /tmp/hs-go/build/herdr-soho ./cmd/herdr-soho`
   → ok; informe o tamanho em bytes.
4. Com `B=/tmp/hs-go/build/herdr-soho` e `J="node skills/herdr-soho/scripts/herdr-soho.mjs"`, para
   `--help`, `-h`, `help`, nenhum argumento e `nao-existe`: stdout, stderr e código do `$B` iguais
   aos do `$J` (mostre o `diff` vazio e os dois códigos).
5. Fallback: `HERDR_SOHO_SKILL_DIR=$PWD/skills/herdr-soho $B kinds` igual a `$J kinds` (stdout,
   stderr, código). Um teste Go do fallback com um `node` falso no `PATH` que grava os argumentos
   recebidos prova o caminho, os argumentos e o código devolvido; outro prova a mensagem sem node
   nem bun.
6. Testes de `internal/platform` portados dos casos de `scripts/test/platform.test.mjs` e dos casos
   de `scripts/test/worktree-state.test.mjs` que chamam `stateProjectRoot`, um `t.Run` por caso com
   `// JS: "<título>"`, mais o diferencial do item 4 das decisões. Caso que não se aplica ao Go:
   liste com o motivo.
7. Uma mutação por função com regra (por exemplo `FindExecutable`, `StateProjectRoot`,
   `AtomicWrite`, `ReadTextFile`), feita numa cópia fora do repositório com
   `/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes, mostrando o teste que fica vermelho (cole a saída).
8. Lista da API criada: cada nome exportado com a assinatura Go, no relatório. As fatias seguintes
   dependem dela.

## Owned files

- `go.mod`
- `cmd/herdr-soho/**`
- `internal/platform/**`
- `internal/cli/**`

## Forbidden

- Tudo em `skills/`, `plugin/`, `docs/`, `README.md`, `AGENTS.md` e `.gitignore`. O JS é a
  especificação: não mude uma linha dele. Divergência ou bug achado no JS vai para o relatório.
- Módulos de terceiros, `go.sum`, rede, `go get`. Cache, build e binários fora de `/tmp/hs-go/build/`.
- Herdr real: nenhum teste chama o `herdr` de verdade (use falso no `PATH` e
  `HERDR_SOCKET_PATH=/tmp/hs-go/build/none.sock`).
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas do terminal. Depois: arquivos criados, a API (item 8),
divergências do JS encontradas e perguntas abertas.
