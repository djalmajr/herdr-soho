# Emenda — C1b-c: dois P3 da revisão da C1c-b

Role: implementer · Agent: build-4 · Report language: pt-BR

## Goal

A revisão da C1c-b passou, com dois P3 pequenos. Entram na sua C1b-c, no mesmo branch:

1. `internal/cli/roster.go:82` — `tabOf` usa `fmt.Sprint(label)`: um rótulo numérico grande sai
   `1.23456789012345` e o JS mostra `1234567890123456`. Número passa pela formatação JS
   (`jsonjs`, como `String(n)`); string continua string, sem aspas.
2. `internal/cli/feedback.go:75-80` — a colisão e a permissão do `feedback send` comparam o errno
   direto; no Windows o `O_EXCL` devolve `ERROR_FILE_EXISTS` e não casa. Use
   `errors.Is(err, fs.ErrExist)` e `errors.Is(err, fs.ErrPermission)`; o texto `(EEXIST)`/`(EACCES)`
   continua o do JS.

Revisão, com a prova de cada um:
`/work/herdr-soho/.herdr-soho/w14/reports/review-2-20260929T000729.md`.

## Expected result

1. Um teste Go para cada um (o rótulo `12345678901234567890`; a colisão pela função que decide o
   texto, com um erro que embrulha `fs.ErrExist`), e o mutante que cada teste mata, com o código de
   saída.
2. Os gates Go do brief continuam ok.

## Owned files

Os do brief C1b-c, mais `internal/cli/roster.go` (só `tabOf`), `internal/cli/feedback.go` (só o
tratamento do erro de abertura) e seus testes.

## Forbidden

Os mesmos do brief C1b-c. No commit, push, tag, or PR. The orchestrator owns git.

## Report

No mesmo relatório da C1b-c, uma seção "Emenda" com os dois itens, `[done]`/`[partial]`/`[skipped]`
e as saídas coladas.
