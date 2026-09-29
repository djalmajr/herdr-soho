# Brief — S3d: saída do seletor por sinal sem travar

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s3`, branch `feat/session-picker`, commit `e4b37c9`.
Caminhos relativos à raiz dela. Revisão que motiva a fatia (leia os dois
achados, com as sondas):
`/tmp/herdr-soho/w14/reports/soho-grok-r4-20260928T134422.md`.

## Goal

`SIGTERM`/`SIGHUP` em qualquer momento (inclusive no primeiro quadro,
"carregando local…") encerram o seletor em menos de 1 s, sem deixar
nenhum `find` vivo e sem desenhar nada depois do sinal.

## Decisions already made

1. **Ordem do `finish` (P1).** Primeiro `state.exit = <motivo>` (para o
   guard do `redraw` valer também no sinal — P3); depois matar todos os
   filhos (`find` local e remotos); depois restaurar o terminal (raw off,
   cursor) com escrita direta e sem esperar o stdin; não use
   `stdin.destroy()` antes do `kill`.
2. **Saída por sinal é forçada.** O handler de `SIGTERM`/`SIGHUP` chama o
   `finish` e, em seguida, `process.exit(0)` (código 0, sem copiar), sem
   esperar `main`/`await load`. Esc e Ctrl-C mantêm o caminho de hoje.
3. **Filhos em voo registrados cedo.** O `find` local entra na lista de
   filhos no momento do `spawn` (antes do primeiro quadro), para que o
   sinal no primeiro quadro o alcance.
4. **Teste num terminal de verdade.** Além do teste com stdin falso, um
   teste que roda o seletor num pty (use `python3 -c 'import pty…'` ou o
   que já houver nos testes; se não houver como no Windows, pule lá com
   motivo) manda `SIGTERM` logo no primeiro quadro, com um `find` falso
   pendurado que grava o pid, e prova: o seletor sai em < 1 s, o pid do
   `find` não existe mais, e nada é escrito na tela depois do sinal além
   da restauração do terminal.

## Acceptance criteria

1. Testes dos itens acima com `// Mutation captured: …` executado (ex.:
   voltar `stdin.destroy()` antes do `kill`; tirar o `process.exit` do
   handler; não setar `state.exit` no `finish`).
2. `node --test plugin/test/` e `bun test --timeout 60000 plugin/test/` →
   0 fail (cole).
3. Nada mais muda.

## When the brief does not decide

Não escolha. Marque `[partial]`, liste a lacuna e as opções em "Open
questions" e siga. Nunca invente nomes, flags ou requisitos.

## Owned files

- `plugin/picker.mjs`, `plugin/test/picker.test.mjs`

## Forbidden

- Todo o resto. Nenhuma chamada ao Herdr real que escreva (painéis,
  plugin, `agent prompt`, `send-keys`, notificações): sondas só com
  `herdr` falso no `PATH` e `HERDR_SOCKET_PATH` apontando para um caminho
  inexistente.
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Checks you may run

Os dos critérios; `node --test <um arquivo>` enquanto itera.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações
executadas.
