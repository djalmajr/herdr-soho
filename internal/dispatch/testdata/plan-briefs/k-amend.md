# Emenda — K: o teste do fallback e a linha `runtime` do `env`

Role: implementer · Agent: build-3 · Report language: pt-BR

## Goal

Fechar os dois itens `[partial]` da K.

## Decisions already made

1. `internal/cli/cli_test.go` (`TestRunFallback`): o comando de exemplo do fallback deixa de ser
   `kinds` (agora servido pelo Go) e passa a ser um que continua no JS até a fase 5: `stats`. Você
   pode editar `internal/cli/cli_test.go` só nesse ponto.
2. A linha `runtime:` do `env` descreve o runtime que roda a CLI: no Go ela é
   `runtime: go <runtime.Version()>` e isso é uma divergência aceita do JS (o JS imprime
   `runtime: node <versão>`). O teste JS do `env` passa a aceitar a linha do Go quando
   `HERDR_SOHO_TEST_BIN` está definida; o orquestrador faz essa mudança no worktree do harness. No seu
   relatório, cole a linha exata que o Go imprime e o trecho do `env.test.mjs` que compara.

## Expected result

- `go test ./...` ok (cole). O resto como na K.

## Owned files

- Os da K, mais `internal/cli/cli_test.go` (só o `TestRunFallback`).

## Forbidden

- O worktree `h1` (o orquestrador faz a mudança lá). O resto como na K.
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo, com saídas
coladas.
