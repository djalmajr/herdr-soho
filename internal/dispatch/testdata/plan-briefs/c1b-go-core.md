# Brief — C1b: `internal/core` parte 1 (config, session, legacy, state) e os comandos `config` e `session`

Role: implementer · Agent: build-2 · Report language: pt-BR

Continuação do seu G1b, no mesmo worktree.

## Goal

Portar o núcleo de configuração e estado — as camadas de config (defaults, usuário, projeto, sessão,
ambiente), a sessão por workspace, a migração dos nomes antigos (`herdr-agents`) e o diretório de
estado com o roster — e servir pelo Go os comandos `config`, `config set` e `session`. Quase todo
comando futuro depende daqui.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/c1b` (criado de `go/port`
com o seu G1b e o `main` atual integrados, commit `108acd2`; o JS de referência é o deste worktree). Outra fatia (C1a) porta agora `internal/herdr`, `internal/codexenv` e
`internal/testutil/fakecli` em outro worktree: não crie esses pacotes aqui.

Plano: `/work/herdr-soho/.herdr-soho/w14/go/PLAN.md` (seções 2 a 5; D6
diz o que entra em `internal/core`).

## Decisions already made

1. `internal/core` recebe, um arquivo por módulo JS: `config.go` ← `lib/config.mjs`, `session.go` ←
   `lib/session.mjs`, `legacy.go` ← `lib/legacy.mjs`, `state.go` ← `lib/state.mjs`. Do `roles.mjs`
   e do `lanes.mjs`, só o que esses quatro usam (`roleFile`, `roleDirs`, `laneNames` e o que eles
   chamarem), em `roles.go` e `lanes.go`, com um comentário `// Parcial: o restante vem na fatia C2`.
2. O `ctx` do JS (`loadConfig`) vira um tipo `core.Config` com as entradas por chave normalizada, a
   grafia original, a camada de origem e a lista de camadas lidas, com os mesmos nomes de função
   (`LoadConfig`, `Cfg`, `CfgSource`, `ConfigFileFor`, …).
3. O `state.mjs` usa `herdr` em nenhum ponto? Confira: se usar, pare nesse item e marque `[partial]`
   (o `herdr` está na outra fatia). O lock do roster (`withRosterLock`) reproduz a semântica do JS
   (arquivo de lock, espera, tempo máximo) e funciona no Windows.
4. Comandos portados em `internal/cli`: `config` (listagem com as fontes), `config set <key> <value>
   [--project|--user]` (e a forma `key=value`), `session set|show|clear`. Cada um sai do fallback.
   O que a entrada JS faz em volta deles (legacy env, `loadConfig`, NOWRITE, friction de comando vivo)
   o Go reproduz.
5. Testes:
   - os casos de `config.test.mjs`, `session.test.mjs`, `legacy.test.mjs`,
     `legacy-migration.test.mjs` e `state.test.mjs` que chamam funções desses módulos, um `t.Run` por
     caso com `// JS: "<título>"`;
   - os goldens de paridade `parity-config.json` (e o de `session`, se houver) rodados contra o Go:
     um teste Go lê o golden e compara com a saída de `cli.Run`;
   - os casos de CLI (`config`, `config set`, `session`) comparados com o JS por roteiro, como no P1.

## Expected result

Com o ambiente da seção 4 do plano (`<slot>` = `build-2`), a partir do worktree:

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...` limpos,
   `go test ./...` ok (cole).
2. O golden `parity-config.json` passa contra o Go (cole o resumo do teste).
3. Comparação Go × JS de `config`, `config set` (projeto, usuário, `key=value`, chave inválida,
   valor inválido, arquivo legado) e `session set|show|clear` (dentro e fora de um workspace
   Herdr falso): `diff` vazio e códigos iguais (cole).
4. Uma mutação por regra importante (precedência das camadas, normalização de chave, validação de
   valor, migração do arquivo legado, lock do roster), numa cópia fora do repositório
   (`/home/user/.agents/skills/herdr-soho/scripts/herdr-soho mutation-guard <cópia>` antes),
   mostrando o teste vermelho.
5. API nova no relatório.

## Owned files

- `internal/core/**`
- `internal/cli/config.go`, `internal/cli/session.go` e os testes deles, e o registro dos comandos em
  `internal/cli/cli.go`

## Forbidden

- `internal/herdr/**`, `internal/codexenv/**`, `internal/testutil/**` (outra fatia), os pacotes do G2
  (em correção em outro worktree), `internal/platform/**` (se precisar mudar, descreva no relatório),
  `go.mod`, tudo em `skills/`, `plugin/`, `docs/`.
- Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato. Um item por entrada de "Expected result", com `[done]`, `[partial]`
ou `[skipped]` e o motivo, saídas coladas. Depois: arquivos criados, API, casos JS não portados com
motivo, divergências do JS e perguntas abertas.
