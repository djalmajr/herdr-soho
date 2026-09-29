# Emenda — B2b: raiz do estado em bare/submódulo e aviso que vê o estado

Role: implementer · Agent: build · Report language: pt-BR

## Goal

Fechar os dois achados da revisão do B2 sem abrir outro. Mesmo worktree
(`/work/herdr-soho/.worktrees/build-3`),
branch `fix/state-across-worktrees`, agora no commit `694d234` (o seu
trabalho foi commitado). Revisão, com as sondas:
`/tmp/herdr-soho/w14/reports/soho-grok-r12-20260928T181350.md`.

## Decisions already made

1. **Achado 1 (raiz do estado).** Com `--git-dir` diferente de
   `--git-common-dir`: se o basename do diretório comum é `.git`, a raiz
   é o pai dele (como hoje). Senão, é o caminho do **primeiro** item de
   `git worktree list --porcelain` quando esse item não é `bare`
   (submódulo com worktree ligado → o checkout do submódulo). Se o
   primeiro item é `bare` ou o comando falha, vale o `projectRoot` de
   hoje (cada worktree de um repositório bare fica com o próprio estado,
   como antes desta fatia). O estado nunca cai dentro de um diretório git.
2. **Achado 2 (aviso).** A checagem de caminho invisível roda quando o cwd
   do worker difere do `projectRoot` **ou** da raiz do estado. Um caminho
   relativo citado é avisado quando existe sob o `projectRoot` **ou** sob a
   raiz do estado e não existe sob o cwd do worker; o absoluto sugerido é
   o de onde ele existe. O texto do aviso e o comportamento em
   `brief_lint=strict` não mudam.

## Expected result

- Testes com as sondas da revisão (repositório bare com worktree,
  submódulo com worktree ligado, submódulo sem worktree, fora de git) e
  com os três arranjos do achado 2 (orquestrador no principal e worker no
  worktree; os dois no mesmo worktree; orquestrador no worktree e worker
  em outro diretório) → estado e aviso corretos. Cada um com
  `// Mutation captured: …` executado (cole a saída vermelha).
- `node --test` e `bun test --timeout 60000` de `worktree-state.test.mjs`,
  `state.test.mjs`, `config.test.mjs`, `dispatch.test.mjs` → 0 fail
  (cole).

## Owned files

Os do contrato do B2 (`platform.mjs` só a função da raiz do estado,
`state.mjs`/`config.mjs` só a resolução, `dispatch.mjs` só o aviso, os
testes, `SKILL.md`/`docs/guide.md` só a frase do B2).

## Forbidden

- Todo o resto e qualquer outro worktree. Herdr real que escreva.
- Nenhum comando git que escreva neste repositório (nos temporários dos
  testes, pode). No commit, push, tag, or PR. The orchestrator owns git.

## Report

Relatório em Markdown no caminho do contrato, por item `[done]` /
`[partial]` / `[skipped]` + motivo, com saídas coladas e as mutações.
