# Brief — S3e: seletor sem panes locais repetidos

Role: implementer · Agent: build · Report language: pt-BR

Worktree `.worktrees/s3`, branch `feat/session-picker`, commit `8788b05`.
Caminhos relativos à raiz dela.

## Problema observado (validação real no Herdr, 2026-09-28 ~18:05Z)

O seletor mostrou cada pane local três vezes (filtro `soho-agy a1` → "3
panes", todas `local/w14:p16`). Causa: o `find --machine <m>` inclui
sempre a máquina local (o `--machine` **acrescenta** máquinas ao padrão
`local`), e o seletor faz uma carga local e mais uma `find --json
--machine <m>` por máquina remota, anexando tudo.

## Decisions already made

- Cada carga remota acrescenta **só** as entradas cuja `machine` é a
  máquina daquela carga; a carga local continua sendo a fonte das
  entradas `local`. Não mude o comando `find` (outro módulo).
- Além disso, a lista final não pode ter duas entradas com o mesmo `ref`
  (mantenha a primeira).
- Teste: `find` falso que, para `--machine windows`, devolve as linhas
  locais **e** as da `windows` (como o real) → a lista final tem cada
  `ref` uma vez; a contagem bate. `// Mutation captured: …` executado
  (tirar o filtro por máquina faz o teste falhar).

## Acceptance criteria

1. O teste acima; `node --test plugin/test/` e
   `bun test --timeout 60000 plugin/test/` → 0 fail (cole).
2. Nada mais muda.

## Owned files

- `plugin/picker.mjs`, `plugin/test/picker.test.mjs`

## Forbidden

- Todo o resto. Nenhuma chamada ao Herdr real que escreva (sondas só com
  `herdr` falso no `PATH` e `HERDR_SOCKET_PATH` inexistente).
- Nenhum comando git que escreva. No commit, push, tag, or PR. The
  orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e a mutação
executada.
