# Brief — C1b-b: correções da revisão da C1b (legado, comentário, E/S, lanes)

Role: implementer · Agent: build-2 · Report language: pt-BR

Volta à sua C1b, no branch da C2 (mesmo pacote `internal/core`).

## Goal

A revisão da C1b reprovou com 2 P2 e 4 P3, provados por um diferencial Go × JS de mais de mil
cenários. Fechar os 6.

Worktree: `/work/herdr-soho/.worktrees/build-2`, branch `go/c2` (com a
sua C2).

Leia antes: `/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260928T220308.md`
(F1 a F6, com os 21 cenários que divergem e as sondas).

## Decisions already made

1. **F1.** Um erro de leitura do arquivo legado na migração não é engolido: segue o que o JS faz
   (mensagem, código e se a escrita continua), com o cenário `legacy unreadable` da revisão como
   teste.
2. **F2.** Teste Go da migração dentro do `config set` e da escolha do arquivo efetivo (novo presente,
   só o legado, os dois).
3. **F3.** `LaneNames` casa como o regex do JS (prefixo e sufixo sem sobreposição), sobre o `lanes.go`
   que a C2 portou.
4. **F4.** `splitTrailingComment` não trata `\r`, U+2028 e U+2029 como parte do comentário, como o JS.
5. **F5.** As falhas de E/S (`dest is directory`, `dest unreadable`, `.agents is a file`, `user dir
   is a file`, e os de sessão da revisão) dão a mesma mensagem, o mesmo código e o mesmo estado final
   de arquivos que o JS; cada um vira teste.
6. **F6.** A tabela de mutações da revisão refeita linha por linha, com o código de saída de cada uma
   colado; todas pegas ou demonstradas equivalentes com prova.

## Expected result

1. `gofmt -l cmd internal` vazio, `go vet ./...`, `GOOS=windows GOARCH=amd64 go vet ./...`,
   `go test ./...` (cole).
2. Os 21 cenários da revisão refeitos Go × JS: iguais (cole).
3. A tabela de mutações.
4. O portão da C2 repetido (os mesmos arquivos de teste JS contra o binário) → 0 fail.

## Owned files

- `internal/core/**`, `internal/cli/config*.go`, `internal/cli/session*.go`

## Forbidden

- Todo o resto. Herdr real. Módulos de terceiros, `go.sum`, rede. Cache e build só em
  `/tmp/hs-go/build-2/`.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por achado e por entrada de "Expected result", `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas.
