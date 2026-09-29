# Emenda — S6a-b: mais três falhas do portão no `setup --detect`/`--panes`

Role: implementer · Agent: build · Report language: pt-BR

## Goal

O portão completo na árvore `gate` (`go/port` + harness H1) achou mais três falhas do `setup` servido
pelo Go, além das 20 do seu brief. Entram na mesma fatia S6a-b:

- `parity: setup --panes / --detect / the config-prompt steps (test-doctor-fix.sh tail, slices 7a/7b)`
- `parity: setup --detect with the custom provider files (test-detect-custom.sh)`
- `parity: setup --detect with custom lanes`

Log com o erro de cada uma (procure o título): `/tmp/hs-go/orch/parity-gate-a6404e9.log`.

## Expected result

1. As três passam contra o seu binário, somadas ao comando do item 2 do brief (acrescente os
   arquivos de teste delas ao `node --test`), 0 fail (cole o resumo).
2. Causa de cada uma no relatório, com a linha do Go corrigida. Se alguma passa pelo fallback JS
   (não é `--plan`/`--detect`/`--probe`), diga qual e por quê.

## Owned files

Os mesmos do brief S6a-b: `internal/setup/**` e `internal/cli/setup*_test.go`.

## Forbidden

Os mesmos do brief S6a-b (outros pacotes, `skills/`, os worktrees `gate` e `h1`, Herdr real, rede).
No commit, push, tag, or PR. The orchestrator owns git.

## Report

No mesmo relatório da S6a-b, uma seção "Emenda" com os dois itens acima, `[done]`/`[partial]`/
`[skipped]` e as saídas coladas.
