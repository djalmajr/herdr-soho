# Emenda — D6: onde pôr as duas funções de `spawn`

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Outra fatia (S5a, o `spawn` inteiro) começou agora em outro worktree e também cria `internal/spawn`.
Para o merge ficar simples: ponha `configNativeArgs` e `ensureOrchestratorName` num arquivo só,
`internal/spawn/native.go` (e o teste em `internal/spawn/native_test.go`), tradução direta do JS,
sem outro código nesse pacote. Se já pôs em outro arquivo, mova.

## Expected result

1. No relatório da D6, a linha "Emenda: `internal/spawn/native.go` tem só `configNativeArgs` e
   `ensureOrchestratorName`", com `ls internal/spawn` colado.

## Owned files

Os do brief D6.

## Forbidden

Os do brief D6. No commit, push, tag, or PR. The orchestrator owns git.

## Report

No mesmo relatório da D6, uma seção "Emenda" com o item acima.
