# Brief — Revisão R-B2: estado único entre worktrees e aviso de caminho invisível

Role: reviewer · Agent: review · Report language: pt-BR

## Goal

Dizer se o commit `694d234` (branch `fix/state-across-worktrees`, sobre
`main` `e7667e6`) pode ir à `main`.

Seu cwd está em `694d234`. Confirme com `git log --oneline -1` e revise
`git diff HEAD~1 HEAD`. Contrato:
`/work/herdr-soho/.herdr-soho/w14/plan-briefs/b2-state-worktrees.md`.
Relatório do implementador (Codex):
`/tmp/herdr-soho/w14/reports/build-3-20260928T174141.md`.

## O que verificar com atenção

1. **Raiz do estado.** `git rev-parse --git-dir` vs `--git-common-dir`:
   caminhos relativos (o git devolve relativo quando o cwd está na raiz),
   submódulos (o `--git-common-dir` de um submódulo aponta para
   `.git/modules/...` do superprojeto — o estado não pode cair lá), repo
   bare com worktrees, Windows (barras, letra de drive). Algum desses
   manda o estado para um lugar errado ou quebra fora de git?
2. **Efeitos colaterais.** O `.gitignore` agora é escrito no checkout
   principal quando o comando roda num worktree: pode escrever num
   arquivo inesperado? O `state_dir` absoluto configurado continua
   respeitado? `HERDR_SOHO_DIR`/`HERDR_SOHO_STATE_DIR` também?
3. **Aviso de caminho.** Falso positivo: caminho git-ignored que existe no
   principal por acaso (ex.: `node_modules/x`, `dist/`), URL, `~/…`,
   caminho com `..` que sai da raiz. Falso negativo: caminho em item de
   lista sem crases. Custo: quantos `stat` para um brief grande?
4. **`--amend`**: o relatório diz que a verificação roda sem as outras
   validações contratuais — confirme que não enfraqueceu o `--amend`.
5. Testes enfraquecidos? Mutações declaradas que não pegam (rode três)?

## Checks you may run

Leitura, `git diff/log/show`; `node --test`/`bun test --timeout 60000` em
`worktree-state.test.mjs`, `state.test.mjs`, `config.test.mjs`,
`dispatch.test.mjs`; mutações em cópias em `/tmp`; repositórios git
temporários em `/tmp` para sondas.

## Forbidden

- Editar qualquer arquivo do checkout. Revisor é somente leitura.
- Nenhuma chamada ao Herdr real que escreva: `herdr` falso no `PATH` e
  `HERDR_SOCKET_PATH` inexistente.
- Nenhum comando git que escreva neste repositório (nos temporários de
  `/tmp`, pode). No commit, push, tag, or PR. The orchestrator owns git.

## Expected result

Primeira linha exata `findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail`,
depois um achado por item com severidade, `arquivo:linha`, cenário,
evidência colada e correção sugerida; `[done]`/`[partial]` por ponto.

## Report

Relatório em Markdown no caminho do contrato, pt-BR.
