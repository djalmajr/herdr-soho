# Brief — S3c: referência limpa no corpo da notificação do seletor

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s3`, branch `feat/session-picker`, commit `e056644`.
Caminhos relativos à raiz dela.

## Goal

O corpo da notificação que o seletor mostra ao copiar (`herdr
notification show "herdr-soho" --body "copied <ref>" --sound none`) não
leva caracteres de controle nem sequências de escape vindos do `ref`
(texto de terceiros).

## Decisions already made

- Aplique ao `ref` a mesma função de limpeza que o seletor já usa para
  desenhar e copiar (em `plugin/picker.mjs`) antes de montar o `--body`.
  Não crie outra função.
- Teste com um `ref` contendo CSI/OSC e `\r`: o argv do `notification
  show` (num `herdr` falso) não tem ESC nem `\r`.

## Acceptance criteria

1. O teste novo, com `// Mutation captured: …` executado (tirar a limpeza
   do `ref` faz o teste falhar).
2. `node --test plugin/test/` e `bun test --timeout 60000 plugin/test/` →
   0 fail (cole).

## Owned files

- `plugin/picker.mjs` (só a montagem do corpo da notificação),
  `plugin/test/picker.test.mjs`

## Forbidden

- Todo o resto. Abrir painéis de verdade, ligar o plugin, mandar teclas.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e a mutação
executada.
