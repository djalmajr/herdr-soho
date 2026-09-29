# Brief — C1a-b: correções da revisão da C1a (`herdr`, `codexenv`, `fakecli`)

Role: implementer · Agent: build · Report language: pt-BR

Volta à sua C1a (que você escreveu antes da GW2 e da G1c), mesmo worktree e branch da G1c.

## Goal

A revisão da C1a reprovou com 3 P2 e 8 P3, todos executados contra o JS. Fechar os 11, sobre o
`fakecli` já mudado pela GW2 e o `RunCli` da G1c.

Worktree: `/work/herdr-soho/.worktrees/s4`, branch `go/gw2`.

Leia antes: `/work/herdr-soho/.herdr-soho/w14/reports/review-20260928T215403.md`
(F1 a F11, com as sondas e as correções sugeridas).

## Decisions already made

1. **F1.** `AgentState` distingue morte por sinal de timeout e nomeia o sinal de qualquer saída
   acima de 128, com o texto do JS.
2. **F2.** O `fakecli` reconhece o próprio nome com `.EXE` maiúsculo (o `PATHEXT` do Windows devolve
   a extensão em maiúsculas).
3. **F3.** `ParseCodexPolicy` trata U+FEFF como espaço, como o `trim()` do JS.
4. **F4 a F7, F10.** Como a revisão pede, cada um com o caso dela como teste; onde a revisão diz
   "decodifica com structs tipadas e diverge", use `jsonjs.Parse` e a mesma navegação do JS.
5. **F8 e F9.** O `fakecli` serializa o registro e a contagem de chamadas (dois processos
   concorrentes não perdem linha), põe a pasta do falso na frente do `PATH` sem deixar o `herdr` real
   alcançável depois dela (o `PATH` do falso não contém o `PATH` do host, ou contém só o que o teste
   pedir), e a API cobre o que a revisão lista como faltando para as próximas fatias.
6. **F11.** Refaça a tabela de mutações da revisão (56 mutações) linha por linha, com o código de
   saída de cada uma colado; as 28 que sobreviveram têm de morrer, ou ser demonstradas equivalentes
   com a prova. Mutações suas no lugar das da revisão não contam.

## Expected result

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` (cole).
2. As sondas de F1, F2, F3, F6 e F7 refeitas contra o JS: iguais (cole).
3. A tabela de mutações (decisão 6).
4. Binários de teste Windows recompilados (o roteiro da GW2).

## Owned files

- `internal/herdr/**`, `internal/codexenv/**`, `internal/testutil/**`

## Forbidden

- Todo o resto. Módulos de terceiros, `go.sum`, rede. Cache e build só em `/tmp/hs-go/build/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
