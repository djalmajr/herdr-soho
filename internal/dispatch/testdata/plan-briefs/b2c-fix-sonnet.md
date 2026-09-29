# Emenda — B2c: `core.worktree` que cai num `.git` e menos processos `git`

Role: implementer · Agent: build-3 · Report language: pt-BR

## Goal

Fechar o último achado da revisão do B2 e cortar os processos `git` repetidos da resolução do estado.
Mesmo worktree (`/work/herdr-soho/.worktrees/build-3`), branch
`fix/state-across-worktrees`, commit `00d1ca2` (o seu trabalho foi commitado). Revisão, com as sondas:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T184916.md`.

## Decisions already made

1. **Achado (P2).** A raiz lida de `core.worktree` (e qualquer candidata a raiz do estado) é
   recusada quando algum segmento do caminho resolvido é `.git` — não só quando fica dentro do
   diretório comum. Recusada, vale o `projectRoot` de hoje. A comparação de segmento vale para `/` e
   `\` (use `path.win32` nos testes do caso Windows, como a revisão fez).
2. **Processos `git`.** A revisão contou 21 processos `git` por `roster` num worktree ligado e 29 no
   checkout principal (8× `--git-dir`, 8× `--git-common-dir`, até 9× `--show-toplevel`). Memorize,
   por processo, o resultado de `projectRoot` e de `stateProjectRoot`: chave = `cwd` resolvido mais
   os valores de `GIT_DIR`, `GIT_WORK_TREE` e `GIT_COMMON_DIR` do `env` recebido; valor = a raiz
   devolvida. Um `Map` no módulo, sem expiração. Exporte uma função de teste que limpa o cache
   (por exemplo `_resetRootCacheForTests`) e use-a nos testes que mudam o repositório entre duas
   chamadas no mesmo cwd.
3. Motivo do item 2: três testes de `init` estouraram o prazo de 20 s na suíte Node completa da
   árvore integrada (passam isolados). Não mude prazos de teste.

## Expected result

- Teste do achado com as sondas `core-up-to-super-git` e `core-abs-git` da revisão → estado fora de
  qualquer `.git`; mais um caso `path.win32` com `C:\repo\.git`. Cada um com
  `// Mutation captured: …` executado (cole a saída vermelha).
- Teste que conta processos `git` num worktree ligado e no checkout principal durante um `roster`
  (git falso no `PATH` que registra as chamadas e delega ao git real) → no máximo 8 em cada um
  (cole a contagem antes e depois).
- `node --test` e `bun test --timeout 60000` de `worktree-state.test.mjs`, `state.test.mjs`,
  `config.test.mjs`, `init.test.mjs`, `dispatch.test.mjs` → 0 fail (cole).

## Owned files

Os do contrato do B2 (`platform.mjs` só a raiz do estado e o cache, `state.mjs`/`config.mjs` só a
resolução, `dispatch.mjs` só o aviso) e os testes citados.

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório (nos temporários dos testes, pode).
- No commit, push, tag, or PR. The orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` / `[partial]` / `[skipped]` + motivo,
com saídas coladas e as mutações.
